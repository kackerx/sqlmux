package app

import (
	"slices"
	"strings"
	"testing"

	"sqlmux/internal/config"
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
// history, filtered by what is typed; C-f stars; ↵ runs one with its ORDER
// and LIMIT; it all outlives a restart (Q-02, §14).
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
	feed(t, a, "/"+clear+"<C-r>")
	if tab.hist == nil || a.mode() != keymap.Command {
		t.Fatal("C-r opens the list")
	}
	feed(t, a, "<C-n><C-f>")
	if len(st.Favorites) != 1 || st.Favorites[0].Where != "status = 'done'" {
		t.Fatalf("favorites %+v", st.Favorites)
	}
	if f := a.render().String(); !strings.Contains(f, "收藏") || !strings.Contains(f, "amount ↑ · 500") {
		t.Errorf("the list:\n%s", f)
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
