package app

import "testing"

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
