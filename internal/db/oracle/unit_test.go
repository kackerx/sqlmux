package oracle

import (
	"testing"
	"time"

	"github.com/kackerx/sqlmux/internal/db"
)

func TestDSN(t *testing.T) {
	if got, err := DSN("oracle://scott@h:1521/ORCLPDB1?SSL=true", "p w"); err != nil || got != "oracle://scott:p%20w@h:1521/ORCLPDB1?SSL=true" {
		t.Errorf("%s %v", got, err)
	}
	if got, _ := DSN("oracle://scott:tiger@h/X", ""); got != "oracle://scott:tiger@h/X" {
		t.Errorf("%s", got)
	}
	if _, err := DSN("jdbc:oracle:thin:@h:1521/X", ""); err == nil {
		t.Error("a JDBC URL took")
	}
}

// A column's type as a CREATE TABLE writes it, from the catalog's parts.
func TestTypeOf(t *testing.T) {
	v := func(s ...string) []db.Val {
		out := make([]db.Val, len(s))
		for i, x := range s {
			out[i] = db.Val{S: x, Null: x == "-"}
		}
		return out
	}
	for want, f := range map[string][]db.Val{
		"NUMBER(10,2)":       v("NUMBER", "10", "2", "0", "", "22"),
		"NUMBER(10)":         v("NUMBER", "10", "0", "0", "", "22"),
		"INTEGER":            v("NUMBER", "-", "0", "0", "", "22"),
		"NUMBER":             v("NUMBER", "-", "-", "0", "", "22"),
		"VARCHAR2(100 CHAR)": v("VARCHAR2", "-", "-", "100", "C", "400"),
		"VARCHAR2(100)":      v("VARCHAR2", "-", "-", "100", "B", "100"),
		"RAW(16)":            v("RAW", "-", "-", "0", "", "16"),
		"TIMESTAMP(3)":       v("TIMESTAMP(3)", "-", "3", "0", "", "11"),
	} {
		if got := typeOf(f); got != want {
			t.Errorf("%v: %s, want %s", f, got, want)
		}
	}
}

// Each argument of a save as its column's type, a CLOB compared by DBMS_LOB.
func TestTypedSame(t *testing.T) {
	e := Engine{}
	for typ, want := range map[string]string{
		"NUMBER(10,2)": "TO_NUMBER(:1)", "BINARY_DOUBLE": "TO_BINARY_DOUBLE(:1)", "DATE": "TO_DATE(:1, 'YYYY-MM-DD HH24:MI:SS')",
		"TIMESTAMP(3)": "TO_TIMESTAMP(:1, 'YYYY-MM-DD HH24:MI:SS.FF')", "TIMESTAMP(3) WITH TIME ZONE": "TO_TIMESTAMP_TZ(:1, 'YYYY-MM-DD HH24:MI:SS.FFTZH:TZM')",
		"RAW(4)": "HEXTORAW(:1)", "CLOB": "TO_CLOB(:1)", "VARCHAR2(10)": ":1",
	} {
		if got := e.Typed(":1", typ); got != want {
			t.Errorf("%s: %s", typ, got)
		}
	}
	if got := e.Same(`"NOTE"`, "TO_CLOB(:2)"); got != `dbms_lob.compare("NOTE", TO_CLOB(:2)) = 0` {
		t.Errorf("clob %s", got)
	}
	if got := e.Same(`"N"`, "TO_NUMBER(:2)"); got != `"N" = TO_NUMBER(:2)` {
		t.Errorf("number %s", got)
	}
	if got := e.Quote("SQLMUX", `we"ird`); got != `"SQLMUX"."we""ird"` {
		t.Errorf("quote %s", got)
	}
}

// A date to the second, a timestamp's fraction without trailing zeros, a zone
// as the time picker reads it.
func TestTimeText(t *testing.T) {
	at := time.Date(2026, 2, 1, 8, 0, 0, 500000000, time.FixedZone("", 2*3600))
	for typ, want := range map[string]string{
		"date": "2026-02-01 08:00:00", "timestampdty": "2026-02-01 08:00:00.5", "timestamptz_dty": "2026-02-01 08:00:00.5+02:00", "timestampltz_dty": "2026-02-01 08:00:00.5",
	} {
		if got := timeText(at, typ); got != want {
			t.Errorf("%s: %s, want %s", typ, got, want)
		}
	}
	for typ, want := range map[string]bool{"CLOB": true, "NUMBER(5)": true, "BLOB": false, "LONG": false, "MDSYS.SDO_GEOMETRY": false} {
		if editable(typ) != want {
			t.Errorf("editable(%s)", typ)
		}
	}
}
