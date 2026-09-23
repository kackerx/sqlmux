package ui

import (
	"reflect"
	"testing"
)

// §7.8: head first, hints greedily by priority, then the object name.
func TestBlockFit(t *testing.T) {
	b := Block{N: 2, Title: "> console", Object: "console_1", Hints: []Hint{
		{Label: "doraemon.public ▾", Prio: 1},
		{Label: "▶ run", Button: true},
		{Key: "↵", Prio: 2},
	}}
	run, drop, enter := b.Hints[1], b.Hints[0], b.Hints[2]
	// head "⟨2⟩ > console" 13, full 25; hints take 1+w each: dropdown 18, " ▶ run " 8, ↵ 2.
	for _, c := range []struct {
		room  int
		title string
		hints []Hint
	}{
		{54, "⟨2⟩ > console · console_1", []Hint{drop, run, enter}}, // everything fits
		{50, "⟨2⟩ > console · cons…", []Hint{drop, run, enter}},     // hints first, the object is cut
		{42, "⟨2⟩ > console", []Hint{drop, run, enter}},             // …down to the head
		{41, "⟨2⟩ > console", []Hint{drop, run}},                    // ↵ is the lowest priority
		{39, "⟨2⟩ > console · console_1", []Hint{run, enter}},       // the dropdown doesn't fit: skipped, ↵ still tried
		{24, "⟨2⟩ > console", []Hint{run, enter}},
		{23, "⟨2⟩ > console", []Hint{run}},
		{21, "⟨2⟩ > console · c…", []Hint{enter}}, // ▶ run no longer fits, ↵ alone does
		{15, "⟨2⟩ > console", nil},
		{12, "⟨2⟩", nil}, // the head doesn't fit: only ⟨n⟩
		{2, "⟨…", nil},
	} {
		title, hints := b.fit(c.room)
		if title != c.title || !reflect.DeepEqual(hints, c.hints) {
			t.Errorf("room %d: %q %v; want %q %v", c.room, title, hints, c.title, c.hints)
		}
	}
}
