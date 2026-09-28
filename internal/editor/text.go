package editor

import (
	"strings"
	"unicode"

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

// prev is the offset of the character before col; 0 at the start.
func prev(s string, col int) int {
	p := 0
	for i := 0; i < col && i < len(s); i = next(s, i) {
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

// at is the character at col; "" past the end.
func at(s string, col int) string {
	return s[col:next(s, col)]
}

// class is vim's character class for word motions (textobject.c cls,
// mbyte.c utf_class): 0 blank, 1 punctuation, 2 word characters; scripts
// that do not separate words with spaces are a class each, so a run of
// Han is one word.
func class(r rune) int {
	switch {
	case r == ' ' || r == '\t' || r == 0 || r == 0xa0 || r == 0x3000:
		return 0
	case r < 0x100: // iskeyword=@,48-57,_,192-255
		if r == '_' || r >= 192 || r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return 2
		}
		return 1
	case unicode.Is(unicode.Han, r):
		return 0x4e00
	case unicode.Is(unicode.Hiragana, r):
		return 0x3040
	case unicode.Is(unicode.Katakana, r):
		return 0x30a0
	case unicode.Is(unicode.Hangul, r):
		return 0xac00
	case r >= 0x1f000:
		return 3 // emoji
	case unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsSpace(r):
		return 1
	}
	return 2
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
