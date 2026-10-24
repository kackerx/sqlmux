package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// clipped reports whether cmd, or one it batches, puts want on the
// clipboard.
func clipped(cmd tea.Cmd, want string) bool {
	if cmd == nil {
		return false
	}
	msg := cmd() // once: a Tick's fires once
	if m, ok := msg.(tea.BatchMsg); ok {
		return slices.ContainsFunc(m, func(c tea.Cmd) bool { return clipped(c, want) })
	}
	return msg == tea.SetClipboard(want)()
}

// clipText is the text cmd, or one it batches, puts on the clipboard.
func clipText(cmd tea.Cmd) (string, bool) {
	if cmd == nil {
		return "", false
	}
	msg := cmd()
	if m, ok := msg.(tea.BatchMsg); ok {
		for _, c := range m {
			if s, ok := clipText(c); ok {
				return s, true
			}
		}
		return "", false
	}
	if msg != nil && fmt.Sprintf("%T", msg) == "tea.setClipboardMsg" {
		return reflect.ValueOf(msg).String(), true
	}
	return "", false
}

// keys presses keys and is the Cmd the last one returned.
func keys(t *testing.T, a *App, s string) (cmd tea.Cmd) {
	t.Helper()
	ks, err := keymap.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range ks {
		_, cmd = a.Update(teaKey(k))
	}
	return cmd
}

// yy copies the row as TSV of the columns shown, changes in, NULL empty,
// a value with a newline or a tab quoted, the row flashing; yl the cell,
// flashing it. p pastes the row yy took as a row added under the cursor's,
// every column's value but a key's with a default and a generated always
// identity's; for another table it pastes nothing (F3.32).
func TestYankRow(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	tab.cols.Cols[0].Default = "nextval('t_order_id_seq'::regclass)"
	tab.hidden["meta"] = true
	feed(t, a, "jlli9<Esc>") // row 2's amount
	want := "2\tdone\t9\tt\t\"line one\nline two\tafter tab \x1b[31mred\"\t2026-09-01 00:02:00+00"
	if cmd := keys(t, a, "yy"); !clipped(cmd, want) || a.grid(a.focused(), tab).Yank == nil || *a.grid(a.focused(), tab).Yank != [2]int{1, -1} {
		t.Fatalf("yy: flash %v", a.flash)
	}
	if st := styleOf(t, a.render(), "done"); st.Bg != a.theme.Yank || st.Fg != a.theme.Bg {
		t.Errorf("the row flashing: %+v", st)
	}
	if cmd := keys(t, a, "yl"); !clipped(cmd, "9") || *a.grid(a.focused(), tab).Yank != [2]int{1, 2} {
		t.Fatalf("yl: flash %v", a.flash)
	}
	a.Update(flashDone{a.flashSeq})
	if a.grid(a.focused(), tab).Yank != nil {
		t.Error("the flash goes")
	}
	tab.cols.Cols[3].Default = "generated always as identity" // paid, as a column taking no value
	feed(t, a, "p")
	if len(tab.added) != 1 || tab.row != 2 {
		t.Fatalf("p: added %d, cursor %d", len(tab.added), tab.row)
	}
	cells := tab.added[0].cells
	if _, ok := cells["id"]; ok || cells["amount"].val.S != "9" || cells["paid"] != (edit{}) || cells["meta"].val.S == "" || cells["note"].val != tab.page.Rows[1][5] {
		t.Errorf("pasted: %+v", cells)
	}
	a.rowCopy.table = tableID{"public", "t_user"}
	if feed(t, a, "p"); len(tab.added) != 1 {
		t.Error("another table's row pasted")
	}
	a.rowCopy.table, a.rowCopy.conn = idOf(tab.table), readyConn("seed") // seed's t_order: another table (F5.10)
	if feed(t, a, "p"); len(tab.added) != 1 {
		t.Error("another connection's t_order row pasted")
	}
}

// A query's yanks flash what they took, as vim.hl.on_yank: a line, a
// word; a delete does not (F3.32).
func TestYankFlashQuery(t *testing.T) {
	a, c := inQuery(t, "", "")
	c.ed.Load("select 1\nfrom t")
	p := a.focused()
	keys(t, a, "yy")
	if v, _ := a.queryView(p, c); v.Yank != (ui.Sel{Mode: ui.SelLines}) {
		t.Fatalf("yy: %+v", v.Yank)
	}
	keys(t, a, "yiw")
	if v, _ := a.queryView(p, c); v.Yank != (ui.Sel{Mode: ui.SelChars, To: ui.TextPos{Col: 5}}) {
		t.Fatalf("yiw: %+v", v.Yank)
	}
	if st := styleOf(t, a.render(), "select"); st.Bg != a.theme.Yank {
		t.Errorf("the word flashing: %+v", st)
	}
	a.Update(flashDone{a.flashSeq})
	if keys(t, a, "dd"); a.flash != nil {
		t.Error("dd flashes")
	}
}

// The clipboard is the first tool on PATH with its display set, OSC 52
// when there is none or it fails (F3.38).
func TestClipTools(t *testing.T) {
	f := filepath.Join(t.TempDir(), "clip")
	write, paste := []string{"sh", "-c", "cat > " + f}, []string{"cat", f}
	if runtime.GOOS == "windows" { // no sh: powershell is the exact write and paste
		write = []string{"powershell", "-NoProfile", "-Command", "[IO.File]::WriteAllText('" + f + "', [Console]::In.ReadToEnd())"}
		paste = []string{"powershell", "-NoProfile", "-Command", "[Console]::Out.Write([IO.File]::ReadAllText('" + f + "'))"}
	}
	clipTools = []clipTool{
		{"", []string{"sqlmux-no-such-copy"}, []string{"sqlmux-no-such-paste"}},
		{"SQLMUX_NO_SUCH_DISPLAY", []string{"true"}, []string{"true"}},
		{"", write, paste},
	}
	defer func() { clipTools = nil }()
	if msg := clipCopy("a\tb\n")(); msg != nil {
		t.Errorf("copied with a tool, and %v", msg)
	}
	if msg := clipPaste(); msg != (tea.ClipboardMsg{Content: "a\tb\n", Selection: 'c'}) {
		t.Errorf("pasted %#v", msg)
	}
	clipTools[2] = clipTool{"", []string{"false"}, []string{"false"}}
	if clipCopy("x")() != tea.SetClipboard("x")() || clipPaste() != tea.ReadClipboard() {
		t.Error("a failing tool is not OSC 52")
	}
}

// The windows row is in the table on windows only, its argv the spec's
// two PowerShell commands, and its read's \r\n lines are normalized (§7.8,
// F18.3).
func TestClipWindowsRow(t *testing.T) {
	rows := slices.DeleteFunc(slices.Clone(clipTable()), func(t clipTool) bool {
		return len(t.copy) == 0 || t.copy[0] != "powershell"
	})
	if runtime.GOOS != "windows" {
		if len(rows) != 0 {
			t.Errorf("the powershell rows are there on %s", runtime.GOOS)
		}
		return
	}
	if len(rows) != 1 {
		t.Fatalf("the windows rows: %d", len(rows))
	}
	want := clipTool{
		"",
		[]string{"powershell", "-NoProfile", "-Command", "[Console]::InputEncoding=[Text.Encoding]::UTF8;Set-Clipboard -Value ([Console]::In.ReadToEnd())"},
		[]string{"powershell", "-NoProfile", "-Command", "[Console]::OutputEncoding=[Text.Encoding]::UTF8;[Console]::Out.Write((Get-Clipboard -Raw))"},
	}
	if !slices.Equal(rows[0].copy, want.copy) || !slices.Equal(rows[0].paste, want.paste) {
		t.Errorf("the windows row:\n got %#v\nwant %#v", rows[0], want)
	}
	clipTools = []clipTool{{"", []string{"cmd", "/c", "rem"}, []string{"cmd", "/c", "echo a\tb"}}}
	defer func() { clipTools = nil }()
	if msg := clipPaste(); msg != (tea.ClipboardMsg{Content: "a\tb\n", Selection: 'c'}) { // echo ends the line with \r\n
		t.Errorf("pasted %#v", msg)
	}
}

// yy and yl on a result's grid copy its row as TSV and its cell; p there
// pastes nothing (F3.32).
func TestYankResult(t *testing.T) {
	r := db.Result{Cols: []db.Col{{Name: "a"}, {Name: "b"}}, Rows: [][]db.Val{{{S: "x y"}, {Null: true}}, {{S: "q\"r"}, {S: "2"}}}}
	a, _ := inRun(t, &execDB{res: map[string]db.Result{"select 1": r}}, "select 1;")
	press(t, a, "<CR>")
	res := a.win().Result
	a.win().focus(res.ID)
	if cmd := keys(t, a, "jyy"); !clipped(cmd, "\"q\"\"r\"\t2") || a.flash == nil || a.flash.cell != [2]int{1, -1} {
		t.Errorf("yy: flash %+v", a.flash)
	}
	if cmd := keys(t, a, "kyl"); !clipped(cmd, "x y") {
		t.Error("yl")
	}
	if feed(t, a, "p"); a.focused() != res || len(res.Tabs) != 2 {
		t.Errorf("p: %d tabs", len(res.Tabs))
	}
}

// yi copies the row as one INSERT of every column but a generated one, as
// it shows, changes in: a number bare, NULL as it is, the rest a literal;
// the keywords in keyword_case's case; the row flashes. A result's row has
// no table: a toast, nothing copied (F8.7).
func TestYankInsert(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	tab.cols.Cols[6].Generated = true // created_at
	feed(t, a, "jlli9<Esc>")          // row 2's amount
	want := "insert into \"public\".\"t_order\" (\"id\", \"status\", \"amount\", \"paid\", \"meta\", \"note\") values (2, 'done', 9, 't', '{\"n\": 2, \"tags\": [\"a\", \"b\"]}', 'line one\nline two\tafter tab \x1b[31mred');"
	if cmd := keys(t, a, "yi"); !clipped(cmd, want) || a.flash == nil || a.flash.cell != [2]int{1, -1} {
		t.Fatalf("yi: flash %+v", a.flash)
	}
	a.keywordCase = "upper"
	want = "INSERT INTO \"public\".\"t_order\" (\"id\", \"status\", \"amount\", \"paid\", \"meta\", \"note\") VALUES (3, 'failed', 3.99, 't', '{\"n\": 3, \"tags\": [\"a\", \"b\"]}', NULL);"
	if cmd := keys(t, a, "jyi"); !clipped(cmd, want) {
		t.Errorf("row 3, upper case, a NULL")
	}
	a.keywordCase = "preserve"
	tab.page.Rows[2][1] = db.Val{S: "it's"}
	if cmd := keys(t, a, "yi"); !clipped(cmd, "insert into \"public\".\"t_order\" (\"id\", \"status\", \"amount\", \"paid\", \"meta\", \"note\") values (3, 'it''s', 3.99, 't', '{\"n\": 3, \"tags\": [\"a\", \"b\"]}', null);") {
		t.Errorf("preserve is lower, a quote doubled")
	}

	r := db.Result{Cols: []db.Col{{Name: "a"}}, Rows: [][]db.Val{{{S: "x"}}}}
	b, _ := inRun(t, &execDB{res: map[string]db.Result{"select 1": r}}, "select 1;")
	press(t, b, "<CR>")
	b.win().focus(b.win().Result.ID)
	if cmd := keys(t, b, "yi"); cmd == nil || clipped(cmd, "") || b.toast != "只有表里的行能复制为 INSERT" {
		t.Errorf("a result: toast %q", b.toast)
	}
}

// A row added with nothing set is the DEFAULT row, written as the save
// writes it; with one cell set only that one is named; a binary placeholder
// refuses yi and p (§10.6, F8.7).
func TestYankInsertAddedAndBinary(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	feed(t, a, "o<Esc>")
	if sr, _, ok := tab.cursor(); !ok || sr.add == nil {
		t.Fatal("not on the added row")
	}
	if cmd := keys(t, a, "yi"); !clipped(cmd, `insert into "public"."t_order" default values;`) {
		t.Errorf("all DEFAULT")
	}
	a.keywordCase = "upper"
	if cmd := keys(t, a, "yi"); !clipped(cmd, `INSERT INTO "public"."t_order" DEFAULT VALUES;`) {
		t.Errorf("all DEFAULT, upper case")
	}
	a.keywordCase = "lower"
	feed(t, a, "i9<Esc>")
	if cmd := keys(t, a, "yi"); !clipped(cmd, `insert into "public"."t_order" ("id") values (9);`) {
		t.Errorf("one cell set")
	}

	b := wide(160, 45)
	bt := loadOrders(t, b, 3)
	bt.page.Rows[0][5] = db.Val{S: db.BinaryShown(4)}
	if cmd := keys(t, b, "yi"); clipped(cmd, "") || b.toast != "有二进制列，复制不了" {
		t.Errorf("yi: toast %q", b.toast)
	}
	b.toast = ""
	keys(t, b, "yy")
	if b.rowCopy == nil {
		t.Fatal("yy took nothing")
	}
	n := len(bt.added)
	if keys(t, b, "p"); len(bt.added) != n || b.toast != "有二进制列，复制不了" {
		t.Errorf("p: %d rows added, toast %q", len(bt.added), b.toast)
	}
}

// A row as a JSON object: columns in order, a number for numeric columns whose
// text is one (else the text), true and false for booleans, null, json as
// itself, the rest as strings, nothing HTML-escaped (F9.5).
func TestRowJSON(t *testing.T) {
	raw, err := rowRaw([]jsonField{
		{"id", ui.ColNumber, db.Val{S: "7"}},
		{"big", ui.ColNumber, db.Val{S: "12345678901234567890.5"}},
		{"nan", ui.ColNumber, db.Val{S: "NaN"}},
		{"zeros", ui.ColNumber, db.Val{S: "007"}},
		{"neg", ui.ColNumber, db.Val{S: "-1.5e3"}},
		{"paid", ui.ColBool, db.Val{S: "t"}},
		{"one", ui.ColBool, db.Val{S: "1"}},    // MySQL's tinyint(1)
		{"yes", ui.ColBool, db.Val{S: "true"}}, // a cell edited
		{"f", ui.ColBool, db.Val{S: "f"}},
		{"no", ui.ColBool, db.Val{S: "false"}},
		{"off", ui.ColBool, db.Val{S: "0"}},
		{"odd", ui.ColBool, db.Val{S: "maybe"}},
		{"gone", ui.ColString, db.Val{Null: true}},
		{"meta", ui.ColJSON, db.Val{S: `{"a": [1, 2], "b": null}`}},
		{"bad", ui.ColJSON, db.Val{S: `{"a": `}},
		{"scalar", ui.ColJSON, db.Val{S: `"x"`}},
		{"at", ui.ColTime, db.Val{S: "2026-09-01 00:00:00+00"}},
		{"num_as_text", ui.ColString, db.Val{S: "42"}},
		{"html", ui.ColString, db.Val{S: "<a href=\"x\">&</a>"}},
		{"id", ui.ColNumber, db.Val{S: "8"}}, // a result may have a name twice
	})
	got := ""
	if err == nil {
		got, err = indentJSON(raw)
	}
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "id": 7,
  "big": 12345678901234567890.5,
  "nan": "NaN",
  "zeros": "007",
  "neg": -1.5e3,
  "paid": true,
  "one": true,
  "yes": true,
  "f": false,
  "no": false,
  "off": false,
  "odd": "maybe",
  "gone": null,
  "meta": {
    "a": [
      1,
      2
    ],
    "b": null
  },
  "bad": "{\"a\": ",
  "scalar": "x",
  "at": "2026-09-01 00:00:00+00",
  "num_as_text": "42",
  "html": "<a href=\"x\">&</a>",
  "id": 8
}`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Errorf("not JSON: %v", err)
	}
}

// Y copies the row as JSON, every column of the table, the hidden ones too, a
// number a number, the jsonb an object, changes in; a result's row too; a
// DEFAULT cell has no key, and flashes as yy does (F9.5).
func TestYankJSON(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	feed(t, a, "jlli9<Esc>") // row 2's amount, changed
	want := `{
  "id": 2,
  "status": "done",
  "amount": 9,
  "paid": true,
  "meta": {
    "n": 2,
    "tags": [
      "a",
      "b"
    ]
  },
  "note": "line one\nline two\tafter tab \u001b[31mred",
  "created_at": "2026-09-01 00:02:00+00"
}`
	if cmd := keys(t, a, "Y"); !clipped(cmd, want) || a.flash == nil || a.flash.cell != [2]int{1, -1} {
		t.Fatalf("Y: flash %+v", a.flash)
	}
	tab.hidden = map[string]bool{"id": true, "note": true} // the columns shown do not decide
	want3 := `{
  "id": 3,
  "status": "failed",
  "amount": 3.99,
  "paid": true,
  "meta": {
    "n": 3,
    "tags": [
      "a",
      "b"
    ]
  },
  "note": null,
  "created_at": "2026-09-01 00:03:00+00"
}`
	if cmd := keys(t, a, "jY"); !clipped(cmd, want3) {
		t.Errorf("row 3, two columns hidden, a NULL")
	}

	r := db.Result{Cols: []db.Col{{Name: "n", Type: "int4"}, {Name: "ok", Type: "bool"}, {Name: "j", Type: "json"}}, Rows: [][]db.Val{{{S: "5"}, {S: "f"}, {S: `[1]`}}}}
	b, _ := inRun(t, &execDB{res: map[string]db.Result{"select 1": r}}, "select 1;")
	press(t, b, "<CR>")
	b.win().focus(b.win().Result.ID)
	if cmd := keys(t, b, "Y"); !clipped(cmd, "{\n  \"n\": 5,\n  \"ok\": false,\n  \"j\": [\n    1\n  ]\n}") {
		t.Errorf("a result's row")
	}

	// the catalog's types not in yet: the page's own
	d := wide(160, 45)
	dt := loadOrders(t, d, 3)
	dt.cols.Cols = nil
	dt.page.Cols[0].Type, dt.page.Cols[3].Type = "int4", "bool"
	wantPage := `{
  "id": 1,
  "status": "running",
  "amount": "1.99",
  "paid": true,
  "meta": "{\"n\": 1, \"tags\": [\"a\", \"b\"]}",
  "note": "note 1",
  "created_at": "2026-09-01 00:01:00+00"
}`
	if cmd := keys(t, d, "Y"); !clipped(cmd, wantPage) {
		t.Errorf("types from the page")
	}

	// nothing to copy: no row of a result, no row of a table
	e, _ := inRun(t, &execDB{res: map[string]db.Result{"select 1": {Cols: []db.Col{{Name: "n", Type: "int4"}}}}}, "select 1;")
	press(t, e, "<CR>")
	e.win().focus(e.win().Result.ID)
	if keys(t, e, "Y"); e.flash != nil {
		t.Errorf("an empty result")
	}
	f := wide(160, 45)
	loadOrders(t, f, 0)
	if keys(t, f, "Y"); f.flash != nil {
		t.Errorf("an empty table")
	}

	c := wide(160, 45)
	loadOrders(t, c, 3)
	feed(t, c, "o<Esc>")
	if cmd := keys(t, c, "Y"); !clipped(cmd, "{}") {
		t.Errorf("an added row, all DEFAULT")
	}
	feed(t, c, "i9<Esc>")
	if cmd := keys(t, c, "Y"); !clipped(cmd, "{\n  \"id\": 9\n}") {
		t.Errorf("an added row, one cell")
	}
}
