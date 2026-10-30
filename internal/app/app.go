// Package app holds the single state tree and its Update/View (tech-design §3).
package app

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

type App struct {
	formTests int // the connection tests started, to tell a late result from the form's own (F12.5)
	w, h      int
	theme     *ui.Theme
	// rendered is the last frame, whose lines the next reuses; probes what it
	// asked of the pointer, seenAt where that was, seen the view it made;
	// fresh is that nothing changed since, reuse that an Update found the
	// pointer moved inside it and View may answer with seen (F8.20)
	rendered ui.Rendered
	files    queryDir // the Queries node's list (F8.21)
	probes   []uv.Rectangle
	seenAt   uv.Position
	seen     tea.View
	fresh    bool
	reuse    bool
	icons    *ui.Icons
	// baseIcons is what `icons` chose, a theme's [icon] not on it, and themeName
	// the theme in use: theme.switch loads another over them (F6.2).
	jump      *jumpState // a flash-style jump going on (F10.8)
	askName   *nameAsk   // a name typed in the status bar: layout.save's, a session's, a query's (F6.5, F8.11)
	lastPath  string     // where the working set is kept as the program quits; "" for nowhere
	plain     bool       // vim = false: query and WHERE edit without modes (§6.9)
	restored  bool       // the sessions are a layout's: query_1 is not looked up in state.json (F6.5)
	baseIcons *ui.Icons
	themeName string
	keys      *keymap.Map
	res       *keymap.Resolver
	// sess is the session attached, one of sessions (§5, F5.1).
	sess     *Session
	lastSess *Session // the one attached before sess (F9.10)
	sessions []*Session

	tabWidth int // the queries' (§14)
	maxRows  int // a query run keeps of each result (§11)
	// redisDelim, scanKeys and loadAll are `[redis]`'s (§8.7); filterSeq numbers
	// the changes of the tree's filter that wait to be asked of the servers (F16.2)
	redisDelim        string
	scanKeys, loadAll int
	filterSeq         int
	resultHeight      float64   // the share the result area first takes
	treeWidth         int       // config [tree] width: the sidebar's default width (§7.8)
	fullWidth         int       // the grid's columns up to this wide are not squeezed (§7.6)
	border            uv.Border // the boxes' lines (§7.1)
	newDesc           bool      // a table opened anew sorts descending, and shows transposed (§14)
	newTurned         bool
	keywordCase       string // gq's (§9.5)
	formatPrg         string
	autoPairs         bool // a query's INSERT, a WHERE and the quick SQL (§7.9)
	valueHints        bool // a query's line of the field its value goes in (§11, F15.3)
	tipsOn            bool // the status bar's line about a feature (§7.8, F15.4)
	tip               tipState
	// confirmDangerous asks before a run that would delete or drop, and a
	// save that deletes rows (§13).
	confirmDangerous bool
	previewSave      bool // a save shows its statements first; a production connection's always does (F17.10)

	palette   *palette     // non-nil while the command palette is open (COMMAND mode)
	sessList  *sessionList // non-nil while the session list is open (§7.8)
	form      *connForm    // non-nil while the new connection form is open (§7.8)
	winRename *ui.Input    // the window's name typed in the status bar, in its place (§5)
	drop      *dropdown    // non-nil while a one-pick dropdown is open (§8.6, §7.8)
	cols      *colsMenu    // non-nil while the COLS list is open (Q-04)
	confirm   *confirmBox  // non-nil while it asks before changes go (§10.5)
	menu      *menu        // non-nil while a right-click menu, the tree's or the grid's, is open (F8.23, F12.3)
	ddl       *ddlPopup    // non-nil while a table's or a column's DDL is shown over the grid (F9.8)
	value     *valueEditor // non-nil while a cell's value is open in its editor (F12.2)
	whichKey  bool         // the which-key overlay is up (§6.5)
	keyHelp   *keyHelp     // non-nil while the ? help is open (§6.5)
	// paneNumbers is SPC q's overlay: the next key picks a pane by its ⟨n⟩.
	paneNumbers bool

	state *config.State // kept between runs (§14)

	rowCopy  *copiedRow           // the row yy took, for p (§10.6)
	clipWait func(string) tea.Cmd // a "+p waiting for the clipboard: what to do with its text (F3.38)
	fetching *palette             // the palette whose DDL fetches may be out (F4.2)
	flash    *yankFlash           // a yank flashing (F3.32)
	flashSeq int

	toast     string
	toastSeq  int
	quitToast int         // toastSeq of the "press C-c again" toast
	warning   string      // shown as a toast on start
	spin      int         // the frame of every spinner (§7.8「转圈」)
	spinOn    bool        // a spinTick is on its way
	want      *startTable // the command line's table, to open when its catalog is in (F19.1)

	// Mouse (§7.4).
	hits        []ui.Hit     // the last frame's hit table
	mouse       uv.Position  // pointer, for hover styles
	drag        *handle      // the split border being dragged
	dragTree    bool         // the sidebar's edge is being dragged
	dragCol     *colDrag     // a column's width is being dragged by its head's edge (F17.14)
	exporting   map[any]bool // the tabs with an export going on (F12.4)
	shiftClick  bool         // the left click being handled had Shift down (a column head's: add to the sorting)
	dragText    int          // the pane whose query text is being selected by dragging; 0 for none
	dragWhere   int          // the same for a WHERE's text (vim = false)
	lastClick   ui.Target    // with lastClickAt, to spot a double click
	lastClickAt time.Time
}

// toastLine flattens a toast onto its one row (§7.8).
func toastLine(s string) string {
	return strings.NewReplacer("\n", i18n.T("；"), "\t", " ").Replace(s)
}

// toastTTL is how long a toast stays up (§7.8).
var toastTTL = 3 * time.Second

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
	a := &App{
		theme: cfg.Theme, icons: cfg.Icons, baseIcons: cfg.BaseIcons, plain: !cfg.Vim, themeName: cfg.ThemeName, tabWidth: cfg.TabWidth, maxRows: cfg.MaxRows, redisDelim: cfg.RedisDelimiter, scanKeys: cfg.RedisScanKeys, loadAll: cfg.RedisLoadAll, resultHeight: cfg.ResultHeight, fullWidth: cfg.GridFullWidth, treeWidth: cfg.TreeWidth, border: cfg.Border, newDesc: cfg.GridDesc, newTurned: cfg.GridTranspose,
		keywordCase: cfg.KeywordCase, formatPrg: cfg.FormatPrg, autoPairs: cfg.AutoPairs, valueHints: cfg.Hints, tipsOn: cfg.Tips, tip: tipState{cur: -1, pick: rand.IntN}, confirmDangerous: cfg.ConfirmDangerous, previewSave: cfg.PreviewSave,
		keys: keys, res: keymap.NewResolver(keys), sess: sess, sessions: []*Session{sess},
		mouse: uv.Pos(-1, -1), warning: JoinNotes(warning, sess.warning), state: st,
	}
	a.plainQueries()
	if a.tipsOn {
		a.nextTip()
	}
	if len(sess.Conns) == 0 { // a start with no connection (F7.4)
		a.openConnForm()
	}
	return a
}

// JoinNotes is the notes that are not empty on one line, the way a toast says
// several.
func JoinNotes(notes ...string) string {
	return strings.Join(slices.DeleteFunc(slices.Clone(notes), func(s string) bool { return s == "" }), i18n.T("；"))
}

// KeyProblems opens a notice of the lines of keymaps.toml that did not take
// (§6.7): the conflicts and the keys it could not read, one a line; ↵ or
// esc closes it. None, nothing opens.
func (a *App) KeyProblems(ps []keymap.Problem) {
	if len(ps) == 0 {
		return
	}
	lines := []string{fmt.Sprintf(i18n.T("keymaps.toml 有 %d 处问题："), len(ps))}
	for _, p := range ps {
		lines = append(lines, "  "+p.String())
	}
	a.confirm = &confirmBox{text: strings.Join(lines, "\n"), yes: i18n.T("知道了")}
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
	cmds := []tea.Cmd{func() tea.Msg { return tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet} }}
	cmds = append(cmds, a.connectsNow()...) // the start's, and a layout's others (F6.5, F12.5)
	if !a.restored {
		for _, t := range a.sess.queries() { // query_1, where it last ran (§11)
			cmds = append(cmds, a.restoreQuery(t))
		}
	}
	if a.warning != "" {
		cmds = append(cmds, a.showToast(a.warning))
	}
	if a.tipsOn {
		cmds = append(cmds, tipTimer())
	}
	return tea.Batch(cmds...)
}

// plainQueries has every query without modes with vim = false (§6.9):
// they are made all over, the sessions of a layout before the App, so what
// there is is gone through before each key, a mapping's too, and after each
// message, for what is drawn.
func (a *App) plainQueries() {
	if a.plain {
		for _, t := range a.queries() {
			t.ed.SetPlain(true)
		}
	}
}

// Update is update, then the columns the queries on screen want (§7.3)
// and the DDL the palette's selection may (F4.2).
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	wasFresh := a.fresh
	a.fresh, a.reuse = false, false
	_, cmd := a.update(msg)
	switch msg.(type) {
	case tea.KeyPressMsg, tea.PasteMsg, tea.MouseMsg:
	default:
		if a.jump != nil { // what came changed the screen the matches are of
			a.jumpScan()
		}
	}
	if wasFresh && a.stillOver(msg) { // a pointer moved inside what it was over: the frame is the same
		a.fresh, a.reuse = true, true
	}
	a.plainQueries()
	a.sess.last = a.conn() // where the focus goes next may name none (§5)
	a.sess.touch(a.focused().tab())
	return a, tea.Batch(cmd, a.wantCols(), a.wantDDL(), a.wantPopupDDL(), a.spinStart())
}

// stillOver is whether msg changed nothing the last frame shows: a pointer
// moving, with none of the rects the frame asked about crossed, and nothing
// else that moving does (a border or text dragged, a list's selection
// following the pointer) (F8.20). The seconds of a run on its way are the
// exception: they move on between ticks, and a pointer moving meanwhile does
// not redraw them; the tick that is due brings the right ones.
func (a *App) stillOver(msg tea.Msg) bool {
	m, ok := msg.(tea.MouseMotionMsg)
	if !ok || a.drag != nil || a.dragTree || a.dragCol != nil || a.dragText != 0 || a.dragWhere != 0 {
		return false
	}
	if t, _ := ui.HitAt(a.hits, a.mouse); t.Kind == ui.KindRow {
		return false
	}
	now := uv.Pos(m.X, m.Y)
	for _, r := range a.probes {
		if a.seenAt.In(r) != now.In(r) {
			return false
		}
	}
	return true
}

func (a *App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		a.jump = nil                         // its matches are where the screen was
		a.menu = nil                         // it was placed by the pointer, on a tree that is laid out again
		if a.ddl != nil && a.ddl.col != "" { // under a cell that has moved
			a.ddl = nil
		}
	case connected:
		return a, a.gotConnected(msg)
	case connTested:
		a.gotConnTested(msg)
	case sessionDialed:
		return a, a.gotSessionDialed(msg)
	case catalogMsg:
		return a, a.gotCatalog(msg)
	case keysMsg:
		return a, a.gotKeys(msg)
	case foundMsg:
		return a, a.gotFound(msg)
	case filterDue:
		return a, a.gotFilterDue(msg)
	case keyDone:
		return a, a.gotKeyDone(msg)
	case cmdsMsg:
		return a, a.gotCommands(msg)
	case keyExists:
		a.gotKeyExists(msg)
	case hintDue:
		return a, a.gotHintDue(msg)
	case hintMsg:
		return a, a.gotHints(msg)
	case namespaceFound:
		return a, a.gotNamespaceFound(msg)
	case pageMsg:
		return a, a.gotPage(msg)
	case countMsg:
		a.gotCount(msg)
	case autoMsg:
		return a, a.gotAuto(msg)
	case quickMsg:
		return a, a.gotQuick(msg)
	case previewDue:
		return a, a.fetchDDL(msg)
	case ddlMsg:
		a.gotDDL(msg)
	case quickRerun:
		return a, a.gotQuickRerun(msg)
	case colsMsg:
		return a, a.gotCols(msg)
	case objectsMsg:
		return a, a.gotObjects(msg)
	case funcMsg:
		return a, a.gotFunction(msg)
	case saveMsg:
		return a, a.gotSave(msg)
	case exportDone:
		return a, a.gotExport(msg)
	case dropped:
		return a, a.gotDropped(msg)
	case txDone:
		return a, a.gotTx(msg)
	case connEvent:
		return a, a.gotConnEvent(msg)
	case autosave:
		if msg.ver == msg.t.ver {
			if err := msg.t.write(false); err != nil {
				return a, a.saveFailed(err)
			}
		}
	case externalDone:
		return a, a.gotExternal(msg)
	case runDone:
		return a, a.gotRun(msg)
	case pageDone:
		return a, a.gotResultPage(msg)
	case formatDone:
		return a, a.gotFormat(msg)
	case spinTick:
		return a, a.spinNext()
	case stateErr:
		return a, a.showToast(msg.err.Error())
	case tipTick:
		a.nextTip()
		return a, tipTimer()
	case toastExpired:
		if msg.seq == a.toastSeq {
			a.toast = ""
		}
	case flashDone:
		if a.flash != nil && msg.seq == a.flash.seq {
			a.flash = nil
		}
	case tea.ClipboardMsg: // what a "+p asked for (F3.38)
		if w := a.clipWait; w != nil {
			a.clipWait = nil
			return a, w(msg.Content)
		}
	case tea.KeyPressMsg:
		return a, a.press(keymap.FromTea(msg.Key()))
	case tea.PasteMsg:
		return a, a.paste(msg.Content)
	case tea.MouseMotionMsg:
		m := msg.Mouse()
		a.mouse = uv.Pos(m.X, m.Y)
		if a.drag != nil { // the border follows the pointer
			a.win().Root = a.win().Root.setRatio(a.drag.idx, a.drag.ratioAt(a.mouse))
		}
		if a.dragTree {
			a.win().TreeW = treeWidth(a.mouse.X, a.w)
		}
		if a.dragCol != nil {
			a.dragColTo(a.mouse.X)
		}
		switch {
		case a.dragText == ui.ValuePane && a.value != nil:
			n, v := a.valueAt(a.mouse)
			a.value.ed.Drag(n, v)
		case a.dragText > 0:
			if t, n, v := a.queryAt(a.dragText, a.mouse); t != nil {
				t.ed.Drag(n, v)
			} else if jv, n, v := a.jsonAt(a.dragText, a.mouse); jv != nil {
				jv.ed.Drag(n, v)
			}
		}
		if a.dragWhere != 0 {
			a.whereDrag(a.dragWhere, a.mouse)
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
		a.drag, a.dragTree, a.dragCol, a.dragText, a.dragWhere = nil, false, nil, 0, 0
	case tea.MouseClickMsg:
		a.jump = nil // a click goes where it is, the jump over (F10.8)
		switch m := msg.Mouse(); m.Button {
		case tea.MouseLeft:
			a.shiftClick = m.Mod.Contains(tea.ModShift) // the terminal says it, or does not (§7.6「表头」)
			return a, a.click(uv.Pos(m.X, m.Y))
		case tea.MouseRight: // the menu of a tree node (F8.23)
			a.rightClick(uv.Pos(m.X, m.Y))
		case tea.MouseMiddle: // a table in the tree opens in a new tab (§7.8)
			if t, _ := ui.HitAt(a.hits, uv.Pos(m.X, m.Y)); t.Kind == ui.KindNode {
				a.treeGo(t.I)
				return a, a.run("tree.open.tab", 0)
			}
		default: // other buttons do nothing
		}
	case tea.MouseWheelMsg:
		a.wheel(msg.Mouse())
	case keyTimeout:
		cmd := a.dispatch(a.res.Timeout(msg.seq))
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
func (a *App) press(k keymap.Key) tea.Cmd { return a.feed(k, true) }

// feed is press with user maps on or off: a mapping's right-hand side comes
// back from the resolver to go in here key by key, maps off (noremap), each
// meeting the editor as it then is (§6.6).
func (a *App) feed(k keymap.Key, maps bool) tea.Cmd {
	if a.ddl != nil && a.ddl.col != "" { // a column's DDL goes with the next key, which is then pressed as it is; esc and C-c are no more (F9.8)
		if a.ddl = nil; k == keymap.Esc || k == "<C-c>" {
			return nil
		}
	}
	a.plainQueries() // one a key before it made, by a mapping, has no modes as this one meets it
	if a.paneNumbers {
		a.jumpToPane(k)
		return nil
	}
	// An editor waiting for the rest of a command takes the keys itself,
	// user maps and all, C-c as esc (§6.4): f<Space>x is no leader key.
	if ed := a.vim(); ed != nil && ed.Pending() != "" {
		if k == "<C-c>" {
			k = keymap.Esc
		}
		return a.dispatch([]keymap.Result{{Keys: []keymap.Key{k}}})
	}
	out, wait := a.res.Feed(a.context(), k, maps)
	if n := len(out); n > 0 && out[n-1].Again != "" { // the shorter binding of an ambiguous pair ran: the key that ended it is read in what that made
		again := out[n-1].Again
		return tea.Batch(a.dispatch(out[:n-1]), a.feed(again, maps))
	}
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
	if a.askName != nil { // so is a click off the name's (F6.5)
		a.askName = nil
		return nil
	}
	if a.winRename != nil { // a click off its input is esc, and no more (§5)
		if t.Action != fmt.Sprintf("window.select %d", a.sess.Active) {
			a.winRename = nil
		}
		return nil
	}
	if !ok || t != (ui.Target{}) && t.Kind != ui.KindRow && !strings.HasPrefix(t.Action, "cell.") { // not the cell's edit (a blank target), its options or ▾: that ends first (§10.1)
		if !a.endEdit() { // or stays, a click going nowhere (§10.7)
			return nil
		}
	}
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
	case ui.KindBackdrop: // outside an overlay: close it; outside a confirm box: no
		var picked tea.Cmd
		if a.cols != nil {
			picked = a.closeCols()
		}
		a.whichKey, a.paneNumbers, a.palette, a.drop, a.cols, a.confirm, a.keyHelp, a.sessList, a.menu, a.ddl = false, false, nil, nil, nil, nil, nil, nil, nil, nil
		a.res.Reset()
		return picked
	case ui.KindSegment:
		if a.form != nil {
			a.formSegment(t.Pane, t.I)
		}
	case ui.KindConn: // a row of the manager's list: selected, and the list has the keys (F8.16)
		if a.form != nil {
			a.selectConn(t.I)
			a.form.list = true
		}
	case ui.KindRow:
		switch tab, c := a.typingTab(), a.completing(); {
		case a.menu != nil:
			return a.menuRun(t.I)
		case tab != nil && tab.cell != nil: // an option applies at once (§10.2)
			if os := tab.options(); t.I < len(os) {
				tab.applyOption(os[t.I])
			}
		case c != nil:
			c.sel = t.I
			return a.acceptCompletion()
		case a.palette != nil:
			return a.paletteRun(t.I, false)
		case a.form != nil && t.Action == "form.pick": // a group of the list (F10.6)
			a.form.groupSel = t.I
			a.groupAccept()
		case a.form != nil: // over the session list, as it has the keys (§6.4)
			a.formClick(t.I)
		case a.sessList != nil: // a row: there, then ↵ (§7.8)
			a.sessionsAttach(t.I)
		case tab != nil && tab.hist != nil:
			es, _ := a.histEntries(tab)
			return a.histApply(tab, histRow(es, t.I))
		case a.drop != nil:
			return a.dropPick(t.I)
		case a.cols != nil && a.cols.order: // that column alone, as ↵ (F12.9)
			return a.orderPick(t.I)
		case a.cols != nil: // a column's row: show or hide it (Q-04)
			a.colsToggle(t.I)
		}
	case ui.KindNode: // a node's row: its ↵ (§7.8)
		a.focusPane(t.Pane)
		a.treeGo(t.I)
		return a.run("tree.open", 0)
	case ui.KindFold: // its ▸ / ▾: open or close, whatever ↵ would do
		a.focusPane(t.Pane)
		a.treeGo(t.I)
		if n, ok := a.treeNode(); ok {
			return a.treeFold(n, !n.Open)
		}
	case ui.KindItem:
		if a.keyHelp != nil {
			if next := a.keys.Next(a.keyHelp.ctx, a.keyHelp.prefix); t.I < len(next) {
				return a.keyHelpKey(next[t.I].Key)
			}
		}
		if next := a.res.Next(); t.I < len(next) {
			return a.press(next[t.I].Key)
		}
	case ui.KindButton:
		return a.run(t.Action, 0)
	case ui.KindCell:
		cmd := tea.Batch(focus(), a.run(t.Action, 0))
		if double { // G-02
			return tea.Batch(cmd, a.run("grid.edit", 0))
		}
		return cmd
	case ui.KindHint, ui.KindRowNo:
		if w := dataOf(a.focused()); t.Action == "grid.where" && t.Pane == a.focused().ID && w != nil && w.typing == "where" { // in it already: the cursor goes there (F3.39)
			a.whereClick(a.focused(), w, p)
			return nil
		}
		return tea.Batch(focus(), a.run(t.Action, 0))
	case ui.KindTitle:
		if double {
			return tea.Batch(focus(), a.run("pane.zoom", 0))
		}
		return focus()
	case ui.KindPane:
		return focus()
	case ui.KindText: // the cursor goes there; dragging from there selects (§11)
		if t.Pane == ui.ValuePane {
			if a.valueActive() {
				n, v := a.valueAt(p)
				a.value.ed.Click(n, v)
				a.dragText = ui.ValuePane
			}
			return nil
		}
		a.focusPane(t.Pane)
		if c, n, v := a.queryAt(t.Pane, p); c != nil {
			c.ed.Click(n, v)
			a.dragText, c.comp = t.Pane, nil
		} else if jv, n, v := a.jsonAt(t.Pane, p); jv != nil {
			jv.ed.Click(n, v)
			a.dragText = t.Pane
		}
	case ui.KindHead:
		return a.headClick(t.Pane, t.I, a.shiftClick)
	case ui.KindTab:
		a.focusPane(t.Pane)
		if p := a.win().pane(t.Pane); p != nil {
			selectTab(p, t.I)
		}
	case ui.KindTreeEdge:
		a.dragTree = true
	case ui.KindColEdge:
		a.startColDrag(t.Pane, t.I, p.X)
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
	a.jump = nil // what it matched moves
	notch := map[tea.MouseButton]int{tea.MouseWheelUp: -1, tea.MouseWheelDown: 1}[m.Button]
	if a.ddl != nil && a.ddl.col != "" && a.topOverlay() == "ddl" { // the column's box goes with the wheel as with a key, which then turns (F9.8)
		a.ddl = nil
	}
	down, right := wheelMove(m)
	switch a.topOverlay() {
	case "":
	case "value": // the panes under it are not for the wheel; it scrolls the value (F12.2)
		if notch != 0 {
			a.valueWheel(notch)
		}
		return
	case "ddl": // the table's DDL scrolls
		a.scrollDDL(notch*wheelStep, false)
		return
	case "keyhelp": // a notch a row (§6.5)
		a.scrollKeyHelp(notch, false)
		return
	case "palette": // over the result's table, that scrolls; over the preview, that; else the list (F17.6)
		_, ms := a.paletteMatches()
		_, _, area := a.paletteBox(len(ms))
		at := uv.Pos(m.X, m.Y)
		over, _ := ui.HitAt(a.hits, at)
		switch {
		case a.quickShows() && at.In(area):
			q := a.palette.quick
			q.top, q.left, _, _ = a.quickView(q).Grid.Scroll(area, down*wheelStep, right)
		case !a.quickShows() && at.In(area):
			if ed := a.previewEd(); ed != nil {
				a.previewSize(ed)
				ed.Scroll(notch * wheelStep)
			} else {
				a.scrollPreviewBy(notch * wheelStep)
			}
		case over.Kind == ui.KindRow: // the list's
			a.paletteMove(notch * wheelStep)
		}
		return
	default: // the others are modal: the panes under them are not for the wheel (F13.7)
		return
	}
	if t, _ := ui.HitAt(a.hits, uv.Pos(m.X, m.Y)); strings.HasPrefix(t.Action, "cell.seg ") { // over a time's part: step it (§10.2)
		d := map[tea.MouseButton]string{tea.MouseWheelUp: "cell.inc ", tea.MouseWheelDown: "cell.dec "}[m.Button]
		if d != "" {
			a.run(d+strings.TrimPrefix(t.Action, "cell.seg "), 0)
		}
		return
	}
	if !a.endEdit() { // the cell it is in may scroll away (§10.1)
		return
	}
	for id, r := range a.layout() {
		if uv.Pos(m.X, m.Y).In(r) {
			a.scrollPane(id, down, right)
		}
	}
}

// wheelMove is the notches a wheel event scrolls down and to the right: Shift
// turns the vertical wheel horizontal, the other buttons do not scroll.
func wheelMove(m tea.Mouse) (down, right int) {
	switch m.Button {
	case tea.MouseWheelUp:
		down = -1
	case tea.MouseWheelDown:
		down = 1
	case tea.MouseWheelLeft:
		right = -1
	case tea.MouseWheelRight:
		right = 1
	default: // the other buttons do not scroll
	}
	if m.Mod.Contains(tea.ModShift) {
		down, right = 0, down
	}
	return down, right
}

// paste types s into the input that has the keys, each its own way, a
// newline as a space: they are single-line (§10.1). On a grid in NORMAL it
// starts editing the current cell with s; a query takes it whole, as
// text in any mode (§11).
func (a *App) paste(s string) tea.Cmd {
	if a.jump != nil { // text for the pattern, whatever has the focus (F10.8)
		a.jumpType(strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(s))
		return nil
	}
	if a.ddl != nil && a.ddl.col != "" { // the column's box goes with it, as with a key, and what is pasted is then typed (F9.8)
		a.ddl = nil
	}
	if a.valueActive() {
		a.valueSize()
		return a.valueDid(a.value.ed.Paste(s))
	}
	if t := a.focusedQuery(); t != nil {
		a.queryView(a.focused(), t)
		return a.queryDid(t, t.ed.Paste(s))
	}
	s = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(s)
	if t := dataOf(a.focused()); t != nil && t.typing == "where" && !a.overlaid() { // text in any mode, as a query takes it (F3.39)
		return a.whereDid(t, t.ed.Paste(s))
	}
	if jv := a.jsonSync(a.focused()); jv != nil && !a.overlaid() { // a read only text says so; no cell changes behind it (F14.5)
		return a.jsonDid(a.focused(), jv, jv.ed.Paste(s))
	}
	if a.mode() == keymap.Normal {
		return a.editCell(&s)
	}
	on := a.autoPairs // what is pasted goes in as it is: no pairs, nothing stepped over (§7.9)
	a.autoPairs = false
	defer func() { a.autoPairs = on }()
	return a.dispatch([]keymap.Result{{Keys: keymap.Typed(s)}})
}

// dispatch runs what the keymap resolved: actions through the registry, and
// unclaimed keys to the focused widget.
func (a *App) dispatch(out []keymap.Result) tea.Cmd {
	var cmds []tea.Cmd
	for _, r := range out {
		if r.Action != "" {
			if c := a.focusedQuery(); r.Action == "jump.start" && c != nil && vimMode(c.ed.Mode()) == keymap.Visual { // a query's VISUAL has its own s
				cmds = append(cmds, a.queryKey(a.focused(), c, "s"))
				continue
			}
			if jv := jsonViewOf(a.focused()); r.Action == "json.yank" && jv != nil && vimMode(jv.ed.Mode()) == keymap.Visual { // so has a JSON view its own Y (F16.9)
				cmds = append(cmds, a.jsonKey(a.focused(), "Y"))
				continue
			}
			cmds = append(cmds, a.run(r.Action, r.Count))
			continue
		}
		for _, k := range r.Map {
			cmds = append(cmds, a.feed(k, false))
		}
		for _, k := range r.Keys { // unbound keys go to the input that has them
			cmds = append(cmds, a.typeInto(k))
		}
	}
	return tea.Batch(cmds...)
}

// typeInto gives key k, which no binding took, to the input that has the keys:
// the overlay on top (topOverlay), else the pane's.
func (a *App) typeInto(k keymap.Key) tea.Cmd {
	if a.jump != nil {
		return a.jumpKey(k)
	}
	ov := a.topOverlay()
	switch ov {
	case "":
	case "value":
		return a.valueKey(k)
	case "keyhelp":
		return a.keyHelpKey(k)
	case "palette":
		if a.previewEd() != nil { // the preview has the focus: its vim (F17.6)
			return a.previewKey(k)
		}
		return a.paletteKey(k)
	case "sessions": // a name typed in place
		if a.sessList.rename != nil {
			return a.renameKey(k)
		}
	case "form": // the manager's list has the keys, and no text (F13.4)
		if !a.form.list {
			a.formKey(k)
		}
	case "rename":
		if a.winRename != nil {
			return a.renameWindowKey(k)
		}
		return a.askNameKey(k)
	case "dropdown":
		a.dropKey(k)
	case "cols":
		if a.cols.typing {
			a.colsFilterKey(k)
		}
	default: // a box with no text to type: the key is none of its
	}
	if ov != "" {
		return nil
	}
	switch t := a.typingTab(); {
	case a.win().tree.filtering:
		return a.filterKey(k)
	case t != nil:
		return a.typeKey(t, k)
	case a.focusedQuery() != nil: // what the query's keymap leaves goes to its vim (§6.4)
		return a.queryKey(a.focused(), a.focusedQuery(), k)
	case jsonViewOf(a.focused()) != nil: // and a grid's JSON view's (F14.5)
		return a.jsonKey(a.focused(), k)
	}
	return nil
}

// mode is derived from state, never stored (§3 principle 3).
func (a *App) mode() keymap.Mode {
	if a.jump != nil { // a pattern typed, a letter jumps (F10.8)
		return keymap.Insert
	}
	if ov := a.topOverlay(); ov != "" { // an overlay has the keys (§7.8); the value editor's mode is its editor's (F12.2)
		return a.overlayContext(ov).Mode
	}
	switch t, ed := a.typingTab(), a.vim(); {
	case t != nil && t.hist != nil:
		return keymap.Command
	case ed != nil: // a query's vim, or a WHERE's (F3.39)
		return vimMode(ed.Mode())
	case a.win().tree.filtering, t != nil:
		return keymap.Insert
	}
	if s := gridStateOf(a.focused()); s != nil && s.visual { // a table's VISUAL (F10.7)
		return keymap.Visual
	}
	return keymap.Normal
}

// context tells the keymap which scopes apply to the next key (§6.4).
func (a *App) context() keymap.Context {
	if a.jump != nil {
		return keymap.Context{Overlay: "jump", Mode: keymap.Insert}
	}
	if ov := a.topOverlay(); ov != "" {
		return a.overlayContext(ov)
	}
	switch typing := a.typingTab(); {
	case typing != nil && typing.hist != nil: // filtered by the WHERE typed
		return keymap.Context{Overlay: "where", Focus: []string{"input"}, Mode: keymap.Command}
	case typing != nil && typing.comp != nil:
		return keymap.Context{Overlay: "complete", Focus: []string{"input"}, Mode: keymap.Insert, Plain: typing.typing == "where" && a.plain}
	case a.focusedQuery() != nil && a.focusedQuery().comp != nil: // ↵ and esc are its vim's, as a WHERE's
		return keymap.Context{Overlay: "complete", Focus: []string{"query", "queryinsert"}, Mode: keymap.Insert, Plain: a.plain}
	case typing != nil && typing.cell != nil:
		c := keymap.Context{Focus: []string{"cell"}, Mode: keymap.Insert}
		if !typing.cell.folded && len(typing.options()) > 0 {
			c.Overlay = "options"
			if typing.cellKind() != ui.NotTime { // a time's parts, its options in a row (§10.2)
				c.Overlay = "segments"
			}
		}
		if typing.cell.add != nil { // a row added's: Tab goes on to the next field (F3.33)
			c.Overlay, c.Under = "newrow", c.Overlay
		}
		return c
	case typing != nil && typing.typing == "where" && a.plain:
		return keymap.Context{Focus: []string{"input"}, Mode: keymap.Insert, Plain: true}
	case typing != nil && typing.typing == "where" && vimMode(typing.ed.Mode()) != keymap.Insert:
		// its vim's NORMAL or VISUAL: user maps as any pane's, esc and ↵ in
		// VISUAL the vim's, as a query's (F3.39)
		c := keymap.Context{Mode: vimMode(typing.ed.Mode())}
		if c.Mode == keymap.Normal {
			c.Focus = []string{"wherenormal"}
		}
		return c
	case a.win().tree.filtering, typing != nil:
		return keymap.Context{Focus: []string{"input"}, Mode: keymap.Insert}
	}
	if jv := jsonViewOf(a.focused()); jv != nil { // a grid's JSON view: its vim, and the few keys that are its own (F14.5)
		return a.jsonContext(jv)
	}
	scope := a.paneScope()
	var firm string
	var over []string // records selected: d, y and esc of the select scope over the grid's keys (F10.7)
	if s := gridStateOf(a.focused()); s != nil && s.selecting() && (scope == "grid" || scope == "result") {
		over, firm = []string{"select"}, "select"
	}
	mode := a.mode()
	if scope != "query" && mode == keymap.Visual { // a table's VISUAL is NORMAL with the select scope over it (F10.7)
		mode = keymap.Normal
	}
	if scope == "result" { // over grid's keys, which its tables move by (§6.4)
		return keymap.Context{Focus: append(over, "result", "grid"), Pane: "grid", Mode: mode, Firm: firm}
	}
	if scope == "query" && mode == keymap.Insert {
		over = append(over, "queryinsert")
	}
	return keymap.Context{Focus: append(over, scope), Pane: scope, Mode: mode, Plain: a.plain && scope == "query", Firm: firm}
}

// overlayContext is context() with overlay ov, topOverlay's, on top.
func (a *App) overlayContext(ov string) keymap.Context {
	switch ov {
	case "value":
		return keymap.Context{Overlay: "value", Mode: vimMode(a.value.ed.Mode()), Plain: a.plain}
	case "palette":
		if ed := a.previewEd(); ed != nil { // the preview's vim, and esc (F17.6)
			return keymap.Context{Overlay: "palettepreview", Mode: vimMode(ed.Mode()), Plain: a.plain}
		}
		if a.palette.comp != nil { // the palette's keys under the list's: its ↵ and esc see to the list first (§9.7)
			return keymap.Context{Overlay: "complete", Under: "palette", Focus: []string{"input"}, Mode: keymap.Command}
		}
	case "sessions":
		if a.sessList.rename != nil { // the name typed in place (§7.8)
			return keymap.Context{Focus: []string{"input"}, Mode: keymap.Insert}
		}
	case "form":
		switch {
		case a.form.list: // the list of connections (F8.16)
			return keymap.Context{Overlay: "conns", Mode: keymap.Command}
		case len(a.groupCands()) > 0: // the group field's list over the form's keys (F10.6)
			return keymap.Context{Overlay: "groups", Under: "form", Mode: keymap.Insert}
		}
		return keymap.Context{Overlay: "form", Mode: keymap.Insert} // its inputs: unbound keys are text
	case "rename":
		return keymap.Context{Focus: []string{"input"}, Mode: keymap.Insert}
	case "cols":
		if a.cols.typing { // its filter: unbound keys are text
			return keymap.Context{Focus: []string{"input"}, Mode: keymap.Command}
		}
		return keymap.Context{Overlay: a.cols.scope(), Mode: keymap.Command}
	}
	return keymap.Context{Overlay: ov, Mode: keymap.Command}
}

// paneScope is the keymap scope of the focused pane: its current tab's.
func (a *App) paneScope() string {
	switch p := a.focused(); {
	case p == a.win().Tree:
		return "tree"
	case p == a.win().Result:
		return "result"
	case dataOf(p) != nil:
		return "grid"
	case queryOf(p) != nil:
		return "query"
	}
	return "landing" // a new tab, or none (§6.4)
}

func (a *App) focused() *Pane {
	if p := a.win().pane(a.win().Focus); p != nil {
		return p
	}
	return a.win().Tree
}

func (a *App) showToast(s string) tea.Cmd { return a.showToastFor(s, toastTTL) }

// showToastFor is showToast for ttl, as long as it takes the user to do what it asks.
func (a *App) showToastFor(s string, ttl time.Duration) tea.Cmd {
	a.toast = toastLine(s) // one row: a driver's error may have many (§7.8)
	a.toastSeq++
	seq := a.toastSeq
	return tea.Tick(ttl, func(time.Time) tea.Msg { return toastExpired{seq} })
}

func (a *App) View() tea.View {
	if a.reuse {
		a.reuse = false
		return a.seen
	}
	f := a.drawOn(a.rendered.NewFrame(a.w, a.h, a.theme))
	a.hits, a.probes, a.seenAt = f.Hits, f.Probes, a.mouse // clicks are looked up in what was drawn
	v := tea.NewView(f.RenderReusing(&a.rendered))
	if c := f.Cursor; c != nil { // the terminal's own cursor, which input methods follow (§12)
		v.Cursor = tea.NewCursor(c.X, c.Y)
		v.Cursor.Shape = tea.CursorBar
		if ed := a.vim(); ed != nil && ed.Mode() != editor.Insert { // vim's: a block, a bar in INSERT, a line under in REPLACE (§11)
			v.Cursor.Shape = tea.CursorBlock
			if ed.Mode() == editor.Replace {
				v.Cursor.Shape = tea.CursorUnderline
			}
		}
	}
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	a.seen, a.fresh = v, true
	return v
}
