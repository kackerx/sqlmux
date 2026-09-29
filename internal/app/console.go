package app

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/config"
	"sqlmux/internal/editor"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// consoleTab is a console open in a pane (§5, §11): its editor and the
// file that keeps its text.
type consoleTab struct {
	ed         *editor.Editor
	path       string
	ver, saved int       // changes made, and the one the file has
	failed     int       // the first line of the statement whose last run failed, its ▶ red; -1 for none
	running    *run      // the run going on, if one is: another ↵ waits for it (§11)
	bar        *errorBar // the last run's error, under the text (§7.8「错误栏」)
	// schema is the one it runs in, first on search_path (§8.6): the
	// tree's as it opens, "" until the catalog is in.
	schema string
	// The candidate list in INSERT (§9.7); the tables whose columns were
	// fetched for it this INSERT, and where it last completed: columns in
	// while the cursor is still there complete again.
	comp  *completion
	asked map[tableID]bool
	wait  *compWait
}

// compWait is where a console last completed, C-n's or not.
type compWait struct {
	at     editor.Pos
	ver    int
	manual bool
}

// openConsole is connection conn's console n, with what its file keeps (§11).
func openConsole(conn string, n int) (*consoleTab, error) {
	path := config.ConsolePath(conn, n)
	text, err := config.ReadConsole(path)
	if err != nil {
		return nil, fmt.Errorf("读取失败：%w", err)
	}
	return &consoleTab{ed: editor.New(text), path: path, failed: -1}, nil
}

func consoleOf(p *Pane) *consoleTab {
	if p.Cur < len(p.Tabs) {
		return p.Tabs[p.Cur].Console
	}
	return nil
}

// focusedConsole is the console that has the keys: the focused pane's
// current tab, with no overlay over it.
func (a *App) focusedConsole() *consoleTab {
	if a.palette != nil || a.drop != nil || a.cols != nil || a.confirm != nil || a.keyHelp != nil {
		return nil
	}
	return consoleOf(a.focused())
}

// consoleView is console t in pane p as it draws, and the area it draws
// in; t's editor gets the size of its text there, which H M L, C-d and
// the scrolling go by.
func (a *App) consoleView(p *Pane, t *consoleTab) (ui.Console, uv.Rectangle) {
	ed := t.ed
	body := bodyRect(a.layout()[p.ID])
	body.Max.Y = max(body.Max.Y-t.bar.rows(), body.Min.Y)
	text := ui.Console{Lines: ed.Lines()}.TextArea(body)
	ed.TabWidth, ed.AutoPairs = a.tabWidth, a.autoPairs
	ed.SetHeight(text.Dy())
	ed.SetWidth(text.Dx())
	c := ui.Console{
		Lines: ed.Lines(), TabWidth: ed.TabWidth, Top: ed.Top(), Left: ed.Left(),
		Cursor: ui.TextPos(ed.Cursor()), CursorCol: ed.CursorCol(), Normal: ed.Mode() == editor.Normal,
		Failed: t.failed, Pane: p.ID,
	}
	c.Names, _ = a.sqlNames(strings.Join(c.Lines, "\n"), cmp.Or(t.schema, a.sess.Schema), nil)
	if f := a.flash; f != nil && f.pane == p.ID {
		c.Yank = f.text
	}
	c.Prompt, c.Text, c.Pos, _ = ed.CmdLine()
	if from, to, ok := ed.Selection(); ok {
		c.Sel = ui.Sel{Mode: ui.SelChars, From: ui.TextPos(from), To: ui.TextPos(to)}
		switch ed.Mode() {
		case editor.VisualLine:
			c.Sel.Mode = ui.SelLines
		case editor.VisualBlock:
			c.Sel.Mode = ui.SelBlock
			_, _, c.Sel.Left, c.Sel.Right, _ = ed.Block()
		}
	}
	return c, body
}

// consoleKey gives key k to the editor of console t, in pane p. With the
// candidates up, ↵ takes the selected one, and is the editor's when that
// changes nothing; esc closes them and leaves INSERT, as nvim-cmp (§9.7).
// In INSERT a character typed completes; C-n does with nothing typed.
func (a *App) consoleKey(p *Pane, t *consoleTab, k keymap.Key) tea.Cmd {
	a.consoleView(p, t)
	switch {
	case t.comp != nil && k == "<CR>":
		if ok, cmd := a.consoleAccept(t); ok {
			return cmd
		}
	case t.comp == nil && k == "<C-n>" && t.ed.Mode() == editor.Insert:
		// ponytail: C-n is fixed here, not in the keymap: [keys.console] is
		// NORMAL's; add a console INSERT scope when it has to be remappable
		return a.consoleComplete(t, true)
	}
	open := t.comp != nil
	cmd := a.consoleDid(t, t.ed.Feed(string(k)))
	switch {
	case t.ed.Mode() != editor.Insert:
		t.asked, t.wait = nil, nil
	case keymap.Text(k) != "" || open && (k == "<BS>" || k == "<C-h>"):
		return tea.Batch(cmd, a.consoleComplete(t, false))
	}
	return cmd
}

// consoleComplete finds console t's candidates for its cursor, in INSERT
// (sqlComplete); manual is C-n's.
func (a *App) consoleComplete(t *consoleTab, manual bool) tea.Cmd {
	t.comp = nil
	if t.ed.Mode() != editor.Insert {
		return nil
	}
	lines, cur := t.ed.Lines(), t.ed.Cursor()
	pos := offsetOf(lines, cur)
	if t.asked == nil {
		t.asked = map[tableID]bool{}
	}
	c, cmds := a.sqlComplete(strings.Join(lines, "\n"), pos, cmp.Or(t.schema, a.sess.Schema), t.asked, manual)
	if c != nil {
		c.start -= pos - cur.Col // a column of the cursor's line: it is a word before the cursor
	}
	t.comp, t.wait = c, &compWait{cur, t.ver, manual} // columns fetched now or before may still come
	return tea.Batch(cmds...)
}

// consoleAccept puts console t's selected candidate in place of the word
// it completes, as typing it would, and closes the list. ok is false when
// that changes nothing, the case aside (§9.7).
func (a *App) consoleAccept(t *consoleTab) (ok bool, cmd tea.Cmd) {
	c, cur := t.comp, t.ed.Cursor()
	t.comp = nil
	if cd := c.items[c.sel]; !strings.EqualFold(t.ed.Lines()[cur.Line][c.start:cur.Col], cd.insert) {
		return true, a.consoleDid(t, t.ed.Complete(c.start, cd.insert))
	}
	return false, nil
}

type autosave struct {
	t   *consoleTab
	ver int
}

// autosaveDelay is how long after the last change a console is written (§11).
var autosaveDelay = time.Second

// consoleDid acts on what the editor did: the clipboard gets what was
// yanked, a change drops the red ▶ and is written a second after the last
// one, a : command it left runs here. The candidate list closes; typing
// on opens it again (consoleKey).
func (a *App) consoleDid(t *consoleTab, eff editor.Effect) tea.Cmd {
	t.comp = nil
	var cmds []tea.Cmd
	if eff.Yanked {
		cmds = append(cmds, tea.SetClipboard(t.ed.Register()))
	}
	if eff.Yank != nil {
		cmds = append(cmds, a.flashYank(yankFlash{pane: a.focused().ID, text: yankSel(*eff.Yank)}))
	}
	if eff.Error != "" {
		cmds = append(cmds, a.showToast(eff.Error, toastTTL))
	}
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
		cmds = append(cmds, a.consoleFormat(t, *eff.Format))
	}
	return tea.Batch(cmds...)
}

// ex runs a : command the editor leaves to the app as the palette's
// aliases do (§11): :w, :q, :wq, :qa.
func (a *App) ex(c string) tea.Cmd {
	if id := exAliases[strings.TrimSpace(c)]; id != "" {
		return a.run(id, 0)
	}
	return a.showToast("不支持的命令："+c, toastTTL)
}

// flush writes console t's file if it changed since it was last written.
// ponytail: written in Update, a slow disk holds up a key; write from a
// Cmd, in order, if that shows.
func (t *consoleTab) flush() error {
	if t.ver == t.saved {
		return nil
	}
	text := strings.Join(t.ed.Lines(), "\n") + "\n"
	if text == "\n" { // nothing: an empty file, as vim writes it
		text = ""
	}
	if err := config.WriteConsole(t.path, text); err != nil {
		return err
	}
	t.saved = t.ver
	return nil
}

// flushAll writes the consoles among panes, and on a failure tells so and
// is false: what asked for it, closing or quitting, must not go on (§11).
func (a *App) flushAll(panes ...*Pane) (tea.Cmd, bool) {
	for _, p := range panes {
		for _, tab := range p.Tabs {
			if tab.Console == nil {
				continue
			}
			if err := tab.Console.flush(); err != nil {
				return a.saveFailed(err), false
			}
		}
	}
	return nil, true
}

func (a *App) saveFailed(err error) tea.Cmd {
	return a.showToast("保存失败："+err.Error(), toastTTL)
}

type externalDone struct {
	t   *consoleTab
	err error
}

// external opens the focused console's file in $VISUAL, $EDITOR or vi
// (§11), written first; what it holds after is one undo step.
func (a *App) external() tea.Cmd {
	t := consoleOf(a.focused())
	if t == nil {
		return nil
	}
	if err := t.flush(); err != nil {
		return a.saveFailed(err)
	}
	ed := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	c := exec.Command("sh", "-c", ed+` "$1"`, "sh", t.path)
	return tea.ExecProcess(c, func(err error) tea.Msg { return externalDone{t, err} })
}

func (a *App) gotExternal(msg externalDone) tea.Cmd {
	t := msg.t
	text, err := config.ReadConsole(t.path)
	if err != nil {
		return a.showToast(err.Error(), toastTTL)
	}
	cmd := a.consoleDid(t, t.ed.Load(text))
	t.saved = t.ver // what the file has
	if msg.err != nil {
		return tea.Batch(cmd, a.showToast(msg.err.Error(), toastTTL))
	}
	return cmd
}

// consoleAt is the console of pane id, and the line and display column of
// its text under p; nil when the pane shows no console.
func (a *App) consoleAt(id int, p uv.Position) (*consoleTab, int, int) {
	pane := a.win().pane(id)
	if pane == nil || consoleOf(pane) == nil {
		return nil, 0, 0
	}
	c := consoleOf(pane)
	view, body := a.consoleView(pane, c)
	text := view.TextArea(body)
	return c, view.Top + p.Y - text.Min.Y, max(view.Left+p.X-text.Min.X, 0)
}
