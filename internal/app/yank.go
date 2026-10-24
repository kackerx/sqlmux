package app

import (
	"bytes"
	"cmp"
	"encoding/csv"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
	"github.com/kackerx/sqlmux/internal/ui"
)

// copiedRow is the row yy took, for p (§10.6): its table, and each
// column's value as it showed, changes in; none for a DEFAULT.
type copiedRow struct {
	conn  *Conn // two connections' t_order are two tables (F5.10)
	table tableID
	vals  map[string]db.Val
}

// yankFlash is a yank flashing in pane (F3.32, nvim's vim.hl.on_yank): a
// grid's record and field, -1 for all of it, or a query's text.
type yankFlash struct {
	pane int
	cell [2]int
	text ui.Sel
	nums map[int]bool // the numbers of the records of a selection yanked (F10.7)
	seq  int
}

type flashDone struct{ seq int }

// yankFlashFor is how long a yank flashes.
var yankFlashFor = 150 * time.Millisecond

// flashYank shows f till yankFlashFor is up, or the next one.
func (a *App) flashYank(f yankFlash) tea.Cmd {
	a.flashSeq++
	f.seq = a.flashSeq
	a.flash = &f
	return tea.Tick(yankFlashFor, func(time.Time) tea.Msg { return flashDone{f.seq} })
}

// change is the change to column col of row sr, of row key key, not
// saved: an edit, or a row added's cell (§10.1, §10.6).
func (t *dataTab) change(sr shownRow, key, col string) (edit, bool) {
	if sr.add != nil {
		e, ok := sr.add.cells[col]
		return e, ok
	}
	e, ok := t.edits[editKey{key, col}]
	return e, ok
}

// value is column col of row sr, of row key key, as it shows, changes in;
// not ok for a DEFAULT.
func (t *dataTab) value(sr shownRow, key, col string) (db.Val, bool) {
	e, ok := t.change(sr, key, col)
	switch {
	case ok:
		return e.val, !e.def
	case sr.add != nil:
		return db.Val{}, false
	}
	return t.page.Rows[sr.rec][slices.IndexFunc(t.page.Cols, func(c db.Col) bool { return c.Name == col })], true
}

// yankGrid is yy (row) and yl on the focused grid (§7.6, F3.32): the row
// as TSV of the columns shown, or the cell, to the clipboard, flashing; a
// table's row kept for p too, every column of it. NULL and DEFAULT are
// empty.
func (a *App) yankGrid(row bool) tea.Cmd {
	p := a.focused()
	gs, _, _, ok := a.gridOf(p)
	if !ok {
		return nil
	}
	var cells []string // the columns shown, in order
	switch tab := p.tab(); {
	case tab.Data != nil:
		t := tab.Data
		sr, key, ok := t.cursor()
		if !ok {
			return nil
		}
		vals := map[string]db.Val{}
		for _, c := range t.page.Cols {
			if v, set := t.value(sr, key, c.Name); set {
				vals[c.Name] = v
			}
		}
		for _, f := range t.shownCols() {
			cells = append(cells, vals[t.page.Cols[f].Name].S)
		}
		if row {
			a.rowCopy = &copiedRow{t.conn, idOf(t.table), vals}
		}
	case gs.row < len(tab.Result.page.Rows): // a result's
		for _, v := range tab.Result.page.Rows[gs.row] {
			cells = append(cells, v.S)
		}
	default:
		return nil
	}
	flash := yankFlash{pane: p.ID, cell: [2]int{gs.row, -1}}
	text := tsv(cells)
	if !row {
		flash.cell[1], text = gs.col, cells[gs.col]
	}
	return tea.Batch(clipCopy(text), a.flashYank(flash))
}

// yankInsert is yi on the focused table's row (§10.6, F8.7): an INSERT of
// every column the page has, but the server's own (generated), as it shows,
// changes in, to the clipboard, flashing. A DEFAULT cell is left out. A
// number is bare, anything else a string literal, NULL as it is; the
// keywords follow keyword_case, preserve being lower.
func (a *App) yankInsert() tea.Cmd {
	p := a.focused()
	gs, _, _, ok := a.gridOf(p)
	if !ok {
		return nil
	}
	tab := p.tab()
	if tab.Data == nil {
		if tab.Result != nil {
			return a.showToast(i18n.T("只有表里的行能复制为 INSERT"))
		}
		return nil
	}
	t := tab.Data
	sr, key, ok := t.cursor()
	if !ok {
		return nil
	}
	if !t.conn.sql() {
		return a.showToast(i18n.T("只有表里的行能复制为 INSERT"))
	}
	eng := t.conn.eng
	var names, vals []string
	for _, f := range t.ordered() { // as COLS has them, hidden ones too (F10.3)
		pc := t.page.Cols[f]
		col := t.column(pc.Name)
		v, set := t.value(sr, key, pc.Name)
		if set && db.IsBinaryShown(v.S) { // the bytes are not here: no statement of what they are
			return a.showToast(i18n.T("有二进制列，复制不了"))
		}
		if col.Generated || !set {
			continue
		}
		names = append(names, eng.Quote(pc.Name))
		switch {
		case v.Null:
			vals = append(vals, a.keyword("null"))
		case (col.Fixed || colType(col.Type) == ui.ColNumber) && number.MatchString(v.S) && v.S != "-0": // bare, PG reads -0 as the integer: a float loses its sign
			vals = append(vals, v.S)
		default:
			vals = append(vals, sqlkit.Literal(v.S, eng.Dialect()))
		}
	}
	text := db.DefaultRow(eng, t.table.Schema, t.table.Name, a.keyword) + ";"
	if len(names) > 0 {
		text = a.keyword("insert into") + " " + eng.Quote(t.table.Schema, t.table.Name) + " (" + strings.Join(names, ", ") + ") " + a.keyword("values") + " (" + strings.Join(vals, ", ") + ");"
	}
	return tea.Batch(clipCopy(text), a.flashYank(yankFlash{pane: p.ID, cell: [2]int{gs.row, -1}}))
}

// jsonField is a column of a row yanked as JSON: its name, the class of its
// type, its value (F9.5).
type jsonField struct {
	name  string
	class ui.ColType
	val   db.Val
}

// jsonOf encodes v, HTML left as it is: this is for people to read.
func jsonOf(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	err := enc.Encode(v)
	return bytes.TrimRight(b.Bytes(), "\n"), err
}

// jsonValue is what a value of a column of class c is in JSON: a number for a
// numeric column when its text is a JSON number, true or false for a boolean
// (PG's t and f, MySQL's 1 and 0), json and jsonb as they are when they are
// JSON, NULL as null; any other value, and one that is not what its column
// says, its text.
func jsonValue(c ui.ColType, v db.Val) any {
	switch {
	case v.Null:
		return nil
	case c == ui.ColNumber && json.Valid([]byte(v.S)): // not "NaN", nor "007"
		return json.RawMessage(v.S)
	case c == ui.ColBool && slices.Contains([]string{"t", "true", "1"}, v.S):
		return true
	case c == ui.ColBool && slices.Contains([]string{"f", "false", "0"}, v.S):
		return false
	case c == ui.ColJSON && json.Valid([]byte(v.S)):
		return json.RawMessage(v.S)
	}
	return v.S
}

// rowRaw is a row as a JSON object on one line: its keys in the order of the
// columns, which a map would not keep, so the braces and commas are written
// here and each key and value by encoding/json.
func rowRaw(fs []jsonField) ([]byte, error) {
	var raw bytes.Buffer
	raw.WriteByte('{')
	for i, f := range fs {
		if i > 0 {
			raw.WriteByte(',')
		}
		k, err := jsonOf(f.name)
		if err != nil {
			return nil, err
		}
		v, err := jsonOf(jsonValue(f.class, f.val))
		if err != nil {
			return nil, err
		}
		raw.Write(k)
		raw.WriteByte(':')
		raw.Write(v)
	}
	raw.WriteByte('}')
	return raw.Bytes(), nil
}

// indentJSON is raw in two spaces' indents (F9.5).
func indentJSON(raw []byte) (string, error) {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return "", err
	}
	return out.String(), nil
}

// jsonFields is record row of tab, a table's or a result's, as the fields of
// a JSON object: every column it has, shown ones or not, in COLS's order for
// a table, as it shows, changes in; a DEFAULT cell has none (F9.5).
func jsonFields(tab *Tab, row int) (fs []jsonField, ok bool) {
	switch {
	case tab.Data != nil:
		t := tab.Data
		sr, key, ok := t.recordAt(row)
		if !ok {
			return nil, false
		}
		for _, f := range t.ordered() {
			c := t.page.Cols[f]
			if v, set := t.value(sr, key, c.Name); set {
				fs = append(fs, jsonField{c.Name, colType(cmp.Or(t.typeOf(c.Name), c.Type)), v})
			}
		}
		return fs, true
	case tab.Result != nil && row < len(tab.Result.page.Rows):
		for i, c := range tab.Result.page.Cols {
			fs = append(fs, jsonField{c.Name, colType(c.Type), tab.Result.page.Rows[row][i]})
		}
		return fs, true
	}
	return nil, false
}

// yankJSON is Y on the focused grid (§7.5「复制为 JSON」, F9.5): the row as a
// JSON object, to the clipboard, flashing.
func (a *App) yankJSON() tea.Cmd {
	p := a.focused()
	gs, _, _, ok := a.gridOf(p)
	if !ok {
		return nil
	}
	fs, ok := jsonFields(p.tab(), gs.row)
	if !ok {
		return nil
	}
	raw, err := rowRaw(fs)
	text := ""
	if err == nil {
		text, err = indentJSON(raw)
	}
	if err != nil {
		return a.showToast(err.Error())
	}
	return tea.Batch(clipCopy(text), a.flashYank(yankFlash{pane: p.ID, cell: [2]int{gs.row, -1}}))
}

// number is what a numeric column's value may be, written bare: no NaN,
// Infinity or hex, which a literal in quotes says better.
var number = regexp.MustCompile(`^-?\d+(\.\d+)?([eE][+-]?\d+)?$`)

// keyword is a SQL keyword as keyword_case writes it (§9.5): preserve has
// nothing to preserve here, so it is lower.
func (a *App) keyword(k string) string {
	if a.keywordCase == "upper" {
		return strings.ToUpper(k)
	}
	return k
}

// tsv is one row of tab separated values, quoted where a value needs it
// as a spreadsheet reads them: a tab, a newline or a " in it.
func tsv(vals []string) string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	w.Comma = '\t'
	_ = w.Write(vals)
	w.Flush()
	return strings.TrimSuffix(b.String(), "\n")
}

// pasteRow is p (§10.6, F3.32): the row yy took, a row added under the
// cursor's with its values, but for the row identity's columns that have
// a default (serial, identity), the generated always identity columns,
// which take none (PG's 428C9), and MySQL's BIT, shown as no value it
// takes: theirs. A row of another table, or none taken, pastes nothing.
// ponytail: a generated (stored) column is copied too, and the save fails
// on it; leave those out when the catalog tells them apart
func (a *App) pasteRow() tea.Cmd {
	t, ok := a.focusedGrid()
	if !ok || a.rowCopy == nil || a.rowCopy.conn != t.conn || a.rowCopy.table != idOf(t.table) {
		return nil
	}
	if slices.ContainsFunc(slices.Collect(maps.Values(a.rowCopy.vals)), func(v db.Val) bool { return db.IsBinaryShown(v.S) }) {
		return a.showToast(i18n.T("有二进制列，复制不了")) // what yy took of them is the text shown, not the bytes
	}
	cmd := a.addRow()
	sr, _, ok := t.cursor()
	if !ok || sr.add == nil { // read only: no row added
		return cmd
	}
	sr.add.cells = map[string]edit{}
	for name, v := range a.rowCopy.vals {
		col := t.column(name)
		if def := col.Default; !col.Fixed && def != "generated always as identity" && (!slices.Contains(t.cols.Key(), name) || def == "") {
			sr.add.cells[name] = edit{val: v}
		}
	}
	return cmd
}

// yankSel is what a query's yank took, as its flash covers it.
func yankSel(y editor.Yank) ui.Sel {
	s := ui.Sel{Mode: ui.SelChars, From: ui.TextPos(y.From), To: ui.TextPos(y.To)}
	switch {
	case y.Block:
		s.Mode, s.Left, s.Right = ui.SelBlock, y.Left, y.Right
	case y.Linewise:
		s.Mode = ui.SelLines
	default:
		s.To.Col-- // To is not in: what starts before it is
	}
	return s
}

// clipTools are the system clipboard's commands, local tools first and
// OSC 52 without them as nvim's clipboard provider has it, with its
// arguments (runtime/autoload/provider/clipboard.vim, v0.12.4); the order
// is §11's, not quite nvim's, which tries xsel before xclip and wants
// wl-paste on PATH with wl-copy. The first on PATH, with its display set
// for Wayland's and X11's, is used, so a tmux dropping OSC 52 doesn't
// matter; with none the terminal is asked over OSC 52 (F3.38). nvim's
// xclip -quiet and xsel --nodetach keep the process it waits on as the
// selection's owner; without them the tools fork one off and exit.
var clipTools = clipTable()

// clipTable is clipTools of the platform: the unix tools, and the windows
// row, clip writing and PowerShell reading, after them, on windows only
// (§7.8); the windows read's \r\n is normalized in clipPaste.
func clipTable() []clipTool {
	tools := []clipTool{
		{"", []string{"pbcopy"}, []string{"pbpaste"}},
		{"WAYLAND_DISPLAY", []string{"wl-copy", "--type", "text/plain"}, []string{"wl-paste", "--no-newline"}},
		{"DISPLAY", []string{"xclip", "-i", "-selection", "clipboard"}, []string{"xclip", "-o", "-selection", "clipboard"}},
		{"DISPLAY", []string{"xsel", "-i", "-b"}, []string{"xsel", "-o", "-b"}},
	}
	if runtime.GOOS == "windows" { // both PowerShell with UTF-8 said out loud: clip reads the console code page, bare Get-Clipboard writes OEM and ends with a CRLF (§7.8)
		tools = append(tools, clipTool{
			"",
			[]string{"powershell", "-NoProfile", "-Command", "[Console]::InputEncoding=[Text.Encoding]::UTF8;Set-Clipboard -Value ([Console]::In.ReadToEnd())"},
			[]string{"powershell", "-NoProfile", "-Command", "[Console]::OutputEncoding=[Text.Encoding]::UTF8;[Console]::Out.Write((Get-Clipboard -Raw))"},
		})
	}
	return tools
}

type clipTool struct {
	env         string // what must be set for it: its display
	copy, paste []string
}

// clipCmd is the tool copying to the clipboard, or pasting from it, nil
// for OSC 52.
func clipCmd(paste bool) *exec.Cmd {
	for _, t := range clipTools {
		argv := t.copy
		if paste {
			argv = t.paste
		}
		if c := exec.Command(argv[0], argv[1:]...); c.Err == nil && (t.env == "" || os.Getenv(t.env) != "") { // Err: not on PATH
			return c
		}
	}
	return nil
}

// clipCopy puts text on the system clipboard, over OSC 52 when no tool
// is there or it fails.
func clipCopy(text string) tea.Cmd {
	return func() tea.Msg {
		if c := clipCmd(false); c != nil {
			c.Stdin = strings.NewReader(text) // no pipe out: the forked owner would hold Run up
			if c.Run() == nil {
				return nil
			}
		}
		return tea.SetClipboard(text)()
	}
}

// clipPaste asks for what the system clipboard holds: it comes as a
// ClipboardMsg, from the tool or else the terminal, or never.
func clipPaste() tea.Msg {
	if c := clipCmd(true); c != nil {
		if out, err := c.Output(); err == nil {
			if runtime.GOOS == "windows" { // the windows read's lines end in \r\n
				out = bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n"))
			}
			return tea.ClipboardMsg{Content: string(out), Selection: 'c'}
		}
	}
	return tea.ReadClipboard()
}
