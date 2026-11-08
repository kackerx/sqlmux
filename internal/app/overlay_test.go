package app

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"sort"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/ui"
)

// overlayFields are the App fields of the overlays topOverlay names.
var overlayFields = map[string]string{
	"confirm": "confirm", "menu": "menu", "value": "value", "ddl": "ddl", "keyHelp": "keyhelp",
	"palette": "palette", "form": "form", "sessList": "sessions", "drop": "dropdown", "cols": "cols",
}

// drawnOrder is the overlays in the order drawOn paints them, bottom to top,
// read from its source: the statements of its body that ask for one.
func drawnOrder(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "view.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "drawOn" {
			continue
		}
		for _, st := range fn.Body.List {
			ifs, ok := st.(*ast.IfStmt)
			if !ok {
				continue
			}
			var field string
			for _, part := range []ast.Node{ifs.Init, ifs.Cond} {
				if part == nil {
					continue
				}
				ast.Inspect(part, func(n ast.Node) bool {
					if sel, ok := n.(*ast.SelectorExpr); ok && field == "" {
						if id, ok := sel.X.(*ast.Ident); ok && id.Name == "a" && overlayFields[sel.Sel.Name] != "" {
							field = sel.Sel.Name
						}
					}
					return true
				})
			}
			if field != "" {
				order = append(order, overlayFields[field])
			}
		}
	}
	return order
}

// openOverlays are the overlays open, as topOverlay names them.
func openOverlays(a *App) []string {
	var s []string
	for _, c := range []struct {
		open bool
		name string
	}{
		{a.confirm != nil, "confirm"}, {a.menu != nil, "menu"}, {a.value != nil, "value"}, {a.ddl != nil, "ddl"},
		{a.keyHelp != nil, "keyhelp"}, {a.palette != nil, "palette"}, {a.form != nil, "form"}, {a.sessList != nil, "sessions"},
		{a.winRename != nil || a.askName != nil, "rename"}, {a.drop != nil, "dropdown"}, {a.cols != nil, "cols"},
	} {
		if c.open {
			s = append(s, c.name)
		}
	}
	return s
}

// The overlays are drawn from the bottom up in the reverse of the order
// topOverlay gives the keys in, the rename's input being on the status bar: the
// one that has the keys is the one on top (F13.7).
func TestOverlaysDrawnInReverseOfTopOverlay(t *testing.T) {
	// topOverlay's order, as the overlays open one at a time and closed in the order show
	a := &App{}
	var keys []string
	for _, step := range []func(){
		func() { a.confirm = &confirmBox{} }, func() { a.menu = &menu{} }, func() { a.value = &valueEditor{} },
		func() { a.ddl = &ddlPopup{} }, func() { a.keyHelp = &keyHelp{} }, func() { a.palette = &palette{} },
		func() { a.form = &connForm{} }, func() { a.sessList = &sessionList{} }, func() { a.drop = &dropdown{} }, func() { a.cols = &colsMenu{} },
	} {
		step()
	}
	for ov := a.topOverlay(); ov != ""; ov = a.topOverlay() {
		keys = append(keys, ov)
		switch ov {
		case "confirm":
			a.confirm = nil
		case "menu":
			a.menu = nil
		case "value":
			a.value = nil
		case "ddl":
			a.ddl = nil
		case "keyhelp":
			a.keyHelp = nil
		case "palette":
			a.palette = nil
		case "form":
			a.form = nil
		case "sessions":
			a.sessList = nil
		case "dropdown":
			a.drop = nil
		case "cols":
			a.cols = nil
		}
	}
	slices.Reverse(keys)
	if got := drawnOrder(t); !slices.Equal(got, keys) {
		t.Errorf("drawn %v, the keys' order reversed %v", got, keys)
	}
}

var overlayBases = []struct {
	name string
	open func(a *App)
}{
	{"cols", func(a *App) { a.run("grid.cols", 0) }},
	{"order", func(a *App) { a.run("grid.order", 0) }},
	{"dropdown", func(a *App) { a.openDrop(dropLimit) }},
	{"sessions", func(a *App) { a.run("session.list", 0) }},
	{"form", func(a *App) { a.openConnForm() }},
	{"conns", func(a *App) { a.openConnManager() }},
	{"keyhelp", func(a *App) { a.run("keyhelp.open", 0) }},
	{"value", func(a *App) { a.run("grid.value", 0) }},
	{"ddl", func(a *App) { a.run("grid.ddl.table", 0) }},
}

// Every overlay open, then C-p and a command of the palette's picked, which may
// open another over it: where two are left open, the keys are the one drawn on
// top's (F13.7).
func TestStackedOverlaysKeysGoToTheOneOnTop(t *testing.T) {
	drawn := drawnOrder(t)
	rank := func(name string) int { return slices.Index(drawn, name) }
	var ids []string
	for id, act := range actions {
		if act.Title != "" && !act.Local && act.On == nil && id != "quit" && !strings.HasPrefix(id, "session.close") {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	seen := map[string]bool{}
	for _, b := range overlayBases {
		for _, id := range ids {
			func() {
				defer func() { _ = recover() }() // a command that wants what is not there
				a, _, _ := withRecorder(t)
				b.open(a)
				if len(openOverlays(a)) == 0 {
					t.Fatalf("%s: not open", b.name)
				}
				if feed(t, a, "<C-p>"); a.palette == nil {
					return // C-p does not get past it
				}
				a.palette = nil
				a.run(id, 0)
				open := slices.DeleteFunc(openOverlays(a), func(n string) bool { return n == "rename" })
				if len(open) < 2 {
					return
				}
				top := slices.MaxFunc(open, func(x, y string) int { return rank(x) - rank(y) })
				key := fmt.Sprintf("%s %v", b.name, open)
				if got := a.topOverlay(); got != top && !seen[key] {
					seen[key] = true
					t.Errorf("%s, then the palette's %s: open %v, the keys go to %q, %q is on top", b.name, id, open, got, top)
				}
			}()
		}
	}
}

// A question lets only its own keys and the cancel through: C-p, the leader's
// keys and any other action open nothing under it (§6.4, F13.7).
func TestConfirmLetsOnlyItsOwnThrough(t *testing.T) {
	a, _, _ := withRecorder(t)
	a.confirm = &confirmBox{text: "?", yes: "y", then: func() tea.Cmd { return nil }}
	feed(t, a, "<C-p>")
	for _, id := range []string{"palette.open", "session.list", "keyhelp.open", "grid.cols"} {
		a.run(id, 0)
	}
	if a.palette != nil || a.sessList != nil || a.keyHelp != nil || a.cols != nil || a.confirm == nil {
		t.Errorf("open under the question: %v", openOverlays(a))
	}
	a.run("confirm.no", 0)
	if a.confirm != nil {
		t.Error("its own action does not close it")
	}
}

// The mode the status bar shows is the one the keys are in, whatever overlay
// is over the connection form (F13.7).
func TestModeIsTheTopOverlays(t *testing.T) {
	for name, over := range map[string]func(a *App){
		"confirm": func(a *App) { a.confirm = &confirmBox{text: "?", yes: "y", then: func() tea.Cmd { return nil }} },
		"palette": func(a *App) { feed(t, a, "<C-p>") },
		"keyhelp": func(a *App) { a.run("keyhelp.open", 0) },
	} {
		a, _, _ := withRecorder(t)
		a.openConnForm()
		over(a)
		if a.context().Mode != a.mode() {
			t.Errorf("%s over the form: context %v, mode() %v", name, a.context().Mode, a.mode())
		}
	}
}

// The lists that keep a top hold it while the selection moves in view, down
// and up again: the form's groups, the right-click menu, the WHERE history and
// a cell's options (F13.10).
func TestListsHoldTheirTop(t *testing.T) {
	// the form's groups: twelve, ↓ to the ninth, ↑ again
	file := groupedFile
	for i := range 12 {
		file += fmt.Sprintf("\n[[connection]]\nname = \"c%02d\"\ngroup = \"p%02d\"\nengine = \"postgres\"\ndsn = \"postgres://u@h/c%02d\"\n", i, i, i)
	}
	a := groupedIn(t, file)
	feed(t, a, "<C-p>conn.new<CR>n<Tab>p")
	a.render()
	var tops []int
	for _, k := range strings.Fields("Down Down Down Down Down Down Down Down Down Up Up Up") {
		feed(t, a, "<"+k+">")
		a.render()
		tops = append(tops, a.form.pickTop)
	}
	if tops[8] == 0 || tops[9] != tops[8] || tops[10] != tops[8] || tops[11] != tops[8] {
		t.Errorf("the groups' top while ↓ then ↑: %v", tops)
	}
	// the menu: twenty items in the rows there are
	b := wide(160, 20)
	tab := loadOrders(t, b, 30)
	b.menu = &menu{at: uv.Pos(40, 4)}
	for i := range 20 {
		b.menu.items = append(b.menu.items, menuItem{label: fmt.Sprint("item ", i)})
	}
	tops = nil
	for _, sel := range []int{12, 11, 10, 9} {
		b.menu.sel = sel
		b.render()
		tops = append(tops, b.menu.top)
	}
	if tops[0] == 0 || tops[1] != tops[0] || tops[2] != tops[0] || tops[3] != tops[0] {
		t.Errorf("the menu's top: %v", tops)
	}
	// the WHERE history: more entries than rows
	b.menu = nil
	st := b.tableState(tab)
	for i := range 40 {
		st.History = append(st.History, config.Query{Where: fmt.Sprint("id = ", i)})
	}
	feed(t, b, "/<C-r>")
	if tab.hist == nil {
		t.Fatal("no history")
	}
	tops = nil
	for _, d := range []int{14, -1, -1, -1} {
		b.histMove(tab, d)
		b.render()
		tops = append(tops, tab.hist.top)
	}
	if tops[0] == 0 || tops[1] != tops[0] || tops[2] != tops[0] || tops[3] != tops[0] {
		t.Errorf("the history's top: %v", tops)
	}
	// a cell's options: an enum of twenty
	c := wide(160, 30)
	ctab := loadOrders(t, c, 5)
	for i := range ctab.cols.Cols {
		if ctab.cols.Cols[i].Name == "status" {
			ctab.cols.Cols[i].Enum = nil
			for j := range 20 {
				ctab.cols.Cols[i].Enum = append(ctab.cols.Cols[i].Enum, fmt.Sprint("state_", j))
			}
		}
	}
	feed(t, c, "li")
	if ctab.cell == nil {
		t.Fatal("no cell edit")
	}
	tops = nil
	for _, k := range strings.Fields("Down Down Down Down Down Down Down Down Down Down Down Down Up Up Up") {
		feed(t, c, "<"+k+">")
		c.render()
		tops = append(tops, ctab.cell.top)
	}
	if tops[11] == 0 || tops[12] != tops[11] || tops[13] != tops[11] || tops[14] != tops[11] {
		t.Errorf("the options' top: %v", tops)
	}
}

// The terminal cursor is the top overlay's, whatever lies under it: an input
// open under another overlay leaves none, and what is on top shows its own
// (F13.7).
func TestCursorIsTheTopOverlays(t *testing.T) {
	for name, c := range map[string]struct {
		under, over func(a *App)
	}{
		"rename of a session, the manager over it": {
			func(a *App) { a.run("session.list", 0); feed(t, a, "$") },
			func(a *App) { a.openConnManager() },
		},
		"the filter of COLS, the sessions over it": {
			func(a *App) { a.run("grid.cols", 0); feed(t, a, "/") },
			func(a *App) { a.run("session.list", 0) },
		},
		"the filter of ORDER, the column's DDL over it": {
			func(a *App) { a.run("grid.order", 0); feed(t, a, "/") },
			func(a *App) { a.run("grid.ddl.table", 0) },
		},
		"the form, the DDL over it": {
			func(a *App) { a.openConnForm() },
			func(a *App) { a.run("grid.ddl.table", 0) },
		},
	} {
		a, _, _ := withRecorder(t)
		c.under(a)
		if a.render().Cursor == nil {
			t.Logf("%s: the lower one shows no cursor of its own, nothing to leak", name)
		}
		c.over(a)
		if got := a.render().Cursor; got != nil { // what is on top has no input: the lists and the DDL
			t.Errorf("%s: top %q, the cursor at %v is the one under it", name, a.topOverlay(), *got)
		}
	}
	// the which-key of a Ctrl leader over the palette: its input is not the one with the keys
	w := configured(t, 100, 24, "[keys]\nleader = \"<C-a>\"")
	feed(t, w, "<C-p>")
	if w.render().Cursor == nil {
		t.Fatal("the palette has no cursor")
	}
	feed(t, w, "<C-a>")
	due(w)
	if !w.whichKey {
		t.Fatal("no which-key")
	}
	if got := w.render().Cursor; got != nil {
		t.Errorf("the which-key over the palette: the cursor at %v is the palette's", *got)
	}
}

// Clicking a row of the form that is over the session list is the form's, not
// the list's: it does not switch the session under it (§6.4, F13.7).
func TestClickOnTheFormOverSessions(t *testing.T) {
	a, _, _ := withRecorder(t)
	c := a.sess.Conns[0]
	a.sessions = append(a.sessions, newSession("report", []*Conn{c}, c, a.freeQuery()))
	sess := a.sess
	a.run("session.list", 0)
	a.openConnForm()
	a.View()
	if a.topOverlay() != "form" {
		t.Fatalf("top %q", a.topOverlay())
	}
	var hit *ui.Hit
	for _, h := range a.hits {
		if h.Target.Kind == ui.KindRow && h.Target.I == 1 && h.Target.Action == "" {
			hh := h
			hit = &hh
		}
	}
	if hit == nil {
		t.Fatal("no row 1 of the form")
	}
	click(a, hit.Rect.Min)
	if a.sess != sess || a.form == nil || a.form.sel != 1 {
		t.Errorf("the click went to the session list under the form: session %q, form open %v", a.sess.Name, a.form != nil)
	}
}

// The rename's input has its place among the overlays: over a dropdown, under
// the session list (F13.7).
func TestRenamePlaceAmongOverlays(t *testing.T) {
	a := &App{}
	a.drop, a.askName = &dropdown{}, &nameAsk{}
	if got := a.topOverlay(); got != "rename" {
		t.Errorf("a name asked over a dropdown: %q", got)
	}
	a.sessList = &sessionList{}
	if got := a.topOverlay(); got != "sessions" {
		t.Errorf("the session list over a name asked: %q", got)
	}
}
