package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

// TimeKind is which parts a time column's text has (§10.2).
type TimeKind int

const (
	NotTime TimeKind = iota
	Date
	Time
	TimeTZ
	Timestamp
	TimestampTZ
)

// timeText matches a kind's text as PG writes it in the ISO DateStyle:
// the groups before the fraction, zone and BC are the parts one steps.
var timeText = [...]*regexp.Regexp{
	Date:        regexp.MustCompile(`^(\d{4,})-(\d\d)-(\d\d)( BC)?$`),
	Time:        regexp.MustCompile(`^(\d\d):(\d\d):(\d\d)(\.\d+)?$`),
	TimeTZ:      regexp.MustCompile(`^(\d\d):(\d\d):(\d\d)(\.\d+)?[+-]\d\d(:\d\d){0,2}$`),
	Timestamp:   regexp.MustCompile(`^(\d{4,})-(\d\d)-(\d\d) (\d\d):(\d\d):(\d\d)(\.\d+)?( BC)?$`),
	TimestampTZ: regexp.MustCompile(`^(\d{4,})-(\d\d)-(\d\d) (\d\d):(\d\d):(\d\d)(\.\d+)?[+-]\d\d(:\d\d){0,2}( BC)?$`),
}

type unit int

const (
	year unit = iota
	month
	day
	hour
	minute
	second
)

// units is each kind's parts, in the text's order.
var units = [...][]unit{
	Date:        {year, month, day},
	Time:        {hour, minute, second},
	TimeTZ:      {hour, minute, second},
	Timestamp:   {year, month, day, hour, minute, second},
	TimestampTZ: {year, month, day, hour, minute, second},
}

// Seg is one part of a time's text that steps: where it sits, its value.
type Seg struct {
	Start, End int
	Val        int
	unit       unit
}

// TimeSegs is the parts of s, a kind k's text, that step; nil when s is no
// such text (infinity, one half typed), or is PG's 24:00:00, which steps
// only into what PG refuses.
func TimeSegs(k TimeKind, s string) []Seg {
	if k == NotTime {
		return nil
	}
	m := timeText[k].FindStringSubmatchIndex(s)
	if m == nil {
		return nil
	}
	var segs []Seg
	for i, u := range units[k] {
		start, end := m[2+2*i], m[3+2*i]
		v, _ := strconv.Atoi(s[start:end])
		if u == hour && v > 23 {
			return nil
		}
		segs = append(segs, Seg{start, end, v, u})
	}
	return segs
}

// StepTime is s with part i stepped by d, round within its range, carrying
// nothing (as macOS and DataGrip do); a year or month stepped keeps the
// day within its month. Only those parts of the text change (§10.2).
func StepTime(k TimeKind, s string, i, d int) string {
	segs := TimeSegs(k, s)
	if i < 0 || i >= len(segs) {
		return s
	}
	vals := map[unit]int{}
	for _, g := range segs {
		vals[g.unit] = g.Val
	}
	g := segs[i]
	bc := strings.HasSuffix(s, " BC")
	lo, hi := g.unit.bounds(vals, bc)
	vals[g.unit] = lo + ((g.Val-lo+d)%(hi-lo+1)+(hi-lo+1))%(hi-lo+1)
	if _, ok := vals[day]; ok && (g.unit == year || g.unit == month) {
		_, last := day.bounds(vals, bc)
		vals[day] = min(vals[day], last)
	}
	var b strings.Builder
	at := 0
	for _, g := range segs {
		b.WriteString(s[at:g.Start])
		fmt.Fprintf(&b, "%0*d", g.End-g.Start, vals[g.unit])
		at = g.End
	}
	return b.String() + s[at:]
}

// bounds is u's range, the day's by the year and month in vals, the year
// before Christ when bc.
func (u unit) bounds(vals map[unit]int, bc bool) (lo, hi int) {
	switch u {
	case year: // PG goes past 9999: such a year stays what it is
		return 1, max(9999, vals[year])
	case month:
		return 1, 12
	case day:
		y := vals[year]
		if bc { // PG counts years astronomically: 1 BC is year 0, a leap year
			y = 1 - y
		}
		return 1, time.Date(y, time.Month(vals[month])+1, 0, 0, 0, 0, 0, time.UTC).Day()
	case hour:
		return 0, 23
	}
	return 0, 59
}

// NowTime is t as a kind k's text: to the second, with its zone's offset
// written as PG writes one (+08, +05:30) for the kinds that keep a zone.
func NowTime(k TimeKind, t time.Time) string {
	s := [...]string{Date: "2006-01-02", Time: "15:04:05", TimeTZ: "15:04:05", Timestamp: "2006-01-02 15:04:05", TimestampTZ: "2006-01-02 15:04:05"}[k]
	s = t.Format(s)
	if k == TimeTZ || k == TimestampTZ {
		_, off := t.Zone()
		sign := "+"
		if off < 0 {
			sign, off = "-", -off
		}
		s += fmt.Sprintf("%s%02d", sign, off/3600)
		if m := off % 3600 / 60; m != 0 {
			s += fmt.Sprintf(":%02d", m)
		}
	}
	return s
}

// TimePick is a time cell's box under its edit (§10.2): ▴ over each part,
// the parts with the current one lit, ▾ under them, and the options in a
// row. Text that doesn't parse dims the parts.
type TimePick struct {
	Kind    TimeKind
	Text    string
	Seg     int // the current part
	Options []string
	Sel     int // the option picked; -1 for none
}

// Size is how big the box is, its border in.
func (p TimePick) Size() (w, h int) {
	line, _ := p.layout()
	opts := 0
	for _, o := range p.Options {
		opts += Width(o) + 3
	}
	return max(Width(line)+2, opts+1) + 2, 6
}

// layout is the parts as they show, "2026 - 09 - 20   02 : 49 : 23", and
// the column each starts at; -- for each when the text doesn't parse.
func (p TimePick) layout() (line string, at []int) {
	segs := TimeSegs(p.Kind, p.Text)
	var b strings.Builder
	for i, u := range units[p.Kind] {
		if i > 0 {
			b.WriteString([...]string{month: " - ", day: " - ", hour: "   ", minute: " : ", second: " : "}[u])
		}
		at = append(at, Width(b.String()))
		switch {
		case segs != nil:
			b.WriteString(p.Text[segs[i].Start:segs[i].End])
		case u == year:
			b.WriteString("----")
		default:
			b.WriteString("--")
		}
	}
	return b.String(), at
}

// Draw paints p in box; too short for all its rows, it leaves the screen
// alone and the keys still step.
func (p TimePick) Draw(f *Frame, box uv.Rectangle) {
	th := f.Theme
	if _, h := p.Size(); box.Dx() < 4 || box.Dy() < h {
		return
	}
	f.Region(box, Target{}) // a click inside is not outside
	f.Fill(box, uv.Style{Bg: th.PaneBg})
	border := uv.NormalBorder().Style(uv.Style{Fg: th.Focus, Bg: th.PaneBg})
	border.Draw(f.Buf, box)
	segs := TimeSegs(p.Kind, p.Text)
	st := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
	if segs == nil { // dimmed, nothing to step (§10.2)
		st.Fg = th.Dim
	}
	x, y, right := box.Min.X+2, box.Min.Y+1, box.Max.X-1
	line, at := p.layout()
	f.Text(x, y+1, right, line, st)
	for i, g := range segs {
		px, w := x+at[i], g.End-g.Start
		mid := px + (w-1)/2
		for _, b := range []struct {
			dy           int
			mark, action string
		}{{0, "▴", "cell.inc "}, {2, "▾", "cell.dec "}} {
			mst := uv.Style{Fg: th.Dim, Bg: th.PaneBg}
			if f.Region(uv.Rect(mid, y+b.dy, 1, 1), Target{Kind: KindButton, Action: b.action + strconv.Itoa(i)}) {
				mst.Bg = th.Select
			}
			f.Text(mid, y+b.dy, right, b.mark, mst)
		}
		lit := st
		if i == p.Seg {
			lit.Bg = th.Select
		}
		f.Region(uv.Rect(px, y+1, w, 1), Target{Kind: KindButton, Action: "cell.seg " + strconv.Itoa(i)})
		f.Text(px, y+1, right, p.Text[g.Start:g.End], lit)
	}
	ox := x
	for i, o := range p.Options {
		ost := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
		if hover := f.Region(uv.Rect(ox-1, y+3, Width(o)+2, 1), Target{Kind: KindRow, I: i}); hover || i == p.Sel {
			ost.Bg = th.Select
		}
		f.Text(ox-1, y+3, right, " "+o+" ", ost)
		ox += Width(o) + 3
	}
}
