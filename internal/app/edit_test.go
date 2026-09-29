package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/jackc/pgx/v5/pgconn"

	"sqlmux/internal/db"
	"sqlmux/internal/db/postgres"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// editsOf is tab's changes as "row/col=value", DEFAULT as <default>.
func editsOf(tab *dataTab) string {
	var out []string
	for k, e := range tab.edits {
		v := e.val.S
		switch {
		case e.def:
			v = "<default>"
		case e.val.Null:
			v = "<null>"
		}
		out = append(out, k.row+"/"+k.col+"="+v)
	}
	return strings.Join(out, " ")
}

// i edits the current cell, its text all selected: typing replaces it, esc
// and ↵ keep it as the cell's change, typing the original back undoes it
// (§10.1). The save button counts the changed cells (Q-05).
func TestEditCell(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "li")
	if tab.cell == nil || a.mode() != keymap.Insert || a.context().Focus[0] != "cell" || !tab.cell.in.All || tab.cell.in.Text != "running" {
		t.Fatalf("i on status: %+v, mode %v", tab.cell, a.mode())
	}
	if info := a.statusLine().Info; info != "-- editing status --" {
		t.Errorf("status info %q", info)
	}
	feed(t, a, "done<Esc>")
	if tab.cell != nil || a.mode() != keymap.Normal || editsOf(tab) != "1/status=done" {
		t.Fatalf("esc keeps it: %q", editsOf(tab))
	}
	g := a.grid(a.focused(), tab)
	if !g.Edited[[2]int{0, 1}] || g.Rows[0][1].S != "done" {
		t.Errorf("the grid shows the change: %v %v", g.Edited, g.Rows[0][1])
	}
	if b := a.queryBar(a.focused(), tab).Buttons[0][2]; b.Action != "save" || b.Tail != "1" {
		t.Errorf("save button %+v", b)
	}
	feed(t, a, "<CR>")
	if tab.cell.in.Text != "done" || !tab.cell.in.All {
		t.Fatalf("again: starts from the change, %+v", tab.cell.in)
	}
	feed(t, a, "<Right><BS><BS><BS><BS>running<CR>")
	if len(tab.edits) != 0 {
		t.Errorf("back to what was loaded: %q", editsOf(tab))
	}
}

// Left as it started, an edit changes nothing: a NULL stays NULL, not ”
// (§10.1).
func TestEditUntouched(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "jj$h") // row 3's note: NULL
	if v := tab.page.Rows[tab.row][tab.fieldAt(tab.col)]; !v.Null {
		t.Fatalf("not on the NULL: %+v", v)
	}
	feed(t, a, "i<Esc>ix<BS><Esc>")
	if len(tab.edits) != 0 {
		t.Errorf("untouched: %q", editsOf(tab))
	}
	feed(t, a, "i<BS>x<CR>")
	if editsOf(tab) != "3/note=x" {
		t.Errorf("typed: %q", editsOf(tab))
	}
}

// Changes stay through other pages and queries: a row coming back shows
// its change again (§10.1).
func TestEditsOutlivePages(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "lix<Esc>")
	_, cols, page := ordersTable(3)
	other := db.Result{Cols: page.Cols, Rows: page.Rows[1:]}
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: cols, page: other})
	if g := a.grid(a.focused(), tab); len(g.Edited) != 0 || a.queryBar(a.focused(), tab).Buttons[0][2].Tail != "1" {
		t.Fatalf("row 1 is not on this page: %v", g.Edited)
	}
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: cols, page: page})
	if g := a.grid(a.focused(), tab); !g.Edited[[2]int{0, 1}] || g.Rows[0][1].S != "x" {
		t.Errorf("back again: %v", g.Edited)
	}
}

// A table with no row identity can't be saved to: no edit, a toast says
// why. A unique index over not-null columns will do (§10.1).
func TestEditNeedsRowIdentity(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	tab.cols.PK = nil
	a.Update(teaKey("i")) // not fed: its toast's Cmd waits out the toast
	if tab.cell != nil || !strings.Contains(a.toast, "t_order 没有主键") {
		t.Fatalf("no key: cell %+v, toast %q", tab.cell, a.toast)
	}
	tab.cols.Unique = [][]string{{"note", "id"}}
	if feed(t, a, "i"); tab.cell == nil || tab.cell.key.row != "note 1\x001" {
		t.Errorf("a unique index: %+v", tab.cell)
	}
}

// A paste goes into the input that has the keys, newlines as spaces; on a
// grid in NORMAL it starts editing the cell with it (§10.1).
func TestPaste(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	a.Update(tea.PasteMsg{Content: "a\nb"})
	if tab.cell == nil || tab.cell.in.Text != "a b" || tab.cell.in.All {
		t.Fatalf("paste in NORMAL: %+v", tab.cell)
	}
	a.Update(tea.PasteMsg{Content: "<Esc>c"})
	if tab.cell.in.Text != "a b<Esc>c" {
		t.Fatalf("paste while editing: %q", tab.cell.in.Text)
	}
	feed(t, a, "<Esc>/")
	a.Update(tea.PasteMsg{Content: "id > 1"})
	if tab.where.Text != "id > 1" {
		t.Errorf("paste in the WHERE: %q", tab.where.Text)
	}
}

// Anything else ends the edit first, keeping it: a click elsewhere (a cell
// then just moves the cursor), the wheel, a global key; C-c is esc (§10.1).
// A double click edits; a click in the input changes nothing.
func TestEditEndsFirst(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	p := a.focused()
	feed(t, a, "lix")
	c := find(t, a, ui.Target{Kind: ui.KindCell, Pane: p.ID, Action: "grid.goto 2 6"}) // clear of the options
	click(a, c.Min)
	if tab.cell != nil || tab.row != 2 || tab.col != 6 || editsOf(tab) != "1/status=x" {
		t.Fatalf("click on a cell: cell %+v at %d,%d, %q", tab.cell, tab.row, tab.col, editsOf(tab))
	}
	feed(t, a, "hiy") // note: text
	a.Update(tea.MouseWheelMsg{X: c.Min.X, Y: c.Min.Y, Button: tea.MouseWheelDown})
	if tab.cell != nil || len(tab.edits) != 2 {
		t.Fatalf("wheel: %q", editsOf(tab))
	}
	feed(t, a, "0i7<C-p>") // id has no options: C-p is the global key (§10.2)
	if tab.cell != nil || a.palette == nil || len(tab.edits) != 3 {
		t.Fatalf("C-p: %q", editsOf(tab))
	}
	feed(t, a, "<Esc>i<C-c>")
	if tab.cell != nil || a.toast != "" {
		t.Errorf("C-c: cell %+v, toast %q", tab.cell, a.toast)
	}
	e := find(t, a, ui.Target{Kind: ui.KindCell, Pane: p.ID, Action: "grid.goto 0 1"})
	click(a, e.Min)
	click(a, e.Min)
	if tab.cell == nil || tab.cell.key.col != "status" {
		t.Errorf("a double click edits: %+v", tab.cell)
	}
	click(a, e.Min) // inside the input: stays
	if tab.cell == nil {
		t.Error("a click in the input ended the edit")
	}
	feed(t, a, "w")
	if click(a, uv.Pos(0, 44)); tab.cell != nil || !strings.Contains(editsOf(tab), "1/status=w") {
		t.Errorf("a click where nothing is: %+v", tab.cell)
	}
}

// The input sits on the cell in the cursor's color, all selected, and
// widens past the cell for a longer text; changed cells read warn on
// edited_bg, underlined with dots (§7.6, §10.1).
func TestEditLooks(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "li")
	f := a.render()
	c := find(t, a, ui.Target{Kind: ui.KindCell, Pane: a.focused().ID, Action: "grid.goto 0 1"})
	if st := f.Buf.CellAt(c.Min.X+1, c.Min.Y).Style; st.Bg != a.theme.Visual {
		t.Errorf("selected: %v", st.Bg)
	}
	if f.Cursor == nil || f.Cursor.Y != c.Min.Y {
		t.Errorf("cursor %v", f.Cursor)
	}
	feed(t, a, strings.Repeat("w", 30))
	f = a.render()
	row := strings.Split(f.String(), "\n")[c.Min.Y]
	if !strings.Contains(row, strings.Repeat("w", 30)) {
		t.Errorf("wider than the cell: %q", row)
	}
	feed(t, a, "<Esc>j")
	f = a.render()
	st := f.Buf.CellAt(c.Min.X+1, c.Min.Y).Style
	if st.Fg != a.theme.Warn || st.Bg != a.theme.EditedBg || st.Underline != uv.UnderlineDotted {
		t.Errorf("changed: %+v", st)
	}
	_ = tab
}

// Editing a cell with more text than it holds: the input runs on over the
// cells to its right (§10.1); no number, a wavy line under it and the hint
// at it, the options past the hint (§10.7).
func TestGoldenCellEdit160x45(t *testing.T) {
	a := wide(160, 45)
	loadOrders(t, a, 60)
	feed(t, a, "jlli"+"a longer amount than the column holds")
	golden.RequireEqual(t, a.render().String())
}

// The three kinds of change as the grid shows them: text, NULL, DEFAULT
// (§7.6, §10.2).
func TestGoldenEdits160x45(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 60)
	tab.edits = map[editKey]edit{
		{"1", "status"}: {val: db.Val{S: "done"}},
		{"2", "note"}:   {val: db.Val{Null: true}, orig: db.Val{S: "line one"}},
		{"3", "amount"}: {def: true, orig: db.Val{S: "3.99"}},
	}
	golden.RequireEqual(t, a.render().String())
}

// saveDB is Main as the tests see it: every statement answers tag, and
// what was sent is kept.
type saveDB struct {
	tag  string
	sqls []string
}

func (s *saveDB) Query(_ context.Context, sql string, args ...db.Val) (db.Result, error) {
	s.sqls = append(s.sqls, fmt.Sprintf("%s %v", sql, args))
	return db.Result{Tag: s.tag}, nil
}
func (s *saveDB) Exec(context.Context, string, int) ([]db.Result, error) { return nil, nil }
func (s *saveDB) Close() error                                           { return nil }

// withMain is sized with t_order loaded and Main answering tag.
func withMain(t *testing.T, tag string) (*App, *dataTab, *saveDB) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	main := &saveDB{tag: tag}
	a.sess.Main = db.NewWorker(main)
	return a, tab, main
}

// C-s writes the tab's changes, an UPDATE a row in row identity order; saved,
// they go, the page and its count load again, and the query bar says so
// until the user fetches again (§10.3, Q-06).
func TestSave(t *testing.T) {
	a, tab, main := withMain(t, "UPDATE 1")
	feed(t, a, "jjlidone<Esc>kkix<Esc>$hinote<Esc>") // row 3's status, row 1's status and note
	_, cmd := a.Update(teaKey("<C-s>"))
	if cmd == nil || !tab.saving {
		t.Fatal("C-s saves")
	}
	if _, again := a.Update(teaKey("<C-s>")); again != nil {
		t.Error("a second save while one is out")
	}
	m := cmd().(saveMsg)
	want := []string{
		"begin []",
		`update "public"."t_order" set "status" = $1, "note" = $2 where "id" = $3 and format('%s', "status") = $4 and format('%s', "note") = $5 [{x false} {note false} {1 false} {running false} {note 1 false}]`,
		`update "public"."t_order" set "status" = $1 where "id" = $2 and format('%s', "status") = $3 [{done false} {3 false} {failed false}]`,
		"commit []",
	}
	if strings.Join(main.sqls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("sent:\n%s", strings.Join(main.sqls, "\n"))
	}
	busy := a.busy
	a.Update(m)
	if len(tab.edits) != 0 || tab.saving || a.busy != busy || !tab.recount {
		t.Fatalf("saved: edits %q, busy %d → %d", editsOf(tab), busy, a.busy)
	}
	if n := a.queryBar(a.focused(), tab).Note; !strings.HasPrefix(n.Head, "已保存 2 行 · ") || n.Fg != nil {
		t.Errorf("note %+v", n)
	}
	if fg := styleOf(t, a.render(), "已保存").Fg; fg != a.theme.Dim {
		t.Errorf("saved: fg %v, want dim", fg)
	}
	answer(a, tab) // the page again: the note stays
	if !strings.HasPrefix(a.queryBar(a.focused(), tab).Note.Head, "已保存") {
		t.Error("the reload dropped the note")
	}
	feed(t, a, "R")
	if a.queryBar(a.focused(), tab).Note != (ui.Note{}) {
		t.Error("a fetch of the user's keeps the note")
	}
	if _, cmd := a.Update(teaKey("<C-s>")); cmd != nil {
		t.Error("nothing to save")
	}
}

// A row that changed or went since it was loaded fails the save: all rolled
// back, the changes kept, the row named on the error bar, which stays till
// a save goes, its number in error until the next change or fetch (§10.3,
// §7.8「错误栏」). A cancel says so on the query bar.
func TestSaveFails(t *testing.T) {
	a, tab, _ := withMain(t, "UPDATE 0")
	feed(t, a, "lix<Esc>")
	_, cmd := a.Update(teaKey("<C-s>"))
	a.Update(cmd())
	if tab.bar == nil || tab.bar.First != (ui.Note{Head: "id = 1 的", Mid: "行数据已变化或行不存在", Tail: "，已回滚"}) || tab.note != (ui.Note{}) || len(tab.edits) != 1 {
		t.Fatalf("bar %+v note %+v, edits %q", tab.bar, tab.note, editsOf(tab))
	}
	if g := a.grid(a.focused(), tab); !g.Failed[0] {
		t.Error("row 1's number is not marked")
	}
	feed(t, a, "jiy<Esc>")
	if tab.failed != "" || tab.bar == nil {
		t.Errorf("a change unmarks the row, the bar stays: %+v", tab.bar)
	}
	a.Update(saveMsg{tab: tab, failed: -1, err: context.Canceled})
	if tab.note.Head != "已取消，已回滚" || len(tab.edits) != 2 {
		t.Errorf("cancelled: %+v", tab.note)
	}
	// the server's error: its message alone, cut to fit between the row and 已回滚
	bad := &pgconn.PgError{Severity: "ERROR", Code: "22P02", Message: `invalid input syntax for type numeric: "abc"`, Hint: "a number"}
	a.Update(saveMsg{tab: tab, rows: []postgres.Row{{Key: []string{"12"}}}, failed: 0, err: bad})
	if b := tab.bar; b.First != (ui.Note{Head: "[22P02] id = 12：", Mid: bad.Message, Tail: "，已回滚"}) || !slices.Equal(b.More, []string{"HINT: a number"}) {
		t.Errorf("a server's error: %+v", b)
	}
	a.Update(saveMsg{tab: tab, failed: -1})
	if tab.bar != nil {
		t.Error("a save that goes takes it away")
	}
}

// Changes made while a save is out stay when it lands (§10.3); one of a
// cell that was sent is checked against what was sent next time.
func TestSaveKeepsNewer(t *testing.T) {
	a, tab, _ := withMain(t, "UPDATE 1")
	feed(t, a, "lix<Esc>li1<Esc>")
	_, cmd := a.Update(teaKey("<C-s>"))
	m := cmd().(saveMsg)
	feed(t, a, "i2<Esc>ji3<Esc>") // amount again, and row 2's
	a.Update(m)
	if got := editsOf(tab); !strings.Contains(got, "1/amount=2") || !strings.Contains(got, "2/amount=3") || strings.Contains(got, "status") {
		t.Errorf("left: %q", got)
	}
	if e := tab.edits[editKey{"1", "amount"}]; e.orig.S != "1" {
		t.Errorf("changed again, the row holds what was sent: orig %+v", e.orig)
	}
}

// Before changes are thrown away, a box asks (§10.5): R, x, SPC x, :qa and
// the second C-c; y goes on, n, esc or a click outside keep them.
func TestConfirm(t *testing.T) {
	a, tab, _ := withMain(t, "UPDATE 1")
	feed(t, a, "lix<Esc>R")
	if a.confirm == nil || a.mode() != keymap.Command || a.context().Overlay != "confirm" {
		t.Fatal("R asks")
	}
	if f := a.render().String(); !strings.Contains(f, "有 1 处修改未保存，刷新会丢弃。") || !strings.Contains(f, "y 刷新") || !strings.Contains(f, "n 取消") {
		t.Errorf("the box:\n%s", f)
	}
	if feed(t, a, "n"); a.confirm != nil || len(tab.edits) != 1 {
		t.Fatal("n keeps them")
	}
	if feed(t, a, "Ry"); a.confirm != nil || len(tab.edits) != 0 {
		t.Fatal("y drops them")
	}
	for keys, text := range map[string]string{
		"x":          "t_order 有 1 处修改未保存，关闭会丢弃。",
		"<Space>x":   "这个 pane 里有 1 处修改未保存，关闭会丢弃。",
		":qa<CR>":    "有 1 处修改未保存，退出会丢弃。",
		"<C-c><C-c>": "有 1 处修改未保存，退出会丢弃。",
	} {
		answer(a, tab)
		feed(t, a, "0lix<Esc>"+keys)
		if a.confirm == nil || a.confirm.text != text {
			t.Fatalf("%s: %+v", keys, a.confirm)
		}
		if feed(t, a, "<Esc>"); a.confirm != nil || len(p(a).Tabs) != 1 {
			t.Fatalf("%s: esc keeps them", keys)
		}
		tab.edits = nil
	}
	feed(t, a, "0lix<Esc>:qa<CR>")
	click(a, uv.Pos(1, 1))
	if a.confirm != nil {
		t.Error("a click outside says no")
	}
	if feed(t, a, ":qa<CR>"); !feed(t, a, "y") {
		t.Error("y quits")
	}
}

func p(a *App) *Pane { return a.focused() }

// Opening a table over a tab with changes opens a new tab instead (§12).
func TestOpenKeepsChanges(t *testing.T) {
	a, tab, _ := withMain(t, "UPDATE 1")
	feed(t, a, "lix<Esc><C-p>@t_user<CR>")
	if pane := a.focused(); tabNames(pane) != "t_order t_user" || len(tab.edits) != 1 {
		t.Errorf("tabs %v, edits %q", tabNames(pane), editsOf(tab))
	}
}

// The box over the table: at the middle of the screen, the buttons at its
// right (§10.5).
func TestGoldenConfirm160x45(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 60)
	tab.edits = map[editKey]edit{{"1", "status"}: {val: db.Val{S: "done"}, orig: db.Val{S: "running"}}}
	feed(t, a, "x")
	golden.RequireEqual(t, a.render().String())
}

// optionLabels is the options of the cell being edited, the picked one in [ ].
func optionLabels(tab *dataTab) string {
	var out []string
	for i, o := range tab.options() {
		if i == tab.cell.sel {
			o.label = "[" + o.label + "]"
		}
		out = append(out, o.label)
	}
	return strings.Join(out, " ")
}

// A cell offers its column's values, filtered once typing starts, then
// NULL, DEFAULT and back to what was loaded, as the column allows; none is
// picked as they open, C-n / C-p and ↑ / ↓ pick around the ends, ↵ applies
// the one picked, else takes the text (§10.2).
func TestCellOptions(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "li")
	if got := optionLabels(tab); got != "pending running done failed ∅ NULL" || !a.grid(a.focused(), tab).EditMenu {
		t.Fatalf("status: %s", got)
	}
	if feed(t, a, "don"); optionLabels(tab) != "done ∅ NULL" {
		t.Fatalf("don: %s", optionLabels(tab))
	}
	if feed(t, a, "<C-n>"); optionLabels(tab) != "[done] ∅ NULL" || a.context().Overlay != "options" {
		t.Fatalf("C-n: %s", optionLabels(tab))
	}
	if feed(t, a, "e"); tab.cell.sel != -1 {
		t.Error("typing drops the pick")
	}
	feed(t, a, "<BS><Down><CR>")
	if tab.cell != nil || editsOf(tab) != "1/status=done" {
		t.Fatalf("↵ on done: %q", editsOf(tab))
	}
	feed(t, a, "i<Up>")
	if got := optionLabels(tab); got != "pending running done failed ∅ NULL [↺ 原值]" {
		t.Fatalf("again, up: %s", got)
	}
	if feed(t, a, "<CR>"); len(tab.edits) != 0 {
		t.Fatalf("↺: %q", editsOf(tab))
	}
	feed(t, a, "izzz<CR>")
	if editsOf(tab) != "1/status=zzz" {
		t.Errorf("↵ with none picked takes the text, not NULL: %q", editsOf(tab))
	}
	tab.edits = nil
	feed(t, a, "llit")
	if got := optionLabels(tab); got != "true ∅ NULL" {
		t.Errorf("paid, t: %s", got)
	}
	feed(t, a, "<C-n><CR>")
	if len(tab.edits) != 0 {
		t.Errorf("true on a t is no change: %q", editsOf(tab))
	}
	tab.cols.Cols[0].Default = "nextval('t_order_id_seq')"
	feed(t, a, "0i")
	if got := optionLabels(tab); got != "DEFAULT" {
		t.Fatalf("id: not null, a default: %s", got)
	}
	if feed(t, a, "<C-p><CR>"); editsOf(tab) != "1/id=<default>" {
		t.Errorf("DEFAULT: %q", editsOf(tab))
	}
	tab.cols.Cols[0].Default, tab.edits = "", nil
	if feed(t, a, "i"); a.grid(a.focused(), tab).EditMenu || tab.options() != nil || a.context().Overlay != "" {
		t.Errorf("no options: %s", optionLabels(tab))
	}
}

// Tab and S-Tab pick options as C-n and C-p do, a time's parts in a time
// cell; folded by ▾, Tab and the arrows do nothing (§10.2).
func TestCellOptionsTab(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "3li")
	if feed(t, a, "<Tab><Tab>"); optionLabels(tab) != "true [false] ∅ NULL" {
		t.Fatalf("paid, Tab Tab: %s", optionLabels(tab))
	}
	if feed(t, a, "<S-Tab>"); optionLabels(tab) != "[true] false ∅ NULL" {
		t.Fatalf("S-Tab: %s", optionLabels(tab))
	}
	if feed(t, a, "<S-Tab><S-Tab><CR>"); tab.cell != nil || editsOf(tab) != "1/paid=f" {
		t.Fatalf("around the ends, back to false: %q", editsOf(tab))
	}
	feed(t, a, "i")
	a.run("cell.options", 0) // ▾
	if feed(t, a, "<Tab><Down>"); tab.cell.sel != -1 || a.context().Overlay != "" {
		t.Errorf("folded: sel %d", tab.cell.sel)
	}
	feed(t, a, "<Esc>$i")
	a.run("cell.options", 0)
	if feed(t, a, "<Tab>"); tab.cell.seg != 0 || a.context().Overlay != "" {
		t.Errorf("a time folded: seg %d, overlay %q", tab.cell.seg, a.context().Overlay)
	}
}

// The mouse: a click on an option applies it, hovering only lights it; the
// ▾ folds them away and back (§10.2).
func TestCellOptionsMouse(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "li")
	arrow := find(t, a, ui.Target{Kind: ui.KindButton, Action: "cell.options"})
	click(a, arrow.Min)
	if tab.cell == nil || !tab.cell.folded {
		t.Fatal("▾ folds")
	}
	if feed(t, a, "<C-n>"); tab.cell.sel != -1 {
		t.Error("folded, nothing is picked")
	}
	click(a, arrow.Min)
	row := find(t, a, ui.Target{Kind: ui.KindRow, I: 2})
	a.Update(tea.MouseMotionMsg{X: row.Min.X, Y: row.Min.Y})
	if tab.cell.sel != -1 {
		t.Error("hovering picks")
	}
	click(a, row.Min)
	if tab.cell != nil || editsOf(tab) != "1/status=done" {
		t.Errorf("click on done: %q", editsOf(tab))
	}
}

// 设为 NULL / 设为 DEFAULT from the palette: the cursor's cell, when its
// column can hold it (§10.2).
func TestSetNull(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "$h<C-p>>设为 NULL<CR>")
	if editsOf(tab) != "1/note=<null>" {
		t.Fatalf("note: %q", editsOf(tab))
	}
	feed(t, a, "0")
	a.run("cell.null", 0)
	a.run("cell.default", 0)
	if len(tab.edits) != 1 {
		t.Errorf("id is not null and has no default: %q", editsOf(tab))
	}
}

// A cell's options under its edit, none picked, the ▾ at the edit's right
// (§10.2).
func TestGoldenCellOptions160x45(t *testing.T) {
	a := wide(160, 45)
	loadOrders(t, a, 60)
	feed(t, a, "jli")
	golden.RequireEqual(t, a.render().String())
}

// A time cell steps its parts: Tab / S-Tab pick one, ↑ / ↓ step it, the
// text following; ◷ 现在 fills the text and the edit goes on; a text that
// doesn't parse steps nothing (§10.2).
func TestTimeCell(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "$i")
	if tab.cellKind() != ui.TimestampTZ || optionLabels(tab) != "◷ 现在 ∅ NULL" {
		t.Fatalf("created_at: %d, %s", tab.cellKind(), optionLabels(tab))
	}
	if f := a.render().String(); !strings.Contains(f, "2026 - 09 - 01   00 : 01 : 00") {
		t.Fatalf("the parts:\n%s", f)
	}
	feed(t, a, "<Tab><Up>")
	if in := tab.cell.in; in.Text != "2026-10-01 00:01:00+00" || in.All || tab.cell.seg != 1 {
		t.Fatalf("month up: %+v", in)
	}
	feed(t, a, "<Down><S-Tab><S-Tab><Up>")
	if tab.cell.in.Text != "2026-09-01 00:01:01+00" || tab.cell.seg != 5 {
		t.Fatalf("round to the seconds, up: %q", tab.cell.in.Text)
	}
	feed(t, a, "<C-n><CR>")
	if tab.cell == nil || ui.TimeSegs(ui.TimestampTZ, tab.cell.in.Text) == nil || tab.cell.in.Text == "2026-09-01 00:01:01+00" {
		t.Fatalf("now: %+v", tab.cell)
	}
	feed(t, a, "<CR>")
	if tab.cell != nil || len(tab.edits) != 1 {
		t.Fatalf("↵ takes it: %q", editsOf(tab))
	}
	feed(t, a, "ix<Up>")
	if tab.cell.in.Text != "x" {
		t.Errorf("unparsed steps: %q", tab.cell.in.Text)
	}
}

// The mouse on a time's parts: ▴ / ▾ step one, a click picks one, the
// wheel over one steps it (§10.2).
func TestTimeCellMouse(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "$i")
	click(a, find(t, a, ui.Target{Kind: ui.KindButton, Action: "cell.inc 2"}).Min)
	if tab.cell == nil || tab.cell.in.Text != "2026-09-02 00:01:00+00" || tab.cell.seg != 2 {
		t.Fatalf("▴ on the day: %+v", tab.cell)
	}
	hour := find(t, a, ui.Target{Kind: ui.KindButton, Action: "cell.seg 3"})
	click(a, hour.Min)
	if tab.cell.seg != 3 {
		t.Fatalf("a click on the hour: %d", tab.cell.seg)
	}
	a.Update(tea.MouseWheelMsg{X: hour.Min.X, Y: hour.Min.Y, Button: tea.MouseWheelUp})
	if tab.cell == nil || tab.cell.in.Text != "2026-09-02 01:01:00+00" {
		t.Errorf("the wheel on the hour: %+v", tab.cell)
	}
}

// A time cell's box under its edit: ▴ / parts / ▾ / options (§10.2).
func TestGoldenTimePick160x45(t *testing.T) {
	a := wide(160, 45)
	loadOrders(t, a, 60)
	feed(t, a, "j$i<Tab>")
	golden.RequireEqual(t, a.render().String())
}

// :wq saves a table's changes and closes it once they are; when the save
// fails the tab stays with the reason on its query bar, and nothing is
// asked (§11「文件」). With nothing to save it is :q.
func TestSaveAndClose(t *testing.T) {
	for _, c := range []struct {
		tag    string
		closed bool
	}{{"UPDATE 1", true}, {"UPDATE 0", false}} {
		a, tab, _ := withMain(t, c.tag)
		p := a.focused()
		feed(t, a, "lix<Esc>")
		cmd := a.run(exAliases["wq"], 0)
		if cmd == nil || a.confirm != nil || dataOf(p) != tab {
			t.Fatalf("%s: :wq saves first: confirm %v", c.tag, a.confirm)
		}
		a.Update(cmd())
		if closed := dataOf(p) != tab; closed != c.closed || a.confirm != nil {
			t.Errorf("%s: closed %v, confirm %v, note %+v", c.tag, closed, a.confirm, tab.note)
		}
	}
	a, tab, _ := withMain(t, "UPDATE 1")
	if a.run("tab.save.close", 0); dataOf(a.focused()) == tab {
		t.Error("nothing to save: :wq closes at once")
	}
}

// cellCases are cellCheck's cases, with whether PG 17 takes each (pg): the
// integration test asks it. Where they differ, ours is a noted ceiling.
var cellCases = []struct {
	typ, text, want string
	pg              bool
}{
	{"integer", "12", "", true}, {"integer", " 12 ", "", true}, {"integer", "+5", "", true}, {"integer", "0x1F", "", true},
	{"integer", "1_000", "", true}, {"integer", "010", "", true}, {"integer", "", "", false},
	{"integer", "1__0", "不是有效的整数", false}, {"integer", "_1", "不是有效的整数", false}, {"integer", "1.0", "不是有效的整数", false},
	{"integer", "1e5", "不是有效的整数", false}, {"integer", "10d", "不是有效的整数", false}, {"integer", "--1", "不是有效的整数", false},
	{"integer", "2147483648", "超出 int4 的范围", false}, {"integer", "-2147483648", "", true},
	{"smallint", "32768", "超出 int2 的范围", false}, {"smallint", "-0x8000", "", true}, {"smallint", "0x_1F", "", true},
	{"bigint", "9223372036854775808", "超出 int8 的范围", false},
	{"numeric", "1.5", "", true}, {"numeric", " .5 ", "", true}, {"numeric", "5.", "", true}, {"numeric", "1e5", "", true},
	{"numeric", "1_000.5", "", true}, {"numeric", "0x10", "", true}, {"numeric", "0b101", "", true}, {"numeric", "1_0e1_0", "", true},
	{"numeric", "inf", "", true}, {"numeric", "+Infinity", "", true}, {"numeric", "-inf", "", true}, {"numeric", "NaN", "", true},
	{"numeric", "1e", "不是有效的数字", false}, {"numeric", "e5", "不是有效的数字", false}, {"numeric(10,2)", "10d", "不是有效的数字", false},
	{"double precision", " 1e3 ", "", true}, {"double precision", "Infinity", "", true}, {"double precision", "-inf", "", true},
	{"double precision", "nan", "", true}, {"double precision", "1e400", "超出 float8 的范围", false},
	{"double precision", "1_000", "不是有效的数字", false}, {"double precision", "abc", "不是有效的数字", false},
	{"real", "3.5", "", true}, {"real", "1e39", "超出 float4 的范围", false},
	{"boolean", "t", "", true}, {"boolean", "tr", "", true}, {"boolean", "ye", "", true}, {"boolean", "of", "", true},
	{"boolean", "on", "", true}, {"boolean", " TrUe ", "", true}, {"boolean", "1", "", true}, {"boolean", "n", "", true},
	{"boolean", "o", "不是有效的布尔值", false}, {"boolean", "2", "不是有效的布尔值", false}, {"boolean", "tx", "不是有效的布尔值", false},
	{"uuid", "{a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11}", "", true}, {"uuid", "a0eebc999c0b4ef8bb6d6bb9bd380a11", "", true},
	{"uuid", "a0ee-bc99-9c0b-4ef8-bb6d-6bb9-bd38-0a11", "", true}, {"uuid", "A0EEBC99-9C0B-4EF8-BB6D-6BB9BD380A11", "", true},
	{"uuid", "{a0eebc999c0b4ef8bb6d6bb9bd380a11", "不是有效的 UUID", false}, {"uuid", " a0eebc999c0b4ef8bb6d6bb9bd380a11", "不是有效的 UUID", false},
	{"uuid", "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a1-1", "不是有效的 UUID", false}, {"uuid", "-a0eebc999c0b4ef8bb6d6bb9bd380a11", "不是有效的 UUID", false},
	{"uuid", "a0eebc99", "不是有效的 UUID", false},
	{"jsonb", `{"a": [1, "b"]}`, "", true}, {"json", " 1 ", "", true}, {"jsonb", `[1,`, "不是有效的 JSON", false},
	{"date", "2026-09-20", "", true}, {"date", "2026-9-1", "", true}, {"date", "2026-09-20 BC", "", true}, {"date", " now ", "", true},
	{"date", "Today", "", true}, {"date", "epoch", "", true}, {"date", "-infinity", "", true},
	{"date", "2026/09/20", "不是有效的日期 / 时间", true}, // PG's too: a ceiling
	{"timestamp without time zone", "2026-09-01", "", true}, {"timestamp without time zone", "2026-09-01 10:00", "", true},
	{"timestamp without time zone", "2026-09-01T10:00:00", "", true}, {"timestamp without time zone", "2026-09-01 10:00:00+08", "", true},
	{"timestamp without time zone", "tomorrow", "", true},
	{"timestamp(3) with time zone", "2026-09-01 10:00:00.5 +08:00", "", true}, {"timestamp with time zone", "2026-09-01 10:00Z", "", true},
	{"timestamp with time zone", "+infinity", "", true}, {"timestamp with time zone", "2026-09-01 10", "不是有效的日期 / 时间", false},
	{"time without time zone", "10:00", "", true}, {"time without time zone", "24:00:00", "", true},
	{"time without time zone", "10:00:00.123", "", true}, {"time without time zone", "now", "", true}, {"time without time zone", "allballs", "", true},
	{"time without time zone", "today", "不是有效的日期 / 时间", false}, {"time without time zone", "epoch", "不是有效的日期 / 时间", false},
	{"time with time zone", "10:00:00+08", "", true}, {"time with time zone", "10:00", "", true},
	{"text", "anything", "", true}, {"integer[]", "{1,2}", "", true},
}

// What a cell's edit warns of, type by type, as PG 17 reads them (§10.7).
func TestCellCheck(t *testing.T) {
	for _, c := range cellCases {
		if got := cellCheck(c.typ, c.text); got != c.want {
			t.Errorf("%s %q: %q, want %q", c.typ, c.text, got, c.want)
		}
	}
}

// Text that is no value of its column: a wavy line under it and the hint
// at it; ↵, a click elsewhere, the wheel and a global key leave it be,
// esc drops it for what the cell had before (§10.7).
func TestCellCheckBlocks(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	p := a.focused()
	feed(t, a, "lli5<Esc>i10d")
	f := a.render()
	if !strings.Contains(f.String(), "不是有效的数字") {
		t.Fatal("no hint")
	}
	if st := styleOf(t, f, "10d"); st.Underline != uv.UnderlineCurly || st.UnderlineColor != a.theme.Error {
		t.Errorf("the line under it: %+v", st)
	}
	feed(t, a, "<CR><C-p>")
	click(a, find(t, a, ui.Target{Kind: ui.KindCell, Pane: p.ID, Action: "grid.goto 2 5"}).Min)
	a.Update(tea.MouseWheelMsg{X: 100, Y: 20, Button: tea.MouseWheelDown})
	if tab.cell == nil || a.palette != nil || tab.row != 0 || tab.cell.in.Text != "10d" {
		t.Fatalf("it stays: cell %+v, palette %v, row %d", tab.cell, a.palette != nil, tab.row)
	}
	if feed(t, a, "<Esc>"); tab.cell != nil || editsOf(tab) != "1/amount=5" {
		t.Fatalf("esc: back to 5: %q", editsOf(tab))
	}
	if feed(t, a, "i10d<BS><CR>"); tab.cell != nil || editsOf(tab) != "1/amount=10" {
		t.Errorf("a number goes: %q", editsOf(tab))
	}
}

// r takes the cursor's cell back to what was loaded, that cell alone; a
// row with changes is marked, for its number in warn (§10.1). In the
// result area r does nothing: its tables are not edited.
func TestRevert(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "lix<Esc>lli7<Esc>")
	if g := a.grid(a.focused(), tab); !g.Changed[0] || g.Changed[1] {
		t.Fatalf("changed rows: %v", g.Changed)
	}
	if feed(t, a, "r"); editsOf(tab) != "1/status=x" {
		t.Fatalf("r on amount: %q", editsOf(tab))
	}
	if feed(t, a, "hhr"); len(tab.edits) != 0 || a.grid(a.focused(), tab).Changed[0] {
		t.Errorf("all taken back: %q", editsOf(tab))
	}
	a.run("grid.revert", 0) // no change here: nothing to take back
}
