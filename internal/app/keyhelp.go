package app

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// keyHelp is the ? help (§6.5): every key of the context it opened in,
// user maps and all, a prefix level at a time.
type keyHelp struct {
	ctx    keymap.Context
	prefix []keymap.Key // the level shown: nil for the top
	top    int          // rows scrolled past
}

func inKeyHelp(a *App) bool { return a.keyHelp != nil }

// keyHelpView is the help as it draws: the which-key overlay's look.
func (a *App) keyHelpView() ui.WhichKey {
	h := a.keyHelp
	w := ui.WhichKey{Prefix: "?", Items: whichKeyItems(a.keys.Next(h.ctx, h.prefix)), Top: h.top}
	if len(h.prefix) > 0 {
		w.Prefix = keymap.Display(h.prefix)
	}
	return w
}

// keyHelpKey is a key the help lists: a prefix goes down a level, a
// binding closes the help and is pressed with its prefix where the help
// opened, as if typed there. Keys it doesn't list do nothing.
func (a *App) keyHelpKey(k keymap.Key) tea.Cmd {
	h := a.keyHelp
	for _, n := range a.keys.Next(h.ctx, h.prefix) {
		switch {
		case n.Key != k:
		case n.Action == "" && n.RHS == nil: // a prefix of longer ones
			h.prefix, h.top = append(slices.Clone(h.prefix), k), 0
			return nil
		default:
			a.keyHelp = nil
			var cmds []tea.Cmd
			for _, k := range append(slices.Clone(h.prefix), k) {
				cmds = append(cmds, a.press(k))
			}
			return tea.Batch(cmds...)
		}
	}
	return nil
}

// scrollKeyHelp moves the help by d rows, or by d half pages (C-d, C-u).
func (a *App) scrollKeyHelp(d int, pages bool) {
	v, area := a.keyHelpView(), a.overlayArea()
	if _, shown := v.Fit(area, 0); pages {
		d *= max(shown/2, 1)
	}
	a.keyHelp.top, _ = v.Fit(area, a.keyHelp.top+d)
}

// overlayArea is where which-key and the help rest: above the status bar.
func (a *App) overlayArea() uv.Rectangle { return uv.Rect(0, 0, a.w, a.h-1) }
