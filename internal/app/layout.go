package app

import (
	"math"

	uv "github.com/charmbracelet/ultraviolet"
)

// Dir is how a split node divides its area.
type Dir uint8

const (
	Leaf  Dir = iota
	Horiz     // A left, B right
	Vert      // A top, B bottom
)

// Node is the binary split tree of a window's main area (tech-design §5).
type Node struct {
	Split Dir
	Ratio float64 // share of A
	A, B  *Node
	Pane  *Pane
}

func leaf(p *Pane) *Node { return &Node{Pane: p} }

// Leaves returns panes in traversal order; ⟨n⟩ is index+1.
func (n *Node) Leaves() []*Pane {
	if n == nil {
		return nil
	}
	if n.Split == Leaf {
		return []*Pane{n.Pane}
	}
	return append(n.A.Leaves(), n.B.Leaves()...)
}

// splitRect cuts r at ratio along d. Side-by-side panes keep a 1-column gap;
// stacked ones touch (§7.8).
func splitRect(r uv.Rectangle, d Dir, ratio float64) (a, b uv.Rectangle) {
	a, b = r, r
	if d == Horiz {
		x := r.Min.X + int(math.Round(float64(max(r.Dx()-1, 0))*ratio))
		a.Max.X, b.Min.X = x, min(x+1, r.Max.X)
	} else {
		y := r.Min.Y + int(math.Round(float64(r.Dy())*ratio))
		a.Max.Y, b.Min.Y = y, y
	}
	return a, b
}

// Rects lays the tree out over r, keyed by pane ID.
func (n *Node) Rects(r uv.Rectangle, out map[int]uv.Rectangle) {
	if n.Split == Leaf {
		out[n.Pane.ID] = r
		return
	}
	a, b := splitRect(r, n.Split, n.Ratio)
	n.A.Rects(a, out)
	n.B.Rects(b, out)
}

// remove returns the tree without the leaf holding pane id: its sibling takes
// the parent's place (tech-design §5). heir is the pane that should get focus,
// the sibling's nearest leaf; root is nil when id was the only leaf. The
// caller must make sure id is in the tree: otherwise it gets a copy, heir nil.
func (n *Node) remove(id int) (root *Node, heir *Pane) {
	if n.Split == Leaf {
		if n.Pane.ID == id {
			return nil, nil
		}
		return n, nil
	}
	a, ha := n.A.remove(id)
	b, hb := n.B.remove(id)
	switch {
	case a == nil:
		return b, b.Leaves()[0]
	case b == nil:
		ls := a.Leaves()
		return a, ls[len(ls)-1]
	case ha != nil:
		hb = ha
	}
	return &Node{Split: n.Split, Ratio: n.Ratio, A: a, B: b}, hb
}
