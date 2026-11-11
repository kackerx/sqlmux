package app

import (
	"fmt"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/db"
)

// tabsApp is an App whose pane ① has n table tabs t1…tn, the last one current.
func tabsApp(t *testing.T, n int) (*App, *Pane) {
	t.Helper()
	a := sized(160, 45, "nerd")
	p := a.win().Root.Leaves()[0]
	a.win().focus(p.ID)
	p.Tabs, p.Cur, p.Prev = nil, 0, -1
	c := a.sess.Conns[0]
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("t%d", i)
		putTab(p, Tab{Name: name, Data: newDataTab(c, db.Table{Schema: "public", Name: name})}, false)
	}
	return a, p
}

// order is the tabs of p as the bar reads them, the pinned ones with a ^ and the current one *.
func order(p *Pane) string {
	var out []string
	for i, tb := range p.Tabs {
		s := tb.Name
		if tb.Pinned {
			s += "^"
		}
		if i == p.Cur {
			s += "*"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

func pinAt(a *App, p *Pane, i int) {
	selectTab(p, i)
	a.run("tab.pin", 0)
}

// tab.pin puts the tab at the end of the pinned ones, and takes it to the first of the
// others when it is unpinned; the current tab stays the one it was; {N}gt goes by what
// the bar shows (F22.6).
func TestPinTab(t *testing.T) {
	a, p := tabsApp(t, 5)
	pinAt(a, p, 2)
	if got := order(p); got != "t3^* t1 t2 t4 t5" {
		t.Fatalf("t3 pinned: %s", got)
	}
	pinAt(a, p, 4)
	if got := order(p); got != "t3^ t5^* t1 t2 t4" {
		t.Fatalf("t5 pinned: %s", got)
	}
	selectTab(p, 3)
	pinAt(a, p, 3)
	if got := order(p); got != "t3^ t5^ t2^* t1 t4" {
		t.Fatalf("t2 pinned: %s", got)
	}
	pinAt(a, p, 0) // unpinned: the first after the pinned ones
	if got := order(p); got != "t5^ t2^ t3* t1 t4" {
		t.Errorf("t3 unpinned: %s", got)
	}
	if p.Prev >= 0 && p.Tabs[p.Prev].Name == "t3" {
		t.Errorf("the previous tab is the current one: %d", p.Prev)
	}
	feed(t, a, "4gt")
	if p.Tabs[p.Cur].Name != "t1" {
		t.Errorf("4gt: %s", order(p))
	}
	a.run("tab.new", 0) // a landing page is not pinned
	if a.run("tab.pin", 0); p.Tabs[p.Cur].Pinned {
		t.Error("a landing page was pinned")
	}
}

// The batch closes keep the pinned tabs and the ones with changes, the toast says how
// many of the second kind, what they close goes to tab.reopen; tab.close closes a
// pinned one (F22.6).
func TestCloseTabs(t *testing.T) {
	for _, tc := range []struct {
		action, want string
		kept         string
	}{
		{"tab.close.others", "t2^ t3* t4", "1 个 tab 没关："},
		{"tab.close.left", "t2^ t3* t4 t5", ""},
		{"tab.close.right", "t2^ t1 t3* t4", "1 个 tab 没关："},
	} {
		a, p := tabsApp(t, 5)
		pinAt(a, p, 1)
		p.Tabs[3].Data.edits = map[editKey]edit{{"1", "x"}: {}} // t4 has a change
		selectTab(p, 2)                                         // t3, after t2^ t1
		a.toast = ""
		a.run(tc.action, 0)
		if got := strings.ReplaceAll(order(p), "^*", "^"); order(p) != tc.want && got != tc.want {
			t.Errorf("%s: %s, want %s", tc.action, order(p), tc.want)
		}
		if tc.kept == "" != (a.toast == "") || !strings.Contains(a.toast, tc.kept) {
			t.Errorf("%s: toast %q, want it to say %q", tc.action, a.toast, tc.kept)
		}
		if a.confirm != nil {
			t.Errorf("%s asked", tc.action)
		}
		if n := len(a.sess.closed); n == 0 {
			t.Errorf("%s: nothing for tab.reopen", tc.action)
		}
	}
	a, p := tabsApp(t, 3)
	pinAt(a, p, 1)
	selectTab(p, 0)
	a.run("tab.close", 0)
	if got := order(p); strings.Contains(got, "t2") {
		t.Errorf("tab.close left the pinned tab: %s", got)
	}
}

// The result area has no batch close: its tabs stay, and nothing is said (F22.6).
func TestCloseTabsNotInResultArea(t *testing.T) {
	d := &execDB{res: map[string]db.Result{"select 1": rows(3)}}
	a, _ := inRun(t, d, "select 1")
	press(t, a, "<CR>")
	res := a.win().Result
	a.win().focus(res.ID)
	selectTab(res, 0)
	n := len(res.Tabs)
	for _, act := range []string{"tab.close.others", "tab.close.left", "tab.close.right"} {
		selectTab(res, 0)
		a.toast = ""
		if a.run(act, 0); len(res.Tabs) != n || a.toast != "" {
			t.Errorf("%s: %d tabs of %d, toast %q", act, len(res.Tabs), n, a.toast)
		}
	}
}

// A pinned tab moved to another pane is pinned there, after the pinned ones that are there;
// a pinned tab reopened is not pinned (F22.6).
func TestPinnedTabMoves(t *testing.T) {
	a, p := tabsApp(t, 2)
	a.splitPane(Horiz)
	dst := a.focused()
	c := a.sess.Conns[0]
	putTab(dst, Tab{Name: "u1", Data: newDataTab(c, db.Table{Schema: "public", Name: "u1"}), Pinned: true}, true)
	putTab(dst, Tab{Name: "u2", Data: newDataTab(c, db.Table{Schema: "public", Name: "u2"})}, false)
	a.win().focus(p.ID)
	pinAt(a, p, 1)
	if a.run("tab.move.next", 0); order(dst) != "u1^ t2^* u2" || a.focused() != dst {
		t.Errorf("moved: %s (%s)", order(dst), order(p))
	}
	a.run("tab.close", 0)
	a.run("tab.reopen", 0)
	if tb := dst.tab(); tb.Name != "t2" || tb.Pinned {
		t.Errorf("reopened: %s", order(dst))
	}
}

// The menu of a tab: what its kind has, the key of each; the tab becomes the current one;
// 向右拆分 moves the tab into the new pane, the pane's only tab stays; 移到下一个 pane is dim with
// one pane (F22.6).
func TestTabMenu(t *testing.T) {
	a, p := tabsApp(t, 3)
	a.removePane(a.win().Root.Leaves()[1].ID) // one pane
	labels := func() string { return strings.Join(menuLabels(a), "|") }
	a.tabMenu(p.ID, 0, uv.Pos(10, 10))
	if p.Cur != 0 || labels() != "关闭|关闭其他|关闭左侧|关闭右侧|向右拆分|向下拆分|移到下一个 pane|固定" {
		t.Fatalf("a table's: cur %d, %s", p.Cur, labels())
	}
	for _, it := range a.menu.items {
		if it.label == "关闭其他" && a.menuKey(it.action) != "SPC wo" {
			t.Errorf("the key of 关闭其他: %q", a.menuKey(it.action))
		}
		if it.label == "移到下一个 pane" && it.run != nil {
			t.Error("with one pane 移到下一个 pane is not dim")
		}
	}
	a.menu = nil
	pinAt(a, p, 0)
	a.tabMenu(p.ID, 0, uv.Pos(10, 10))
	if !strings.HasSuffix(labels(), "取消固定") {
		t.Errorf("a pinned one: %s", labels())
	}
	a.menu = nil
	a.tabMenu(p.ID, 2, uv.Pos(10, 10))
	for _, it := range a.menu.items {
		if it.label == "向右拆分" {
			a.menu = nil
			it.run()
		}
	}
	if len(a.win().Root.Leaves()) != 2 || order(a.focused()) != "t3*" || len(p.Tabs) != 2 {
		t.Errorf("向右拆分: %d panes, new %s, old %s", len(a.win().Root.Leaves()), order(a.focused()), order(p))
	}
	only := a.focused()
	a.menu = nil
	a.tabMenu(only.ID, 0, uv.Pos(10, 10))
	for _, it := range a.menu.items {
		if it.label == "向下拆分" {
			a.menu = nil
			it.run()
		}
	}
	if n := len(a.win().Root.Leaves()); n != 3 || len(only.Tabs) != 1 || len(a.focused().Tabs) != 0 {
		t.Errorf("a pane's only tab only splits: %d panes, %d tabs, %d in the new one", n, len(only.Tabs), len(a.focused().Tabs))
	}
	a.menu = nil
	a.run("tab.new", 0)
	a.tabMenu(a.focused().ID, 0, uv.Pos(10, 10))
	if got := labels(); got != "关闭|向右拆分|向下拆分" {
		t.Errorf("a landing page's: %s", got)
	}
}

// The tab bar draws the pinned ones with the pin after the name, the menu over a table's tab
// and a landing page's (F22.6).
func TestGoldenPinnedTabs(t *testing.T) {
	for _, icons := range []string{"nerd", "ascii"} { // ascii's pin is a ^, not the * of the current tab
		t.Run(icons, func(t *testing.T) {
			a, p := tabsApp(t, 5)
			if icons == "ascii" {
				a = sized(160, 45, "ascii")
				p = a.win().Root.Leaves()[0]
				a.win().focus(p.ID)
				p.Tabs, p.Cur, p.Prev = nil, 0, -1
				for i := 1; i <= 5; i++ {
					name := fmt.Sprintf("t%d", i)
					putTab(p, Tab{Name: name, Data: newDataTab(a.sess.Conns[0], db.Table{Schema: "public", Name: name})}, false)
				}
			}
			pinAt(a, p, 1)
			pinAt(a, p, 3)
			a.menu = nil
			golden.RequireEqual(t, a.render().String())
		})
	}
}

// The tab menu drawn over a table's tab, a landing page's and a result's (F22.6).
func TestGoldenTabMenu(t *testing.T) {
	t.Run("table", func(t *testing.T) {
		a, p := tabsApp(t, 3)
		pinAt(a, p, 0)
		a.tabMenu(p.ID, 1, uv.Pos(30, 12))
		golden.RequireEqual(t, a.render().String())
	})
	t.Run("landing", func(t *testing.T) {
		a, p := tabsApp(t, 1)
		a.run("tab.new", 0)
		a.tabMenu(p.ID, 1, uv.Pos(30, 12))
		golden.RequireEqual(t, a.render().String())
	})
	t.Run("result", func(t *testing.T) {
		d := &execDB{res: map[string]db.Result{"select 1": rows(3)}}
		a, _ := inRun(t, d, "select 1")
		press(t, a, "<CR>")
		res := a.win().Result
		for i, tb := range res.Tabs {
			if tb.Result != nil && tb.Result.run != nil {
				a.tabMenu(res.ID, i, uv.Pos(30, 20))
				break
			}
		}
		if a.menu == nil {
			t.Fatal("no menu on a result's tab")
		}
		golden.RequireEqual(t, a.render().String())
	})
}
