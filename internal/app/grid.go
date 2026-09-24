package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/db"
	"sqlmux/internal/db/postgres"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// Tab is one tab of a pane (§5); a data pane's carries its table.
type Tab struct {
	Name string
	Data *dataTab
}

// dataTab is a table open in a data pane (§5, §7.6, §7.8「查询条」).
// Positions are the data's, transposed or not, among the columns COLS shows.
type dataTab struct {
	table     db.Table
	cols      db.Columns // the catalog's, as fetched: PK and types for the grid
	page      db.Result  // the rows on screen
	next      bool       // a page follows (§8.5)
	row, col  int        // the cursor
	top, left int        // the first record and field shown
	transpose bool
	seq       int    // the last request's; older answers are dropped (§8.3)
	err       string // the last request's error, drawn instead of the table

	// The query bar's.
	where    ui.Input        // the WHERE input
	applied  string          // the WHERE in effect: the input shows it unless typing
	order    string          // the column sorted by; "" for the row identity
	desc     bool            // ORDER's direction
	limit    int             // rows a page: 100, 500 or 1000 (§8.5)
	pageNo   int             // from 0
	hidden   map[string]bool // columns COLS hides
	pageIn   ui.Input        // PAGE's page number, while typed
	typing   string          // the input that has the keys: "where", "page" or ""
	count    int64           // the rows the WHERE keeps, as far as counted says
	counted  countState
	countSeq int // the last count's; older ones are dropped
}

type countState int

const (
	counting  countState = iota // …
	countDone                   // n
	estimated                   // ~n: no WHERE and a big table (§8.5)
	countLost                   // ?: timed out or failed
)

// countTimeout bounds a count (§8.3).
var countTimeout = 3 * time.Second

// bigTable is past where a table without WHERE shows its estimate (§8.5).
const bigTable = 1_000_000

func newDataTab(t db.Table) *dataTab {
	return &dataTab{table: t, limit: limits[0], hidden: map[string]bool{}}
}

type tableID struct{ schema, name string }

func idOf(t db.Table) tableID { return tableID{t.Schema, t.Name} }

// pageMsg answers fetch.
type pageMsg struct {
	tab  *dataTab
	seq  int
	cols db.Columns // the catalog's, to cache
	page db.Result
	next bool
	err  error
}

// countMsg answers a count.
type countMsg struct {
	tab *dataTab
	seq int
	n   int64
	err error
}

// query is what t asks of its table.
func (t *dataTab) query() postgres.Query {
	return postgres.Query{
		Schema: t.table.Schema, Table: t.table.Name, Where: t.applied,
		Order: t.order, Desc: t.desc, Limit: t.limit, Offset: t.pageNo * t.limit,
	}
}

// fetch reads t's page on Meta, after its columns when the catalog hasn't
// got them yet: they give the row identity it orders by (§8.4, §10.1).
// recount counts the rows again once the page is in (§8.3), unless an
// estimate stands for them (§8.5).
func (a *App) fetch(t *dataTab, recount bool) tea.Cmd {
	t.seq++
	a.busy++
	seq, table, meta, q := t.seq, t.table, a.sess.Meta, t.query()
	cols, cached := a.sess.cols[idOf(table)]
	page := func() tea.Msg {
		ctx := context.Background()
		if !cached {
			var err error
			if cols, err = postgres.TableColumns(ctx, meta, table.Schema, table.Name); err != nil {
				return pageMsg{tab: t, seq: seq, err: err}
			}
		}
		q := q
		q.Key = cols.Key()
		r, next, err := postgres.Page(ctx, meta, q)
		return pageMsg{tab: t, seq: seq, cols: cols, page: r, next: next, err: err}
	}
	switch {
	case !recount:
		return page
	case strings.TrimSpace(t.applied) == "" && table.Rows > bigTable:
		t.count, t.counted = int64(table.Rows), estimated
		return page
	}
	t.countSeq++
	t.counted = counting
	cseq := t.countSeq
	count := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), countTimeout)
		defer cancel()
		n, err := postgres.Count(ctx, meta, q)
		return countMsg{tab: t, seq: cseq, n: n, err: err}
	}
	return tea.Sequence(page, count) // the timeout starts once the page is in
}

func (a *App) gotPage(m pageMsg) tea.Cmd {
	a.busy--
	if m.cols.Cols != nil {
		a.sess.cols[idOf(m.tab.table)] = m.cols
	}
	t := m.tab
	switch {
	case m.seq != t.seq: // a newer request is on its way
	case errors.Is(m.err, context.Canceled): // the old rows stay (§8.3)
		return a.showToast("查询已取消", toastTTL)
	case m.err != nil:
		t.err = m.err.Error()
	default:
		t.err, t.cols, t.page, t.next = "", m.cols, m.page, m.next
		t.row = max(min(t.row, len(t.page.Rows)-1), 0)
		t.col = max(min(t.col, len(t.shown())-1), 0)
	}
	return nil
}

func (a *App) gotCount(m countMsg) {
	if t := m.tab; m.seq == t.countSeq {
		t.count, t.counted = m.n, countDone
		if m.err != nil { // DeadlineExceeded past 3s, or the WHERE's own error
			t.counted = countLost
		}
	}
}

// pages is how many pages t's count makes, and whether that is known.
func (t *dataTab) pages() (int64, bool) {
	if t.counted != countDone && t.counted != estimated {
		return 0, false
	}
	return max((t.count+int64(t.limit)-1)/int64(t.limit), 1), true
}

// shown is the columns COLS lets through, as indexes into the page's.
func (t *dataTab) shown() []int {
	var out []int
	for i, c := range t.page.Cols {
		if !t.hidden[c.Name] {
			out = append(out, i)
		}
	}
	return out
}

// fieldAt is the page's index of shown column i, or -1.
func (t *dataTab) fieldAt(i int) int {
	if s := t.shown(); i >= 0 && i < len(s) {
		return s[i]
	}
	return -1
}

// nearestShown is the shown index of page column field, or of the first
// shown one after it, or else the last shown one.
func (t *dataTab) nearestShown(field int) int {
	s := t.shown()
	if i := slices.IndexFunc(s, func(f int) bool { return f >= field }); i >= 0 {
		return i
	}
	return max(len(s)-1, 0)
}

// typeOf is the catalog's type for column name, or "".
func (t *dataTab) typeOf(name string) string {
	if i := slices.IndexFunc(t.cols.Cols, func(c db.Column) bool { return c.Name == name }); i >= 0 {
		return t.cols.Cols[i].Type
	}
	return ""
}

// stopTyping gives the keys back to the grid, the WHERE input showing what
// is in effect again (§7.8).
func (t *dataTab) stopTyping() {
	t.typing, t.where = "", ui.Input{Text: t.applied, Pos: len(t.applied)}
}

// dataOf is pane p's current table, or nil.
func dataOf(p *Pane) *dataTab {
	if p.Cur < len(p.Tabs) {
		return p.Tabs[p.Cur].Data
	}
	return nil
}

// bodyRect is where a pane at r shows its tab's content: inside the border,
// above the tab bar.
func bodyRect(r uv.Rectangle) uv.Rectangle {
	return uv.Rect(r.Min.X+1, r.Min.Y+1, max(r.Dx()-2, 0), max(r.Dy()-3, 0))
}

// gridRect is where a data pane at r draws its table: under the query bar.
func gridRect(r uv.Rectangle) uv.Rectangle {
	b := bodyRect(r)
	b.Min.Y = min(b.Min.Y+ui.QueryBarRows, b.Max.Y)
	return b
}

// grid is t as pane p draws it: the columns COLS shows, with what the
// catalog said of them.
func (a *App) grid(p *Pane, t *dataTab) ui.Grid {
	g := ui.Grid{
		Row: t.row, Col: t.col, Top: t.top, Left: t.left, Transpose: t.transpose, First: t.pageNo * t.limit,
		Focused: a.win().Focus == p.ID, Key: a.icons.Key, Pane: p.ID,
	}
	shown := t.shown()
	for _, i := range shown {
		name := t.page.Cols[i].Name
		g.Cols = append(g.Cols, ui.GridCol{Name: name, PK: slices.Contains(t.cols.PK, name), Type: colType(t.typeOf(name))})
	}
	for _, row := range t.page.Rows {
		vals := make([]db.Val, len(shown))
		for j, i := range shown {
			vals[j] = row[i]
		}
		g.Rows = append(g.Rows, vals)
	}
	return g
}

// queryBar is the two rows over t's table (§7.8「查询条」).
func (a *App) queryBar(p *Pane, t *dataTab) ui.QueryBar {
	ic := a.icons
	order := "—" // no row identity to sort by
	switch key := t.cols.Key(); {
	case t.order != "" && t.desc:
		order = t.order + " ↓"
	case t.order != "":
		order = t.order + " ↑"
	case key != nil:
		order = strings.Join(key, ",") + " ↑"
	}
	pages := "?"
	if n, ok := t.pages(); ok {
		pages = strconv.FormatInt(n, 10)
		if t.counted == estimated {
			pages = "~" + pages
		}
	}
	page := ui.Chip{Label: "PAGE", Value: fmt.Sprintf("%d/%s", t.pageNo+1, pages), Action: "grid.page"}
	if t.typing == "page" {
		page.Input, page.Suffix = &t.pageIn, "/"+pages
	}
	rows := "…"
	switch t.counted {
	case countLost:
		rows = "?"
	case countDone:
		rows = strconv.FormatInt(t.count, 10)
	case estimated:
		rows = "~" + ui.Magnitude(float64(t.count))
	}
	right := "auto · " + rows + " 行"
	if t.page.Cols != nil {
		right += " · " + t.page.Took.Round(time.Millisecond).String()
	}
	return ui.QueryBar{
		Where: t.where, Typing: t.typing == "where", Pane: p.ID, Right: right,
		Chips: []ui.Chip{
			{Label: "ORDER", Value: order, Action: "grid.order"},
			{Label: "LIMIT", Value: strconv.Itoa(t.limit), Action: "grid.limit"},
			page,
			{Label: "COLS", Value: fmt.Sprintf("%d/%d", len(t.shown()), len(t.page.Cols)), Action: "grid.cols"},
		},
		Buttons: []ui.Button{
			{Icon: ic.Save}, // ponytail: does nothing until saving (M2)
			{Icon: ic.Refresh, Action: "grid.refresh"},
			{Icon: ic.Transpose, Action: "grid.transpose"},
		},
	}
}

// chipRect is where pane p draws the chip of t's query bar running action.
func (a *App) chipRect(p *Pane, t *dataTab, action string) uv.Rectangle {
	return a.queryBar(p, t).ChipRect(bodyRect(a.layout()[p.ID]), action)
}

// focusedGrid is the focused pane's table, if it shows one: loaded, not
// an error in its place, with columns to show.
func (a *App) focusedGrid() (*Pane, *dataTab, bool) {
	p := a.focused()
	t := dataOf(p)
	return p, t, t != nil && t.err == "" && len(t.shown()) > 0
}

// typingTab is the focused table whose query bar input has the keys.
func (a *App) typingTab() *dataTab {
	if t := dataOf(a.focused()); t != nil && t.typing != "" {
		return t
	}
	return nil
}

// gridMove moves the focused grid's cursor to where to puts it, in screen
// terms (§7.6: j is always down, whichever way the data is turned), and
// scrolls it into view.
func (a *App) gridMove(to func(r, c, rows, cols int) (int, int)) {
	p, t, ok := a.focusedGrid()
	if !ok {
		return
	}
	rows, cols := len(t.page.Rows), len(t.shown())
	r, c := t.row, t.col
	if t.transpose {
		rows, cols, r, c = cols, rows, c, r
	}
	r, c = to(r, c, rows, cols)
	r, c = max(min(r, rows-1), 0), max(min(c, cols-1), 0)
	if t.transpose {
		r, c = c, r
	}
	t.row, t.col = r, c
	t.top, t.left = a.grid(p, t).View(gridRect(a.layout()[p.ID]))
}

// gridGoto is a click on a cell or a row number: "rec field".
func (a *App) gridGoto(arg string) {
	r, c, _ := strings.Cut(arg, " ")
	rec, err1 := strconv.Atoi(r)
	field, err2 := strconv.Atoi(c)
	if _, t, ok := a.focusedGrid(); ok && err1 == nil && err2 == nil {
		t.row, t.col = rec, field
		a.gridMove(func(r, c, _, _ int) (int, int) { return r, c })
	}
}

func (a *App) gridTranspose() {
	if _, t, ok := a.focusedGrid(); ok {
		t.transpose = !t.transpose
		a.gridMove(func(r, c, _, _ int) (int, int) { return r, c })
	}
}

// scrollGrid is the wheel over pane p: dr rows and dc columns of the view,
// the cursor pulled along (§7.6).
func (a *App) scrollGrid(p *Pane, dr, dc int) {
	if t := dataOf(p); t != nil && len(t.shown()) > 0 {
		t.top, t.left, t.row, t.col = a.grid(p, t).Scroll(gridRect(a.layout()[p.ID]), dr, dc)
	}
}

// turnPage is ] and [ (§7.8): not past the last page, nor before the first.
func (a *App) turnPage(d int) tea.Cmd {
	t := dataOf(a.focused())
	if t == nil || d > 0 && !t.next || d < 0 && t.pageNo == 0 {
		return nil
	}
	t.pageNo += d
	return a.fetch(t, false)
}

// typeKey edits the query bar input that has the keys (§7.8): ↵ runs it,
// esc drops it.
func (a *App) typeKey(t *dataTab, k keymap.Key) tea.Cmd {
	switch k {
	case keymap.Esc:
		t.stopTyping()
		return nil
	case "<CR>":
		if t.typing == "where" {
			t.applied = t.where.Text
			t.stopTyping()
			t.pageNo = 0
			return a.fetch(t, true)
		}
		n, err := strconv.Atoi(strings.TrimSpace(t.pageIn.Text))
		t.stopTyping()
		if err != nil {
			return nil
		}
		if pages, ok := t.pages(); ok { // past the ends: to the end
			n = min(n, int(pages))
		}
		t.pageNo = max(n-1, 0)
		return a.fetch(t, false)
	}
	in := &t.where
	if t.typing == "page" {
		in = &t.pageIn
	}
	editInput(in, k)
	return nil
}

// colType is the grid's class for a column of the catalog's type (§7.6).
func colType(t string) ui.ColType {
	if strings.HasSuffix(t, "[]") {
		return ui.ColOther
	}
	// format_type puts one modifier at most: "numeric(10,2)", "timestamp(3) with time zone"
	if i, j := strings.Index(t, "("), strings.Index(t, ")"); 0 <= i && i < j {
		t = t[:i] + t[j+1:]
	}
	switch t {
	case "smallint", "integer", "bigint", "numeric", "real", "double precision", "oid":
		return ui.ColNumber
	case "date", "time without time zone", "time with time zone",
		"timestamp without time zone", "timestamp with time zone":
		return ui.ColTime
	case "boolean":
		return ui.ColBool
	case "json", "jsonb":
		return ui.ColJSON
	case "text", "character varying", "character", `"char"`, "name", "citext":
		return ui.ColString
	}
	if t == "interval" || strings.HasPrefix(t, "interval ") { // "interval day to second"
		return ui.ColTime
	}
	return ui.ColOther
}
