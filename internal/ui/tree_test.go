package ui

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestMagnitude(t *testing.T) {
	for n, want := range map[float64]string{
		-1: "?", 0: "0", 999: "999", 1000: "1.0k", 8199: "8.1k", 9999: "9.9k", 10000: "10k",
		48e3: "48k", 999999: "999k", 2.19e6: "2.1m", 12e6: "12m", 3.4e9: "3.4b", 1e12: "1000b",
	} {
		if got := Magnitude(n); got != want {
			t.Errorf("Magnitude(%v) = %q, want %q", n, got, want)
		}
	}
}

// Width is what Draw spends on a row: at Width the name, the aside and the
// note are whole, a column under it one of them is cut (tree.fit, F22.11).
func TestTreeNodeWidth(t *testing.T) {
	for name, n := range map[string]TreeNode{
		"name":         {Text: "t_order"},
		"icon":         {Icon: NerdIcons.Table, Text: "t_order"},
		"prefix":       {Prefix: "①", Icon: NerdIcons.Table, Text: "t_order"},
		"depth":        {Depth: 3, Branch: true, Icon: NerdIcons.Table, Text: "t_order"},
		"cjk":          {Icon: NerdIcons.Table, Text: "订单明细表"},
		"note":         {Icon: NerdIcons.Table, Text: "t_order", Note: "1.2m"},
		"aside, note":  {Prefix: "②", Icon: NerdIcons.Table, Text: "t_order", Aside: "id > 5", Note: "1.2m"},
		"cjk aside":    {Depth: 1, Text: "t_order", Aside: "name = '甲乙丙丁'"},
		"cjk and note": {Depth: 2, Icon: NerdIcons.Table, Text: "订单", Note: "测试库"},
	} {
		whole := func(w int) bool {
			f := NewFrame(w, 8, TokyonightStorm)
			Tree{Nodes: []TreeNode{n}, Icons: NerdIcons}.Draw(f, uv.Rect(0, 0, w, 8))
			row := ansi.Strip(strings.Split(f.String(), "\n")[2])
			return strings.Contains(row, n.Text) && strings.Contains(row, n.Aside) && strings.Contains(row, n.Note)
		}
		if w := n.Width(); !whole(w) || whole(w-1) {
			t.Errorf("%s: %d wide whole %v, %d wide whole %v", name, w, whole(w), w-1, whole(w-1))
		}
	}
}
