package app

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/db"
	"sqlmux/internal/db/postgres"
	"sqlmux/internal/ui"
)

// Tab is one tab of a pane (§5); a data pane's carries its table.
type Tab struct {
	Name string
	Data *dataTab
}

// dataTab is a table open in a data pane (§5, §7.6). Positions are the
// data's, transposed or not.
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
}

// pageRows is how many rows a page holds (§8.5).
// ponytail: fixed until F1.4's LIMIT chip.
const pageRows = 100

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

// fetch reads t's first page on Meta, after its columns when the catalog
// hasn't got them yet: they give the row identity it orders by (§8.4, §10.1).
func (a *App) fetch(t *dataTab) tea.Cmd {
	t.seq++
	a.busy++
	seq, table, meta := t.seq, t.table, a.sess.Meta
	cols, cached := a.sess.cols[idOf(table)]
	return func() tea.Msg {
		ctx := context.Background()
		if !cached {
			var err error
			if cols, err = postgres.TableColumns(ctx, meta, table.Schema, table.Name); err != nil {
				return pageMsg{tab: t, seq: seq, err: err}
			}
		}
		page, next, err := postgres.Page(ctx, meta, table.Schema, table.Name, cols.Key(), pageRows, 0)
		return pageMsg{tab: t, seq: seq, cols: cols, page: page, next: next, err: err}
	}
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
		t.col = max(min(t.col, len(t.page.Cols)-1), 0)
	}
	return nil
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

// grid is t as pane p draws it, its columns the page's with what the
// catalog said of them.
func (a *App) grid(p *Pane, t *dataTab) ui.Grid {
	cols := t.cols
	g := ui.Grid{
		Rows: t.page.Rows, Row: t.row, Col: t.col, Top: t.top, Left: t.left, Transpose: t.transpose,
		Focused: a.win().Focus == p.ID, Key: a.icons.Key, Pane: p.ID,
	}
	for _, c := range t.page.Cols {
		typ := ""
		if i := slices.IndexFunc(cols.Cols, func(cc db.Column) bool { return cc.Name == c.Name }); i >= 0 {
			typ = cols.Cols[i].Type
		}
		g.Cols = append(g.Cols, ui.GridCol{Name: c.Name, PK: slices.Contains(cols.PK, c.Name), Type: colType(typ)})
	}
	return g
}

// focusedGrid is the focused pane's table and its grid, if it has one loaded.
func (a *App) focusedGrid() (*Pane, *dataTab, bool) {
	p := a.focused()
	t := dataOf(p)
	return p, t, t != nil && len(t.page.Cols) > 0
}

// gridMove moves the focused grid's cursor to where to puts it, in screen
// terms (§7.6: j is always down, whichever way the data is turned), and
// scrolls it into view.
func (a *App) gridMove(to func(r, c, rows, cols int) (int, int)) {
	p, t, ok := a.focusedGrid()
	if !ok {
		return
	}
	rows, cols := len(t.page.Rows), len(t.page.Cols)
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
	t.top, t.left = a.grid(p, t).View(bodyRect(a.layout()[p.ID]))
}

// gridGoto is a click on a cell: "rec field".
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
	if t := dataOf(p); t != nil && len(t.page.Cols) > 0 {
		t.top, t.left, t.row, t.col = a.grid(p, t).Scroll(bodyRect(a.layout()[p.ID]), dr, dc)
	}
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
