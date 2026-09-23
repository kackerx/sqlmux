package app

import (
	"cmp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// palette is the open command palette (§12).
type palette struct {
	input    ui.Input
	sel, top int // selected candidate, first one shown
}

// exAliases rank their command first when typed exactly in the command
// scope, so :q↵ and :qa↵ work as they always have (§12).
var exAliases = map[string]string{"q": "tab.close", "qa": "quit", "w": "save"}

func (a *App) openPalette(text string) {
	a.palette = &palette{input: ui.Input{Text: text, Pos: len(text)}}
}

// paletteMatches ranks the commands for what is typed: fzf over "title id"
// (§12); with nothing typed, recent ones first, the rest by action id.
// ponytail: commands only; tables, panes and windows join in F0.14.
func (a *App) paletteMatches() (ids []string, ms []ui.Match) {
	for id, act := range actions {
		if act.Title != "" {
			ids = append(ids, id)
		}
	}
	recent := func(id string) int {
		if i := slices.Index(a.recent, id); i >= 0 {
			return i
		}
		return len(a.recent)
	}
	slices.SortFunc(ids, func(x, y string) int { return cmp.Or(recent(x)-recent(y), strings.Compare(x, y)) })
	texts := make([]string, len(ids))
	for i, id := range ids {
		texts[i] = actions[id].Title + " " + id
	}
	query, commandScope := strings.CutPrefix(a.palette.input.Text, ">")
	ms = ui.Filter(query, texts)
	if id, ok := exAliases[strings.TrimSpace(query)]; ok && commandScope {
		i := slices.Index(ids, id)
		ms = slices.Insert(slices.DeleteFunc(ms, func(m ui.Match) bool { return m.Index == i }), 0, ui.Match{Index: i})
	}
	return ids, ms
}

// paletteView is what the palette draws.
func (a *App) paletteView() ui.Palette {
	ids, ms := a.paletteMatches()
	p := ui.Palette{Input: a.palette.input, Sel: a.palette.sel, Top: a.palette.top}
	for _, m := range ms {
		id := ids[m.Index]
		right := a.keyFor(id)
		if on := actions[id].On; on != nil {
			right = map[bool]string{true: "ON", false: "OFF"}[on(a)]
		}
		p.Rows = append(p.Rows, ui.PaletteRow{Name: actions[id].Title, Where: id, Pos: m.Pos, Right: right})
	}
	p.Footer = bound(
		ui.Hint{Key: a.hints("palette", "/", "palette.up", "palette.down"), Label: "移动"},
		ui.Hint{Key: a.keys.Hint("palette.close", "palette"), Label: "关闭", Action: "palette.close"},
	)
	enter := "执行"
	if a.palette.sel < len(ms) && actions[ids[ms[a.palette.sel].Index]].On != nil {
		enter = "切换"
	}
	p.Enter = ui.Hint{Key: a.keys.Hint("palette.run", "palette"), Label: enter, Action: "palette.run"}
	return p
}

// keyFor is the key an action has where the user is: the focused pane's
// scope, then NORMAL, then global.
func (a *App) keyFor(id string) string {
	for _, scope := range []string{a.paneScope(), "normal", "global"} {
		if k := a.keys.Hint(id, scope); k != "" {
			return k
		}
	}
	return ""
}

// paletteMove moves the selection by d, scrolling the list to keep it shown.
func (a *App) paletteMove(d int) {
	_, ms := a.paletteMatches()
	p := a.palette
	p.sel = max(min(p.sel+d, len(ms)-1), 0)
	_, rows := ui.PaletteBox(a.window(), len(ms))
	p.top = max(min(p.top, p.sel), p.sel-rows+1)
}

// paletteRun runs candidate i. A toggle leaves the palette open, so its
// ON / OFF can be seen to change (§12).
func (a *App) paletteRun(i int) tea.Cmd {
	ids, ms := a.paletteMatches()
	if i >= len(ms) {
		return nil
	}
	id := ids[ms[i].Index]
	a.recent = slices.Insert(slices.DeleteFunc(a.recent, func(r string) bool { return r == id }), 0, id)
	if actions[id].On == nil {
		a.palette = nil
		return a.run(id, 0)
	}
	cmd := a.run(id, 0)
	ids, ms = a.paletteMatches() // it may have moved up among the recent ones
	a.palette.sel = slices.IndexFunc(ms, func(m ui.Match) bool { return ids[m.Index] == id })
	return cmd
}

// paletteKey edits the palette's input. Its own editing keys are not
// bindings, as with any input.
func (a *App) paletteKey(k keymap.Key) {
	in := &a.palette.input
	switch k {
	case "<Left>":
		in.Left()
		return
	case "<Right>":
		in.Right()
		return
	case "<BS>":
		in.Backspace()
	default:
		if keymap.Text(k) == "" {
			return
		}
		in.Insert(keymap.Text(k))
	}
	a.palette.sel, a.palette.top = 0, 0
}
