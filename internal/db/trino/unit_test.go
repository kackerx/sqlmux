package trino

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/kackerx/sqlmux/internal/db"
)

// A DSN is trino:// with the catalog and schema in its path, or the Go
// client's http(s):// with them as parameters (§8.10).
func TestParseDSN(t *testing.T) {
	for dsn, want := range map[string]config{
		"trino://me@h/hive/web":                                   {endpoint: "http://h:8080", user: "me", catalog: "hive", schema: "web", session: map[string]string{}},
		"trino://me:pw@h:8443/hive?ssl=true":                      {endpoint: "https://h:8443", user: "me", password: "pw", catalog: "hive", session: map[string]string{}},
		"https://h?catalog=c&schema=s&session_properties=a:1;b:x": {endpoint: "https://h:443", user: "sqlmux", catalog: "c", schema: "s", session: map[string]string{"a": "1", "b": "x"}},
	} {
		got, err := parseDSN(dsn)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v %v, want %+v", dsn, got, err, want)
		}
	}
	if _, err := parseDSN("presto://h"); err == nil {
		t.Error("presto:// took")
	}
}

// Each JSON value as the text the server wrote it: a number's digits as they
// came, a string's own text, a structure's JSON, a varbinary by its size.
func TestJSONValues(t *testing.T) {
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(`[1.0E20, "a\"b", null, true, [1,{"k":"v"}], "AP8="]`), &raw); err != nil {
		t.Fatal(err)
	}
	got := values(raw, []string{"double", "varchar", "varchar", "boolean", "array(json)", "varbinary"})
	want := []db.Val{{S: "1.0E20"}, {S: `a"b`}, {Null: true}, {S: "true"}, {S: `[1,{"k":"v"}]`}, {S: db.BinaryShown(2)}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
}

// The line and column an error points at is a character of the statement, the
// message without its own "line 2:3: ".
func TestToError(t *testing.T) {
	e := toError(&serverError{Message: "line 2:3: mismatched input 'x'", ErrorName: "SYNTAX_ERROR", ErrorLocation: &struct {
		Line   int `json:"lineNumber"`
		Column int `json:"columnNumber"`
	}{2, 3}}, "sélect\nab x")
	if e.Position != 10 || e.Message != "mismatched input 'x'" || e.Name != "SYNTAX_ERROR" {
		t.Errorf("%+v", e)
	}
	for typ, want := range map[string]string{"bigint": "CAST($1 AS bigint)", "json": "json_parse($1)", "timestamp(3) with time zone": "CAST($1 AS timestamp(3) with time zone)"} {
		if got := (Engine{}).Typed("$1", typ); got != want {
			t.Errorf("Typed %s: %s", typ, got)
		}
	}
	for typ, want := range map[string]bool{"varchar": true, "decimal(10,2)": true, "varbinary": false, "array(integer)": false, "map(varchar, integer)": false, "row(a integer)": false} {
		if editable(typ) != want {
			t.Errorf("editable(%q)", typ)
		}
	}
}
