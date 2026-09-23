package ui

import (
	"reflect"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

var tables = []string{"t_order_item", "mt_task", "t_order", "t_user", "T_REFUND"}

func names(ms []Match, items []string) string {
	var out []string
	for _, m := range ms {
		out = append(out, items[m.Index])
	}
	return strings.Join(out, " ")
}

func TestFilterRanks(t *testing.T) {
	for pattern, want := range map[string]string{
		"tord":   "t_order t_order_item", // shorter first on a tie, as fzf
		"":       strings.Join(tables, " "),
		"Order":  "", // smartcase: an upper-case letter makes the term exact about case
		"refund": "T_REFUND",
		// extended syntax (§9.7)
		"t ord":     "t_order t_order_item",
		"'der_":     "t_order_item",
		"^t_":       "t_user t_order T_REFUND t_order_item", // lower case matches either case
		"item$":     "t_order_item",
		"^t_ !ord":  "t_user T_REFUND",
		"^t_order$": "t_order",
	} {
		if got := names(Filter(pattern, tables), tables); got != want {
			t.Errorf("%q: %q, want %q", pattern, got, want)
		}
	}
}

func TestFilterPositions(t *testing.T) {
	for pattern, want := range map[string][]int{
		"tord":     {0, 2, 3, 4},
		"'rde":     {3, 4, 5},
		"t_ !user": {0, 1},
		"der item": {4, 5, 6, 8, 9, 10, 11},
	} {
		ms := Filter(pattern, []string{"t_order_item"})
		if len(ms) != 1 || !reflect.DeepEqual(ms[0].Pos, want) {
			t.Errorf("%q: %+v, want positions %v", pattern, ms, want)
		}
	}
}

// Highlights follow graphemes: é typed as e + U+0301 is one cell.
func TestTextMatch(t *testing.T) {
	th := TokyonightStorm
	st, hl := uv.Style{Fg: th.Fg}, uv.Style{Fg: th.Fg, Bg: th.Warn}
	s := "café t_order"
	m := Filter("eor", []string{s})
	if len(m) != 1 {
		t.Fatalf("no match: %v", m)
	}
	f := NewFrame(20, 1, th)
	f.TextMatch(0, 0, 20, s, m[0].Pos, st, hl)
	var lit []int
	for x := range 12 {
		if f.Buf.CellAt(x, 0).Style.Bg == th.Warn {
			lit = append(lit, x)
		}
	}
	if !reflect.DeepEqual(lit, []int{3, 7, 8}) { // é, o, r
		t.Errorf("highlighted cells %v in %q", lit, f.String())
	}
}
