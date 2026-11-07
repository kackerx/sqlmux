package clickhouse

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kackerx/sqlmux/internal/db"
)

// A DSN is clickhouse:// or the interface's own http(s)://, the HTTP port by
// default; the native protocol's port is said to be the wrong one (§8.9).
func TestParseDSN(t *testing.T) {
	for dsn, want := range map[string]config{
		"clickhouse://u:p@h/db":                          {endpoint: "http://h:8123/", user: "u", password: "p", database: "db", settings: map[string][]string{}},
		"clickhouse://h:9123?secure=true":                {endpoint: "https://h:9123/", user: "default", settings: map[string][]string{}},
		"clickhouse://h?secure=1&max_threads=2":          {endpoint: "https://h:8443/", user: "default", settings: map[string][]string{"max_threads": {"2"}}},
		"https://h/?database=x&user=me&skip_verify=true": {endpoint: "https://h:8443/", user: "me", database: "x", insecure: true, settings: map[string][]string{}},
		"http://[::1]:8123/a":                            {endpoint: "http://[::1]:8123/", user: "default", database: "a", settings: map[string][]string{}},
	} {
		got, err := parseDSN(dsn)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v %v, want %+v", dsn, got, err, want)
		}
	}
	for _, dsn := range []string{"clickhouse://h:9000/db", "tcp://h/db"} {
		if _, err := parseDSN(dsn); err == nil {
			t.Errorf("%s: no error", dsn)
		}
	}
}

// TabSeparated's escapes undone, \N alone a NULL (§8.9).
func TestCell(t *testing.T) {
	for in, want := range map[string]db.Val{
		`\N`:            {Null: true},
		`\\N`:           {S: `\N`},
		`a\tb\nc\\d\'e`: {S: "a\tb\nc\\d'e"},
		`x\0y`:          {S: "x\x00y"},
		"\xff":          {S: db.BinaryShown(1)},
	} {
		if got := cell(in); got != want {
			t.Errorf("%q: %+v, want %+v", in, got, want)
		}
	}
}

// The server's error text is its number, its name and the message, the
// version's (official build) and all left out.
func TestParseError(t *testing.T) {
	err := parseError("Code: 47. DB::Exception: Unknown identifier `x` in scope SELECT x. Maybe you meant: ['y']. (UNKNOWN_IDENTIFIER) (version 25.8.33.6 (official build))\n")
	e, ok := err.(Error)
	if !ok || e.Code != 47 || e.Name != "UNKNOWN_IDENTIFIER" || e.Message != "Unknown identifier `x` in scope SELECT x. Maybe you meant: ['y']" {
		t.Errorf("%#v", err)
	}
	if se := (Engine{}).ErrorOf(err); se.Code != "UNKNOWN_IDENTIFIER" || strings.Contains(se.Message, "version") {
		t.Errorf("%+v", se)
	}
	if e := parseError("Code: 516. DB::Exception: sqlmux: Authentication failed. (AUTHENTICATION_FAILED) (version 25.8.1.1)").(Error); e.Name != "AUTHENTICATION_FAILED" {
		t.Errorf("%#v", e)
	}
	if e := parseError("bad gateway").(Error); e.Message != "bad gateway" || e.Code != 0 {
		t.Errorf("%#v", e)
	}
}

// What the grid can and cannot set from text, a key of columns or of none,
// an enum's labels in order (§10.1, §10.2).
func TestColumnRules(t *testing.T) {
	for typ, want := range map[string]bool{
		"String": true, "Nullable(Decimal(10, 2))": true, "Array(String)": true, "LowCardinality(Nullable(String))": true,
		"Map(String, String)": false, "Nullable(Tuple(a UInt8))": false, "JSON": false, "Array(Map(String, UInt8))": false, "AggregateFunction(sum, UInt64)": false,
	} {
		if got := castable(typ); got != want {
			t.Errorf("castable(%q) = %v", typ, got)
		}
	}
	cols := []db.Column{{Name: "id"}, {Name: "user_id"}, {Name: "at"}}
	for key, want := range map[string][]string{"id": {"id"}, "id, `user_id`": {"id", "user_id"}, "": nil, "toDate(at)": nil, "id, toDate(at)": nil} {
		if got := keyColumns(key, cols); !reflect.DeepEqual(got, want) {
			t.Errorf("keyColumns(%q) = %v", key, got)
		}
	}
	if got := enumLabels("Nullable(Enum8('a' = 1, 'it''s' = 2, 'b\\\\c' = -3))"); !reflect.DeepEqual(got, []string{"a", "it's", `b\c`}) {
		t.Errorf("labels %q", got)
	}
	if enumLabels("String") != nil {
		t.Error("a String has labels")
	}
	if got := (Engine{}).Quote("we`ird", `a\b`); got != "`we\\`ird`.`a\\\\b`" {
		t.Errorf("quote %s", got)
	}
	if got := useTarget("use `my db`"); got != "my db" {
		t.Errorf("use %q", got)
	}
}
