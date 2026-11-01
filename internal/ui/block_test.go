package ui

import (
	"image/color"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// §7.8: head first, hints greedily by priority, then the object name.
func TestBlockFit(t *testing.T) {
	b := Block{Num: "⟨2⟩", Title: "> query", Object: "query_1", Hints: []Hint{
		{Label: "doraemon.public ▾", Prio: 1},
		{Label: "▶ run", Button: true},
		{Key: "↵", Prio: 2, Attached: true},
	}}
	run, drop, enter := b.Hints[1], b.Hints[0], b.Hints[2]
	// head "⟨2⟩ > query" 11, full 21; hints take 1+w each: dropdown 18, " ▶ run " 8, ↵ 2.
	for _, c := range []struct {
		room  int
		title string
		hints []Hint
	}{
		{50, "⟨2⟩ > query · query_1", []Hint{drop, run, enter}}, // everything fits
		{47, "⟨2⟩ > query · que…", []Hint{drop, run, enter}},    // hints first, the object is cut
		{42, "⟨2⟩ > query", []Hint{drop, run, enter}},           // …down to the head
		{39, "⟨2⟩ > query", []Hint{drop, run}},                  // ↵ is the lowest priority
		{36, "⟨2⟩ > query · query_1", []Hint{run, enter}},       // the dropdown doesn't fit: skipped, ↵ still tried
		{24, "⟨2⟩ > query", []Hint{run, enter}},
		{21, "⟨2⟩ > query", []Hint{run}},
		{17, "⟨2⟩ > query · qu…", nil}, // ↵ would fit, but never without ▶ run
		{10, "⟨2⟩", nil},               // the head doesn't fit: only ⟨n⟩
		{2, "⟨…", nil},
	} {
		title, hints := b.fit(c.room)
		if title != c.title || !reflect.DeepEqual(hints, c.hints) {
			t.Errorf("room %d: %q %v; want %q %v", c.room, title, hints, c.title, c.hints)
		}
	}
}

// With a Nerd icon there is no type (§7.7): the object follows the icon, and
// the shortest title is "⟨n⟩ <icon>".
func TestBlockFitWithoutType(t *testing.T) {
	b := Block{Num: "⟨2⟩", Icon: Icon{Text: ">"}, Object: "query_1"}
	for room, want := range map[int]string{20: "⟨2⟩ > query_1", 8: "⟨2⟩ > q…", 5: "⟨2⟩ >", 4: "⟨2⟩"} {
		if title, _ := b.fit(room); title != want {
			t.Errorf("room %d: %q, want %q", room, title, want)
		}
	}
}

// The sidebar's schema: the object takes its room before the hints, the ▾
// stays after a cut name and goes with it (§7.8).
func TestBlockFitObjectFirst(t *testing.T) {
	b := Block{Num: "⓪", Icon: Icon{Text: "#"}, Object: "public", Suffix: " ▾", ObjectFirst: true, Hints: []Hint{{Key: "SPC b"}}}
	for room, want := range map[int]string{
		20: "⓪ # public ▾ + hint", // 12 + " SPC b"
		12: "⓪ # public ▾",
		11: "⓪ # publ… ▾",
		8:  "⓪ # p… ▾",
		7:  "⓪ #",
		2:  "⓪",
	} {
		title, hints := b.fit(room)
		if len(hints) > 0 {
			title += " + hint"
		}
		if title != want {
			t.Errorf("room %d: %q, want %q", room, title, want)
		}
	}
	b.ObjectFirst = false // a pane's order: the hint first, then what's left
	if title, hints := b.fit(16); title != "⓪ # pu… ▾" || len(hints) != 1 {
		t.Errorf("hints first: %q %v", title, hints)
	}
}

// A zoomed pane whose zoom icon a theme has emptied has no blank where it was,
// as any icon left out (F9.6).
func TestBlockZoomIconEmpty(t *testing.T) {
	b := Block{Num: "①", Icon: Icon{Text: "T"}, Title: "data", Zoomed: true, ZoomIcon: Icon{Text: "Z"}}
	if got, _ := b.fit(40); got != "① Z T data" {
		t.Errorf("with the icon: %q", got)
	}
	b.ZoomIcon = Icon{}
	if got, _ := b.fit(40); got != "① T data" {
		t.Errorf("without it: %q", got)
	}
}

// An icon with a color of its own keeps it beside a zoom icon, and where a
// theme has left the zoom icon out (F9.6).
func TestBlockIconColorWithZoom(t *testing.T) {
	th := TokyonightStorm
	for _, zoom := range []string{"Z", ""} {
		f := NewFrame(30, 3, th)
		own := c("#ff0000")
		Block{Num: "1", Icon: Icon{Text: "T", Fg: own}, Title: "data", Zoomed: true, ZoomIcon: Icon{Text: zoom}}.Draw(f, uv.Rect(0, 0, 30, 3))
		got := false
		for x := range 30 {
			if cell := f.Buf.CellAt(x, 0); cell.String() == "T" {
				got = cell.Style.Fg == own
			}
		}
		if !got {
			t.Errorf("zoom icon %q: the icon lost its color", zoom)
		}
	}
}

// A production pane's border is in the production color, a zoomed one's too: production
// is said over the rest (F17.9).
func TestBlockProductionBorder(t *testing.T) {
	th := *TokyonightStorm
	th.Production = c("#010203") // not the error color it is by default
	for _, tc := range []struct {
		zoomed, prod bool
		want         color.Color
	}{{false, false, th.Border}, {true, false, th.Zoom}, {false, true, th.Production}, {true, true, th.Production}} {
		f := NewFrame(30, 3, &th)
		Block{Num: "1", Title: "data", Zoomed: tc.zoomed, Production: tc.prod}.Draw(f, uv.Rect(0, 0, 30, 3))
		if got := f.Buf.CellAt(0, 1).Style.Fg; got != tc.want {
			t.Errorf("zoomed %v production %v: border %v, want %v", tc.zoomed, tc.prod, got, tc.want)
		}
	}
}

// A focused pane's border and the title on it are the theme's border_focus, not
// its focus; a zoomed one's is the zoom, a production one's the production (F20.6).
func TestBlockFocusedBorder(t *testing.T) {
	th := *TokyonightStorm
	th.BorderFocus = c("#010203")
	for _, tc := range []struct {
		zoomed, prod bool
		want         color.Color
	}{{false, false, th.BorderFocus}, {true, false, th.Zoom}, {false, true, th.Production}} {
		f := NewFrame(30, 3, &th)
		Block{Num: "1", Title: "data", Focused: true, Zoomed: tc.zoomed, Production: tc.prod}.Draw(f, uv.Rect(0, 0, 30, 3))
		if got := f.Buf.CellAt(0, 1).Style.Fg; got != tc.want {
			t.Errorf("zoomed %v production %v: border %v, want %v", tc.zoomed, tc.prod, got, tc.want)
		}
		if tc.zoomed || tc.prod {
			continue
		}
		title := -1
		for x := range 30 {
			if f.Buf.CellAt(x, 0).String() == "d" {
				title = x
			}
		}
		if title < 0 || f.Buf.CellAt(title, 0).Style.Fg != th.BorderFocus {
			t.Errorf("the title on the border is not in border_focus: %v", f.Buf.CellAt(title, 0))
		}
	}
}
