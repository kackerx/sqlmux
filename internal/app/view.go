package app

import (
	"fmt"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/ui"
)

// sidebarWidth is the ⟨0⟩ schema sidebar's width, borders included (§7.8).
func sidebarWidth(w int) int {
	if w < 100 {
		return 24
	}
	return 32
}

// hintKey is the displayed key for an action; unbound actions show nothing.
// ponytail: fixed table until the keymap lands (F0.4).
func hintKey(action string) string {
	return map[string]string{
		"tree.toggle": "SPC b", "console.run": "↵", "console.schema": "gs",
		"grid.left": "h", "grid.down": "j", "grid.up": "k", "grid.right": "l",
		"grid.edit": "↵", "grid.transpose": "T", "tab.next": "gt", "tab.prev": "gT",
		"tree.down": "j", "tree.up": "k", "tree.open": "↵", "tree.open.tab": "t",
		"console.format": "gq",
	}[action]
}

func joinKeys(sep string, actions ...string) string {
	ks := make([]string, len(actions))
	for i, a := range actions {
		ks[i] = hintKey(a)
	}
	return strings.Join(ks, sep)
}

// layout places the sidebar and every pane of the current window, keyed by
// pane ID. The last row is the status bar.
func (a *App) layout() map[int]uv.Rectangle {
	win := a.sess.Win()
	main := uv.Rect(0, 0, max(a.w, 0), max(a.h-1, 0))
	rects := map[int]uv.Rectangle{}
	if win.TreeOpen {
		side := main
		side.Max.X = min(main.Min.X+sidebarWidth(a.w), main.Max.X)
		main.Min.X = min(side.Max.X+1, main.Max.X)
		rects[win.Tree.ID] = side
	}
	win.Root.Rects(main, rects)
	return rects
}

func (a *App) render() *ui.Frame {
	th := a.theme
	f := ui.NewFrame(a.w, a.h, th, a.icons)
	f.Fill(f.Bounds(), uv.Style{Fg: th.Fg, Bg: th.Bg})
	if a.w <= 0 || a.h <= 0 {
		return f
	}
	win := a.sess.Win()
	rects := a.layout()
	if win.TreeOpen {
		a.drawSidebar(f, win, rects[win.Tree.ID])
	}
	for i, p := range win.Root.Leaves() {
		a.drawPane(f, win, p, i+1, rects[p.ID])
	}

	y := a.h - 1
	if a.cmdline != nil {
		f.Text(0, y, a.w, ":"+*a.cmdline, uv.Style{Fg: th.Fg, Bg: th.Bg})
	}
	if a.toast != "" && y > 0 {
		t := " " + a.toast + " "
		f.Text(max(a.w-ui.Width(t)-1, 0), y-1, a.w, t, uv.Style{Fg: th.Warn, Bg: th.Row})
	}
	return f
}

func (a *App) drawPane(f *ui.Frame, win *Window, p *Pane, n int, r uv.Rectangle) {
	th := f.Theme
	b := ui.Block{
		Title:   fmt.Sprintf("⟨%d⟩ %s %s · %s", n, a.kindIcon(p.Kind), p.Kind, p.Object()),
		Focused: win.Focus == p.ID,
		Pane:    p.ID,
	}
	var tabHints []ui.Hint
	switch p.Kind {
	case KindConsole:
		b.Hints = append([]ui.Hint{{Label: "doraemon.public ▾", Action: "console.schema", Color: th.PK}},
			bound(ui.Hint{Label: "▶ run", Key: hintKey("console.run"), Action: "console.run", Button: true})...)
		tabHints = bound(
			ui.Hint{Key: hintKey("console.format"), Label: "format", Action: "console.format"},
			ui.Hint{Key: joinKeys("/", "tab.next", "tab.prev")},
		)
	case KindData:
		tabHints = bound(
			ui.Hint{Key: joinKeys("", "grid.left", "grid.down", "grid.up", "grid.right")},
			ui.Hint{Key: hintKey("grid.edit"), Label: "edit", Action: "grid.edit"},
			ui.Hint{Key: hintKey("grid.transpose"), Label: "转置", Action: "grid.transpose"},
			ui.Hint{Key: joinKeys("/", "tab.next", "tab.prev")},
		)
	}
	in := b.Draw(f, r)
	if in.Empty() {
		return
	}
	body := in
	if len(p.Tabs) > 0 {
		body.Max.Y--
		ui.Tabs{Names: p.Tabs, Cur: p.Cur, Prev: p.Prev, Hints: tabHints, Pane: p.ID}.
			Draw(f, uv.Rect(in.Min.X, in.Max.Y-1, in.Dx(), 1))
	}
	st := uv.Style{Fg: th.FgMuted, Bg: th.PaneBg}
	for i := 0; i < body.Dy() && p.Scroll+i < len(p.Lines); i++ {
		f.Text(body.Min.X+1, body.Min.Y+i, body.Max.X-1, p.Lines[p.Scroll+i], st)
	}
}

// drawSidebar paints the ⟨0⟩ schema tree placeholder (§7.8).
func (a *App) drawSidebar(f *ui.Frame, win *Window, r uv.Rectangle) {
	th := f.Theme
	p := win.Tree
	b := ui.Block{
		Title:   fmt.Sprintf("⟨0⟩ %s schema", a.icons.Schema),
		Hints:   bound(ui.Hint{Key: hintKey("tree.toggle"), Action: "tree.toggle"}),
		Focused: win.Focus == p.ID,
		Pane:    p.ID,
	}
	in := b.Draw(f, r)
	if in.Dy() < 1 {
		return
	}
	bg := uv.Style{Bg: th.PaneBg}
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
		st, icon := bg, uv.Style{Fg: th.Func, Bg: th.PaneBg}
		if t.name == "t_order" { // the table open in ⟨1⟩
			st.Bg, icon = th.Select, uv.Style{Fg: th.Focus, Bg: th.Select}
			f.Fill(uv.Rect(list.Min.X, row, list.Dx(), 1), st)
		}
		cnt := t.rows
		cx := right - ui.Width(cnt)
		x := f.Text(list.Min.X+1, row, right, a.icons.Table+" ", icon)
		f.Text(x, row, cx-1, t.name, uv.Style{Fg: th.Fg, Bg: st.Bg})
		f.Text(cx, row, right, cnt, uv.Style{Fg: th.Border, Bg: st.Bg})
	}
	if in.Dy() >= 4 {
		sep(in.Max.Y - 2)
		hx := in.Min.X + 1
		for _, h := range []struct{ key, label string }{
			{joinKeys("/", "tree.down", "tree.up"), "move"},
			{hintKey("tree.open"), "open"},
			{hintKey("tree.open.tab"), "tab"},
		} {
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
	return [...]string{ic.Schema, ic.Data, ic.Console, ic.Result}[k]
}
