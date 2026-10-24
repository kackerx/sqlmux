package app

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// queryTab is a query open in a pane (§5, §11): its editor and the
// file that keeps its text.
type queryTab struct {
	conn *Conn // where it runs, on its Main (§5)
	// want is the connection it goes to once connected, picked or kept
	// for its file, with wantSchema, "" for that one's Schema (§7.8
	// 「query 标题」); nil for none.
	want       *Conn
	wantSchema string
	ed         *editor.Editor
	name       string // the file's name, query_1: the state keeps where it runs by it
	path       string
	ver, saved int // changes made, and the one the file has
	// disk is the file as this tab last read or wrote it; another process or
	// editor having changed it since, a write is refused once (F13.2). armed
	// is the file as it was when that was said, by an autosave or a save: a
	// save of it is the user's yes, one of a file changed again is told again;
	// told: an autosave said it, not to say it again.
	disk, armed fileStamp
	told        bool
	failed      int       // the first line of the statement whose last run failed, its ▶ red; -1 for none
	running     *run      // the run going on, if one is: another ↵ waits for it (§11)
	bar         *errorBar // the last run's error, under the text (§7.8「错误栏」)
	// schema is the one it runs in, first on search_path (§8.6): its
	// connection's Schema as it opens, "" until the catalog is in.
	schema string
	// The candidate list in INSERT (§9.7); the tables whose columns were
	// fetched for it this INSERT, and where it last completed: columns in
	// while the cursor is still there complete again.
	comp    *completion
	asked   map[tableID]bool
	wait    *compWait
	hintSeq int // the last key or field completion asked for: older ones are dropped (§9.8)
}

// compWait is where a query last completed, C-n's or not.
type compWait struct {
	at     editor.Pos
	ver    int
	manual bool
}

// openQuery is the query called name on connection c, in its Schema,
// with what its file keeps (§11).
func openQuery(c *Conn, name string) (*queryTab, error) {
	path := config.QueryPath(name)
	disk := stampOf(path) // before the read: a file changed between is then not taken for the one read
	text, err := config.ReadQuery(path)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("读取失败：%w"), err)
	}
	return &queryTab{conn: c, schema: c.Schema, ed: editor.New(text), name: name, path: path, disk: disk, failed: -1}, nil
}

// fileStamp is what tells a file changed under us: its time and size; zero
// when there is no file.
type fileStamp struct {
	mod  time.Time
	size int64
}

func stampOf(path string) fileStamp {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{fi.ModTime(), fi.Size()}
}

// changedOutside is a write refused: the file is not what this tab read or
// wrote, as vim's W11 says (F13.2).
func changedOutside() error {
	return errors.New(i18n.T("文件在别处改过，没有保存；再保存一次会覆盖它"))
}

func queryOf(p *Pane) *queryTab {
	if p.Cur < len(p.Tabs) {
		return p.Tabs[p.Cur].Query
	}
	return nil
}

// focusedQuery is the query that has the keys: the focused pane's
// current tab, with no overlay over it.
func (a *App) focusedQuery() *queryTab {
	if a.overlaid() {
		return nil
	}
	return queryOf(a.focused())
}

// topOverlay names the overlay that has the keys, "" with none (§7.8): the one
// place that says which there are and which is over which, as the keymap's
// scope names them. drawOn draws them from the bottom up, this order reversed.
func (a *App) topOverlay() string {
	switch {
	case a.confirm != nil:
		return "confirm"
	case a.menu != nil:
		return "menu"
	case a.value != nil:
		return "value"
	case a.ddl != nil:
		return "ddl"
	case a.keyHelp != nil:
		return "keyhelp"
	case a.palette != nil:
		return "palette"
	case a.form != nil: // over the session list: the manager opened from it
		return "form"
	case a.sessList != nil:
		return "sessions"
	case a.winRename != nil, a.askName != nil:
		return "rename"
	case a.drop != nil:
		return "dropdown"
	case a.cols != nil:
		return "cols"
	}
	return ""
}

// overlaid reports whether an overlay has the keys (§7.8).
func (a *App) overlaid() bool { return a.topOverlay() != "" }

// vim is the editor that has the keys: the value editor's, or with nothing over
// it the focused query's or the WHERE's being typed (F3.39).
func (a *App) vim() *editor.Editor {
	if a.valueActive() {
		return a.value.ed
	}
	if a.overlaid() {
		return nil
	}
	if c := queryOf(a.focused()); c != nil {
		return c.ed
	}
	if t := dataOf(a.focused()); t != nil && t.typing == "where" && t.hist == nil {
		return t.ed
	}
	if jv := jsonViewOf(a.focused()); jv != nil { // a grid's JSON view, under the WHERE typed over it (F14.5)
		return jv.ed
	}
	return nil
}

// vimMode is the keymap's mode for a vim's (§6.4).
func vimMode(m editor.Mode) keymap.Mode {
	return [...]keymap.Mode{
		editor.Normal: keymap.Normal, editor.Insert: keymap.Insert, editor.Replace: keymap.Insert,
		editor.Visual: keymap.Visual, editor.VisualLine: keymap.Visual, editor.VisualBlock: keymap.Visual,
		editor.Command: keymap.Command, editor.Select: keymap.Insert, // what is typed goes in: INSERT's keys
	}[m]
}

// sizeEditor gives ed the program's settings and the size of its text in text,
// which H M L, C-d and the scrolling go by.
func (a *App) sizeEditor(ed *editor.Editor, text uv.Rectangle) {
	ed.TabWidth, ed.AutoPairs = a.tabWidth, a.autoPairs
	ed.SetHeight(text.Dy())
	ed.SetWidth(text.Dx())
}

// queryView is query t in pane p as it draws, and the area it draws
// in; t's editor gets the size of its text there, which H M L, C-d and
// the scrolling go by.
func (a *App) queryView(p *Pane, t *queryTab) (ui.Query, uv.Rectangle) {
	ed := t.ed
	body := bodyRect(a.layout()[p.ID])
	body.Max.Y = max(body.Max.Y-t.bar.rows(), body.Min.Y)
	text := ui.Query{Lines: ed.Lines()}.TextArea(body)
	a.sizeEditor(ed, text)
	c := ui.Query{
		Lines: ed.Lines(), TabWidth: ed.TabWidth, Top: ed.Top(), Left: ed.Left(),
		Cursor: ui.TextPos(ed.Cursor()), CursorCol: ed.CursorCol(), Normal: ed.Mode() == editor.Normal,
		Failed: t.failed, Pane: p.ID, Dialect: t.conn.dialect(), Lang: t.conn.lang(),
	}
	if f := a.flash; f != nil && f.pane == p.ID {
		c.Yank = f.text
	}
	c.Prompt, c.Text, c.Pos, _ = ed.CmdLine()
	c.Sel = vimSel(ed)
	searchView(ed, &c)
	return c, body
}

// vimSel is what a vim's VISUAL selects, none out of it.
func vimSel(ed *editor.Editor) (s ui.Sel) {
	if from, to, ok := ed.Selection(); ok {
		s = ui.Sel{Mode: ui.SelChars, From: ui.TextPos(from), To: ui.TextPos(to)}
		switch ed.Mode() {
		case editor.VisualLine:
			s.Mode = ui.SelLines
		case editor.VisualBlock:
			s.Mode = ui.SelBlock
			_, _, s.Left, s.Right, _ = ed.Block()
		default: // the other modes keep the character-wise selection
		}
	}
	return s
}

// queryKey gives key k to the editor of query t, in pane p. With the
// candidates up, ↵ takes the selected one and does no more; esc closes them and leaves INSERT, as nvim-cmp (§9.7).
// In INSERT a character typed completes; C-n does with nothing typed.
func (a *App) queryKey(p *Pane, t *queryTab, k keymap.Key) tea.Cmd {
	a.queryView(p, t)
	switch {
	case t.comp != nil && t.comp.sel >= 0 && k == "<CR>": // nothing selected: ↵ is the editor's (F8.17)
		return a.queryAccept(t)
	case t.comp == nil && k == "<C-n>" && t.ed.Mode() == editor.Insert:
		// ponytail: C-n is fixed here, not in the keymap: [keys.query] is
		// NORMAL's; add a query INSERT scope when it has to be remappable
		return a.queryComplete(t, true)
	}
	m := t.ed.Mode()
	open, inInsert := t.comp != nil, m == editor.Insert || m == editor.Select // i, a, A, o type nothing: no list (§9.7); a character over a selected tabstop does (F15.8)
	cmd := a.queryDid(t, t.ed.Feed(string(k)))
	switch {
	case t.ed.Mode() != editor.Insert:
		t.asked, t.wait = nil, nil
	case inInsert && keymap.Text(k) != "" || open && (k == "<BS>" || k == "<C-h>"):
		return tea.Batch(cmd, a.queryComplete(t, false))
	}
	return cmd
}

// queryComplete finds query t's candidates for its cursor, in INSERT
// (sqlComplete); manual is C-n's.
func (a *App) queryComplete(t *queryTab, manual bool) tea.Cmd {
	t.comp = nil
	if t.ed.Mode() != editor.Insert {
		return nil
	}
	lines, cur := t.ed.Lines(), t.ed.Cursor()
	pos := offsetOf(lines, cur)
	if t.asked == nil {
		t.asked = map[tableID]bool{}
	}
	var c *completion
	var cmds []tea.Cmd
	if t.conn.sql() {
		c, cmds = a.sqlComplete(t.conn, strings.Join(lines, "\n"), pos, cmp.Or(t.schema, t.conn.Schema), t.asked, manual)
	} else {
		c, cmds = a.redisComplete(t, lines[cur.Line][:cur.Col], pos, manual)
	}
	if c != nil {
		c.start -= pos - cur.Col // a column of the cursor's line: it is a word before the cursor
	}
	t.comp, t.wait = c, &compWait{cur, t.ver, manual} // columns fetched now or before may still come
	return tea.Batch(cmds...)
}

// queryAccept puts query t's selected candidate in place of the word
// it completes, as typing it would, and closes the list (§9.7).
func (a *App) queryAccept(t *queryTab) tea.Cmd {
	c, cur := t.comp, t.ed.Cursor()
	t.comp = nil
	p := c.plan(t.ed.Lines()[cur.Line], cur.Col)
	cmd := a.queryDid(t, p.put(t.ed))
	if stops := c.items[c.sel].stops; len(stops) > 0 { // a snippet: its tabstops are the first to fill in (F15.11)
		var rs []editor.Range
		for _, off := range stops {
			at := editor.Pos{Line: cur.Line, Col: p.start + off}
			rs = append(rs, editor.Range{From: at, To: at})
		}
		t.ed.Snippet(editor.Pos{Line: cur.Line, Col: p.start}, editor.Pos{Line: cur.Line, Col: p.start + len(p.ins)}, rs)
	}
	if c.items[c.sel].space { // the next word may be a column: its list comes up at once (F8.17)
		cmd = tea.Batch(cmd, a.queryComplete(t, false))
	}
	return cmd
}

// vimCmds are what any vim's eff asks of the app: the clipboard gets
// what "+ took, or is asked for what "+p puts, which put takes once it
// comes (F3.38); a yank flashes in the focused pane (F3.32); an error is
// toasted.
func (a *App) vimCmds(eff editor.Effect, put func(text string, p editor.ClipPut) tea.Cmd) []tea.Cmd {
	var cmds []tea.Cmd
	if eff.Yank != nil {
		cmds = append(cmds, a.flashYank(yankFlash{pane: a.focused().ID, text: yankSel(*eff.Yank)}))
	}
	if eff.Clip != nil {
		cmds = append(cmds, clipCopy(*eff.Clip))
	}
	if p := eff.Paste; p != nil {
		a.clipWait = func(text string) tea.Cmd { return put(text, *p) }
		cmds = append(cmds, clipPaste)
	}
	if eff.Error != "" {
		cmds = append(cmds, a.showToast(eff.Error))
	}
	return cmds
}

type autosave struct {
	t   *queryTab
	ver int
}

// autosaveDelay is how long after the last change a query is written (§11).
var autosaveDelay = time.Second

// queryDid acts on what the editor did: vimCmds, then a change drops
// the red ▶ and is written a second after the last one, a : command it
// left runs here. The candidate list closes; typing on opens it again
// (queryKey).
func (a *App) queryDid(t *queryTab, eff editor.Effect) tea.Cmd {
	t.comp = nil
	cmds := a.vimCmds(eff, func(text string, p editor.ClipPut) tea.Cmd {
		if !slices.Contains(a.queries(), t) {
			return nil
		}
		return a.queryDid(t, t.ed.PutClip(text, p))
	})
	if eff.Changed {
		t.failed = -1
		t.ver++
		ver := t.ver
		cmds = append(cmds, tea.Tick(autosaveDelay, func(time.Time) tea.Msg { return autosave{t, ver} }))
	}
	if eff.Ex != "" {
		cmds = append(cmds, a.ex(eff.Ex))
	}
	if eff.Format != nil {
		cmds = append(cmds, a.queryFormat(t, *eff.Format, false))
	}
	return tea.Batch(cmds...)
}

// ex runs a : command the editor leaves to the app as the palette's
// aliases do (§11): :w, :q, :wq, :qa.
func (a *App) ex(c string) tea.Cmd {
	if id := exAliases[strings.TrimSpace(c)]; id != "" {
		return a.run(id, 0)
	}
	return a.showToast(i18n.T("不支持的命令：%s", c))
}

// flush writes query t's file if it changed since it was last written, for
// the user's asking (a save, a close, a quit): a file changed outside since
// this tab had it is refused once, the next flush is the yes (F13.2).
// ponytail: written in Update, a slow disk holds up a key; write from a
// Cmd, in order, if that shows.
func (t *queryTab) flush() error { return t.write(true) }

// write is flush, or for an autosave (explicit false) one that never
// overwrites a file changed outside: the text stays in the buffer, said once.
func (t *queryTab) write(explicit bool) error {
	if t.ver == t.saved {
		return nil
	}
	if cur := stampOf(t.path); cur != (fileStamp{}) && cur != t.disk && (!explicit || cur != t.armed) { // a file gone is written again
		t.armed = cur // what was said, an autosave's too, the next explicit write of this very file is the yes
		if !explicit {
			if t.told {
				return nil
			}
			t.told = true
		}
		return changedOutside()
	}
	text := strings.Join(t.ed.Lines(), "\n") + "\n"
	if text == "\n" { // nothing: an empty file, as vim writes it
		text = ""
	}
	if err := config.WriteQuery(t.path, text); err != nil {
		return err
	}
	t.saved, t.disk, t.armed, t.told = t.ver, stampOf(t.path), fileStamp{}, false
	return nil
}

// flushAll writes the queries among panes, and on a failure tells so and
// is false: what asked for it, closing or quitting, must not go on (§11).
func (a *App) flushAll(panes ...*Pane) (tea.Cmd, bool) {
	for _, p := range panes {
		for _, tab := range p.Tabs {
			if tab.Query == nil {
				continue
			}
			if err := tab.Query.flush(); err != nil {
				return a.saveFailed(err), false
			}
		}
	}
	return nil, true
}

func (a *App) saveFailed(err error) tea.Cmd {
	return a.showToast(i18n.T("保存失败：%s", err.Error()))
}

type externalDone struct {
	t   *queryTab
	err error
}

// external opens the focused query's file in $VISUAL, $EDITOR or vi
// (notepad on windows) (§11), written first; what it holds after is one
// undo step.
func (a *App) external() tea.Cmd {
	t := queryOf(a.focused())
	if t == nil {
		return nil
	}
	if err := t.flush(); err != nil {
		return a.saveFailed(err)
	}
	return tea.ExecProcess(editorCmd(t.path), func(err error) tea.Msg { return externalDone{t, err} })
}

func (a *App) gotExternal(msg externalDone) tea.Cmd {
	t := msg.t
	disk := stampOf(t.path)
	text, err := config.ReadQuery(t.path)
	if err != nil {
		return a.showToast(err.Error())
	}
	cmd := a.queryDid(t, t.ed.Load(text))
	t.saved, t.disk, t.armed, t.told = t.ver, disk, fileStamp{}, false // what the file has
	if msg.err != nil {
		return tea.Batch(cmd, a.showToast(msg.err.Error()))
	}
	return cmd
}

// queryAt is the query of pane id, and the line and display column of
// its text under p; nil when the pane shows no query.
func (a *App) queryAt(id int, p uv.Position) (*queryTab, int, int) {
	pane := a.win().pane(id)
	if pane == nil || queryOf(pane) == nil {
		return nil, 0, 0
	}
	c := queryOf(pane)
	view, body := a.queryView(pane, c)
	text := view.TextArea(body)
	return c, view.Top + p.Y - text.Min.Y, max(view.Left+p.X-text.Min.X, 0)
}
