package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

// Each kind's PG ISO text parses into its parts; a step rewrites its part
// alone, round within its range, the fraction, zone and BC as they were;
// a step of 0 gives the text back (§10.2).
func TestTimeSegs(t *testing.T) {
	for _, c := range []struct {
		k       TimeKind
		s       string
		vals    []int
		i, d    int
		stepped string
	}{
		{Date, "2026-09-20", []int{2026, 9, 20}, 1, 1, "2026-10-20"},
		{Date, "0044-03-15 BC", []int{44, 3, 15}, 0, 1, "0045-03-15 BC"},
		{Date, "9999-01-01", []int{9999, 1, 1}, 0, 1, "0001-01-01"},
		{Time, "02:49:23.305808", []int{2, 49, 23}, 2, 1, "02:49:24.305808"},
		{TimeTZ, "23:59:59+05:30", []int{23, 59, 59}, 0, 1, "00:59:59+05:30"},
		{Timestamp, "2026-12-31 23:59:59", []int{2026, 12, 31, 23, 59, 59}, 5, 1, "2026-12-31 23:59:00"},
		{TimestampTZ, "2026-09-20 02:49:23.305808+08", []int{2026, 9, 20, 2, 49, 23}, 3, -3, "2026-09-20 23:49:23.305808+08"},
		{TimestampTZ, "12026-01-01 00:00:00-03:30:15", []int{12026, 1, 1, 0, 0, 0}, 4, -1, "12026-01-01 00:59:00-03:30:15"},
	} {
		var vals []int
		for _, g := range TimeSegs(c.k, c.s) {
			vals = append(vals, g.Val)
		}
		if !slices.Equal(vals, c.vals) {
			t.Errorf("%s: %v", c.s, vals)
		}
		if got := StepTime(c.k, c.s, c.i, c.d); got != c.stepped {
			t.Errorf("%s part %d by %d: %s, want %s", c.s, c.i, c.d, got, c.stepped)
		}
		if got := StepTime(c.k, c.s, 0, 0); got != c.s {
			t.Errorf("%s by 0: %s", c.s, got)
		}
	}
	for _, s := range []string{"infinity", "2026-09", "2026-09-20 02:49", "02:49:23+08", ""} {
		if TimeSegs(Timestamp, s) != nil || TimeSegs(Time, s) != nil {
			t.Errorf("%q parses", s)
		}
	}
	if TimeSegs(Time, "24:00:00") != nil || TimeSegs(TimeTZ, "24:00:00+08") != nil { // PG's end of day: no step of it is one PG takes
		t.Error("24:00:00 steps")
	}
}

// A year or month stepped keeps the day within its month, February by the
// leap years (§10.2).
func TestStepTimeKeepsTheDay(t *testing.T) {
	for s, want := range map[string]string{
		"2026-01-31": "2026-02-28",
		"2024-01-31": "2024-02-29",
		"2024-02-29": "2024-03-29",
	} {
		if got := StepTime(Date, s, 1, 1); got != want {
			t.Errorf("%s, month +1: %s, want %s", s, got, want)
		}
	}
	if got := StepTime(Date, "2024-02-29", 0, 1); got != "2025-02-28" {
		t.Errorf("a leap day, year +1: %s", got)
	}
	if got := StepTime(Date, "2026-02-28", 2, 1); got != "2026-02-01" {
		t.Errorf("the day goes round its month: %s", got)
	}
	// before Christ PG counts years astronomically: 1 BC and 5 BC leap, 4 BC doesn't
	for _, c := range []struct {
		s    string
		i    int
		want string
	}{
		{"0004-01-31 BC", 1, "0004-02-28 BC"},
		{"0001-01-31 BC", 1, "0001-02-29 BC"},
		{"0004-02-28 BC", 2, "0004-02-01 BC"},
		{"0005-02-28 BC", 2, "0005-02-29 BC"},
	} {
		if got := StepTime(Date, c.s, c.i, 1); got != c.want {
			t.Errorf("%s part %d +1: %s, want %s", c.s, c.i, got, c.want)
		}
	}
}

// Now is to the second, with the zone written as PG writes it (§10.2).
func TestNowTime(t *testing.T) {
	at := time.Date(2026, 9, 28, 18, 26, 45, 123456789, time.FixedZone("", 8*3600))
	for k, want := range map[TimeKind]string{
		Date:        "2026-09-28",
		Time:        "18:26:45",
		TimeTZ:      "18:26:45+08",
		Timestamp:   "2026-09-28 18:26:45",
		TimestampTZ: "2026-09-28 18:26:45+08",
	} {
		if got := NowTime(k, at); got != want || TimeSegs(k, got) == nil {
			t.Errorf("kind %d: %s, want %s", k, got, want)
		}
	}
	for off, want := range map[int]string{5*3600 + 1800: "+05:30", -(3*3600 + 1800): "-03:30", 0: "+00"} {
		if got := NowTime(TimeTZ, at.In(time.FixedZone("", off))); !strings.HasSuffix(got, want) {
			t.Errorf("offset %d: %s", off, got)
		}
	}
}

// The box: ▴ over each part, the current part lit, ▾ under, the options in
// a row; a text that doesn't parse dims the parts, and nothing steps.
func TestTimePickDraw(t *testing.T) {
	p := TimePick{Kind: Timestamp, Text: "2026-09-20 02:49:23", Seg: 1, Options: []string{"◷ 现在", "∅ NULL"}, Sel: -1}
	w, h := p.Size()
	f := NewFrame(w, h, TokyonightStorm)
	p.Draw(f, uv.Rect(0, 0, w, h))
	lines := strings.Split(f.String(), "\n")
	if !strings.Contains(lines[2], "2026 - 09 - 20   02 : 49 : 23") || strings.Count(lines[1], "▴") != 6 || strings.Count(lines[3], "▾") != 6 || !strings.Contains(lines[4], "◷ 现在   ∅ NULL") {
		t.Fatalf("box:\n%s", f.String())
	}
	x := Width(lines[2][:strings.Index(lines[2], "09")])
	if f.Buf.CellAt(x, 2).Style.Bg != TokyonightStorm.Select || f.Buf.CellAt(x-5, 2).Style.Bg == TokyonightStorm.Select {
		t.Error("the month is the current part, alone")
	}
	if got := f.Hits[len(f.Hits)-3].Target; got != (Target{Kind: KindButton, Action: "cell.seg 5"}) {
		t.Errorf("the seconds' hit: %+v", got)
	}
	short := NewFrame(w, h, TokyonightStorm)
	if p.Draw(short, uv.Rect(0, 0, w, h-1)); strings.TrimSpace(short.String()) != "" {
		t.Errorf("too short, drawn anyway:\n%s", short.String())
	}
	p.Text = "infinity"
	f = NewFrame(w, h, TokyonightStorm)
	p.Draw(f, uv.Rect(0, 0, w, h))
	if lines := strings.Split(f.String(), "\n"); !strings.Contains(lines[2], "---- - -- - --   -- : -- : --") || strings.Contains(f.String(), "▴") || f.Buf.CellAt(3, 2).Style.Fg != TokyonightStorm.Dim {
		t.Errorf("unparsed:\n%s", f.String())
	}
}
