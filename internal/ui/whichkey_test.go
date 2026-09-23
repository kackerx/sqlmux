package ui

import (
	"strings"
	"testing"
)

// Items run down the columns, the box sits right above bottom, and rows that
// don't fit are cut rather than drawn over the status bar.
func TestWhichKeyLayout(t *testing.T) {
	w := WhichKey{Prefix: "SPC", Items: []WhichKeyItem{
		{"a", "one"}, {"b", "two"}, {"c", "three"}, {"d", "four"}, {"e", "five"},
	}}
	f := NewFrame(30, 8, TokyonightStorm)
	w.Draw(f, 7)
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
	w.Draw(f, 4) // room for 2 item rows only
	if got := strings.Split(f.String(), "\n")[2]; !strings.Contains(got, "b → two") || strings.Contains(f.String(), "c → three") {
		t.Errorf("clipped overlay:\n%s", f.String())
	}
}
