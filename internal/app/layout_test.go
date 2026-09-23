package app

import (
	"math"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func ids(n *Node) (out []int) {
	for _, p := range n.Leaves() {
		out = append(out, p.ID)
	}
	return out
}

func TestRemove(t *testing.T) {
	p := func(id int) *Node { return leaf(&Pane{ID: id}) }
	// (1 | (2 / 3))
	tree := &Node{Split: Horiz, Ratio: 0.5, A: p(1), B: &Node{Split: Vert, Ratio: 0.3, A: p(2), B: p(3)}}
	for _, c := range []struct {
		id   int
		want []int
		heir int
	}{
		{1, []int{2, 3}, 2}, // the sibling subtree takes over; focus its first leaf
		{2, []int{1, 3}, 3},
		{3, []int{1, 2}, 2}, // the leaf nearest to the closed one
	} {
		root, heir := tree.remove(c.id)
		if got := ids(root); len(got) != len(c.want) || got[0] != c.want[0] || got[1] != c.want[1] || heir.ID != c.heir {
			t.Errorf("remove %d: %v heir %d; want %v heir %d", c.id, got, heir.ID, c.want, c.heir)
		}
	}
	if got := ids(tree); len(got) != 3 {
		t.Errorf("remove must not change the original tree: %v", got)
	}
	if root, _ := p(1).remove(1); root != nil {
		t.Error("removing the only leaf leaves nothing")
	}
	if root, _ := tree.remove(1); root.Ratio != 0.3 {
		t.Error("the promoted sibling keeps its own ratio")
	}
}

func TestSplit(t *testing.T) {
	p := func(id int) *Node { return leaf(&Pane{ID: id}) }
	tree := &Node{Split: Horiz, Ratio: 0.6, A: p(1), B: p(2)}
	got := tree.split(2, Vert, &Pane{ID: 3})
	if ids := ids(got); len(ids) != 3 || ids[2] != 3 || got.B.Split != Vert || got.B.Ratio != 0.5 || got.Ratio != 0.6 {
		t.Fatalf("split: %v %+v", ids, got.B)
	}
	if len(ids(tree)) != 2 {
		t.Error("split must not change the original tree")
	}
}

func TestResize(t *testing.T) {
	p := func(id int) *Node { return leaf(&Pane{ID: id}) }
	// (1 | (2 / 3))
	tree := &Node{Split: Horiz, Ratio: 0.5, A: p(1), B: &Node{Split: Vert, Ratio: 0.5, A: p(2), B: p(3)}}
	if r := tree.resize(3, Horiz, 0.1); r.Ratio != 0.6 || r.B.Ratio != 0.5 {
		t.Errorf("L on 3 moves the outer border: %v %v", r.Ratio, r.B.Ratio)
	}
	if r := tree.resize(3, Vert, -0.2); r.Ratio != 0.5 || math.Abs(r.B.Ratio-0.3) > 1e-9 {
		t.Errorf("K on 3 moves the inner border: %v %v", r.Ratio, r.B.Ratio)
	}
	if r := tree.resize(1, Vert, 0.1); r.Ratio != 0.5 || r.B.Ratio != 0.5 {
		t.Error("no vertical split around 1: nothing moves")
	}
	if r := tree.resize(1, Horiz, -1); r.Ratio != minRatio {
		t.Errorf("clamped to %v, got %v", minRatio, r.Ratio)
	}
	if tree.Ratio != 0.5 {
		t.Error("resize must not change the original tree")
	}
}

func TestNeighbor(t *testing.T) {
	//  0 | 1 | 2
	//    |---+---
	//    |   3
	rects := map[int]uv.Rectangle{
		0: uv.Rect(0, 0, 10, 20),
		1: uv.Rect(11, 0, 10, 10),
		2: uv.Rect(22, 0, 10, 10),
		3: uv.Rect(11, 10, 21, 10),
	}
	for _, c := range []struct {
		from int
		side string
		want int
		ok   bool
	}{
		{1, "left", 0, true},
		{1, "right", 2, true},
		{1, "down", 3, true},
		{3, "up", 1, true}, // 1 and 2 both touch 3 with the same overlap: the lower id
		{3, "left", 0, true},
		{0, "right", 1, true}, // 1 and 3 both adjacent, same overlap: the lower id
		{2, "right", 0, false},
		{0, "up", 0, false},
	} {
		got, ok := neighbor(rects, c.from, c.side)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("%d %s: %d %v, want %d %v", c.from, c.side, got, ok, c.want, c.ok)
		}
	}
}
