package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// state.json round-trips, is 0600, and an older snapshot never overwrites
// a newer one (§14).
func TestState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if s, err := LoadState(); err != nil || len(s.Tables) != 0 {
		t.Fatalf("no file: %+v %v", s, err)
	}
	at := time.Date(2026, 9, 24, 14, 5, 0, 0, time.UTC)
	want := &State{
		Recent: []Recent{{Kind: "table", ID: "public.t_order"}},
		Tables: map[string]*TableState{"doraemon/public.t_order": {
			History:   []Query{{Where: "id > 5", At: at}},
			Favorites: []Query{{Where: "status = 'done'", Order: "amount", Desc: true, Limit: 500, At: at}},
		}},
	}
	save := func(s *State, tick int) {
		t.Helper()
		data, _ := json.Marshal(s)
		if err := SaveState(data, tick); err != nil {
			t.Fatal(err)
		}
	}
	save(want, 5)
	save(&State{}, 4) // an older snapshot, saved late
	got, err := LoadState()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v %v", got, err)
	}
	info, _ := os.Stat(filepath.Join(StateDir(), "state.json"))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode().Perm())
	}
	path := filepath.Join(StateDir(), "state.json")
	os.WriteFile(path, []byte("{"), 0o600)
	if s, err := LoadState(); err == nil || len(s.Tables) != 0 {
		t.Error("a broken file is an error, and an empty state")
	}
	if b, err := os.ReadFile(path + ".broken"); err != nil || string(b) != "{" {
		t.Errorf("the broken file is kept aside: %q %v", b, err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("and out of the next save's way")
	}
}
