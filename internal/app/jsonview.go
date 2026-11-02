package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// jsonView is a grid's other view (F14.5, §7.6): the page as a JSON array in
// a read only editor, in the place of the table. It is the tab's, not the
// layout's.
type jsonView struct {
	ed   *editor.Editor
	sig  string // what the text was made of: it is made again when that changes
	text string // what it was made as: the editor is loaded when that is another
}

// jsonViewOf is the JSON view of the grid pane p's current tab shows, nil when
// it shows its table.
func jsonViewOf(p *Pane) *jsonView {
	if s := gridStateOf(p); s != nil {
		return s.jv
	}
	return nil
}

// jsonArea is where pane p's grid is drawn, and so its JSON view.
func (a *App) jsonArea(p *Pane) uv.Rectangle {
	if t := dataOf(p); t != nil {
		return gridRect(a.layout()[p.ID], t)
	}
	return bodyRect(a.layout()[p.ID])
}

// jsonText is the page of tab as a JSON array, each record as Y yanks it
// (§7.5), a table's with its changes in, indented, in full.
func jsonText(tab *Tab) string {
	if t := tab.Data; t != nil && t.table.Kind == "k" {
		return keyJSON(tab)
	}
	var objects []string
	for row := 0; ; row++ {
		fs, ok := jsonFields(tab, row)
		if !ok {
			break
		}
		raw, err := rowRaw(fs)
		if err != nil {
			return err.Error()
		}
		objects = append(objects, string(raw))
	}
	text, err := indentJSON([]byte("[" + strings.Join(objects, ",") + "]"))
	if err != nil {
		return err.Error()
	}
	return text
}

// keyJSON is the page of a key tab as the key's own value (§8.7): a hash is an
// object, a list or a set an array, a zset an array of [member, score], a stream
// an array of {id, fields} with the fields an entry has; a string or a RedisJSON
// value is itself. A page of the longer ones is a part of the key.
func keyJSON(tab *Tab) string {
	var typ string
	for _, at := range tab.Data.info {
		if at.Name == "type" {
			typ = at.Value
		}
	}
	var items []string
	var failed error
	enc := func(f jsonField) string {
		raw, err := jsonOf(jsonValue(f.class, f.val))
		if err != nil {
			failed = err
		}
		return string(raw)
	}
	for row := 0; ; row++ {
		fs, ok := jsonFields(tab, row)
		if !ok {
			break
		}
		by := map[string]jsonField{}
		for _, f := range fs {
			by[f.name] = f
		}
		switch typ {
		case "hash":
			k, _ := jsonOf(by["field"].val.S)
			items = append(items, string(k)+":"+enc(by["value"]))
		case "list":
			items = append(items, enc(by["value"]))
		case "set":
			items = append(items, enc(by["member"]))
		case "zset":
			items = append(items, "["+enc(by["member"])+","+enc(by["score"])+"]")
		case "stream":
			var fields []string
			for _, f := range fs {
				if f.name != "id" && !f.val.Null {
					k, _ := jsonOf(f.name)
					fields = append(fields, string(k)+":"+enc(f))
				}
			}
			items = append(items, `{"id":`+enc(by["id"])+`,"fields":{`+strings.Join(fields, ",")+`}}`)
		default: // string, json
			items = append(items, enc(fs[0]))
		}
	}
	text := "null"
	switch {
	case typ == "hash":
		text = "{" + strings.Join(items, ",") + "}"
	case typ == "string" || typ == "json":
		if len(items) > 0 {
			text = items[0]
		}
	default:
		text = "[" + strings.Join(items, ",") + "]"
	}
	if failed != nil {
		return failed.Error()
	}
	out, err := indentJSON([]byte(text))
	if err != nil {
		return err.Error()
	}
	return out
}

// jsonSig is what jsonText depends on: the rows, as the page's slice and its
// length, the columns as they are shown, the changes of a table, each by what
// it holds.
func jsonSig(tab *Tab) string {
	if t := tab.Data; t != nil {
		var b strings.Builder
		fmt.Fprintf(&b, "%p %d %v %v %v", t.page.Rows, len(t.page.Rows), t.ordered(), t.edits, t.deleted)
		for _, r := range t.added {
			fmt.Fprintf(&b, " %d %d %v", r.page, r.after, r.cells)
		}
		return b.String()
	}
	rt := tab.Result
	return fmt.Sprintf("%p %d %d", rt.page.Rows, len(rt.page.Rows), len(rt.page.Cols))
}

// jsonSync makes the text of pane p's JSON view what its tab has now, as a
// page comes in or a change is made, the cursor where it was as far as the
// new text allows; the view, nil with none.
func (a *App) jsonSync(p *Pane) *jsonView {
	jv := jsonViewOf(p)
	if jv == nil {
		return nil
	}
	if sig := jsonSig(p.tab()); sig != jv.sig { // a refresh that brings the same rows leaves the search and the selection as they are
		jv.sig = sig
		if text := jsonText(p.tab()); text != jv.text {
			jv.text = text
			jv.ed.Load(text)
		}
	}
	jv.ed.SetPlain(a.plain)
	return jv
}

// jsonTextArea is where the text of pane p's JSON view is, its area less the
// gutter.
func (a *App) jsonTextArea(p *Pane, jv *jsonView) uv.Rectangle {
	return ui.Query{Lines: jv.ed.Lines()}.TextArea(a.jsonArea(p))
}

// jsonSize gives the editor the size of its area, which H M L, C-d and the
// scrolling go by.
func (a *App) jsonSize(p *Pane, jv *jsonView) {
	a.sizeEditor(jv.ed, a.jsonTextArea(p, jv))
}

// viewJSON is grid.view.json on the focused table or result: its JSON view
// opens, or closes; turned the other way, it goes (F14.5).
func (a *App) viewJSON() tea.Cmd {
	p := a.focused()
	s := gridStateOf(p)
	if s == nil {
		return nil
	}
	if s.jv != nil {
		s.jv = nil
		return nil
	}
	ed := editor.New("")
	ed.ReadOnly = true
	s.transpose, s.jv = false, &jsonView{ed: ed}
	a.jsonSync(p)
	return nil
}

// jsonYank is Y in a JSON view: the whole text, as the page has it, to the
// system clipboard, flashing as a yank of all of it (F16.9).
func (a *App) jsonYank() tea.Cmd {
	p := a.focused()
	jv := a.jsonSync(p)
	if jv == nil { // an action run with no JSON view (the palette's)
		return nil
	}
	all := ui.Sel{Mode: ui.SelLines, To: ui.TextPos{Line: len(jv.ed.Lines()) - 1}}
	return tea.Batch(a.clipCopy(jv.text), a.flashYank(yankFlash{pane: p.ID, text: all}))
}

// jsonKey gives key k, which no binding of the view took, to its editor: esc
// in NORMAL brings the table back, as the binding of esc does (§6.8).
func (a *App) jsonKey(p *Pane, k keymap.Key) tea.Cmd {
	jv := a.jsonSync(p)
	if jv == nil { // an action run with no JSON view (the palette's run of everything)
		return nil
	}
	a.jsonSize(p, jv)
	return a.roKey(jv.ed, k, true, a.viewJSON, func(eff editor.Effect) tea.Cmd { return a.jsonDid(p, jv, eff) })
}

// jsonDid acts on what the editor did: the clipboard, the toasts, :q.
func (a *App) jsonDid(p *Pane, jv *jsonView, eff editor.Effect) tea.Cmd {
	return a.roDid(eff, func(text string, pt editor.ClipPut) tea.Cmd {
		if jsonViewOf(p) != jv {
			return nil
		}
		return a.jsonDid(p, jv, jv.ed.PutClip(text, pt))
	}, func(c string) tea.Cmd {
		if c = strings.TrimSpace(c); c == "q" || c == "q!" {
			return a.viewJSON()
		}
		return a.showToast(i18n.T("不支持的命令：%s", c))
	})
}

// jsonAt is the JSON view of pane id, and the line and display column of its
// text under p; nil when the pane shows none.
func (a *App) jsonAt(id int, p uv.Position) (*jsonView, int, int) {
	pane := a.win().pane(id)
	if pane == nil {
		return nil, 0, 0
	}
	jv := a.jsonSync(pane)
	if jv == nil {
		return nil, 0, 0
	}
	a.jsonSize(pane, jv)
	text := a.jsonTextArea(pane, jv)
	return jv, jv.ed.Top() + p.Y - text.Min.Y, max(jv.ed.Left()+p.X-text.Min.X, 0)
}

// drawJSON paints pane p's JSON view in its grid's place.
func (a *App) drawJSON(f *ui.Frame, p *Pane) {
	jv := a.jsonSync(p)
	if jv == nil {
		return
	}
	a.jsonSize(p, jv)
	if cur := a.textView(jv.ed, p.ID, true).Draw(f, a.jsonArea(p)); cur.X >= 0 && a.win().Focus == p.ID {
		f.Cursor = &cur
	}
}

// jsonContext is where a key lands in a JSON view: its own scope, and of the
// table's or the result's only the keys that page and refresh, whatever they
// are bound to (F15.9).
func (a *App) jsonContext(jv *jsonView) keymap.Context {
	c := keymap.Context{Focus: []string{"jsonview", "grid"}, Mode: vimMode(jv.ed.Mode()), Plain: a.plain, ReadOnly: "jsonview",
		Only: []string{"grid.page.next", "grid.page.prev", "grid.refresh"}}
	if resultOf(a.focused()) != nil {
		c.Focus, c.Only = []string{"jsonview", "result", "grid"}, append(c.Only, "result.page.next", "result.page.prev", "result.rerun")
	}
	return c
}
