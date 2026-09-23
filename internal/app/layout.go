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

// split returns the tree with pane id's leaf divided along d: the old pane
// keeps the left / top half, p takes the other.
func (n *Node) split(id int, d Dir, p *Pane) *Node {
	if n.Split == Leaf {
		if n.Pane.ID == id {
			return &Node{Split: d, Ratio: 0.5, A: n, B: leaf(p)}
		}
		return n
	}
	return &Node{Split: n.Split, Ratio: n.Ratio, A: n.A.split(id, d, p), B: n.B.split(id, d, p)}
}

// Ratio bounds keep a resized pane from vanishing.
const minRatio, maxRatio = 0.1, 0.9

// resize moves the border of the nearest split along d around pane id by
// delta: negative moves it left / up, as tmux's resize-pane -L / -U.
func (n *Node) resize(id int, d Dir, delta float64) *Node {
	out, _ := n.resized(id, d, delta)
	return out
}

func (n *Node) resized(id int, d Dir, delta float64) (*Node, bool) {
	if n.Split == Leaf {
		return n, false
	}
	a, doneA := n.A.resized(id, d, delta)
	b, doneB := n.B.resized(id, d, delta)
	out := &Node{Split: n.Split, Ratio: n.Ratio, A: a, B: b}
	if doneA || doneB {
		return out, true
	}
	if n.Split == d && n.has(id) {
		out.Ratio = min(max(n.Ratio+delta, minRatio), maxRatio)
		return out, true
	}
	return out, false
}

func (n *Node) has(id int) bool {
	for _, p := range n.Leaves() {
		if p.ID == id {
			return true
		}
	}
	return false
}

// neighbor finds the pane next to from on side (left / down / up / right):
// among the nearest ones on that side that overlap it along the other axis,
// the one prefer ranks first (tech-design §5). Nothing there: no move, no
// wrapping round.
func neighbor(rects map[int]uv.Rectangle, from int, side string, prefer func(a, b int) bool) (int, bool) {
	f := rects[from]
	best, bestDist := 0, 0
	found := false
	for id, r := range rects {
		var dist, over int
		switch side {
		case "left":
			dist, over = f.Min.X-r.Max.X, overlap(f.Min.Y, f.Max.Y, r.Min.Y, r.Max.Y)
		case "right":
			dist, over = r.Min.X-f.Max.X, overlap(f.Min.Y, f.Max.Y, r.Min.Y, r.Max.Y)
		case "up":
			dist, over = f.Min.Y-r.Max.Y, overlap(f.Min.X, f.Max.X, r.Min.X, r.Max.X)
		case "down":
			dist, over = r.Min.Y-f.Max.Y, overlap(f.Min.X, f.Max.X, r.Min.X, r.Max.X)
		}
		if id == from || dist < 0 || over <= 0 {
			continue
		}
		if !found || dist < bestDist || dist == bestDist && prefer(id, best) {
			best, bestDist, found = id, dist, true
		}
	}
	return best, found
}

func overlap(a0, a1, b0, b1 int) int { return min(a1, b1) - max(a0, b0) }

// handle is a split's drag handle (§7.4): the gap column between side-by-side
// halves, or the bottom border row of the top half of a stacked split.
type handle struct {
	idx  int // the split's pre-order index among splits
	d    Dir
	rect uv.Rectangle // where to grab it
	area uv.Rectangle // the split's whole area, to turn a pointer into a ratio
}

// handles lists the splits' drag handles laid out over r.
func (n *Node) handles(r uv.Rectangle) []handle {
	var out []handle
	idx := 0
	var walk func(n *Node, r uv.Rectangle)
	walk = func(n *Node, r uv.Rectangle) {
		if n.Split == Leaf {
			return
		}
		h := handle{idx: idx, d: n.Split, area: r}
		idx++
		a, b := splitRect(r, n.Split, n.Ratio)
		if n.Split == Horiz {
			h.rect = uv.Rect(a.Max.X, r.Min.Y, b.Min.X-a.Max.X, r.Dy())
		} else {
			h.rect = uv.Rect(r.Min.X, a.Max.Y-1, r.Dx(), 1)
		}
		out = append(out, h)
		walk(n.A, a)
		walk(n.B, b)
	}
	walk(n, r)
	return out
}

// ratioAt turns a pointer on h into the ratio that puts the border there.
func (h handle) ratioAt(p uv.Position) float64 {
	var r float64
	if h.d == Horiz {
		r = float64(p.X-h.area.Min.X) / float64(max(h.area.Dx()-1, 1))
	} else {
		r = float64(p.Y+1-h.area.Min.Y) / float64(max(h.area.Dy(), 1))
	}
	return min(max(r, minRatio), maxRatio)
}

// setRatio returns the tree with the idx-th split (pre-order) set to ratio.
func (n *Node) setRatio(idx int, ratio float64) *Node {
	i := 0
	var walk func(n *Node) *Node
	walk = func(n *Node) *Node {
		if n.Split == Leaf {
			return n
		}
		out := &Node{Split: n.Split, Ratio: n.Ratio}
		if i == idx {
			out.Ratio = ratio
		}
		i++
		out.A, out.B = walk(n.A), walk(n.B)
		return out
	}
	return walk(n)
}
