// Package keymap parses vim-notation bindings, merges scopes by priority and
// resolves key sequences to actions (tech-design §6.2–§6.7).
package keymap

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// Key is one keystroke in canonical vim notation: a single character ("a",
// "A", "%"), or a bracketed name with modifiers in C-M-S order ("<C-p>",
// "<Space>", "<S-Tab>"). "<" itself is "<lt>" so a sequence can be
// concatenated and re-parsed unambiguously.
type Key string

const (
	Esc    Key = "<Esc>"
	Leader Key = "<Leader>" // placeholder, expanded to the configured leader
)

// names maps lower-cased notation names to their canonical spelling. The
// defaults use only the §6.2 portable keys; the rest (F-keys, Home, aliases
// like <Enter>) are here for user configs.
var names = map[string]string{
	"space": "Space", "cr": "CR", "enter": "CR", "return": "CR", "esc": "Esc",
	"tab": "Tab", "bs": "BS", "backspace": "BS", "del": "Del", "delete": "Del",
	"up": "Up", "down": "Down", "left": "Left", "right": "Right",
	"home": "Home", "end": "End", "pageup": "PageUp", "pagedown": "PageDown",
	"lt": "lt", "bar": "|", "bslash": `\`, "leader": "Leader",
	"f1": "F1", "f2": "F2", "f3": "F3", "f4": "F4", "f5": "F5", "f6": "F6",
	"f7": "F7", "f8": "F8", "f9": "F9", "f10": "F10", "f11": "F11", "f12": "F12",
}

// Parse splits a vim-notation string into canonical keys. A "<" that does not
// start a valid <...> name is the literal character, as in vim.
func Parse(s string) ([]Key, error) {
	var out []Key
	for s != "" {
		if s[0] == '<' {
			if end := strings.IndexByte(s, '>'); end > 1 {
				if k, err := parseBracket(s[1:end]); err == nil {
					out = append(out, k)
					s = s[end+1:]
					continue
				} else if looksLikeName(s[1:end]) {
					return nil, err
				}
			}
		}
		r, n := utf8.DecodeRuneInString(s)
		out = append(out, plain(r))
		s = s[n:]
	}
	return out, nil
}

// looksLikeName tells a mistyped <name> (an error) from literal text like "<="
// or "<<" (just characters).
func looksLikeName(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' {
			return false
		}
	}
	return true
}

// plain is the key for a typed character. A literal space is <Space>, as in
// vim, so it matches what FromTea reports.
func plain(r rune) Key {
	switch r {
	case '<':
		return "<lt>"
	case ' ':
		return "<Space>"
	}
	return Key(string(r))
}

func parseBracket(body string) (Key, error) {
	var ctrl, alt, shift bool
	for len(body) > 2 && body[1] == '-' {
		switch unicode.ToUpper(rune(body[0])) {
		case 'C':
			ctrl = true
		case 'M', 'A':
			alt = true
		case 'S':
			shift = true
		default:
			return "", fmt.Errorf("<%s>：不认识的修饰键 %q", body, body[0])
		}
		body = body[2:]
	}
	mod := ctrl || alt || shift
	if name, ok := names[strings.ToLower(body)]; ok {
		switch {
		case name == "Leader" && mod:
			return "", fmt.Errorf("<%s>：<Leader> 不能带修饰键", body)
		case len(name) == 1 && !mod: // <Bar>, <Bslash> are just the characters
			return Key(name), nil
		}
		return mods(ctrl, alt, shift, name), nil
	}
	r, n := utf8.DecodeRuneInString(body)
	if n != len(body) {
		return "", fmt.Errorf("<%s>：不认识的键名", body)
	}
	switch {
	case ctrl:
		r = unicode.ToLower(r) // <C-P> is <C-p>, as in vim
	case shift && unicode.IsLetter(r):
		r, shift = unicode.ToUpper(r), false // <S-a> is A
	}
	if !ctrl && !alt && !shift {
		return plain(r), nil
	}
	name := string(r)
	if r == '<' {
		name = "lt"
	}
	return mods(ctrl, alt, shift, name), nil
}

// String renders keys back to vim notation.
func String(ks []Key) string {
	var b strings.Builder
	for _, k := range ks {
		b.WriteString(string(k))
	}
	return b.String()
}

var display = map[string]string{
	"Space": "SPC", "CR": "↵", "Esc": "esc", "lt": "<",
	"Up": "↑", "Down": "↓", "Left": "←", "Right": "→",
}

// Display renders a key the way the UI shows it (PRD style): "C-p", "SPC",
// "esc", "↵". Keys print joined ("gt"), with a space where a multi-letter
// name would run into its neighbour ("SPC s").
func Display(ks []Key) string {
	var b strings.Builder
	prevLong := false
	for i, k := range ks {
		d := displayKey(k)
		long := utf8.RuneCountInString(d) > 1
		if i > 0 && (long || prevLong) {
			b.WriteByte(' ')
		}
		b.WriteString(d)
		prevLong = long
	}
	return b.String()
}

func displayKey(k Key) string {
	s := string(k)
	if len(s) < 3 || s[0] != '<' {
		return s
	}
	body := s[1 : len(s)-1]
	mods := ""
	for len(body) > 2 && body[1] == '-' {
		mods += body[:2]
		body = body[2:]
	}
	if d, ok := display[body]; ok {
		body = d
	}
	return mods + body
}

var teaNames = map[rune]string{
	tea.KeyEnter: "CR", tea.KeyEscape: "Esc", tea.KeyTab: "Tab", tea.KeyBackspace: "BS",
	tea.KeySpace: "Space", tea.KeyDelete: "Del", tea.KeyUp: "Up", tea.KeyDown: "Down",
	tea.KeyLeft: "Left", tea.KeyRight: "Right", tea.KeyHome: "Home", tea.KeyEnd: "End",
	tea.KeyPgUp: "PageUp", tea.KeyPgDown: "PageDown",
	tea.KeyF1: "F1", tea.KeyF2: "F2", tea.KeyF3: "F3", tea.KeyF4: "F4", tea.KeyF5: "F5", tea.KeyF6: "F6",
	tea.KeyF7: "F7", tea.KeyF8: "F8", tea.KeyF9: "F9", tea.KeyF10: "F10", tea.KeyF11: "F11", tea.KeyF12: "F12",
}

// FromTea converts a key press to canonical notation.
func FromTea(k tea.Key) Key {
	ctrl, alt, shift := k.Mod.Contains(tea.ModCtrl), k.Mod.Contains(tea.ModAlt), k.Mod.Contains(tea.ModShift)
	if name, ok := teaNames[k.Code]; ok {
		if name == "Space" && !ctrl && !alt {
			return "<Space>"
		}
		return mods(ctrl, alt, shift, name)
	}
	if !ctrl && !alt {
		if k.Text == "" {
			return plain(k.Code)
		}
		r, _ := utf8.DecodeRuneInString(k.Text)
		return plain(r)
	}
	// Same folding as parseBracket: <C-P> is <C-p>, <M-S-a> is <M-A>.
	r := k.Code
	switch {
	case ctrl:
		r = unicode.ToLower(r)
	case shift:
		r = unicode.ToUpper(r)
	}
	name := string(r)
	if r == '<' {
		name = "lt"
	}
	return mods(ctrl, alt, ctrl && shift, name)
}

func mods(ctrl, alt, shift bool, name string) Key {
	s := "<"
	if ctrl {
		s += "C-"
	}
	if alt {
		s += "M-"
	}
	if shift {
		s += "S-"
	}
	return Key(s + name + ">")
}

// Text is what typing k inserts into an input: the character itself, a space
// for <Space>; "" for keys that type nothing.
func Text(k Key) string {
	switch k {
	case "<Space>":
		return " "
	case "<lt>":
		return "<"
	}
	if utf8.RuneCountInString(string(k)) == 1 {
		return string(k)
	}
	return ""
}
