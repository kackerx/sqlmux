package editor

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// A character is one grapheme cluster: vim moves over a letter and its
// combining marks as one.

// next is the offset of the character after the one at col in s; len(s)
// at the end.
func next(s string, col int) int {
	if col >= len(s) {
		return len(s)
	}
	g, _ := ansi.FirstGraphemeCluster(s[col:], ansi.GraphemeWidth)
	return col + max(len(g), 1)
}

// prev is the offset of the character before col; 0 at the start. It
// walks from the last pair of ASCII bytes before col, a character boundary
// (a line has no CR LF).
// ponytail: a long line with no ASCII pair (all Han) walks from its start,
// O(n) per step; keep a boundary index per line if that shows.
func prev(s string, col int) int {
	col = min(col, len(s))
	from := 0
	for i := col - 1; i > 0; i-- {
		if s[i] < utf8.RuneSelf && s[i-1] < utf8.RuneSelf {
			from = i
			break
		}
	}
	p := from
	for i := from; i < col; i = next(s, i) {
		p = i
	}
	return p
}

// head is the start of the character col is in.
func head(s string, col int) int {
	if col >= len(s) {
		return len(s)
	}
	return prev(s, next(s, col))
}

// last is the offset of the last character of s; 0 when s is empty.
func last(s string) int { return prev(s, len(s)) }

// classAt is the class of the character at i in s.
func classAt(s string, i int) int {
	r, _ := utf8.DecodeRuneInString(s[i:])
	return class(r)
}

// firstNonBlank is where vim's beginline(BL_WHITE | BL_FIX) puts the
// cursor: the first non-blank, or the last character of a blank line.
func firstNonBlank(l string) int { return head(l, min(nonBlank(l), last(l))) }

// nonBlank is the offset of the first character that is not a space or a
// tab; len(s) when there is none.
func nonBlank(s string) int {
	return len(s) - len(strings.TrimLeft(s, " \t"))
}

// width is how many columns the character at col takes when it starts at
// display column vcol: a tab reaches the next multiple of ts.
func width(s string, col, vcol, ts int) int {
	if s[col] == '\t' {
		return ts - vcol%ts
	}
	_, w := ansi.FirstGraphemeCluster(s[col:], ansi.GraphemeWidth)
	return max(w, 1)
}

// vcol is the display column the character at col starts at.
func vcol(s string, col, ts int) int {
	v := 0
	for i := 0; i < col && i < len(s); i = next(s, i) {
		v += width(s, i, v, ts)
	}
	return v
}

// keyText is what typing key k inserts: the character, a space for
// <Space>; "" for keys that type nothing. Keys are in keymap's notation.
func keyText(k string) string {
	switch {
	case k == "<Space>":
		return " "
	case k == "<lt>":
		return "<"
	case strings.HasPrefix(k, "<") && len(k) > 1:
		return ""
	}
	return k
}
