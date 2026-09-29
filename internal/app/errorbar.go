package app

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"sqlmux/internal/db/postgres"
	"sqlmux/internal/ui"
)

// errorBar is a tab's error bar (§7.8「错误栏」) and the kind of request
// it came from, whose next success clears it: "fetch", "save" or "run".
type errorBar struct {
	ui.ErrorBar
	kind string
}

// newErrorBar says e on a bar: [SQLSTATE] when a server sent one, head,
// the message, cut in the middle to fit, and tail; then DETAIL, HINT and
// more a line each.
func newErrorBar(kind string, e postgres.ServerError, head, tail string, more ...string) *errorBar {
	if e.Code != "" {
		head = "[" + e.Code + "] " + head
	}
	return &errorBar{ui.ErrorBar{First: ui.Note{Head: head, Mid: e.Message, Tail: tail}, More: append(e.More, more...)}, kind}
}

// rows is how many rows b takes from its tab's content; none for no bar.
func (b *errorBar) rows() int {
	if b == nil {
		return 0
	}
	return b.Rows()
}

// clearBar drops *b when it is kind's: that kind of request went well.
func clearBar(b **errorBar, kind string) {
	if *b != nil && (*b).kind == kind {
		*b = nil
	}
}

// errorAt is where character pos (from 1) of stmt is in a console's text,
// stmt starting at its byte off: 位置：第 3 行第 12 列.
func errorAt(text string, off int, stmt string, pos int) string {
	i := 0
	for n := 1; n < pos && i < len(stmt); n++ { // past the end of what ran (a LIMIT added): its end
		_, w := utf8.DecodeRuneInString(stmt[i:])
		i += w
	}
	at := off + i
	start := strings.LastIndexByte(text[:at], '\n') + 1
	return fmt.Sprintf("位置：第 %d 行第 %d 列", strings.Count(text[:at], "\n")+1, utf8.RuneCountInString(text[start:at])+1)
}
