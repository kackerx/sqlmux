// Package app holds the single state tree and its Update/View (tech-design §3).
package app

import (
	"fmt"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"sqlmux/internal/config"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

type App struct {
	w, h  int
	theme *ui.Theme
	icons *ui.Icons
	keys  *keymap.Map
	res   *keymap.Resolver
	sess  *Session

	palette  *palette  // non-nil while the command palette is open (COMMAND mode)
	drop     *dropdown // non-nil while a one-pick dropdown is open (§8.6, §7.8)
	cols     *colsMenu // non-nil while the COLS list is open (Q-04)
	whichKey bool      // the which-key overlay is up (§6.5)
	// paneNumbers is SPC q's overlay: the next key picks a pane by its ⟨n⟩.
	paneNumbers bool

	busy int // table requests out on Meta (§8.3)

	state *config.State // kept between runs (§14)

	toast     string
	toastSeq  int
	quitToast int    // toastSeq of the "press C-c again" toast
	warning   string // shown as a toast on start

	// Mouse (§7.4).
	hits        []ui.Hit    // the last frame's hit table
	mouse       uv.Position // pointer, for hover styles
	drag        *handle     // the split border being dragged
	dragTree    bool        // the sidebar's edge is being dragged
	lastClick   ui.Target   // with lastClickAt, to spot a double click
	lastClickAt time.Time
}

// toastTTL is how long a toast stays up (§7.8).
const toastTTL = 3 * time.Second

// doubleClick is how soon a second click on the same target makes a double (§7.4).
const doubleClick = 400 * time.Millisecond

type (
	toastExpired struct{ seq int }
	keyTimeout   struct{ seq int }
	whichKeyDue  struct{ seq int }
)

// whichKeyDelay is how long a pure prefix waits before which-key shows (§6.5).
var whichKeyDelay = 400 * time.Millisecond

// New is the app over sess and the state kept from before; warning, if
// not "", shows as a toast on start.
func New(cfg *config.Config, keys *keymap.Map, sess *Session, st *config.State, warning string) *App {
	return &App{
		theme: cfg.Theme, icons: cfg.Icons,
		keys: keys, res: keymap.NewResolver(keys), sess: sess,
		mouse: uv.Pos(-1, -1), warning: warning, state: st,
	}
}

// Init switches Bubble Tea's renderer to grapheme widths, the same as Frame
// (§7.1「宽度」): with wcwidth it re-flows lines holding 👍🏽 or ❤️ and pushes
// the border off the row. Bubble Tea v2.0.9 only switches when the terminal
// answers the mode 2027 query; tmux doesn't answer and Terminal.app is never
// asked. There is no public setting, so this hands its event loop the answer
// the terminal would have sent. When upgrading Bubble Tea, check that
// TestRendererUsesGraphemeWidths still passes, and whether a real option
// has appeared.
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg { return tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet} },
		a.loadCatalog(),
	}
	if a.warning != "" {
		cmds = append(cmds, a.showToast(a.warning, toastTTL))
	}
	return tea.Batch(cmds...)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
	case catalogMsg:
		return a, a.gotCatalog(msg)
	case pageMsg:
		return a, a.gotPage(msg)
	case countMsg:
		a.gotCount(msg)
	case quickMsg:
		return a, a.gotQuick(msg)
	case colsMsg:
		return a, a.gotCols(msg)
	case stateErr:
		return a, a.showToast(msg.err.Error(), toastTTL)
	case toastExpired:
		if msg.seq == a.toastSeq {
			a.toast = ""
		}
	case tea.KeyPressMsg:
		return a, a.press(keymap.FromTea(msg.Key()))
	case tea.MouseMotionMsg:
		m := msg.Mouse()
		a.mouse = uv.Pos(m.X, m.Y)
		if a.drag != nil { // the border follows the pointer
			a.win().Root = a.win().Root.setRatio(a.drag.idx, a.drag.ratioAt(a.mouse))
		}
		if a.dragTree {
			a.win().TreeW = treeWidth(a.mouse.X, a.w)
		}
		if t, _ := ui.HitAt(a.hits, a.mouse); t.Kind == ui.KindRow { // hover selects (K-03, §9.7)
			switch c := a.completing(); {
			case c != nil:
				c.sel = t.I
			case a.palette != nil:
				a.palette.sel = t.I
			}
		}
	case tea.MouseReleaseMsg:
		a.drag, a.dragTree = nil, false
	case tea.MouseClickMsg:
		switch m := msg.Mouse(); m.Button {
		case tea.MouseLeft:
			return a, a.click(uv.Pos(m.X, m.Y))
		case tea.MouseMiddle: // a table in the tree opens in a new tab (§7.8)
			if t, _ := ui.HitAt(a.hits, uv.Pos(m.X, m.Y)); t.Kind == ui.KindTable {
				a.win().tree.cursor = t.I
				return a, a.run("tree.open.tab", 0)
			}
		}
	case tea.MouseWheelMsg:
		a.wheel(msg.Mouse())
	case keyTimeout:
		cmd := a.dispatch(a.res.Timeout(a.context(), msg.seq))
		a.whichKey = a.whichKey && len(a.res.Next()) > 0
		return a, cmd
	case whichKeyDue: // only opens it; key presses close it
		if msg.seq == a.res.Seq() && len(a.res.Next()) > 0 {
			a.whichKey = true
		}
	}
	return a, nil
}

// press handles one key, typed or clicked (a which-key item).
func (a *App) press(k keymap.Key) tea.Cmd {
	if a.paneNumbers {
		a.jumpToPane(k)
		return nil
	}
	out, wait := a.res.Feed(a.context(), k)
	cmd := a.dispatch(out)
	seq := a.res.Seq()
	switch {
	case wait: // ambiguous: the shorter binding fires after timeoutlen
		cmd = tea.Batch(cmd, tea.Tick(a.keys.Timeout, func(time.Time) tea.Msg { return keyTimeout{seq} }))
	case len(a.res.Next()) == 0:
		a.whichKey = false
	case !a.whichKey: // a pure prefix: which-key shows if nothing follows soon
		cmd = tea.Batch(cmd, tea.Tick(whichKeyDelay, func(time.Time) tea.Msg { return whichKeyDue{seq} }))
	}
	return cmd
}

// click turns a left click into what its target stands for (§7.4): the
// same actions keys run.
func (a *App) click(p uv.Position) tea.Cmd {
	t, ok := ui.HitAt(a.hits, p)
	if !ok {
		return nil
	}
	now := time.Now()
	double := t == a.lastClick && now.Sub(a.lastClickAt) <= doubleClick
	a.lastClick, a.lastClickAt = t, now
	if double {
		a.lastClick = ui.Target{} // a third click starts over
	}
	focus := func() tea.Cmd { return a.run(fmt.Sprintf("pane.focus %d", t.Pane), 0) }
	switch t.Kind {
	case ui.KindNumber:
		a.jumpToPane(keymap.Key(strconv.Itoa(t.I)))
	case ui.KindBackdrop: // outside an overlay: close it
		a.whichKey, a.paneNumbers, a.palette, a.drop, a.cols = false, false, nil, nil, nil
		a.res.Reset()
	case ui.KindRow:
		switch tab, c := a.typingTab(), a.completing(); {
		case c != nil:
			c.sel = t.I
			a.acceptCompletion()
			return nil
		case a.palette != nil:
			return a.paletteRun(t.I, false)
		case tab != nil && tab.hist != nil:
			return a.histApply(tab, a.histIndex(tab, t.I))
		case a.drop != nil:
			return a.dropPick(t.I)
		case a.cols != nil: // a column's row: show or hide it (Q-04)
			a.colsToggle(t.I)
		}
	case ui.KindTable:
		a.win().tree.cursor = t.I
		return a.run("tree.open", 0)
	case ui.KindItem:
		if next := a.res.Next(); t.I < len(next) {
			return a.press(next[t.I].Key)
		}
	case ui.KindButton:
		return a.run(t.Action, 0)
	case ui.KindHint, ui.KindCell, ui.KindRowNo:
		return tea.Batch(focus(), a.run(t.Action, 0))
	case ui.KindTitle:
		if double {
			return tea.Batch(focus(), a.run("pane.zoom", 0))
		}
		return focus()
	case ui.KindPane:
		return focus()
	case ui.KindTab:
		a.focusPane(t.Pane)
		if p := a.win().pane(t.Pane); p != nil {
			selectTab(p, t.I)
		}
	case ui.KindTreeEdge:
		a.dragTree = true
	case ui.KindBorder:
		for _, h := range a.win().Root.handles(a.mainArea()) {
			if h.idx == t.I {
				a.drag = &h
			}
		}
	}
	return nil
}

// wheel scrolls the pane under the pointer, not the focused one (§7.4):
// Shift turns the vertical wheel horizontal, as does a touchpad's sideways
// swipe (buttons 6 and 7).
func (a *App) wheel(m tea.Mouse) {
	down, right := 0, 0
	switch m.Button {
	case tea.MouseWheelUp:
		down = -1
	case tea.MouseWheelDown:
		down = 1
	case tea.MouseWheelLeft:
		right = -1
	case tea.MouseWheelRight:
		right = 1
	}
	if m.Mod.Contains(tea.ModShift) {
		down, right = 0, down
	}
	if a.palette != nil && a.quickShows() { // over the result's table: that scrolls
		_, ms := a.paletteMatches()
		if _, _, area := a.paletteBox(len(ms)); uv.Pos(m.X, m.Y).In(area) {
			q := a.palette.quick
			q.top, q.left, _, _ = a.quickView(q).Grid.Scroll(area, down*wheelStep, right)
			return
		}
	}
	for id, r := range a.layout() {
		if uv.Pos(m.X, m.Y).In(r) {
			a.scrollPane(id, down, right)
		}
	}
}

// dispatch runs what the keymap resolved: actions through the registry, and
// unclaimed keys to the focused widget.
func (a *App) dispatch(out []keymap.Result) tea.Cmd {
	var cmds []tea.Cmd
	for _, r := range out {
		if r.Action != "" {
			cmds = append(cmds, a.run(r.Action, r.Count))
			continue
		}
		for _, k := range r.Keys { // unbound keys go to the input that has them
			switch t := a.typingTab(); {
			case a.palette != nil:
				cmds = append(cmds, a.paletteKey(k))
			case a.drop != nil:
				a.dropKey(k)
			case a.cols != nil:
				if a.cols.typing {
					a.colsFilterKey(k)
				}
			case a.win().tree.filtering:
				a.filterKey(k)
			case t != nil:
				cmds = append(cmds, a.typeKey(t, k))
			}
		}
	}
	return tea.Batch(cmds...)
}

// mode is derived from state, never stored (§3 principle 3).
// ponytail: no VISUAL until the console editor (M3).
func (a *App) mode() keymap.Mode {
	switch t := a.typingTab(); {
	case a.palette != nil, a.drop != nil, a.cols != nil, t != nil && t.hist != nil: // an overlay has the keys (§7.8)
		return keymap.Command
	case a.win().tree.filtering, t != nil:
		return keymap.Insert
	}
	return keymap.Normal
}

// context tells the keymap which scopes apply to the next key (§6.4).
func (a *App) context() keymap.Context {
	switch typing := a.typingTab(); {
	case a.palette != nil && a.palette.comp != nil: // esc is the input's, as in a WHERE (§9.7)
		return keymap.Context{Overlay: "complete", Focus: []string{"input"}, Mode: keymap.Command}
	case a.palette != nil:
		return keymap.Context{Overlay: "palette", Mode: keymap.Command}
	case a.drop != nil:
		return keymap.Context{Overlay: "dropdown", Mode: keymap.Command}
	case a.cols != nil && !a.cols.typing:
		return keymap.Context{Overlay: "cols", Mode: keymap.Command}
	case a.cols != nil: // its filter: unbound keys are text
		return keymap.Context{Focus: []string{"input"}, Mode: keymap.Command}
	case typing != nil && typing.hist != nil: // filtered by the WHERE typed
		return keymap.Context{Overlay: "where", Focus: []string{"input"}, Mode: keymap.Command}
	case typing != nil && typing.comp != nil:
		return keymap.Context{Overlay: "complete", Focus: []string{"input"}, Mode: keymap.Insert}
	case a.win().tree.filtering, typing != nil:
		return keymap.Context{Focus: []string{"input"}, Mode: keymap.Insert}
	}
	return keymap.Context{Focus: []string{a.paneScope()}, Pane: a.paneScope()}
}

// paneScope is the keymap scope of the focused pane.
func (a *App) paneScope() string {
	return [...]string{KindSchema: "tree", KindData: "grid", KindConsole: "console"}[a.focused().Kind]
}

func (a *App) focused() *Pane {
	if p := a.win().pane(a.win().Focus); p != nil {
		return p
	}
	return a.win().Tree
}

func (a *App) showToast(s string, ttl time.Duration) tea.Cmd {
	a.toast = s
	a.toastSeq++
	seq := a.toastSeq
	return tea.Tick(ttl, func(time.Time) tea.Msg { return toastExpired{seq} })
}

func (a *App) View() tea.View {
	f := a.render()
	a.hits = f.Hits // clicks are looked up in what was drawn
	v := tea.NewView(f.Render())
	if c := f.Cursor; c != nil { // the terminal's own cursor, which input methods follow (§12)
		v.Cursor = tea.NewCursor(c.X, c.Y)
		v.Cursor.Shape = tea.CursorBar
	}
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}
