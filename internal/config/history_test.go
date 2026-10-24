package config

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func historyFile(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// A statement is one line of JSON, with what it came to.
func TestAppendHistoryFormat(t *testing.T) {
	historyFile(t)
	at := time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC)
	if err := AppendHistory(History{At: at, Conn: "play", Schema: "public", SQL: "select 1\nfrom t", Tag: "SELECT 1", TookMS: 3}); err != nil {
		t.Fatal(err)
	}
	if err := AppendHistory(History{At: at, Conn: "play", SQL: "selec", Error: "syntax error"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(historyPath())
	want := `{"at":"2026-10-12T09:30:00Z","conn":"play","schema":"public","sql":"select 1\nfrom t","tag":"SELECT 1","took_ms":3}
{"at":"2026-10-12T09:30:00Z","conn":"play","sql":"selec","took_ms":0,"error":"syntax error"}
`
	if string(data) != want {
		t.Fatalf("history.jsonl:\n%s\nwant:\n%s", data, want)
	}
	if fi, _ := os.Stat(historyPath()); fi.Mode().Perm() != 0o600 && runtime.GOOS != "windows" { // windows has no group/other bits to strip
		t.Fatalf("mode %v: the history is the user's alone (§13)", fi.Mode())
	}
}

// A statement past 64 KB is cut there, at a character, and says so.
func TestAppendHistoryTruncates(t *testing.T) {
	historyFile(t)
	long := strings.Repeat("a", historySQLMax-1) + "é" + "tail" // é straddles the limit
	if err := AppendHistory(History{Conn: "c", SQL: long}); err != nil {
		t.Fatal(err)
	}
	hs, err := LoadHistory()
	if err != nil || len(hs) != 1 {
		t.Fatalf("%v %v", hs, err)
	}
	if !hs[0].Truncated || hs[0].SQL != long[:historySQLMax-1] {
		t.Fatalf("truncated %v, %d bytes", hs[0].Truncated, len(hs[0].SQL))
	}
	if err := AppendHistory(History{Conn: "c", SQL: "short"}); err != nil {
		t.Fatal(err)
	}
	if hs, _ := LoadHistory(); hs[1].Truncated {
		t.Fatal("a short statement is not truncated")
	}
}

// Writers with a file of their own each (as two processes have) lose none.
func TestAppendHistoryConcurrent(t *testing.T) {
	historyFile(t)
	var wg sync.WaitGroup
	for w := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 50 {
				if err := AppendHistory(History{Conn: "c", SQL: fmt.Sprintf("select %d, %d", w, i)}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	hs, err := LoadHistory()
	seen := map[string]bool{}
	for _, h := range hs {
		seen[h.SQL] = true
	}
	if err != nil || len(hs) != 400 || len(seen) != 400 {
		t.Fatalf("%d lines, %d distinct: %v", len(hs), len(seen), err)
	}
}

// The file is cut to the newest history_max lines, no more than that; and
// left alone when it is not past it or is not there.
func TestTrimHistory(t *testing.T) {
	historyFile(t)
	if err := TrimHistory(3); err != nil {
		t.Fatalf("no file: %v", err)
	}
	for i := range 5 {
		_ = AppendHistory(History{Conn: "c", SQL: fmt.Sprint("select ", i)})
	}
	if err := TrimHistory(5); err != nil {
		t.Fatal(err)
	}
	if hs, _ := LoadHistory(); len(hs) != 5 {
		t.Fatalf("at the limit: %d", len(hs))
	}
	if err := TrimHistory(3); err != nil {
		t.Fatal(err)
	}
	hs, _ := LoadHistory()
	if len(hs) != 3 || hs[0].SQL != "select 2" || hs[2].SQL != "select 4" {
		t.Fatalf("%+v", hs)
	}
	if err := AppendHistory(History{Conn: "c", SQL: "select 5"}); err != nil {
		t.Fatal(err)
	}
	if hs, _ := LoadHistory(); len(hs) != 4 {
		t.Fatalf("an append after the trim: %d", len(hs))
	}
}

// A line that is no entry is left out of what is read, not an error.
func TestLoadHistorySkipsBadLines(t *testing.T) {
	historyFile(t)
	_ = AppendHistory(History{Conn: "c", SQL: "select 1"})
	f, _ := os.OpenFile(historyPath(), os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("not json\n")
	f.Close()
	_ = AppendHistory(History{Conn: "c", SQL: "select 2"})
	if hs, err := LoadHistory(); err != nil || len(hs) != 2 {
		t.Fatalf("%v %v", hs, err)
	}
}

func TestParseHistoryMax(t *testing.T) {
	if c, err := Parse(""); err != nil || c.HistoryMax != 1000 {
		t.Fatalf("default: %v %v", c, err)
	}
	if c, err := Parse("[query]\nhistory_max = 50\n"); err != nil || c.HistoryMax != 50 {
		t.Fatalf("%v %v", c, err)
	}
	if _, err := Parse("[query]\nhistory_max = 0\n"); err == nil {
		t.Fatal("0 is no limit to keep")
	}
}

// A statement run in another database of the connection says which; one in the
// DSN's does not, and a line of before F15.1 reads as that (F15.1).
func TestHistoryDatabase(t *testing.T) {
	historyFile(t)
	if err := AppendHistory(History{Conn: "play", DB: "other", SQL: "select 1"}); err != nil {
		t.Fatal(err)
	}
	if err := AppendHistory(History{Conn: "play", SQL: "select 2"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(historyPath())
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if !strings.Contains(lines[0], `"db":"other"`) || strings.Contains(lines[1], `"db"`) {
		t.Fatalf("history.jsonl:\n%s", data)
	}
	f, _ := os.OpenFile(historyPath(), os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"at":"2026-10-12T09:30:00Z","conn":"play","sql":"old","took_ms":1}` + "\n")
	f.Close()
	hs, err := LoadHistory()
	if err != nil || len(hs) != 3 || hs[0].DB != "other" || hs[1].DB != "" || hs[2].DB != "" || hs[2].SQL != "old" {
		t.Fatalf("%+v %v", hs, err)
	}
}
