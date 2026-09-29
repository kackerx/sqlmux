package editor

import (
	"slices"
	"strings"
)

// Paste puts text in as a terminal's paste does in nvim (vim.paste): after
// the cursor in NORMAL, the cursor then on the last character put in; at
// the cursor in INSERT, with no indent added; over the selection in
// VISUAL, which goes to the register as with d; over as many bytes in
// REPLACE, the cursor staying; the first line only on the : or / line. CR
// LF and a lone CR break lines, as nvim_paste's crlf. What it changes is
// one undo step, or part of the INSERT's.
func (e *Editor) Paste(text string) Effect {
	e.eff = Effect{}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if text == "" && !e.visual() { // nvim still deletes a selection
		return e.eff
	}
	ls := strings.Split(text, "\n")
	switch e.mode {
	case Command:
		cl := e.cl
		cl.text, cl.pos = cl.text[:cl.pos]+ls[0]+cl.text[cl.pos:], cl.pos+len(ls[0])
	case Insert, Replace:
		e.editKey(pasteKey + text) // kept with the keys: a count types it again
	default:
		// ponytail: pending keys go; nvim keeps an operator (d then a
		// paste pastes, d still waiting). Keep it if anyone pastes there.
		e.keys = nil
		after := true
		if e.visual() {
			mode, start := e.mode, e.selection("d").start
			e.visualOp("d", cmd{})
			after = e.cur.Col < start.Col // the selection went to the end of its line
			if mode == VisualLine {
				after = false
				if e.cur.Line < start.Line { // the last lines went: a line to put into
					e.insertLines(e.cur.Line+1, "")
					e.cur = Pos{e.cur.Line + 1, 0}
				}
			}
		}
		if col := e.cur.Col; text != "" {
			if l := e.line(); after && l != "" {
				col = next(l, col)
			}
			end := e.insertText(col, ls)
			e.cur = Pos{end.Line, prev(e.lines[end.Line], end.Col)}
		}
		e.want = wantUnset
		e.settle()
	}
	e.scrollToCursor()
	return e.eff
}

// pasteKey starts the key a paste in INSERT or REPLACE is kept as, the
// text following it (nvim's paste_store puts a paste in the redo buffer).
const pasteKey = "\x00paste"

// putText is a paste in INSERT, at the cursor with no indent added, or in
// REPLACE, over as many bytes with the cursor staying. It leaves the
// autoindent as it was (do_put does not reset did_ai).
func (e *Editor) putText(ls []string) {
	e.arrived()
	if e.mode == Insert {
		e.cur = e.insertText(e.cur.Col, ls)
		return
	}
	n := 0
	for _, l := range ls {
		n += len(l)
	}
	l, c := e.line(), e.cur.Col
	end := min(c+n, len(l))
	if end < len(l) && head(l, end) < end { // not into a character (nvim cuts bytes)
		end = next(l, end)
	}
	e.cur = e.insertText(c, ls)
	e.setLine(e.cur.Line, e.line()[:e.cur.Col]+e.line()[e.cur.Col+end-c:])
	e.cur = Pos{e.cur.Line - len(ls) + 1, c}
}

// insertText puts ls in at col of the cursor's line, charwise: the first
// joins the text before col, the last the text after it. It returns where
// what was put in ends.
func (e *Editor) insertText(col int, ls []string) Pos {
	n, l := e.cur.Line, e.line()
	last := len(ls) - 1
	end := Pos{n + last, len(ls[last])}
	if last == 0 {
		end.Col += col
	}
	ls = slices.Clone(ls)
	ls[last] += l[col:]
	e.setLine(n, l[:col]+ls[0])
	if last > 0 {
		e.insertLines(n+1, ls[1:]...)
	}
	return end
}
