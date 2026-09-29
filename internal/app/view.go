package app

import (
	"fmt"
	"image/color"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/editor"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// sidebarWidth is the ⟨0⟩ schema sidebar's default width, borders included (§7.8).
func sidebarWidth(w int) int {
	if w < 100 {
		return 24
	}
	return 32
}

// treeWidth is the width a dragged sidebar gets in a window w wide: 16
// columns at least, half the window at most (§7.8). The dragged width itself
// is kept, so a window that widens again gets it back.
func treeWidth(want, w int) int { return min(max(want, 16), w/2) }

// hints joins several actions' keys, e.g. "hjkl" or "gt/gT"; "" if any is unbound.
func (a *App) hints(scope, sep string, actions ...string) string {
	ks := make([]string, len(actions))
	for i, act := range actions {
		if ks[i] = a.keys.Hint(act, scope); ks[i] == "" {
			return ""
		}
	}
	return strings.Join(ks, sep)
}

// thinBarWidth is the folded sidebar: two borders around one column (§7.8).
const thinBarWidth = 3

// window is everything above the status bar.
func (a *App) window() uv.Rectangle { return uv.Rect(0, 0, max(a.w, 0), max(a.h-1, 0)) }

// sidebarRect is where the ⟨0⟩ sidebar, open or folded, goes.
func (a *App) sidebarRect() uv.Rectangle {
	side := a.window()
	w := sidebarWidth(a.w)
	if a.win().TreeW != 0 {
		w = treeWidth(a.win().TreeW, a.w)
	}
	if !a.win().TreeOpen {
		w = thinBarWidth
	}
	side.Max.X = min(side.Min.X+w, side.Max.X)
	return side
}

// mainArea is what the split tree lays out over: right of the sidebar.
func (a *App) mainArea() uv.Rectangle {
	main := a.window()
	main.Min.X = min(a.sidebarRect().Max.X+1, main.Max.X)
	return main
}

// layout places the sidebar and every pane of the current window, keyed by
// pane ID. The last row is the status bar.
func (a *App) layout() map[int]uv.Rectangle {
	win := a.win()
	if win.Zoom != 0 { // the zoomed pane takes the whole window, sidebar included
		return map[int]uv.Rectangle{win.Zoom: a.window()}
	}
	rects := map[int]uv.Rectangle{win.Tree.ID: a.sidebarRect()}
	win.Root.Rects(a.mainArea(), rects)
	return rects
}

func (a *App) render() *ui.Frame {
	th := a.theme
	f := ui.NewFrame(a.w, a.h, th)
	f.Fill(f.Bounds(), uv.Style{Fg: th.Fg, Bg: th.Bg})
	if a.w <= 0 || a.h <= 0 {
		return f
	}
	f.Mouse = a.mouse
	rects := a.layout()
	if r, ok := rects[a.win().Tree.ID]; ok {
		a.drawSidebar(f, r)
	}
	for i, p := range a.win().Root.Leaves() {
		if r, ok := rects[p.ID]; ok {
			a.drawPane(f, p, i+1, r)
		}
	}
	if a.win().Zoom == 0 {
		for _, h := range a.win().Root.handles(a.mainArea()) {
			f.Region(h.rect, ui.Target{Kind: ui.KindBorder, I: h.idx})
		}
		if a.win().TreeOpen { // the gap right of the sidebar sets its width
			f.Region(uv.Rect(a.sidebarRect().Max.X, 0, 1, a.window().Dy()), ui.Target{Kind: ui.KindTreeEdge})
		}
	}
	if a.paneNumbers {
		a.drawPaneNumbers(f, rects)
	}

	y := a.h - 1
	a.statusLine().Draw(f, uv.Rect(0, y, a.w, 1))
	if a.whichKey {
		a.whichKeyOverlay().Draw(f, uv.Rect(0, 0, a.w, y))
	}
	if t := a.typingTab(); t != nil { // the WHERE's lists (§9.7), a cell's options (§10.2)
		switch p := a.focused(); {
		case t.cell != nil && !t.cell.folded:
			a.drawCellMenu(f, p, t)
		case t.hist != nil:
			v, box, rows := a.histView(p, t)
			v.Draw(f, box, rows)
		case t.comp != nil:
			at := a.whereAt(p, t)
			at.X += ui.Width(t.where.Text[:t.comp.start]) // under what it completes
			// ponytail: off by the input's scroll once the text outgrows it
			v, box, rows := a.completeView(t.comp, at)
			v.Draw(f, box, rows)
		}
	}
	if a.drop != nil {
		d := a.dropView()
		box, rows := a.dropBox(d)
		if c := d.Draw(f, box, rows); c.X >= 0 {
			f.Cursor = &c
		}
	}
	if a.cols != nil {
		d := a.colsView()
		box, rows := a.colsBox(d)
		if c := d.Draw(f, box, rows); c.X >= 0 {
			f.Cursor = &c
		}
	}
	if a.palette != nil {
		if c := a.paletteView().Draw(f, a.window()); c.X >= 0 {
			f.Cursor = &c
			if comp := a.palette.comp; comp != nil { // under what it completes, which ends at the cursor
				in, at := a.palette.input, c
				at.X -= ui.Width(in.Text[comp.start:in.Pos])
				v, box, rows := a.completeView(comp, at)
				v.Draw(f, box, rows)
			}
		}
	}
	if c := a.confirm; c != nil {
		ui.Confirm{
			Text:   c.text,
			YesKey: a.keys.Hint("confirm.yes", "confirm"), Yes: c.yes,
			NoKey: a.keys.Hint("confirm.no", "confirm"), No: "取消",
		}.Draw(f, a.window())
	}
	if a.toast != "" && y > 0 { // over the palette's mask too: dimmed, it can hardly be read (§7.5)
		t := " " + a.toast + " "
		f.Text(max(a.w-ui.Width(t)-1, 0), y-1, a.w, t, uv.Style{Fg: th.Warn, Bg: th.Bar})
	}
	return f
}

// drawPane paints pane p, ⟨n⟩, over r: the title, the hints and the body
// its current tab's type has (§5), a landing page with no tab or a new one.
func (a *App) drawPane(f *ui.Frame, p *Pane, n int, r uv.Rectangle) {
	th := f.Theme
	icon, word := a.tabIcon(p.tab())
	b := ui.Block{
		Num:     a.icons.Number(n),
		Icon:    icon,
		Title:   a.label(word),
		Object:  p.Object(),
		Focused: a.win().Focus == p.ID,
		Pane:    p.ID,
	}
	var tabHints []ui.Hint
	switch {
	case consoleOf(p) != nil:
		// Drawn left to right; Prio says what goes first when space runs out
		// (§7.8). The schema dropdown comes before ▶ run in F3.11.
		b.Hints = append([]ui.Hint{
			{Label: "▶ run", Action: "console.run", Button: true},
		}, bound(ui.Hint{Key: a.keys.Hint("console.run", "console"), Action: "console.run", Prio: 2, Attached: true})...)
		tabHints = bound(
			ui.Hint{Key: a.keys.Hint("console.format", "console"), Label: "format", Action: "console.format"},
			ui.Hint{Key: a.hints("normal", "/", "tab.next", "tab.prev")},
		)
	case dataOf(p) != nil:
		tabHints = bound(
			ui.Hint{Key: a.hints("grid", "", "grid.left", "grid.down", "grid.up", "grid.right")},
			ui.Hint{Key: a.keys.Hint("grid.edit", "grid"), Label: "edit", Action: "grid.edit"},
			ui.Hint{Key: a.keys.Hint("grid.transpose", "grid"), Label: "转置", Action: "grid.transpose"},
			ui.Hint{Key: a.hints("normal", "/", "tab.next", "tab.prev")},
		)
	}
	paneRegions(f, p.ID, r)
	in := b.Draw(f, r)
	if in.Empty() {
		return
	}
	tabs := ui.Tabs{Cur: p.Cur, Prev: p.Prev, Hints: tabHints, Pane: p.ID}
	for i := range p.Tabs {
		ic, _ := a.tabIcon(&p.Tabs[i])
		tabs.Names, tabs.Icons = append(tabs.Names, p.Tabs[i].Name), append(tabs.Icons, ic)
	}
	tabs.Draw(f, uv.Rect(in.Min.X, in.Max.Y-1, in.Dx(), 1))
	if p.tab().landing() { // what it can become (§5「引导页」)
		ui.Landing{Pane: p.ID, Buttons: []ui.LandingButton{
			{Icon: a.icons.Table, Label: "打开表", Key: a.keys.Hint("tab.table", "landing"), Action: "tab.table"},
			{Icon: a.icons.Console, Label: "新建 console", Key: a.keys.Hint("console.new", "landing"), Action: "console.new"},
		}}.Draw(f, bodyRect(r))
		return
	}
	if c := consoleOf(p); c != nil {
		view, body := a.consoleView(p, c)
		if cur := view.Draw(f, body); cur.X >= 0 && b.Focused && a.focusedConsole() == c {
			f.Cursor = &cur
		}
		return
	}
	t := dataOf(p)
	if t == nil {
		return
	}
	if c := a.queryBar(p, t).Draw(f, bodyRect(r)); c.X >= 0 {
		f.Cursor = &c
	}
	body := gridRect(r)
	switch {
	case t.err != "": // what the database said, in place of the table (§7.6)
		f.Text(body.Min.X+1, body.Min.Y, body.Max.X-1, t.err, uv.Style{Fg: th.Error, Bg: th.PaneBg})
	case t.page.Cols != nil:
		if c := a.grid(p, t).Draw(f, body); c.X >= 0 {
			f.Cursor = &c
		}
	}
}

// drawSidebar paints the ⟨0⟩ schema tree (§7.8).
func (a *App) drawSidebar(f *ui.Frame, r uv.Rectangle) {
	win := a.win()
	if !win.TreeOpen {
		a.drawThinBar(f, r)
		return
	}
	paneRegions(f, win.Tree.ID, r)
	b := ui.Block{ // the session's: the name goes before SPC b (§7.8)
		Num:         a.icons.Number(0),
		Icon:        a.icons.Conn,
		Object:      a.sess.Name,
		ObjectFirst: true,
		Hints:       bound(ui.Hint{Key: a.keys.Hint("tree.toggle", "normal"), Action: "tree.toggle"}),
		Focused:     win.Focus == win.Tree.ID,
		Pane:        win.Tree.ID,
	}
	in := b.Draw(f, r)
	ns, matches := a.treeNodes()
	t := ui.Tree{
		Total:     len(a.sess.Tables),
		Matches:   matches,
		Filter:    win.tree.filter,
		Filtering: win.tree.filtering,
		Focused:   b.Focused,
		Icons:     a.icons,
		Hints: bound(
			ui.Hint{Key: a.hints("tree", "/", "tree.down", "tree.up"), Label: "move"},
			ui.Hint{Key: a.keys.Hint("tree.open", "tree"), Label: "open", Action: "tree.open"},
			ui.Hint{Key: a.keys.Hint("tree.open.tab", "tree"), Label: "tab", Action: "tree.open.tab"},
		),
		Pane: win.Tree.ID,
	}
	t.Cursor, t.Top = a.treeAt(ns)
	for _, n := range ns {
		t.Nodes = append(t.Nodes, n.TreeNode)
	}
	if c := t.Draw(f, in); c.X >= 0 {
		f.Cursor = &c
	}
}

// bound drops hints whose action has no key.
func bound(hs ...ui.Hint) []ui.Hint {
	out := hs[:0]
	for _, h := range hs {
		if h.Key != "" {
			out = append(out, h)
		}
	}
	return out
}

// label is s where the icons need words beside them (ascii), else nothing:
// a Nerd icon says it by itself (§7.7).
func (a *App) label(s string) string {
	if a.icons.Labeled {
		return s
	}
	return ""
}

// tabIcon is the icon of tab t's type and the word ascii icons need beside
// it (§7.7): none for a landing tab, or no tab.
func (a *App) tabIcon(t *Tab) (ui.Icon, string) {
	switch {
	case t != nil && t.Data != nil:
		return a.icons.Table, "table"
	case t != nil && t.Console != nil:
		return a.icons.Console, "console"
	}
	return ui.Icon{}, ""
}

// iconRuns is " <icon>" and then tail as status bar runs, the icon in its own
// color if it has one (§7.7).
func iconRuns(i ui.Icon, st uv.Style, tail string) []ui.Run {
	return []ui.Run{{Text: " ", Style: st}, {Text: i.Text, Style: i.On(st)}, {Text: tail, Style: st}}
}

// Drop order of status bar segments when the bar is too narrow (§7.8); the
// mode extra info goes before all of them, the session name is cut last.
const (
	dropConn = iota + 1
	dropWindow
	dropCursor
)

// statusLine lays out the bottom bar (B-01~B-03, §7.8).
func (a *App) statusLine() ui.StatusLine {
	th, ic := a.theme, a.icons
	bar := func(fg color.Color) uv.Style { return uv.Style{Fg: fg, Bg: th.Bar} }
	mode := a.mode()
	var s ui.StatusLine

	sess := uv.Style{Fg: th.Bg, Bg: th.Focus, Attrs: uv.AttrBold}
	// ponytail: postgres only; pick the icon by a.sess.Engine when MySQL lands (M5).
	s.Left = []ui.Segment{{Runs: append(iconRuns(ic.Postgres, sess, " "),
		ui.Run{Text: a.sess.Name, Style: sess, Shrink: true},
		ui.Run{Text: " ▾ ", Style: sess},
	)}}
	for i, w := range a.sess.Windows {
		seg := ui.Segment{Runs: []ui.Run{{Text: fmt.Sprintf(" %d: %s ", i, w.Name), Style: bar(th.Dim)}}, Drop: dropWindow}
		if i == a.sess.Active {
			seg = ui.Segment{Runs: []ui.Run{{Text: fmt.Sprintf(" %d: %s* ", i, w.Name), Style: uv.Style{Fg: th.Fg, Bg: th.Border}}}}
		}
		s.Left = append(s.Left, seg)
	}

	pending := ui.Run{Text: "·", Style: bar(th.Dim)}
	ks := keymap.Display(a.res.Pending())
	if c := a.focusedConsole(); ks == "" && c != nil { // what the editor waits on: nvim's showcmd
		ks = c.ed.Pending()
	}
	if ks != "" {
		pending = ui.Run{Text: ks, Style: uv.Style{Fg: th.Warn, Bg: th.Bar, Attrs: uv.AttrBold}}
	}
	// At least 3 columns, left-aligned: SPC, g or a count don't shift the bar (§7.8).
	pending.Text += strings.Repeat(" ", max(3-ui.Width(pending.Text), 0))
	modeColor := [...]color.Color{keymap.Normal: th.Focus, keymap.Visual: th.Keyword, keymap.Insert: th.Warn, keymap.Command: th.Info}[mode]
	conn := uv.Style{Fg: th.Info, Bg: th.Sep}
	s.Right = []ui.Segment{
		{Runs: iconRuns(ic.Search, bar(th.Info), strings.TrimRight(" "+a.label(a.keys.Hint("palette.open", "global")), " ")+" "), Action: "palette.open"},
		{Runs: append(iconRuns(ic.Keys, bar(th.FgMuted), " "), pending, ui.Run{Text: " ", Style: bar(th.FgMuted)})},
	}
	switch t, c := a.typingTab(), a.focusedConsole(); {
	case t != nil && t.typing == "where":
		s.Info = "-- editing WHERE --" // §7.8
	case t != nil && t.cell != nil:
		s.Info = "-- editing " + t.cell.key.col + " --"
	case c != nil:
		s.Info = a.selectionInfo(c.ed)
	}
	// the cursor's row,col, with a table loaded in the focused pane (§7.8)
	if _, t, ok := a.focusedGrid(); ok && len(t.page.Rows) > 0 {
		at := fmt.Sprintf(" %d,%d ", t.shown.pageNo*t.shown.limit+t.row+1, t.col+1)
		s.Right = append(s.Right, ui.Segment{Runs: []ui.Run{{Text: at, Style: bar(th.FgMuted)}}, Drop: dropCursor})
	}
	s.Right = append(s.Right, ui.Segment{Runs: iconRuns(ic.Conn, conn, " "+a.sess.Addr+" "), Drop: dropConn})
	if a.busy > 0 { // a click cancels it, as C-c does (§8.3)
		busy := " busy "
		if k := a.keys.Hint("cancel", "global"); k != "" {
			busy += "· " + k + " 取消 "
		}
		s.Right = append(s.Right, ui.Segment{Runs: []ui.Run{{Text: busy, Style: bar(th.Warn)}}, Action: "cancel"})
	}
	name := strings.ToUpper(mode.String())
	if c := a.focusedConsole(); c != nil { // V-LINE, V-BLOCK and REPLACE in the colors of VISUAL and INSERT
		name = c.ed.Mode().String()
	}
	s.Right = append(s.Right, ui.Segment{Runs: []ui.Run{{Text: " " + name + " ", Style: uv.Style{Fg: th.Bg, Bg: modeColor, Attrs: uv.AttrBold}}}})
	return s
}

// selectionInfo is the status bar's word on a console's VISUAL (§7.8):
// "4 行 · ↵ run", "12 字符 · ↵ run", "3 行 × 4 列 · ↵ run".
func (a *App) selectionInfo(ed *editor.Editor) string {
	lines, n := ed.Size()
	var s string
	switch {
	case lines == 0:
		return ""
	case ed.Mode() == editor.VisualBlock:
		s = fmt.Sprintf("%d 行 × %d 列", lines, n)
	case n > 0:
		s = fmt.Sprintf("%d 字符", n)
	default:
		s = fmt.Sprintf("%d 行", lines)
	}
	if k := a.keys.Hint("console.run", "console"); k != "" {
		s += " · " + k + " run"
	}
	return s
}

// whichKeyOverlay lists what can follow the pending keys (§6.5).
func (a *App) whichKeyOverlay() ui.WhichKey {
	w := ui.WhichKey{Prefix: keymap.Display(a.res.Pending())}
	for _, n := range a.res.Next() {
		t := title(n.Action)
		switch {
		case n.RHS != nil:
			t = keymap.String(n.RHS)
		case n.Action == "":
			t = "…" // a longer prefix
		}
		w.Items = append(w.Items, ui.WhichKeyItem{Key: keymap.Display([]keymap.Key{n.Key}), Title: t})
	}
	return w
}

// drawThinBar is the folded sidebar (§7.8): "»" on top, then "schema · SPC b"
// down the middle column, one character per row.
func (a *App) drawThinBar(f *ui.Frame, r uv.Rectangle) {
	th := f.Theme
	f.Region(r, ui.Target{Kind: ui.KindButton, Action: "tree.toggle"}) // a click unfolds it
	in := ui.Block{Pane: a.win().Tree.ID}.Draw(f, r)
	if in.Empty() {
		return
	}
	text := "»schema"
	if k := a.keys.Hint("tree.toggle", "normal"); k != "" {
		text += " · " + k
	}
	y := in.Min.Y
	for _, ch := range text {
		st := uv.Style{Fg: th.Dim, Bg: th.PaneBg}
		if y == in.Min.Y {
			st.Fg = th.Focus
		}
		f.Text(in.Min.X, y, in.Max.X, string(ch), st)
		if y++; y >= in.Max.Y {
			break
		}
	}
}

// drawPaneNumbers is SPC q's overlay: each pane's ⟨n⟩ in its middle, until
// a digit jumps there.
func (a *App) drawPaneNumbers(f *ui.Frame, rects map[int]uv.Rectangle) {
	th := f.Theme
	f.Region(f.Bounds(), ui.Target{Kind: ui.KindBackdrop}) // outside the panes, a click only closes it (§7.4)
	for n, p := range a.panesByNumber() {
		r, ok := rects[p.ID]
		if !ok {
			continue
		}
		f.Region(r, ui.Target{Kind: ui.KindNumber, I: n}) // clicking a pane is pressing its number (§5)
		label := fmt.Sprintf(" %d ", n)
		x, y := r.Min.X+(r.Dx()-ui.Width(label))/2, r.Min.Y+r.Dy()/2
		f.Text(x, y, r.Max.X, label, uv.Style{Fg: th.Bg, Bg: th.Warn, Attrs: uv.AttrBold})
	}
}

// paneRegions makes a pane clickable (focus, wheel) and its title row
// double-clickable (zoom); hints drawn after it sit on top.
func paneRegions(f *ui.Frame, id int, r uv.Rectangle) {
	f.Region(r, ui.Target{Kind: ui.KindPane, Pane: id})
	f.Region(uv.Rect(r.Min.X, r.Min.Y, r.Dx(), 1), ui.Target{Kind: ui.KindTitle, Pane: id})
}
