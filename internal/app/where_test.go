package app

import (
	"strings"
	"testing"

	"sqlmux/internal/config"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// Completion (§9.7): columns and keywords while a word is typed, an enum's
// values where one goes; the first is picked as the list opens, Tab / S-Tab
// move, ↵ takes the pick, esc closes the list before the input.
func TestWhereCompletion(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
	feed(t, a, "/sta")
	if tab.comp == nil || tab.comp.items[0].label != "status" || tab.comp.sel != 0 || a.mode() != keymap.Insert {
		t.Fatalf("sta: %+v", tab.comp)
	}
	f := a.render()
	at := a.whereAt(a.focused(), tab)
	row := strings.Split(f.String(), "\n")[at.Y+2] // under the input and the list's border
	if i := strings.Index(row, "status"); i < 0 || f.Buf.CellAt(ui.Width(row[:i]), at.Y+2).Style.Bg != a.theme.Warn {
		t.Errorf("the match lit: %q", row)
	}
	feed(t, a, "<CR>")
	if tab.where.Text != "status" || tab.comp != nil || tab.applied != "" {
		t.Fatalf("↵ takes the first: %q applied %q", tab.where.Text, tab.applied)
	}
	feed(t, a, " = ")
	var labels []string
	for _, c := range tab.comp.items {
		labels = append(labels, c.label)
	}
	if strings.Join(labels, " ") != "pending running done failed" {
		t.Fatalf("values: %v", labels)
	}
	for _, c := range []struct {
		keys string
		sel  int
	}{{"<Tab>", 1}, {"<S-Tab>", 0}, {"<S-Tab>", 0}, {"<C-n><Down>", 2}, {"<Up>", 1}, {"<C-p>", 0}, {"<Tab>", 1}} {
		if feed(t, a, c.keys); tab.comp.sel != c.sel {
			t.Errorf("%s: sel %d, want %d", c.keys, tab.comp.sel, c.sel)
		}
	}
	feed(t, a, "<CR>")
	if tab.where.Text != "status = 'running'" || tab.applied != "" {
		t.Fatalf("↵ on running: %q applied %q", tab.where.Text, tab.applied)
	}
	feed(t, a, " and pa<Esc>")
	if tab.comp != nil || tab.typing != "where" {
		t.Fatal("the first esc closes the list")
	}
	feed(t, a, "<CR>")
	if tab.applied != "status = 'running' and pa" || tab.typing != "" {
		t.Errorf("↵ with the list closed runs: %q", tab.applied)
	}
	feed(t, a, "/ i<Esc><Esc>")
	if tab.typing != "" {
		t.Error("the second esc leaves the input")
	}
}

// Every WHERE run goes into the table's history; C-r lists favorites then
// history, filtered by what is typed; C-f stars; ↵ runs one with its ORDER
// and LIMIT; it all outlives a restart (Q-02, §14).
func TestWhereHistory(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
	feed(t, a, "/id > 5<CR>")
	tab.order, tab.limit = "amount", 500
	clear := strings.Repeat("<BS>", 20)
	feed(t, a, "/"+clear+"status = 'done'<CR>")
	tab.order, tab.limit = "", 100
	feed(t, a, "/"+clear+"id > 5<CR>") // again: to the top, once
	st := a.tableState(tab)
	if len(st.History) != 2 || st.History[0].Where != "id > 5" || st.History[1].Limit != 500 {
		t.Fatalf("history %+v", st.History)
	}
	feed(t, a, "/"+clear+"<C-r>")
	if tab.hist == nil || a.mode() != keymap.Command {
		t.Fatal("C-r opens the list")
	}
	feed(t, a, "<C-n><C-f>")
	if len(st.Favorites) != 1 || st.Favorites[0].Where != "status = 'done'" {
		t.Fatalf("favorites %+v", st.Favorites)
	}
	if f := a.render().String(); !strings.Contains(f, "收藏") || !strings.Contains(f, "amount ↑ · 500") {
		t.Errorf("the list:\n%s", f)
	}
	feed(t, a, clear+"don")                                      // filters the list
	if es, _ := a.histEntries(tab); len(es) != 2 || !es[0].fav { // the favorite, and its run
		t.Fatalf("filtered: %+v", es)
	}
	feed(t, a, "<CR>")
	if tab.applied != "status = 'done'" || tab.order != "amount" || tab.limit != 500 || tab.hist != nil {
		t.Fatalf("applied %q order %q limit %d", tab.applied, tab.order, tab.limit)
	}

	loaded, err := config.LoadState()
	if err != nil || len(loaded.Tables["doraemon/public.t_order"].Favorites) != 1 {
		t.Fatalf("after a restart: %+v %v", loaded, err)
	}
}

// The palette's recent picks are kept in the state by kind and id (§12, §14).
func TestPaletteRecentInState(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>@t_user<CR>")
	if r := a.state.Recent; len(r) != 1 || r[0] != (config.Recent{Kind: "table", ID: "public.t_user"}) {
		t.Fatalf("recent %+v", r)
	}
	a.state.Recent = append(a.state.Recent, config.Recent{Kind: "command", ID: "pane.zoom"})
	if names := namesOf(openPaletteOn(t, a)); names[0] != "表:t_user" || names[1] != "命令:缩放 / 还原" {
		t.Errorf("recent first: %v", names[:2])
	}
}

func openPaletteOn(t *testing.T, a *App) *App {
	t.Helper()
	feed(t, a, "<C-p>")
	return a
}
