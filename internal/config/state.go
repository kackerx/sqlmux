package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// State is what the app keeps between runs (§14), in state.json: the
// palette's recent picks, and each table's WHERE history and favorites.
type State struct {
	Recent []Recent               `json:"recent,omitempty"`
	Tables map[string]*TableState `json:"tables,omitempty"` // by "<connection>/<schema>.<table>"
}

// Recent is one palette pick (§12): its kind and its id within the kind.
type Recent struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type TableState struct {
	History   []Query `json:"history,omitempty"` // newest first
	Favorites []Query `json:"favorites,omitempty"`
}

// Query is a WHERE as it ran, with the ORDER and LIMIT it ran with (Q-02).
type Query struct {
	Where string    `json:"where"`
	Order string    `json:"order,omitempty"`
	Desc  bool      `json:"desc,omitempty"`
	Limit int       `json:"limit,omitempty"`
	At    time.Time `json:"at"`
}

// StateDir is $XDG_STATE_HOME/sqlmux, falling back to ~/.local/state/sqlmux (§14).
func StateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "sqlmux")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "sqlmux")
}

func statePath() string { return filepath.Join(StateDir(), "state.json") }

// LoadState reads state.json; a missing file is an empty state. One that
// doesn't read is moved to state.json.broken, so the next save can't
// overwrite what the user had, and the state starts empty (§14).
func LoadState() (*State, error) {
	data, err := os.ReadFile(statePath())
	if errors.Is(err, fs.ErrNotExist) {
		return &State{}, nil
	}
	s := &State{}
	if err == nil {
		err = json.Unmarshal(data, s)
	}
	if err != nil {
		if rerr := os.Rename(statePath(), statePath()+".broken"); rerr != nil { // the next save will overwrite it
			err = errors.Join(err, rerr)
		}
		return &State{}, fmt.Errorf("%s: %w", statePath(), err)
	}
	return s, nil
}

var (
	saveMu          sync.Mutex
	tick, savedTick int // the last snapshot's, and the one on disk's
)

// Snapshot serializes s as it is now and returns what writes it (§14):
// call it in Update, run what it returns on a Cmd's goroutine. Writes run
// in any order; one of an older snapshot than the file holds does nothing.
func Snapshot(s *State) func() error {
	data, err := json.Marshal(s)
	saveMu.Lock()
	tick++
	t := tick
	saveMu.Unlock()
	return func() error {
		if err != nil {
			return err
		}
		return save(data, t)
	}
}

// save writes the file whole and renames it into place, readable by the
// user only (§13: history holds literals).
func save(data []byte, t int) error {
	saveMu.Lock()
	defer saveMu.Unlock()
	if t <= savedTick {
		return nil
	}
	if err := os.MkdirAll(StateDir(), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(StateDir(), "state-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // gone once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), statePath()); err != nil {
		return err
	}
	savedTick = t
	return nil
}
