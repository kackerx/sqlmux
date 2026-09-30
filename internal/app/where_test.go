package app

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/config"
	"sqlmux/internal/editor"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// Completion (§9.7): columns and keywords with a word starting as the one typed does,
// an enum's values where one goes. The first is selected as the list opens,
// Tab and S-Tab move around the ends, ↵ takes the selected one and runs
// when that changes nothing; esc closes the list before the input.
func TestWhereCompletion(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
	feed(t, a, "/a")
	if tab.comp == nil || len(tab.comp.items) != 3 || tab.comp.items[0].label != "amount" || tab.comp.items[1].label != "created_at" || tab.comp.sel != 0 || a.mode() != keymap.Insert {
		t.Fatalf("a: amount, created_at (a word of it), and: %+v", tab.comp)
	}
	at := a.whereAt(a.focused(), tab)
	f := a.render()
	row := strings.Split(f.String(), "\n")[at.Y+2] // under the input and the list's border
	i := strings.Index(row, "amount")
	if bg := f.Buf.CellAt(ui.Width(row[:i])-1, at.Y+2).Style.Bg; bg != a.theme.Select { // the cell before it: the row's, not the match's
		t.Errorf("selected as it opens: %v", bg)
	}
	if feed(t, a, "<Tab>"); tab.comp.sel != 1 {
		t.Fatalf("the first Tab moves to the second: %+v", tab.comp)
	}
	feed(t, a, "<BS>sta")
	if tab.comp == nil || tab.comp.items[0].label != "status" || tab.comp.sel != 0 {
		t.Fatalf("sta: %+v", tab.comp)
	}
	feed(t, a, "<CR>")
	if tab.where.Text != "status" || tab.comp != nil || tab.applied != "" {
		t.Fatalf("↵ takes status: %q applied %q", tab.where.Text, tab.applied)
	}
	feed(t, a, " = ")
	var labels []string
	for _, c := range tab.comp.items {
		labels = append(labels, c.label)
	}
	if strings.Join(labels, " ") != "pending running done failed" {
		t.Fatalf("values: %v", labels)
	}
	for _, c := range []struct {
		keys string
		sel  int
	}{{"<S-Tab>", 3}, {"<Tab>", 0}, {"<C-p><Up>", 2}, {"<C-n><Down>", 0}, {"<Down>", 1}} {
		if feed(t, a, c.keys); tab.comp.sel != c.sel {
			t.Errorf("%s: sel %d, want %d", c.keys, tab.comp.sel, c.sel)
		}
	}
	feed(t, a, "<CR>")
	if tab.where.Text != "status = 'running'" || tab.applied != "" {
		t.Fatalf("↵ on running: %q applied %q", tab.where.Text, tab.applied)
	}
	feed(t, a, " and paid is not nu")
	if tab.comp == nil || tab.comp.items[0].label != "null" || len(tab.comp.items) != 1 {
		t.Fatalf("nu: only null starts with n, not is null: %+v", tab.comp)
	}
	feed(t, a, "ll<CR>")
	if tab.applied != "status = 'running' and paid is not null" || tab.typing != "" {
		t.Errorf("↵ that changes nothing runs: %q", tab.applied)
	}
	feed(t, a, "/ i<Esc>")
	if tab.comp != nil || tab.typing != "where" {
		t.Fatal("the first esc closes the list")
	}
	feed(t, a, "<Esc>")
	if tab.typing != "" {
		t.Error("the second esc leaves the input")
	}
}

// Completion ignores case throughout, first character and fuzzy rest
// alike; the palette keeps fzf's smart case (§9.7).
func TestRankedIgnoresCase(t *testing.T) {
	cands := []candidate{{label: "status"}, {label: "amount"}, {label: "t_order"}, {label: "Mixed_Case"}}
	for pattern, want := range map[string]string{"St": "status", "STA": "status", "Am": "amount", "T_OR": "t_order", "mix": "Mixed_Case"} {
		if c := ranked(pattern, 0, cands); c == nil || len(c.items) != 1 || c.items[0].label != want {
			t.Errorf("%s: %+v, want %s", pattern, c, want)
		}
	}
	a := sized(160, 45, "nerd")
	if feed(t, a, "<C-p>@t_ord"); !slices.Contains(namesOf(a), "表:t_order") {
		t.Fatalf("palette: %v", namesOf(a))
	}
	if feed(t, a, "<BS><BS><BS><BS><BS>T_ORD"); slices.Contains(namesOf(a), "表:t_order") {
		t.Error("the palette's T_ORD must stay exact about case")
	}
}

// The first character typed matches at a word start: the first, one after
// _ . - $, or a lower-to-upper turn; the rest is fuzzy (§9.7).
func TestRankedWordStart(t *testing.T) {
	cands := []candidate{{label: "mt_event"}, {label: "t_order"}, {label: "t_event"}, {label: "max"}, {label: "exists"}, {label: "orderId"}, {label: "a.b-c$d"}}
	for pattern, want := range map[string][]string{
		"evt": {"t_event", "mt_event"}, "tord": {"t_order"}, "ev": {"t_event", "mt_event"}, "x": nil,
		"id": {"orderId"}, "b": {"a.b-c$d"}, "c": {"a.b-c$d"}, "d": {"a.b-c$d"},
	} {
		var got []string
		if c := ranked(pattern, 0, cands); c != nil {
			for _, it := range c.items {
				got = append(got, it.label)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", pattern, got, want)
		}
	}
	// fzf's best match takes the run abcdef after x; the one from the word
	// start after _ still counts, and is what gets highlighted
	s := "xabcdef_a" + strings.Repeat("q", 20) + "bcdef"
	if m := ui.Filter("abcdef", []string{s}); len(m) != 1 || m[0].Pos[0] != 1 {
		t.Fatalf("fzf no longer prefers the run: %+v", m)
	}
	c := ranked("abcdef", 0, []candidate{{label: s}})
	if c == nil || !slices.Equal(c.items[0].pos, []int{8, 29, 30, 31, 32, 33}) {
		t.Errorf("from the word start: %+v", c)
	}
	// a value with a space: fzf's terms each match anywhere, the first
	// position may be the second term's
	c = ranked("in p", 0, []candidate{{label: "prod in"}, {label: "in progress"}, {label: "on hold"}})
	if c == nil || len(c.items) != 1 || c.items[0].label != "in progress" {
		t.Errorf("in p: %+v", c)
	}
}

// Autopairs in a WHERE, the quick SQL and a console's INSERT, not in a
// cell's edit or the palette's other scopes, off with autopairs = false
// (§7.9). Taking a value swallows the closing quote after the cursor.
func TestAutoPairs(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
	feed(t, a, "/status = '")
	if tab.where.Text != "status = ''" || tab.where.Pos != 10 {
		t.Fatalf("paired: %q at %d", tab.where.Text, tab.where.Pos)
	}
	if feed(t, a, "done'"); tab.where.Text != "status = 'done'" || tab.where.Pos != 15 {
		t.Fatalf("stepped over: %q at %d", tab.where.Text, tab.where.Pos)
	}
	feed(t, a, "<BS><BS><BS><BS><BS><BS>")
	if tab.where.Text != "status = " {
		t.Fatalf("BS: %q", tab.where.Text)
	}
	if feed(t, a, "'ru<CR>"); tab.where.Text != "status = 'running'" || tab.where.Pos != 18 || tab.applied != "" {
		t.Fatalf("taking 'running': %q at %d", tab.where.Text, tab.where.Pos)
	}
	feed(t, a, "<Esc><Esc>i(")
	if tab.cell == nil || tab.cell.in.Text != "(" {
		t.Fatalf("a cell's edit doesn't pair: %+v", tab.cell)
	}
	feed(t, a, "<Esc><C-p>(")
	if a.palette.input.Text != "(" {
		t.Fatalf("the palette's other scopes don't: %q", a.palette.input.Text)
	}
	if feed(t, a, "<BS>;select count("); a.palette.input.Text != ";select count()" || a.palette.input.Pos != 14 {
		t.Fatalf("quick SQL: %q at %d", a.palette.input.Text, a.palette.input.Pos)
	}
	a.paste("count(")
	if a.palette.input.Text != ";select count(count()" || !a.autoPairs {
		t.Fatalf("a paste goes in as it is: %q", a.palette.input.Text)
	}
	a.autoPairs = false
	if in := (ui.Input{}); a.editPaired(&in, "(") && in.Text != "(" {
		t.Errorf("off: %q", in.Text)
	}

	a, c := inConsole(t, "")
	if feed(t, a, "icount("); text(c) != "count()" || c.ed.Cursor().Col != 6 {
		t.Fatalf("console: %q at %v", text(c), c.ed.Cursor())
	}
	if feed(t, a, "<Esc>u"); text(c) != "" {
		t.Errorf("one u takes the INSERT: %q", text(c))
	}
	a, c = inConsole(t, "autopairs = false")
	if feed(t, a, "i("); text(c) != "(" {
		t.Errorf("console off: %q", text(c))
	}
}

// Taking a candidate that differs from the word only in case changes
// nothing: SQL's keywords and bare names ignore it (§9.7).
func TestAcceptIgnoresCase(t *testing.T) {
	c := &completion{items: []candidate{{label: "null", insert: "null"}}, start: 4}
	in := ui.Input{Text: "a = NULL", Pos: 8}
	if c.accept(&in) || in.Text != "a = NULL" {
		t.Errorf("NULL: changed to %q", in.Text)
	}
	in = ui.Input{Text: "a = nu", Pos: 6}
	if !c.accept(&in) || in.Text != "a = null" || in.Pos != 8 {
		t.Errorf("nu: %q at %d", in.Text, in.Pos)
	}
}

// Every WHERE run goes into the table's history; C-r lists favorites then
// history, a star or a clock before each, all of it till something is
// typed, then filtered by it; Tab, ↓, S-Tab and ↑ move around the ends;
// C-f stars; ↵ runs one with its ORDER and LIMIT; it all outlives a
// restart (Q-02, §9.7, §14).
func TestWhereHistory(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
	feed(t, a, "/id > 5<CR>")
	tab.order, tab.limit = "amount", 500
	clear := strings.Repeat("<BS>", 20)
	feed(t, a, "/"+clear+"status = 'done'<CR>")
	tab.order, tab.limit = "", 100
	feed(t, a, "/"+clear+"id > 5<CR>") // again: to the top, once
	st := a.tableState(tab)
	if len(st.History) != 2 || st.History[0].Where != "id > 5" || st.History[1].Limit != 500 {
		t.Fatalf("history %+v", st.History)
	}
	feed(t, a, "/"+clear+"xyz<C-r>") // what is there does not filter it
	if es, _ := a.histEntries(tab); tab.hist == nil || a.mode() != keymap.Command || len(es) != 2 {
		t.Fatalf("C-r opens the list, all of it: %+v", es)
	}
	feed(t, a, "<C-n><C-f>")
	if len(st.Favorites) != 1 || st.Favorites[0].Where != "status = 'done'" {
		t.Fatalf("favorites %+v", st.Favorites)
	}
	f := a.render().String()
	if !strings.Contains(f, a.icons.Star.Text+" status = 'done'") || !strings.Contains(f, a.icons.History.Text+" id > 5") || strings.Contains(f, "收藏") || !strings.Contains(f, "amount ↑ · 500") {
		t.Errorf("the list:\n%s", f)
	}
	for keys, want := range map[string]int{"<Up>": 2, "<Down>": 1, "<Tab><Tab><Tab>": 0, "<S-Tab>": 2} { // from the first
		if tab.hist.sel = 0; feed(t, a, keys) || tab.hist.sel != want {
			t.Errorf("%s: at %d, want %d", keys, tab.hist.sel, want)
		}
	}
	feed(t, a, clear+"don")                                      // filters the list
	if es, _ := a.histEntries(tab); len(es) != 2 || !es[0].fav { // the favorite, and its run
		t.Fatalf("filtered: %+v", es)
	}
	feed(t, a, "<CR>")
	if tab.applied != "status = 'done'" || tab.order != "amount" || tab.limit != 500 || tab.hist != nil {
		t.Fatalf("applied %q order %q limit %d", tab.applied, tab.order, tab.limit)
	}

	loaded, err := config.LoadState()
	if err != nil || len(loaded.Tables["doraemon/public.t_order"].Favorites) != 1 {
		t.Fatalf("after a restart: %+v %v", loaded, err)
	}
}

// A table's WHERE history keeps its last 100 runs (§9.7).
func TestWhereHistoryRows(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
	for i := range whereRows + 1 {
		feed(t, a, "/<C-u>id = "+strconv.Itoa(i)+"<CR>")
	}
	if h := a.tableState(tab).History; len(h) != whereRows || h[0].Where != "id = 100" || h[len(h)-1].Where != "id = 1" {
		t.Errorf("%d kept, %q to %q", len(h), h[0].Where, h[len(h)-1].Where)
	}
}

// The palette's recent picks are kept in the state by kind and id (§12, §14).
func TestPaletteRecentInState(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>@t_user<CR>")
	if r := a.state.Recent; len(r) != 1 || r[0] != (config.Recent{Kind: "table", ID: "public.t_user"}) {
		t.Fatalf("recent %+v", r)
	}
	a.state.Recent = append(a.state.Recent, config.Recent{Kind: "command", ID: "pane.zoom"})
	if names := namesOf(openPaletteOn(t, a)); names[0] != "表:t_user" || names[1] != "命令:缩放 / 还原" {
		t.Errorf("recent first: %v", names[:2])
	}
}

func openPaletteOn(t *testing.T, a *App) *App {
	t.Helper()
	feed(t, a, "<C-p>")
	return a
}

// Names in SQL for their colors: a table the catalog has, folded unless
// quoted; a column of the tables its statement names, not of an alias's
// or a CTE's; the tables whose columns are not fetched yet missing, and
// asked for once, the consoles on screen's (§7.3).
func TestSQLNames(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	text := `select o.status, "status", "Status", AMOUNT from T_ORDER o; select status from t_user; with x as (select 1) select status from x`
	names, missing := a.sqlNames(text, "public", nil)
	for _, c := range []struct {
		word string
		n    int // the nth of it in text
		want ui.SQLName
	}{
		{"T_ORDER", 0, ui.TableName}, {"t_user", 0, ui.TableName}, {"o", 0, ui.OtherName},
		{"status", 0, ui.ColumnName}, {`"status"`, 0, ui.ColumnName}, {`"Status"`, 0, ui.OtherName}, {"AMOUNT", 0, ui.ColumnName},
		{"status", 2, ui.OtherName}, // t_user's, not fetched
		{"status", 3, ui.OtherName}, // x's, a CTE
	} {
		at := -1
		for range c.n + 1 {
			at += 1 + strings.Index(text[at+1:], c.word)
		}
		if got := names(at, c.word); got != c.want {
			t.Errorf("%s #%d at %d: %v, want %v", c.word, c.n, at, got, c.want)
		}
	}
	if len(missing) != 1 || missing[0].Name != "t_user" {
		t.Errorf("missing %v", missing)
	}
	if n := a.queryBar(a.focused(), tab).Names; n(0, "note") != ui.ColumnName || n(0, "t_user") != ui.TableName {
		t.Error("the WHERE's: the table's columns")
	}
	consoleOf(a.win().pane(2)).ed = editor.New("select * from t_user") // on screen
	if _, cmd := a.Update(tea.WindowSizeMsg{Width: 160, Height: 45}); cmd == nil || !a.sess.colsAsked[tableID{"public", "t_user"}] {
		t.Fatal("the console's t_user: its columns asked for")
	}
	if _, cmd := a.Update(tea.WindowSizeMsg{Width: 160, Height: 45}); cmd != nil {
		t.Error("asked again")
	}
}

// The WHERE input is a vim of one line (F3.39): esc goes to its NORMAL,
// where dd, ciw, u and VISUAL work, a yank flashing, o does nothing and a
// click moves the cursor; ↵ runs it, / opens the history, what is typed then filtering
// it; esc or C-c goes back to the table, the change dropped. [map.normal]
// applies, a grid's maps don't; "+P puts the clipboard (F3.38).
func TestWhereVim(t *testing.T) {
	a := configured(t, 160, 45, "[map.normal]\nQ = \"dd\"\n[map.grid.normal]\nX = \"dd\"\n")
	tab := loadOrders(t, a, 10)
	mode := func() string { r := a.statusLine().Right; return r[len(r)-1].Runs[0].Text }
	feed(t, a, "/id > 5<Esc>")
	if tab.where.Text != "id > 5" || tab.where.Pos != 5 || mode() != " NORMAL " || a.View().Cursor.Shape != tea.CursorBlock {
		t.Fatalf("NORMAL: %+v in %q", tab.where, mode())
	}
	if feed(t, a, "X"); tab.where.Text != "id >5" {
		t.Errorf("X is vim's, not the grid's map: %q", tab.where.Text)
	}
	if feed(t, a, "Q"); tab.where.Text != "" {
		t.Errorf("[map.normal]'s dd: %q", tab.where.Text)
	}
	if feed(t, a, "u0ciwamount<Esc>o"); tab.where.Text != "amount >5" || len(tab.ed.Lines()) != 1 || mode() != " NORMAL " {
		t.Errorf("u ciw o: %q in %q", tab.ed.Lines(), mode())
	}
	r := a.queryBar(a.focused(), tab).InputRect(bodyRect(a.layout()[a.focused().ID]))
	feed(t, a, "0ve")
	if f := a.render(); f.Buf.CellAt(r.Min.X+5, r.Min.Y).Style.Bg != a.theme.Visual || f.Buf.CellAt(r.Min.X+6, r.Min.Y).Style.Bg == a.theme.Visual || mode() != " VISUAL " {
		t.Errorf("VISUAL: amount not selected alone, in %q", mode())
	}
	keys(t, a, "y")
	if st := a.render().Buf.CellAt(r.Min.X, r.Min.Y).Style; st.Bg != a.theme.Yank || mode() != " NORMAL " {
		t.Errorf("y flashes what it took: %+v", st)
	}
	a.Update(flashDone{a.flashSeq})
	if click(a, uv.Pos(r.Min.X+3, r.Min.Y)); tab.where.Pos != 3 || tab.ed.Mode() != editor.Normal {
		t.Errorf("click: %+v in %v", tab.where, tab.ed.Mode())
	}
	if feed(t, a, "/"); tab.hist == nil || tab.ed.Mode() != editor.Insert || a.mode() != keymap.Command {
		t.Fatalf("/: history %v in %v", tab.hist, tab.ed.Mode())
	}
	if feed(t, a, " "); !tab.hist.typed {
		t.Error("typing does not filter the history")
	}
	if feed(t, a, "<BS><Esc><Esc><CR>"); tab.applied != "amount >5" || tab.typing != "" {
		t.Fatalf("↵: applied %q typing %q", tab.applied, tab.typing)
	}
	for _, ks := range []string{"/<Esc>dd<Esc>", "/<Esc>dd<C-c>"} {
		if feed(t, a, ks); tab.typing != "" || tab.where.Text != "amount >5" || tab.ed != nil {
			t.Errorf("%s: typing %q input %q", ks, tab.typing, tab.where.Text)
		}
	}
	feed(t, a, `/<Esc>0"+P`)
	if a.Update(tea.ClipboardMsg{Content: "x\n"}); tab.where.Text != "xamount >5" {
		t.Errorf(`"+P puts the clipboard's line as characters: %q`, tab.where.Text)
	}
}

// The history over the WHERE's vim in any mode (▾, C-r, NORMAL's /) has
// it in INSERT or REPLACE, so what is typed filters the list: VISUAL and
// a command pending end first (F3.39).
func TestWhereHistoryModes(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
	for _, ks := range []string{"/id > 5<Esc>vl", "/id > 5<Esc>d", "/id > 5<Esc>0R"} {
		feed(t, a, ks)
		a.run("where.history", 0)
		if feed(t, a, "x"); tab.hist == nil || !tab.hist.typed || tab.where.Text != "id > 5x" && tab.where.Text != "xd > 5" {
			t.Errorf("%s: %q in %v", ks, tab.where.Text, tab.ed.Mode())
		}
		feed(t, a, "<Esc><Esc><Esc>") // the list, INSERT, the WHERE
	}
}
