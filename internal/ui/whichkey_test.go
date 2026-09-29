package ui

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// Items run down the columns, the box sits right above bottom, and rows that
// don't fit are cut rather than drawn over the status bar.
func TestWhichKeyLayout(t *testing.T) {
	w := WhichKey{Prefix: "SPC", Items: []WhichKeyItem{
		{"a", "one", ""}, {"b", "two", ""}, {"c", "three", ""}, {"d", "four", ""}, {"e", "five", ""},
	}}
	f := NewFrame(30, 8, TokyonightStorm)
	w.Draw(f, uv.Rect(0, 0, 30, 7))
	lines := strings.Split(f.String(), "\n")
	// colw = 1 + 3 + 5 + 3 = 12; (30-4+3)/12 = 2 columns, 3 rows, box rows 2..6
	for i, want := range []string{
		"┌─ SPC ──────────────────────┐",
		"│ a → one     d → four       │",
		"│ b → two     e → five       │",
		"│ c → three                  │",
		"└────────────────────────────┘",
	} {
		if got := lines[2+i]; got != want {
			t.Errorf("row %d: %q, want %q", 2+i, got, want)
		}
	}
	if strings.TrimSpace(lines[7]) != "" {
		t.Errorf("the status bar row must stay free: %q", lines[7])
	}

	f = NewFrame(30, 5, TokyonightStorm)
	w.Draw(f, uv.Rect(0, 0, 30, 4)) // room for 2 item rows only
	if got := strings.Split(f.String(), "\n")[2]; !strings.Contains(got, "b → two") || strings.Contains(f.String(), "c → three") {
		t.Errorf("clipped overlay:\n%s", f.String())
	}
}

// Each group's items go in columns under its table's heading; Top scrolls
// the rows, kept to what there is by Fit (§6.5).
func TestWhichKeyGroups(t *testing.T) {
	w := WhichKey{Prefix: "?", Items: []WhichKeyItem{{"a", "one", "keys.grid"}, {"b", "two", "keys.grid"}, {"c", "three", "keys.normal"}}}
	f := NewFrame(30, 7, TokyonightStorm)
	w.Draw(f, uv.Rect(0, 0, 30, 6))
	if got := strings.Join(strings.Split(f.String(), "\n")[:6], "\n"); got != strings.Join([]string{
		"┌─ ? ────────────────────────┐",
		"│ [keys.grid]                │",
		"│ a → one     b → two        │",
		"│ [keys.normal]              │",
		"│ c → three                  │",
		"└────────────────────────────┘",
	}, "\n") {
		t.Errorf("groups:\n%s", got)
	}
	area := uv.Rect(0, 0, 30, 4)
	if top, shown := w.Fit(area, 5); top != 2 || shown != 2 {
		t.Errorf("Fit: top %d, shown %d", top, shown)
	}
	w.Top = 1
	f = NewFrame(30, 4, TokyonightStorm)
	w.Draw(f, area)
	if rows := strings.Split(f.String(), "\n"); !strings.Contains(rows[1], "a → one") || !strings.Contains(rows[2], "[keys.normal]") {
		t.Errorf("scrolled by one:\n%s", f.String())
	}
}
