package app

import (
	"fmt"
	"image/color"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// sidebarWidth is the ⟨0⟩ schema sidebar's width, borders included (§7.8).
func sidebarWidth(w int) int {
	if w < 100 {
		return 24
	}
	return 32
}

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
	}
	if a.paneNumbers {
		a.drawPaneNumbers(f, rects)
	}

	y := a.h - 1
	a.statusLine().Draw(f, uv.Rect(0, y, a.w, 1))
	if a.whichKey {
		a.whichKeyOverlay().Draw(f, uv.Rect(0, 0, a.w, y))
	}
	if a.toast != "" && y > 0 {
		t := " " + a.toast + " "
		f.Text(max(a.w-ui.Width(t)-1, 0), y-1, a.w, t, uv.Style{Fg: th.Warn, Bg: th.Row})
	}
	return f
}

func (a *App) drawPane(f *ui.Frame, p *Pane, n int, r uv.Rectangle) {
	th := f.Theme
	b := ui.Block{
		N:       n,
		Title:   a.kindIcon(p.Kind) + " " + p.Kind.String(),
		Object:  p.Object(),
		Focused: a.win().Focus == p.ID,
		Pane:    p.ID,
	}
	var tabHints []ui.Hint
	switch {
	case len(p.Tabs) == 0:
		// An empty pane: ▶ run and the schema dropdown act on the current tab,
		// and there is none (§7.8).
	case p.Kind == KindConsole:
		// Drawn left to right; Prio says what goes first when space runs out (§7.8).
		b.Hints = append([]ui.Hint{
			{Label: "doraemon.public ▾", Action: "console.schema", Color: th.PK, Prio: 1},
			{Label: "▶ run", Action: "console.run", Button: true},
		}, bound(ui.Hint{Key: a.keys.Hint("console.run", "console"), Action: "console.run", Prio: 2, Attached: true})...)
		tabHints = bound(
			ui.Hint{Key: a.keys.Hint("console.format", "console"), Label: "format", Action: "console.format"},
			ui.Hint{Key: a.hints("normal", "/", "tab.next", "tab.prev")},
		)
	case p.Kind == KindData:
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
	if len(p.Tabs) == 0 { // an empty pane: nothing but the + to open a tab
		ui.Tabs{Pane: p.ID}.Draw(f, uv.Rect(in.Min.X, in.Max.Y-1, in.Dx(), 1))
		return
	}
	body := in
	body.Max.Y--
	ui.Tabs{Names: p.Tabs, Cur: p.Cur, Prev: p.Prev, Hints: tabHints, Pane: p.ID}.
		Draw(f, uv.Rect(in.Min.X, in.Max.Y-1, in.Dx(), 1))
	st := uv.Style{Fg: th.FgMuted, Bg: th.PaneBg}
	for i := 0; i < body.Dy() && p.Scroll+i < len(p.Lines); i++ {
		f.Text(body.Min.X+1, body.Min.Y+i, body.Max.X-1, p.Lines[p.Scroll+i], st)
	}
}

// drawSidebar paints the ⟨0⟩ schema tree placeholder (§7.8).
func (a *App) drawSidebar(f *ui.Frame, r uv.Rectangle) {
	th := f.Theme
	p := a.win().Tree
	if !a.win().TreeOpen {
		a.drawThinBar(f, r)
		return
	}
	paneRegions(f, p.ID, r)
	b := ui.Block{
		Title:   a.icons.Schema + " schema",
		Hints:   bound(ui.Hint{Key: a.keys.Hint("tree.toggle", "normal"), Action: "tree.toggle"}),
		Focused: a.win().Focus == p.ID,
		Pane:    p.ID,
	}
	in := b.Draw(f, r)
	if in.Dy() < 1 {
		return
	}
	x, y, right := in.Min.X+1, in.Min.Y, in.Max.X-1
	x = f.Text(x, y, right, a.icons.Filter+" ", uv.Style{Fg: th.Info, Bg: th.PaneBg})
	x = f.Text(x, y, right, "/ ", uv.Style{Fg: th.Fg, Bg: th.PaneBg})
	f.Text(x, y, right, fmt.Sprintf("%d tables", len(fakeTables)), uv.Style{Fg: th.Dim, Bg: th.PaneBg})

	sep := func(y int) {
		f.Text(in.Min.X, y, in.Max.X, strings.Repeat("─", in.Dx()), uv.Style{Fg: th.Sep, Bg: th.PaneBg})
	}
	sep(y + 1)
	list := uv.Rect(in.Min.X, y+2, in.Dx(), max(in.Dy()-4, 0))
	for i := 0; i < list.Dy() && p.Scroll+i < len(fakeTables); i++ {
		t := fakeTables[p.Scroll+i]
		row := list.Min.Y + i
		bg, icon := th.PaneBg, th.Func
		if t.name == "t_order" { // the table open in ⟨1⟩
			bg, icon = th.Select, th.Focus
			f.Fill(uv.Rect(list.Min.X, row, list.Dx(), 1), uv.Style{Bg: bg})
		}
		cx := right - ui.Width(t.rows)
		x := f.Text(list.Min.X+1, row, right, a.icons.Table+" ", uv.Style{Fg: icon, Bg: bg})
		f.Text(x, row, cx-1, t.name, uv.Style{Fg: th.Fg, Bg: bg})
		f.Text(cx, row, right, t.rows, uv.Style{Fg: th.Border, Bg: bg})
	}
	if in.Dy() >= 4 {
		sep(in.Max.Y - 2)
		hx := in.Min.X + 1
		for _, h := range []struct{ key, label string }{
			{a.hints("tree", "/", "tree.down", "tree.up"), "move"},
			{a.keys.Hint("tree.open", "tree"), "open"},
			{a.keys.Hint("tree.open.tab", "tree"), "tab"},
		} {
			// Unbound actions get no hint (§6.7); an item that doesn't fit whole is left out.
			if h.key == "" || hx+ui.Width(h.key+" "+h.label) > right {
				continue
			}
			hx = f.Text(hx, in.Max.Y-1, right, h.key, uv.Style{Fg: th.Focus, Bg: th.PaneBg, Attrs: uv.AttrBold})
			hx = f.Text(hx, in.Max.Y-1, right, " "+h.label+"  ", uv.Style{Fg: th.Dim, Bg: th.PaneBg})
		}
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

func (a *App) kindIcon(k PaneKind) string {
	ic := a.icons
	return [...]string{ic.Schema, ic.Data, ic.Console}[k]
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
	bar := func(fg color.Color) uv.Style { return uv.Style{Fg: fg, Bg: th.Row} }
	mode := a.mode()
	var s ui.StatusLine

	if mode == keymap.Command {
		// The command line takes the left side, like tmux's prompt, and gets
		// space first: at least half the bar, wider as the input grows (§7.8).
		cmd := " :" + *a.cmdline + " "
		cmd += strings.Repeat(" ", max(a.w/2-ui.Width(cmd), 0))
		s.Left = []ui.Segment{{Runs: []ui.Run{{Text: cmd, Style: bar(th.Fg)}}}}
		s.Info = strings.Join(matchCommands(*a.cmdline), " | ")
	} else {
		sess := uv.Style{Fg: th.Bg, Bg: th.Focus, Attrs: uv.AttrBold}
		// ponytail: postgres only; pick the icon by a.sess.Engine when MySQL lands (M5).
		s.Left = []ui.Segment{{Runs: []ui.Run{
			{Text: " " + ic.Postgres + " ", Style: sess},
			{Text: a.sess.Name, Style: sess, Shrink: true},
			{Text: " ▾ ", Style: sess},
		}}}
		for i, w := range a.sess.Windows {
			seg := ui.Segment{Runs: []ui.Run{{Text: fmt.Sprintf(" %d: %s ", i, w.Name), Style: bar(th.Dim)}}, Drop: dropWindow}
			if i == a.sess.Active {
				seg = ui.Segment{Runs: []ui.Run{{Text: fmt.Sprintf(" %d: %s* ", i, w.Name), Style: uv.Style{Fg: th.Fg, Bg: th.Border}}}}
			}
			s.Left = append(s.Left, seg)
		}
	}

	pending := ui.Run{Text: "·", Style: bar(th.Dim)}
	if ks := a.res.Pending(); len(ks) > 0 {
		pending = ui.Run{Text: keymap.Display(ks), Style: uv.Style{Fg: th.Warn, Bg: th.Row, Attrs: uv.AttrBold}}
	}
	// At least 3 columns, left-aligned: SPC, g or a count don't shift the bar (§7.8).
	pending.Text += strings.Repeat(" ", max(3-ui.Width(pending.Text), 0))
	modeColor := [...]color.Color{keymap.Normal: th.Focus, keymap.Visual: th.Keyword, keymap.Insert: th.Warn, keymap.Command: th.Info}[mode]
	palette := strings.TrimRight(" "+ic.Search+" "+a.keys.Hint("palette.open", "global"), " ") + " "
	s.Right = []ui.Segment{
		{Runs: []ui.Run{{Text: palette, Style: bar(th.Info)}}, Action: "palette.open"},
		{Runs: []ui.Run{{Text: " " + ic.Keys + " ", Style: bar(th.FgMuted)}, pending, {Text: " ", Style: bar(th.FgMuted)}}},
		{Runs: []ui.Run{{Text: " 1,1 ", Style: bar(th.FgMuted)}}, Drop: dropCursor}, // ponytail: M0 has no cursor yet
		{Runs: []ui.Run{{Text: " " + ic.Conn + " " + a.sess.Addr + " ", Style: uv.Style{Fg: th.Info, Bg: th.Sep}}}, Drop: dropConn},
		{Runs: []ui.Run{{Text: " " + strings.ToUpper(mode.String()) + " ", Style: uv.Style{Fg: th.Bg, Bg: modeColor, Attrs: uv.AttrBold}}}},
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
	f.Region(f.Bounds(), ui.Target{Kind: ui.KindBackdrop}) // a click anywhere closes it (§7.4)
	for n, p := range a.panesByNumber() {
		r, ok := rects[p.ID]
		if !ok {
			continue
		}
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
