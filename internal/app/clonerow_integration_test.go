//go:build integration

package app

import (
	"slices"
	"testing"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
)

// A row cloned and saved is a row more in the table: the key the database's
// next, the generated column its own, the rest what the row had (F22.4).
func TestCloneRowSaves(t *testing.T) {
	eachServer(t, func(t *testing.T, eng, _ string) {
		a := opened(t, config.Connection{Name: "play", Engine: eng, DSN: ownDB(t, eng)})
		play := a.sess.Conns[0]
		key := "id serial primary key"
		if eng == "mysql" {
			key = "id int auto_increment primary key"
		}
		for _, s := range []string{
			"create table t_clone (" + key + ", name varchar(20), qty int, twice int generated always as (qty * 2) stored)",
			"insert into t_clone (name, qty) values ('ann', 3), ('bob', 4)",
		} {
			if _, err := play.Main.Query(t.Context(), s); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
		}
		settle(a, a.loadCatalog(play))
		i := slices.IndexFunc(play.Tables, func(tb db.Table) bool { return tb.Schema == play.Schema && tb.Name == "t_clone" })
		settle(a, a.openTable(play, play.Tables[i]))
		tab := dataOf(a.focused())
		if tab == nil || len(tab.page.Rows) != 2 {
			t.Fatalf("t_clone: %+v", tab)
		}
		keys(t, a, "yp")
		if len(tab.added) != 1 {
			t.Fatalf("yp: %d rows added, toast %q", len(tab.added), a.toast)
		}
		settle(a, a.save())
		r, err := play.Meta.Query(t.Context(), "select id, name, qty, twice from t_clone order by id")
		if err != nil || len(r.Rows) != 3 {
			t.Fatalf("after the save: %v %v", r.Rows, err)
		}
		var got [][]string
		for _, row := range r.Rows {
			got = append(got, []string{row[0].S, row[1].S, row[2].S, row[3].S})
		}
		want := [][]string{{"1", "ann", "3", "6"}, {"2", "bob", "4", "8"}, {"3", "ann", "3", "6"}}
		if !slices.EqualFunc(got, want, slices.Equal) { // the clone is under row 1, the new id is the next in the sequence
			t.Errorf("rows %v, want %v", got, want)
		}
	})
}
