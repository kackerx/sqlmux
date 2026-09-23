package ui

import (
	"reflect"
	"testing"
)

// §7.8's three fallback steps, each triggered by a narrower room.
func TestBlockFit(t *testing.T) {
	b := Block{N: 2, Title: "> console", Object: "console_1", Hints: []Hint{
		{Label: "doraemon.public ▾", Prio: 1},
		{Label: "▶ run", Button: true},
		{Key: "↵", Prio: 2},
	}}
	// full title 25, head 13; hints " doraemon.public ▾" 18, "  ▶ run " 8, " ↵" 2 → 28, +1 gap
	for _, c := range []struct {
		room      int
		title     string
		hintCount int
	}{
		{54, "⟨2⟩ > console · console_1", 3}, // everything fits
		{50, "⟨2⟩ > console · cons…", 3},     // step 1: cut the object…
		{42, "⟨2⟩ > console", 3},             // …down to the head
		{41, "⟨2⟩ > console", 2},             // step 2: drop ↵ first (lowest priority)
		{39, "⟨2⟩ > console · console_1", 1}, // then the dropdown; the object gets its room back
		{22, "⟨2⟩ > console", 1},             // ▶ run is kept longest
		{21, "⟨2⟩ > console · cons…", 0},     // …then dropped too
		{12, "⟨2⟩", 0},                       // step 3: only ⟨n⟩
		{2, "⟨…", 0},
	} {
		title, hints := b.fit(c.room)
		if title != c.title || len(hints) != c.hintCount {
			t.Errorf("room %d: %q with %d hints; want %q with %d", c.room, title, len(hints), c.title, c.hintCount)
		}
	}
	if _, hints := b.fit(39); !reflect.DeepEqual(hints, b.Hints[1:2]) {
		t.Errorf("room 39 should keep only ▶ run, got %+v", hints)
	}
}
