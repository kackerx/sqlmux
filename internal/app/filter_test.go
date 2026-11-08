package app

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/sqlkit"
	"github.com/kackerx/sqlmux/internal/ui"
)

// A cell's condition follows the column's type: numbers and booleans bare,
// the rest strings with their quotes doubled (MySQL's backslashes too), NULL
// IS NULL, and a type with no literal or no = is none (F12.3).
func TestCellCondition(t *testing.T) {
	for _, c := range []struct {
		d     sqlkit.Dialect
		typ   string
		fixed bool
		v     db.Val
		want  string // "" for none
	}{
		{sqlkit.PG, "text", false, db.Val{S: "done"}, `"c" = 'done'`},
		{sqlkit.PG, "character varying(20)", false, db.Val{S: "O'Brien"}, `"c" = 'O''Brien'`},
		{sqlkit.PG, "text", false, db.Val{S: `a\b`}, `"c" = 'a\b'`},
		{sqlkit.MySQL, "varchar(20)", false, db.Val{S: `a\b'`}, `"c" = 'a\\b'''`},
		{sqlkit.PG, "numeric(10,2)", false, db.Val{S: "-1.50"}, `"c" = -1.50`},
		{sqlkit.PG, "double precision", false, db.Val{S: "1e+20"}, `"c" = 1e+20`},
		{sqlkit.PG, "numeric", false, db.Val{S: "NaN"}, `"c" = 'NaN'`},
		{sqlkit.PG, "numeric", false, db.Val{S: "1; drop table t"}, `"c" = '1; drop table t'`},
		{sqlkit.MySQL, "int unsigned", false, db.Val{S: "7"}, `"c" = 7`},
		{sqlkit.PG, "boolean", false, db.Val{S: "t"}, `"c" = true`},
		{sqlkit.PG, "boolean", false, db.Val{S: "f"}, `"c" = false`},
		{sqlkit.MySQL, "tinyint(1)", false, db.Val{S: "1"}, `"c" = 1`},
		{sqlkit.PG, "timestamp with time zone", false, db.Val{S: "2026-09-01 00:01:00+00"}, `"c" = '2026-09-01 00:01:00+00'`},
		{sqlkit.PG, "uuid", false, db.Val{S: "8b6e0a5e-0000-4000-8000-000000000001"}, `"c" = '8b6e0a5e-0000-4000-8000-000000000001'`},
		{sqlkit.PG, "jsonb", false, db.Val{S: `{"a": 1}`}, `"c" = '{"a": 1}'`},
		{sqlkit.PG, "order_status", false, db.Val{S: "done"}, `"c" = 'done'`},
		{sqlkit.PG, "text", false, db.Val{Null: true}, `"c" IS NULL`},
		{sqlkit.PG, "bigint", false, db.Val{Null: true}, `"c" IS NULL`},
		{sqlkit.PG, "bytea", false, db.Val{S: `\x00`}, ""},
		{sqlkit.PG, "real", false, db.Val{S: "0.1"}, ""},
		{sqlkit.MySQL, "float unsigned", false, db.Val{S: "0.1"}, ""},
		{sqlkit.MySQL, "double", false, db.Val{S: "0.1"}, `"c" = 0.1`},
		{sqlkit.PG, "xml", false, db.Val{S: "<a/>"}, ""},
		{sqlkit.MySQL, "float unsigned zerofill", false, db.Val{S: "0.1"}, ""},
		{sqlkit.MySQL, "unsigned float", false, db.Val{S: "0.1"}, ""},
		{sqlkit.MySQL, "unsigned bigint", false, db.Val{S: "7"}, `"c" = 7`},
		{sqlkit.MySQL, "geomcollection", false, db.Val{S: "\x00\x00\x00\x00\x01\x07"}, ""}, // valid UTF-8: only its type keeps it out
		{sqlkit.MySQL, "geomcollection", false, db.Val{S: "<binary 34 bytes>"}, ""},
		{sqlkit.MySQL, "geometry", false, db.Val{S: "<binary 34 bytes>"}, ""},
		{sqlkit.MySQL, "text", false, db.Val{S: "<binary 3 bytes>"}, ""}, // whatever the type says, bytes the grid does not have are no value to ask for
		{sqlkit.MySQL, "longblob", false, db.Val{S: "x"}, ""},
		{sqlkit.MySQL, "varbinary(16)", false, db.Val{S: "x"}, ""},
		{sqlkit.PG, "json", false, db.Val{S: "{}"}, ""},
		{sqlkit.MySQL, "json", false, db.Val{S: "{}"}, ""},
		{sqlkit.PG, "integer[]", false, db.Val{S: "{1}"}, ""},
		{sqlkit.PG, "text[]", false, db.Val{Null: true}, ""},
		{sqlkit.MySQL, "bit(8)", true, db.Val{S: "5"}, ""},
	} {
		got, ok := cellCondition(c.d, `"c"`, c.typ, c.fixed, c.v)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%v %s %+v: %q %v, want %q", c.d, c.typ, c.v, got, ok, c.want)
		}
	}
}

// * puts the cell under the cursor in the WHERE, joined with AND, from the
// first page, and the history has it; one that is there is toasted, not run;
// a NULL is IS NULL; a type with no = is toasted (F12.3).
func TestFilterCell(t *testing.T) {
	a, tab, rec := withRecorder(t)
	tab.pageNo = 2
	feed(t, a, "l") // status of the first row: running
	sql := batchSQL(t, rec, a.run("grid.filter.cell", 0))
	if tab.applied != `"status" = 'running'` || tab.pageNo != 0 || tab.where.Text != tab.applied || !strings.Contains(sql, `"status" = 'running'`) {
		t.Fatalf("*: applied %q page %d, %s", tab.applied, tab.pageNo, sql)
	}
	answer(a, tab)
	feed(t, a, "l")
	sql = batchSQL(t, rec, a.run("grid.filter.cell", 0)) // amount 1.99
	if want := `"status" = 'running' AND "amount" = 1.99`; tab.applied != want || !strings.Contains(sql, want) {
		t.Fatalf("a second *: applied %q, %s", tab.applied, sql)
	}
	if h := a.tableState(tab).History; len(h) == 0 || h[0].Where != tab.applied {
		t.Errorf("the history: %+v", h)
	}
	answer(a, tab)
	n := len(rec.sqls)
	if cmd := a.run("grid.filter.cell", 0); cmd != nil && a.toast == "" || len(rec.sqls) != n || a.toast != "条件已在 WHERE 里" {
		t.Errorf("the same again: toast %q, sent %d", a.toast, len(rec.sqls)-n)
	}

	feed(t, a, "2l") // meta, jsonb: a string
	if sql = batchSQL(t, rec, a.run("grid.filter.cell", 0)); !strings.Contains(sql, `AND "meta" = '{"n": 1, "tags": ["a", "b"]}'`) {
		t.Errorf("jsonb: %s", sql)
	}
	answer(a, tab)

	a, tab, rec = withRecorder(t)
	feed(t, a, "5l2j") // note of the third row: NULL
	if sql = batchSQL(t, rec, a.run("grid.filter.cell", 0)); tab.applied != `"note" IS NULL` || !strings.Contains(sql, `"note" IS NULL`) {
		t.Errorf("NULL: %q, %s", tab.applied, sql)
	}
	feed(t, a, "o") // a row added has nothing in the database
	before := tab.applied
	if cmd := a.run("grid.filter.cell", 0); cmd != nil || tab.applied != before {
		t.Errorf("a row added: applied %q", tab.applied)
	}

	a, tab, rec = withRecorder(t)
	tab.cols.Cols[tab.fieldAt(0)].Type = "bytea"
	if a.run("grid.filter.cell", 0); a.toast != "这一列的类型不支持按值过滤" || tab.applied != "" || len(rec.sqls) != 0 {
		t.Errorf("bytea: toast %q applied %q", a.toast, tab.applied)
	}
}

// gs takes the cursor's column alone, then the other way, then back to the
// default; gS puts columns in the sorting, as the dropdown's space does; the
// row identity alone is where the chip starts: its first turn is the other
// way (F12.3).
func TestOrderHeadKeys(t *testing.T) {
	a, tab, rec := withRecorder(t)
	feed(t, a, "l")
	for i, want := range []struct {
		order []db.OrderCol
		sql   string
	}{
		{orderOf("status"), `order by "status" asc, "id" limit`},
		{orderOf("status-"), `order by "status" desc, "id" limit`},
		{nil, `order by "id" limit`},
	} {
		tab.pageNo = 2 // the rows are others now: from the first page
		sql := lastSQL(t, rec, a.run("grid.order.cycle", 0))
		if len(tab.order) != len(want.order) || len(want.order) > 0 && tab.order[0] != want.order[0] || !strings.Contains(sql, want.sql) || tab.pageNo != 0 {
			t.Fatalf("gs %d: %v, %s", i+1, tab.order, sql)
		}
		answer(a, tab)
	}
	feed(t, a, "h") // id, which the default sorts by
	for i, want := range []string{`order by "id" desc limit`, `order by "id" limit`} {
		if sql := lastSQL(t, rec, a.run("grid.order.cycle", 0)); !strings.Contains(sql, want) {
			t.Fatalf("gs on id, %d: %s", i+1, sql)
		}
		answer(a, tab)
	}
	feed(t, a, "l")
	a.run("grid.order.cycle", 0)
	answer(a, tab)
	feed(t, a, "l")
	if sql := lastSQL(t, rec, a.run("grid.order.add", 0)); !strings.Contains(sql, `order by "status" asc, "amount" asc, "id" limit`) {
		t.Errorf("gS: %s", sql)
	}
	// the keys, not the actions
	answer(a, tab)
	feed(t, a, "gs")
	if len(tab.order) != 1 || tab.order[0].Col != "amount" {
		t.Errorf("gs: %v", tab.order)
	}
	feed(t, a, "gS")
	if len(tab.order) != 1 || !tab.order[0].Desc {
		t.Errorf("gS on a column in it: turned: %v", tab.order)
	}
	feed(t, a, "gS")
	if len(tab.order) != 1 || tab.order[0].Desc {
		t.Errorf("gS again: turned back, not out: %v", tab.order)
	}
}

// A click on a head is gs; Shift-click is gS; the column clicked is the
// cursor's; a turned table's heads are record numbers and no targets (F12.3).
func TestOrderHeadClick(t *testing.T) {
	a, tab, rec := withRecorder(t)
	head := func(i int) uv.Position {
		return find(t, a, ui.Target{Kind: ui.KindHead, Pane: 1, I: i}).Min.Add(uv.Pos(1, 0))
	}
	p := head(2) // amount
	if sql := lastSQL(t, rec, click(a, p)); tab.col != 2 || !strings.Contains(sql, `order by "amount" asc, "id" limit`) {
		t.Fatalf("click: col %d, %s", tab.col, sql)
	}
	answer(a, tab)
	if sql := lastSQL(t, rec, click(a, p)); !strings.Contains(sql, `order by "amount" desc, "id" limit`) {
		t.Errorf("click again: %s", sql)
	}
	answer(a, tab)
	p = head(1)
	a.View()
	_, cmd := a.Update(tea.MouseClickMsg{X: p.X, Y: p.Y, Button: tea.MouseLeft, Mod: tea.ModShift})
	a.Update(tea.MouseReleaseMsg{X: p.X, Y: p.Y, Button: tea.MouseLeft})
	if sql := lastSQL(t, rec, cmd); !strings.Contains(sql, `order by "amount" desc, "status" asc, "id" limit`) {
		t.Errorf("Shift-click: %s", sql)
	}
	answer(a, tab)
	feed(t, a, "T")
	a.View()
	for _, h := range a.hits {
		if h.Target.Kind == ui.KindHead {
			t.Fatal("a head of the turned table is a target")
		}
	}
}

// A column in the sorting has its way after its name, numbered once there are
// two; the default sorting by the row identity shows none, and a click back to
// it takes the mark away (F12.7).
func TestHeadSortMarks(t *testing.T) {
	a, tab, rec := withRecorder(t)
	head := func(i int) uv.Position {
		return find(t, a, ui.Target{Kind: ui.KindHead, Pane: 1, I: i}).Min.Add(uv.Pos(1, 0))
	}
	asc, desc := a.icons.SortAsc.Text, a.icons.SortDesc.Text
	headRow := func() string {
		p := head(0)
		return strings.Split(a.render().String(), "\n")[p.Y]
	}
	if h := headRow(); strings.ContainsAny(h, asc+desc) {
		t.Fatalf("the default sorting: %q", h)
	}
	lastSQL(t, rec, click(a, head(2)))
	answer(a, tab)
	if h := headRow(); !strings.Contains(h, "amount "+asc) {
		t.Fatalf("one column: %q", h)
	}
	a.run("grid.order.cycle", 0)
	answer(a, tab)
	a.Update(tea.MouseClickMsg{X: head(1).X, Y: head(1).Y, Button: tea.MouseLeft, Mod: tea.ModShift})
	a.Update(tea.MouseReleaseMsg{X: head(1).X, Y: head(1).Y, Button: tea.MouseLeft})
	answer(a, tab)
	h := headRow()
	if !strings.Contains(h, "amount 1"+desc) || !strings.Contains(h, "status 2"+asc) {
		t.Fatalf("two columns: %q", h)
	}
	golden.RequireEqual(t, a.render().String())
	tab.order = nil
	a.fetch(tab, false)
	answer(a, tab)
	if h := headRow(); strings.ContainsAny(h, asc+desc) {
		t.Errorf("back to the default: %q", h)
	}
}

// A head cut at the pane's edge is brought into sight by a click on it and by
// a right-click, as a move to its cell is (F12.3).
func TestHeadClipped(t *testing.T) {
	for _, how := range []string{"click", "right"} {
		a, tab, _ := withRecorder(t)
		a.Update(tea.WindowSizeMsg{Width: 80, Height: 45}) // the last head shows two columns of itself
		a.View()
		heads := 0
		for _, h := range a.hits {
			if h.Target.Kind == ui.KindHead {
				heads++
			}
		}
		p := find(t, a, ui.Target{Kind: ui.KindHead, Pane: 1, I: heads - 1}).Min.Add(uv.Pos(1, 0))
		if how == "click" {
			click(a, p)
		} else {
			rightClick(a, p)
		}
		if tab.left == 0 || tab.col != heads-1 {
			t.Errorf("%s on the cut head: left %d, col %d", how, tab.left, tab.col)
		}
	}
}

// batchSQL is lastSQL for a cmd that is a batch with the page's fetch in it.
func batchSQL(t *testing.T, rec *recDB, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		t.Fatal("no fetch")
	}
	msgs := []tea.Msg{cmd()}
	if b, ok := msgs[0].(tea.BatchMsg); ok {
		msgs = nil
		for _, c := range b {
			msgs = append(msgs, c())
		}
	}
	for _, m := range msgs {
		if _, ok := m.(pageMsg); ok {
			return rec.sqls[len(rec.sqls)-1]
		}
	}
	t.Fatal("not a page fetch")
	return ""
}

func rightClick(a *App, p uv.Position) {
	a.View()
	a.Update(tea.MouseClickMsg{X: p.X, Y: p.Y, Button: tea.MouseRight})
}

// The grid's right-click menu: on a cell, the cursor goes there and the
// items are the cell's and its column's; on a head, the column's; each item
// does what its action does (F12.3).
func TestGridMenu(t *testing.T) {
	a, tab, rec := withRecorder(t)
	cell := find(t, a, ui.Target{Kind: ui.KindCell, Pane: 1, Action: "grid.goto 2 1"}).Min.Add(uv.Pos(1, 0))
	rightClick(a, cell)
	if tab.row != 2 || tab.col != 1 {
		t.Errorf("the cursor: %d, %d", tab.row, tab.col)
	}
	want := []string{"在值编辑器中打开", "按这个值过滤", "按这一列排序 ↑", "按这一列排序 ↓", "加入排序", "隐藏这一列", "复制这一格", "复制行为 JSON", "以 JSON 查看", "导出…", "属性"}
	if got := menuLabels(a); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("the cell's menu: %v", got)
	}
	golden.RequireEqual(t, a.render().String())
	nerd := a.icons
	a.icons = ui.ASCIIIcons // no icons in the menu
	t.Run("ascii", func(t *testing.T) { golden.RequireEqual(t, a.render().String()) })
	a.icons = nerd
	// the last item, the ninth, is drawn and takes the selection with 8 j
	feed(t, a, "jjjjjjjjj")
	if row := strings.Split(a.render().String(), "\n"); !slices.ContainsFunc(row, func(l string) bool { return strings.Contains(l, "导出…") }) || a.menu.sel != 9 {
		t.Errorf("the last item is not drawn: sel %d", a.menu.sel)
	}
	feed(t, a, "<Esc>")
	// a window with room for five rows: the list scrolls with the selection, the ninth is seen with its highlight
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 12})
	rightClick(a, find(t, a, ui.Target{Kind: ui.KindCell, Pane: 1, Action: "grid.goto 0 1"}).Min.Add(uv.Pos(1, 0)))
	feed(t, a, "jjjjjjjjj")
	f := a.render()
	hit := -1
	for y, l := range strings.Split(f.String(), "\n") {
		if strings.Contains(l, "导出…") {
			hit = y
		}
	}
	if hit < 0 || a.menu.sel != 9 || strings.Contains(f.String(), "在值编辑器中打开") {
		t.Errorf("short window: ninth item at row %d, sel %d", hit, a.menu.sel)
	} else if x := strings.Index(strings.Split(f.String(), "\n")[hit], "导出…"); f.Buf.CellAt(ui.Width(strings.Split(f.String(), "\n")[hit][:x])-1, hit).Style.Bg != a.theme.Select {
		t.Error("the ninth item is not highlighted")
	}
	feed(t, a, "<Esc>")
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	rightClick(a, cell)

	tab.pageNo = 2
	feed(t, a, "jjj<CR>") // 按这一列排序 ↓
	if len(tab.order) != 1 || tab.order[0].Col != "status" || !tab.order[0].Desc || a.menu != nil || tab.pageNo != 0 {
		t.Errorf("↓: %v, page %d", tab.order, tab.pageNo)
	}
	answer(a, tab)
	rightClick(a, cell)
	feed(t, a, "j<CR>")                       // 按这个值过滤
	if tab.applied != `"status" = 'failed'` { // the third record's
		t.Errorf("filter: %q", tab.applied)
	}
	answer(a, tab)
	rightClick(a, cell)
	feed(t, a, "jjjjj<CR>") // 隐藏这一列
	if !tab.hidden["status"] || tab.col != 1 || a.fieldNameAtCursor(tab) != "amount" {
		t.Errorf("hide: hidden %v col %d", tab.hidden, tab.col)
	}

	feed(t, a, "$") // the last column hidden: the cursor is on the one before it
	last := len(tab.shownCols()) - 1
	a.columnItems(tab, true)[5].run()
	if len(tab.shownCols()) != last || tab.col != last-1 {
		t.Errorf("the last hidden: %d shown, col %d", len(tab.shownCols()), tab.col)
	}

	feed(t, a, "0")
	head := find(t, a, ui.Target{Kind: ui.KindHead, Pane: 1, I: 2}).Min.Add(uv.Pos(1, 0))
	rightClick(a, head)
	if got := menuLabels(a); strings.Join(got, "|") != "按这一列排序 ↑|按这一列排序 ↓|加入排序|隐藏这一列" || tab.col != 2 {
		t.Errorf("the head's menu: %v, col %d", got, tab.col)
	}
	tab.pageNo = 2
	feed(t, a, "jj<CR>") // 加入排序
	if len(tab.order) != 2 || tab.pageNo != 0 {
		t.Errorf("add: %v, page %d", tab.order, tab.pageNo)
	}
	_ = rec
}

func (a *App) fieldNameAtCursor(t *dataTab) string { return t.page.Cols[t.fieldAt(t.col)].Name }

// A result's cell has the value editor and the copy; its head, no menu at
// all: a result has no ORDER to put it in nor columns to hide (F12.3).
func TestGridMenuResult(t *testing.T) {
	r := db.Result{Cols: []db.Col{{Name: "doc", Type: "jsonb"}}, Rows: [][]db.Val{{{S: `{"k": 1}`}}, {{S: `{"k": 2}`}}}}
	a, _ := inRun(t, &execDB{res: map[string]db.Result{"select 1": r}}, "select 1;")
	press(t, a, "<CR>")
	id := a.win().Result.ID
	cell := find(t, a, ui.Target{Kind: ui.KindCell, Pane: id, Action: "grid.goto 1 0"}).Min.Add(uv.Pos(1, 0))
	rightClick(a, cell) // the second row's: the cursor goes there, not the menu of the first
	if got := menuLabels(a); strings.Join(got, "|") != "在值编辑器中打开|复制这一格|以 JSON 查看|导出…" || a.win().Result.tab().Result.row != 1 {
		t.Fatalf("the result cell's menu: %v, row %d", got, a.win().Result.tab().Result.row)
	}
	feed(t, a, "<CR>")
	if a.value == nil || !a.value.readOnly || !strings.Contains(a.value.text(), `"k": 2`) {
		t.Errorf("the value editor: %v", a.value)
	}
	a.value = nil
	a.View()
	for _, h := range a.hits {
		if h.Target.Kind == ui.KindHead {
			t.Fatal("a result's head is a target")
		}
	}
}

// O opens the value editor as e does; fv filters by the cell as * does, fe
// exports as ge does; fc hides the column under the cursor as COLS does, the
// cursor on the one next to it (F12.11).
func TestGridFGroup(t *testing.T) {
	a, tab, _ := withRecorder(t)
	feed(t, a, "llO") // amount
	if a.value == nil {
		t.Fatal("O: no value editor")
	}
	feed(t, a, ":q!<CR>")
	feed(t, a, "fv")
	if !strings.Contains(tab.where.Text, "amount") {
		t.Errorf("fv: WHERE %q", tab.where.Text)
	}
	feed(t, a, "fe")
	if a.drop == nil || a.drop.kind != dropExport {
		t.Fatal("fe: no export list")
	}
	feed(t, a, "<Esc>")
	before := len(tab.shownCols())
	feed(t, a, "fc")
	if got := len(tab.shownCols()); got != before-1 || !tab.hidden["amount"] || tab.page.Cols[tab.fieldAt(tab.col)].Name != "paid" {
		t.Errorf("fc: %d shown, hidden %v, cursor on %q", got, tab.hidden, tab.page.Cols[tab.fieldAt(tab.col)].Name)
	}
	feed(t, a, "T")
	feed(t, a, "fc") // turned: the field under the cursor
	if !tab.hidden["paid"] {
		t.Errorf("fc turned: hidden %v", tab.hidden)
	}
	feed(t, a, "T$fc") // the last column: the cursor goes to the one before it
	last := tab.page.Cols[len(tab.page.Cols)-1].Name
	if !tab.hidden[last] || tab.col != len(tab.shownCols())-1 || tab.page.Cols[tab.fieldAt(tab.col)].Name == last {
		t.Errorf("fc on the last column: hidden %v, cursor %d of %d", tab.hidden, tab.col, len(tab.shownCols()))
	}
}
