package app

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// expectedJSON is the page of the table as an array of the records Y yanks,
// each as the clipboard got it (the table's own view is on, as it is not in the
// JSON view); the clipboard is not jsonText's own work.
func expectedJSON(t *testing.T, a *App, rows int) string {
	t.Helper()
	p := a.focused()
	gs, _, _, ok := a.gridOf(p)
	if !ok {
		t.Fatal("no grid")
	}
	was := gs.row
	defer func() { gs.row = was }()
	var objects []string
	for row := range rows {
		gs.row = row
		text, ok := clipText(a.yankJSON())
		if !ok {
			t.Fatalf("Y of record %d put nothing on the clipboard", row)
		}
		objects = append(objects, "  "+strings.ReplaceAll(text, "\n", "\n  "))
	}
	return "[\n" + strings.Join(objects, ",\n") + "\n]"
}

// gj puts the page of the table as a JSON array in its place: each record as Y
// has it, a chip JSON on the query bar, a vim over the text that takes no
// change; esc brings the table back; the text follows the page; it is the tab's
// and no layout's (F14.5).
func TestJSONView(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	p := a.focused()
	feed(t, a, "gj")
	jv := jsonViewOf(p)
	if jv == nil {
		t.Fatal("gj opens no JSON view")
	}
	a.jsonSync(p)
	want := expectedJSON(t, a, 3)
	if got := strings.Join(jv.ed.Lines(), "\n"); got != want || !strings.HasPrefix(want, "[\n  {\n    \"id\": ") {
		t.Fatalf("the text:\n%s\nwant\n%s", got, want)
	}
	view := a.render().String()
	if !strings.Contains(view, `"status"`) || !strings.Contains(view, "JSON") || strings.Contains(view, "│ amount │") {
		t.Errorf("the screen has no JSON in the table's place:\n%s", view)
	}
	feed(t, a, "jj")
	if jv.ed.Cursor().Line != 2 {
		t.Errorf("j is the vim's: %v", jv.ed.Cursor())
	}
	if feed(t, a, "ix"); strings.Join(jv.ed.Lines(), "\n") != want || a.toast != "只读" {
		t.Errorf("a read only text: toast %q", a.toast)
	}
	a.toast = ""
	if jv.ed.Mode() != editor.Normal {
		t.Fatalf("a read only editor does not go to INSERT: %v", jv.ed.Mode())
	}
	feed(t, a, "<Esc>")
	if jsonViewOf(p) != nil || !strings.Contains(a.render().String(), "│ amount │") {
		t.Error("esc brings the table back")
	}
	// the page changes under it: the text follows
	feed(t, a, "gj")
	jv = jsonViewOf(p)
	a.jsonSync(p)
	_, cols, page := ordersTable(2)
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: cols, page: page, next: true})
	a.render()
	if got := strings.Join(jv.ed.Lines(), "\n"); got != expectedJSON(t, a, 2) || strings.Count(got, `"id"`) != 2 {
		t.Errorf("a new page:\n%s", got)
	}
	// turned and JSON are one view at a time, the other way as well
	a.run("grid.transpose", 0)
	if jsonViewOf(p) != nil || !tab.transpose {
		t.Errorf("transpose from the JSON view: jv %v, transpose %v", jsonViewOf(p), tab.transpose)
	}
	feed(t, a, "gj")
	if jsonViewOf(p) == nil || tab.transpose {
		t.Errorf("gj from the turned table: jv %v, transpose %v", jsonViewOf(p), tab.transpose)
	}
	feed(t, a, "<Esc>")
	if !tab.transpose {
		t.Error("esc brings the turned table back turned")
	}
	feed(t, a, "gj")
	// a pending command is the editor's first: esc after y is the y's, q is a key of the view
	feed(t, a, "y")
	if feed(t, a, "<Esc>"); jsonViewOf(p) == nil {
		t.Error("esc after a y is the y's")
	}
	feed(t, a, "T") // vim's till-backward, no key of the view's
	if jsonViewOf(p) == nil || tab.transpose {
		t.Error("T is the editor's")
	}
	feed(t, a, "<Esc>")
	feed(t, a, "gj")
	if jsonViewOf(p) != nil {
		t.Error("gj again brings the table back")
	}
	feed(t, a, "gjq")
	if jsonViewOf(p) != nil {
		t.Error("q brings the table back")
	}
	feed(t, a, "gj:q<CR>") // as the read only value editor's :q
	if jsonViewOf(p) != nil {
		t.Error(":q brings the table back")
	}
	feed(t, a, "gj:w<CR>")
	if jsonViewOf(p) == nil || !strings.Contains(a.toast, "不支持的命令") {
		t.Errorf(":w is none of its: toast %q", a.toast)
	}
}

// The JSON view takes the table's turn with it and gives it back, by esc, q
// and gj alike, turned or not, a table or a result (F22.1).
func TestJSONViewKeepsTurn(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	d := &execDB{res: map[string]db.Result{"select 1": rows(3)}}
	ra, _ := inRun(t, d, "select 1")
	press(t, ra, "<CR>")
	res := ra.win().Result
	ra.win().focus(res.ID)
	for _, back := range []string{"<Esc>", "q", "gj"} {
		for _, turned := range []bool{true, false} {
			tab.transpose = turned
			feed(t, a, "gj")
			feed(t, a, back)
			if jsonViewOf(a.focused()) != nil || tab.transpose != turned {
				t.Errorf("table turned %v, gj %s: view %v, turned %v", turned, back, jsonViewOf(a.focused()) != nil, tab.transpose)
			}
			rt := resultOf(res)
			rt.transpose = turned
			feed(t, ra, "gj")
			feed(t, ra, back)
			if jsonViewOf(res) != nil || rt.transpose != turned {
				t.Errorf("result turned %v, gj %s: view %v, turned %v", turned, back, jsonViewOf(res) != nil, rt.transpose)
			}
		}
	}
}

// A result has its JSON view too, its page, no more: a result is read only (F14.5).
func TestJSONViewOfResult(t *testing.T) {
	d := &execDB{res: map[string]db.Result{"select 1": rows(3)}}
	a, _ := inRun(t, d, "select 1")
	press(t, a, "<CR>")
	res := a.win().Result
	a.win().focus(res.ID)
	if resultOf(res) == nil || resultOf(res).run == nil {
		t.Fatalf("no result tab: %v", tabNames(res))
	}
	feed(t, a, "gj")
	jv := jsonViewOf(res)
	if jv == nil {
		t.Fatal("no JSON view on a result")
	}
	a.jsonSync(res)
	if got, want := strings.Join(jv.ed.Lines(), "\n"), expectedJSON(t, a, 3); got != want {
		t.Errorf("the text:\n%s\nwant\n%s", got, want)
	}
	if view := a.render().String(); !strings.Contains(view, `"n"`) {
		t.Errorf("not in the screen:\n%s", view)
	}
	tabs := len(res.Tabs)
	feed(t, a, "x") // the result's x closes the tab: not a key of the view
	if len(res.Tabs) != tabs || jsonViewOf(res) == nil {
		t.Errorf("x: %d tabs, view %v", len(res.Tabs), jsonViewOf(res) != nil)
	}
	if _, cmd := a.Update(teaKey("R")); cmd == nil || a.sess.Conns[0].busy != 1 { // result.rerun, the result's refresh (F15.9)
		t.Errorf("R reruns the statement: busy %d", a.sess.Conns[0].busy)
	}
	a.sess.Conns[0].busy = 0
	feed(t, a, "<Esc>")
	if jsonViewOf(res) != nil {
		t.Error("esc brings the result's table back")
	}
}

// The text of the table has what is changed and not yet saved, as Y has it; a
// change made while the view is open shows (F14.5).
func TestJSONViewHasChanges(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	p := a.focused()
	feed(t, a, "gj")
	jv := a.jsonSync(p)
	before := strings.Join(jv.ed.Lines(), "\n")
	tab.edits = map[editKey]edit{{tab.rowKey(0), "note"}: {val: db.Val{S: "changed here"}, orig: tab.page.Rows[0][noteField(tab)]}}
	a.render()
	if got := strings.Join(jv.ed.Lines(), "\n"); got == before || !strings.Contains(got, "changed here") {
		t.Errorf("a change shows: %q", got)
	}
}

func noteField(t *dataTab) int {
	for i, c := range t.page.Cols {
		if c.Name == "note" {
			return i
		}
	}
	return 0
}

// The JSON view has gj and esc of its own, and pages and refreshes by the keys
// the table's scope has for it, as the user has them; no other key of the
// table's gets in, and the cursor's keys are the vim's (F14.5, F15.9).
func TestJSONViewKeys(t *testing.T) {
	a := sizedWith(160, 45, userConfig(t, "", "[keys.grid]\n\"<\" = \"grid.page.prev\"\n\">\" = \"grid.page.next\"\n\"U\" = \"grid.refresh\"\n"))
	tab := loadOrders(t, a, 3)
	p := a.focused()
	feed(t, a, "gj")
	for key, want := range map[string]string{"gj": "grid.view.json", "esc": "json.leave"} {
		if got := a.keys.Hint(want, "jsonview"); got != key {
			t.Errorf("%s is bound to %q", want, got)
		}
	}
	for _, c := range []struct{ keys, what string }{{"]", "the default next page"}, {"[", "the default previous page"}, {"R", "the default refresh"}, {">", "the user's next page"}, {"<", "the user's previous page"}, {"U", "the user's refresh"}} {
		seq := tab.seq
		feed(t, a, c.keys)
		if tab.seq == seq || jsonViewOf(p) == nil {
			t.Errorf("%s (%s) does not reload, or leaves the view", c.keys, c.what)
		}
	}
	tabs := len(p.Tabs)
	feed(t, a, "x") // the table's x closes the tab; here it is the editor's, which is read only
	if len(p.Tabs) != tabs || jsonViewOf(p) == nil || a.toast != "只读" {
		t.Errorf("x: %d tabs, view %v, toast %q", len(p.Tabs), jsonViewOf(p) != nil, a.toast)
	}
	if a.mode() != 0 || a.vim() != jsonViewOf(p).ed {
		t.Errorf("the mode and the editor that has the keys: %v", a.mode())
	}
	if jv := jsonViewOf(p); jv.ed.Mode() != editor.Normal {
		t.Errorf("mode %v", jv.ed.Mode())
	}
}

// Y in the JSON view puts the whole text on the clipboard, not the line vim's Y
// would, from the keys and from the palette; with the table in sight it does
// nothing (F16.9).
func TestJSONViewYank(t *testing.T) {
	a := sized(160, 45, "nerd")
	loadOrders(t, a, 3)
	p := a.focused()
	if _, ok := clipText(a.run("json.yank", 0)); ok {
		t.Error("json.yank with the table in sight copies")
	}
	feed(t, a, "gj")
	feed(t, a, "jj") // the cursor is not on the first line
	want := expectedJSON(t, a, 3)
	cmd := keys(t, a, "Y")
	if got, ok := clipText(cmd); !ok || got != want || got != jsonViewOf(p).text {
		t.Errorf("Y copied %q, want %q", got, want)
	}
	if a.flash == nil || a.flash.pane != p.ID || a.flash.text.Mode != ui.SelLines || a.flash.text.To.Line != strings.Count(want, "\n") {
		t.Errorf("the flash: %+v", a.flash)
	}
	a.openPalette(">")
	if !slices.ContainsFunc(a.paletteItems(), func(it paletteItem) bool { return it.kind == itemCommand && it.id == "json.yank" }) {
		t.Error("the palette has no json.yank")
	}
	feed(t, a, "<Esc>")
	// in VISUAL the Y is vim's: the lines selected, not the whole text
	feed(t, a, "ggVj")
	a.flash = nil
	if got, ok := clipText(keys(t, a, "Y")); ok {
		t.Errorf("VISUAL Y copied %q", got)
	}
	if f := a.flash; f == nil || f.text.Mode != ui.SelLines || f.text.From.Line != 0 || f.text.To.Line != 1 || jsonViewOf(p).ed.Mode() != editor.Normal {
		t.Errorf("VISUAL Y: flash %+v", a.flash)
	}
	if feed(t, a, "ggVs"); a.jump == nil { // and the s is the jump's
		t.Error("s in VISUAL does not jump")
	}
}

func TestGoldenJSONView160x45(t *testing.T) {
	a := wide(160, 45)
	loadOrders(t, a, 3)
	feed(t, a, "gj")
	golden.RequireEqual(t, a.render().String())
}

func jvText(jv *jsonView) string { return strings.Join(jv.ed.Lines(), "\n") }

// The text is what the changes make of the page, each time: a second edit of
// the same cell, a row added and a cell of it, a row marked for deletion
// (F14.5).
func TestJSONViewFollowsChanges(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	p := a.focused()
	feed(t, a, "gj")
	a.render()
	k := editKey{tab.rowKey(0), "note"}
	tab.edits = map[editKey]edit{}
	for _, s := range []string{"first", "second"} {
		tab.edits[k] = edit{val: db.Val{S: s}, orig: tab.page.Rows[0][noteField(tab)]}
		a.render()
		if !strings.Contains(jvText(jsonViewOf(p)), `"`+s+`"`) {
			t.Errorf("an edit of the same cell, %s, does not show", s)
		}
	}
	tab.added = []*newRow{{cells: map[string]edit{"status": {val: db.Val{S: "added-1"}}}}}
	a.render()
	if !strings.Contains(jvText(jsonViewOf(p)), "added-1") {
		t.Error("a row added shows")
	}
	tab.added[0].cells["status"] = edit{val: db.Val{S: "added-2"}}
	a.render()
	if got := jvText(jsonViewOf(p)); !strings.Contains(got, "added-2") || strings.Contains(got, "added-1") {
		t.Error("a cell of a row added, changed, shows")
	}
	tab.colOrder = []string{"status", "id"} // COLS' order is the order of the keys
	a.render()
	if got := jvText(jsonViewOf(p)); strings.Index(got, `"status"`) > strings.Index(got, `"id"`) {
		t.Error("the columns' order shows")
	}
	tab.deleted = map[string]bool{tab.rowKey(0): true}
	a.render()
	if strings.Count(jvText(jsonViewOf(p)), `"id"`) != 3 { // one deleted, one added
		t.Errorf("a row marked for deletion: %d records", strings.Count(jvText(jsonViewOf(p)), `"id"`))
	}
}

// The same rows again, from a refresh, leave what the editor was doing
// alone: a / line being typed, VISUAL; a change in them does not (F14.5).
func TestJSONViewRefreshKeepsEditorState(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	p := a.focused()
	feed(t, a, "gj")
	a.render()
	jv := jsonViewOf(p)
	again := func(n int) {
		_, cols, page := ordersTable(n)
		tab.seq++
		a.Update(pageMsg{tab: tab, seq: tab.seq, cols: cols, page: page, next: true})
		a.render()
	}
	feed(t, a, "/stat")
	again(3)
	if _, txt, _, ok := jv.ed.CmdLine(); !ok || txt != "stat" {
		t.Errorf("the same rows closed the / line: %q %v", txt, ok)
	}
	feed(t, a, "<Esc>ggvjj")
	again(3)
	if jv.ed.Mode() != editor.Visual {
		t.Errorf("the same rows ended VISUAL: %v", jv.ed.Mode())
	}
	again(2)
	if jv.ed.Mode() == editor.Visual || strings.Count(jvText(jv), `"id"`) != 2 {
		t.Errorf("other rows are loaded: mode %v", jv.ed.Mode())
	}
	if len(jv.ed.Lines()) != strings.Count(jv.text, "\n")+1 {
		t.Error("the text kept is the editor's")
	}
}

// A paste in the JSON view says it is read only; it and ↵, the palette's
// grid.edit and a click on the pane's ↵ edit hint open no cell editor on the
// table that is not in sight (F14.5).
func TestJSONViewEditsNothing(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	p := a.focused()
	tab.col = noteField(tab)
	feed(t, a, "gj")
	a.render()
	a.Update(tea.PasteMsg{Content: "PASTED"})
	if tab.cell != nil || a.toast != "只读" {
		t.Errorf("a paste: cell %v, toast %q", tab.cell != nil, a.toast)
	}
	feed(t, a, "<CR>")
	a.run("grid.edit", 0)
	if tab.cell != nil || len(tab.edits) != 0 || strings.Contains(jvText(jsonViewOf(p)), "PASTED") {
		t.Errorf("↵ and grid.edit: cell %v, edits %d", tab.cell != nil, len(tab.edits))
	}
	for _, h := range a.hits {
		if h.Target.Action == "grid.edit" {
			t.Errorf("the ↵ edit hint is still on the pane: %+v", h.Target)
		}
	}
	if view := a.render().String(); !strings.Contains(view, "]/[ 翻页 · esc 回表格") || strings.Contains(view, "↵ edit") {
		t.Errorf("the hint row of the view:\n%s", view)
	}
}

// A click on the JSON text focuses its pane and puts the cursor there,
// dragging selects; the wheel scrolls it (F14.5).
func TestJSONViewClickAndWheel(t *testing.T) {
	a := sized(160, 45, "nerd")
	loadOrders(t, a, 30)
	p := a.focused()
	feed(t, a, "gj")
	a.render()
	area := a.jsonArea(p)
	jv := jsonViewOf(p)
	a.win().focus(a.win().Tree.ID)
	click(a, uv.Pos(area.Min.X+12, area.Min.Y+5))
	if a.win().Focus != p.ID || jv.ed.Cursor().Line == 0 {
		t.Errorf("click: focus %d (want %d), cursor %v", a.win().Focus, p.ID, jv.ed.Cursor())
	}
	a.Update(tea.MouseClickMsg{X: area.Min.X + 12, Y: area.Min.Y + 5, Button: tea.MouseLeft})
	a.Update(tea.MouseMotionMsg{X: area.Min.X + 20, Y: area.Min.Y + 8})
	if jv.ed.Mode() != editor.Visual {
		t.Errorf("a drag selects: mode %v", jv.ed.Mode())
	}
	a.Update(tea.MouseReleaseMsg{X: area.Min.X + 20, Y: area.Min.Y + 8, Button: tea.MouseLeft})
	jv.ed.Feed("<Esc>")
	if jv.ed.Top() != 0 {
		t.Fatalf("top %d", jv.ed.Top())
	}
	a.Update(tea.MouseWheelMsg{X: area.Min.X + 5, Y: area.Min.Y + 5, Button: tea.MouseWheelDown})
	if jv.ed.Top() == 0 {
		t.Error("the wheel scrolls the text")
	}
}

// A WHERE typed with the JSON view open is the WHERE's: its mode, its keys
// (f<Space> is no leader there), not the view's editor's (F14.5).
func TestJSONViewUnderTheWhere(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	p := a.focused()
	feed(t, a, "gj")
	click(a, find(t, a, ui.Target{Kind: ui.KindHint, Pane: p.ID, Action: "grid.where"}).Min)
	feed(t, a, "id = 1 and x")
	if a.mode() != keymap.Insert || a.vim() != tab.ed {
		t.Errorf("typing: mode %v, the WHERE's editor %v", a.mode(), a.vim() == tab.ed)
	}
	feed(t, a, "<Esc>0f<Space>")
	if tab.ed.Cursor().Col != 2 {
		t.Errorf("f<Space> in the WHERE's NORMAL: %v", tab.ed.Cursor())
	}
}

// ] [ R, gj, esc and q work without modes too: the editor is read only, no key
// of it is text; esc takes a selection off first; :q leaves (F14.5).
func TestJSONViewPlain(t *testing.T) {
	a := sizedWith(160, 45, userConfig(t, "vim = false", ""))
	tab := loadOrders(t, a, 3)
	p := a.focused()
	feed(t, a, "gj")
	if jsonViewOf(p) == nil {
		t.Fatal("gj opens it")
	}
	seq := tab.seq
	feed(t, a, "]")
	if tab.seq == seq || a.toast == "只读" {
		t.Errorf("] pages: seq %d -> %d, toast %q", seq, tab.seq, a.toast)
	}
	seq = tab.seq
	feed(t, a, "R")
	if tab.seq == seq {
		t.Error("R refreshes")
	}
	jv := jsonViewOf(p)
	feed(t, a, "<S-Down><S-Right>")
	if !jv.ed.HasSelection() {
		t.Fatal("shift + arrows select")
	}
	feed(t, a, "<Esc>")
	if jsonViewOf(p) == nil || jv.ed.HasSelection() {
		t.Errorf("esc takes the selection off first: view %v, selection %v", jsonViewOf(p) != nil, jv.ed.HasSelection())
	}
	feed(t, a, "x")
	if a.toast != "只读" {
		t.Errorf("typing is read only: %q", a.toast)
	}
	feed(t, a, "<Esc>")
	if jsonViewOf(p) != nil {
		t.Error("esc at rest leaves")
	}
	feed(t, a, "gjq")
	if jsonViewOf(p) != nil {
		t.Error("q leaves")
	}
}

// The view survives the result's paging and its rerun, and the rerun's new tab
// is turned or in JSON as the one it takes the place of was (F14.5).
func TestJSONViewOfResultPagesAndReruns(t *testing.T) {
	const q = "select * from t order by id"
	d := &pagesDB{pages: map[string]db.Result{
		q + "\nLIMIT 4":          cut(rows(3)),
		q + "\nLIMIT 4 OFFSET 3": rows(2),
	}}
	a, _ := inRun(t, &d.execDB, "")
	a.sess.Conns[0].Main = db.NewWorker(d)
	a.maxRows = 3
	queryOf(a.focused()).ed.Load(q + ";")
	press(t, a, "<CR>")
	p := a.win().Result
	a.win().focus(p.ID)
	selectTab(p, 1)
	press(t, a, "gj")
	press(t, a, "]")
	if rt := resultOf(p); rt.offset != 3 || jsonViewOf(p) == nil || !strings.Contains(jvText(jsonViewOf(p)), `"n"`) {
		t.Errorf("a page: offset %d, view %v", rt.offset, jsonViewOf(p) != nil)
	}
	press(t, a, "R")
	if jsonViewOf(a.win().Result) == nil {
		t.Error("R: the new result tab is in JSON as the one it replaced was")
	}
	press(t, a, "gj")
	a.run("grid.transpose", 0)
	press(t, a, "R")
	if rt := resultOf(a.win().Result); rt == nil || !rt.transpose {
		t.Error("R: the new result tab is turned as the one it replaced was")
	}
}

// The result's right-click menu has the JSON view too (F14.5).
func TestJSONViewInResultMenu(t *testing.T) {
	d := &execDB{res: map[string]db.Result{"select 1": rows(3)}}
	a, _ := inRun(t, d, "select 1")
	press(t, a, "<CR>")
	res := a.win().Result
	a.win().focus(res.ID)
	a.View()
	var at uv.Rectangle
	for _, h := range a.hits {
		if h.Target.Kind == ui.KindCell && h.Target.Pane == res.ID {
			at = h.Rect
			break
		}
	}
	if at.Empty() {
		t.Fatal("no result cell")
	}
	rightClick(a, at.Min)
	if got := menuLabels(a); !slices.Contains(got, "以 JSON 查看") {
		t.Errorf("the result's cell menu: %v", got)
	}
}

// What the editor shows of itself, as the value editor's box does: JSON in
// color, VISUAL on select, the / line, a yank flashing; the cursor only while the
// pane has the focus; ] and R of both kinds of tab (F14.5).
func TestJSONViewDraws(t *testing.T) {
	a := sized(160, 45, "nerd")
	loadOrders(t, a, 3)
	p := a.focused()
	feed(t, a, "gj")
	view := a.render()
	if view.Cursor == nil {
		t.Error("the focused view has the cursor")
	}
	if styleOf(t, view, `"status"`) == styleOf(t, view, "[") {
		t.Error("a key and a bracket are colored alike")
	}
	feed(t, a, "jjvjj")
	if sel := a.render(); styleOf(t, sel, `"id"`).Bg == styleOf(t, view, "{").Bg {
		t.Error("VISUAL shows")
	}
	feed(t, a, "<Esc>/stat")
	if !strings.Contains(a.render().String(), "/stat") {
		t.Error("the / line shows")
	}
	feed(t, a, "<Esc>ggyy")
	text := ui.Query{Lines: jsonViewOf(p).ed.Lines()}.TextArea(a.jsonArea(p))
	if a.flash == nil || a.flash.pane != p.ID || a.render().Buf.CellAt(text.Min.X, text.Min.Y).Style.Bg != a.theme.Yank {
		t.Errorf("a yank flashes in its pane: %+v", a.flash)
	}
	a.win().focus(a.win().Tree.ID)
	if a.render().Cursor != nil {
		t.Error("a view without the focus has no cursor")
	}
}

// ] and R in the JSON view of a table ask for the next page and for the page
// again, as the table's do (F14.5).
func TestJSONViewPagesAndRefreshes(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "gj")
	seq := tab.seq
	feed(t, a, "R")
	if tab.seq == seq || jsonViewOf(a.focused()) == nil {
		t.Errorf("R: seq %d -> %d", seq, tab.seq)
	}
	seq = tab.seq
	feed(t, a, "]")
	if tab.seq == seq || tab.pageNo != 1 {
		t.Errorf("]: seq %d -> %d, page %d", seq, tab.seq, tab.pageNo)
	}
	feed(t, a, "[")
	if tab.pageNo != 0 {
		t.Errorf("[: page %d", tab.pageNo)
	}
}

// The hint row of a result's JSON view has the paging keys of the result's
// scope, not the table's: with the table's rebound, they are the result's still (F15.9).
func TestJSONViewOfResultHintsItsOwnKeys(t *testing.T) {
	d := &execDB{res: map[string]db.Result{"select 1": rows(3)}}
	a, c := inQuery(t, "", "[keys.grid]\n\"]\" = \"grid.down\"\n\"[\" = \"grid.up\"\n")
	a.sess.Conns[0].Main = db.NewWorker(d)
	a.confirmDangerous = false
	c.ed.Load("select 1")
	c.schema = ""
	press(t, a, "<CR>")
	res := a.win().Result
	a.win().focus(res.ID)
	feed(t, a, "gj")
	if view := a.render().String(); !strings.Contains(view, "]/[ 翻页") {
		t.Errorf("the hints:\n%s", view)
	}
}
