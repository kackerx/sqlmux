package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/db"
)

// streamDB is a Conn that streams n rows of cols made by row, the SQL it was
// asked for kept; with gate set it stops after the first row until the gate
// opens or the request is cancelled, started closed when it is there.
type streamDB struct {
	*execDB
	cols    []db.Col
	n       int
	row     func(i int) []db.Val
	gate    chan struct{}
	started chan struct{}
	once    sync.Once
	mu      sync.Mutex
	sqls    []string
	saved   []string // the savepoint statements it was asked
}

func (d *streamDB) Query(ctx context.Context, sql string, args ...db.Val) (db.Result, error) {
	if strings.Contains(sql, "savepoint") {
		d.mu.Lock()
		d.saved = append(d.saved, sql)
		d.mu.Unlock()
	}
	return d.execDB.Query(ctx, sql, args...)
}

func (d *streamDB) Stream(ctx context.Context, sql string, head func([]db.Col) error, row func([]db.Val) error) error {
	d.mu.Lock()
	d.sqls = append(d.sqls, sql)
	d.mu.Unlock()
	if err := head(d.cols); err != nil {
		return err
	}
	for i := range d.n {
		if err := row(d.row(i)); err != nil {
			return err
		}
		if d.gate != nil && i == 0 {
			d.once.Do(func() { close(d.started) })
			select {
			case <-d.gate:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

// ordersStream is n rows of t_order's columns, as the server sends them.
func ordersStream(n int) *streamDB {
	_, cols, _ := ordersTable(0)
	d := &streamDB{execDB: &execDB{}, n: n}
	for _, c := range cols.Cols {
		d.cols = append(d.cols, db.Col{Name: c.Name})
	}
	d.row = func(i int) []db.Val {
		note := db.Val{Null: true}
		if i%2 == 0 {
			note = db.Val{S: "a, \"b\"\nc"}
		}
		return []db.Val{{S: strconv.Itoa(i + 1)}, {S: "done"}, {S: strconv.Itoa(i) + ".5"}, {S: "t"}, {S: `{"n": ` + strconv.Itoa(i) + `}`}, note, {S: "2026-09-01 00:00:00+00"}}
	}
	return d
}

// inDir runs the rest of the test in a directory of its own, where exports
// are written.
func inDir(t *testing.T) string {
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

// exportAt picks item at of the export dropdown of the focused pane and
// delivers the export's end.
func exportAt(t *testing.T, a *App, at int) {
	t.Helper()
	a.run("grid.export", 0)
	if a.drop == nil || a.drop.kind != dropExport {
		t.Fatal("no export dropdown")
	}
	for range at {
		a.Update(teaKey("<C-n>"))
	}
	_, cmd := a.Update(teaKey("<CR>"))
	deliver(a, cmd)
}

// The page is exported as it is shown: the columns COLS lets through in their
// order, NULL as nothing in CSV and null in JSON, a number a number, a
// boolean a boolean, json as json; the file is named after the table and one
// of that name is not touched (F12.4).
func TestExportPage(t *testing.T) {
	dir := inDir(t)
	a := wide(160, 45)
	tab := loadOrders(t, a, 4)
	tab.hidden["meta"], tab.hidden["created_at"] = true, true
	tab.colOrder = []string{"note", "id"}
	exportAt(t, a, exportCSVPage)
	want := "note,id,status,amount,paid\n" +
		"note 1,1,running,1.99,t\n" +
		"\"line one\nline two\tafter tab \x1b[31mred\",2,done,2.99,t\n" +
		",3,failed,3.99,t\n" +
		"note 4,4,pending,4.99,t\n"
	if got, err := os.ReadFile(filepath.Join(dir, "t_order.csv")); err != nil || string(got) != want || !strings.Contains(a.toast, filepath.Join(dir, "t_order.csv")) || !strings.HasSuffix(a.toast, " · 4 行") {
		t.Fatalf("csv: %q %v, toast %q", got, err, a.toast)
	}
	exportAt(t, a, exportJSONPage)
	got, err := os.ReadFile(filepath.Join(dir, "t_order.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(got, &rows); err != nil || len(rows) != 4 || rows[0]["id"] != float64(1) || rows[0]["paid"] != true || rows[2]["note"] != nil || rows[0]["amount"] != 1.99 {
		t.Fatalf("json: %s %v", got, err)
	}
	if _, has := rows[0]["meta"]; has || !strings.HasPrefix(string(got), "[\n  {\n    \"note\": \"note 1\",\n    \"id\": 1,") {
		t.Errorf("the columns: %s", got)
	}
	exportAt(t, a, exportCSVPage) // there is one: the next is t_order-2
	if _, err := os.Stat(filepath.Join(dir, "t_order-2.csv")); err != nil {
		t.Errorf("not -2: %v", err)
	}
	if first, _ := os.ReadFile(filepath.Join(dir, "t_order.csv")); string(first) != want {
		t.Error("the first file was written over")
	}
	tab.page.Rows = nil // an empty page is an empty array
	exportAt(t, a, exportJSONPage)
	if got, _ := os.ReadFile(filepath.Join(dir, "t_order-2.json")); string(got) != "[]\n" {
		t.Errorf("empty: %q", got)
	}
}

// "All" asks the statement of the page again with no LIMIT and no OFFSET, its
// WHERE and ORDER as they are, and writes the rows as they come; the connection
// is not busy after (F12.4).
func TestExportAll(t *testing.T) {
	dir := inDir(t)
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	d := ordersStream(1203)
	tab.conn.Meta = db.NewWorker(d)
	tab.applied, tab.order, tab.limit, tab.pageNo = "id > 0", orderOf("status-"), 100, 2
	tab.shown = tab.request
	tab.hidden["meta"] = true
	exportAt(t, a, exportCSVAll)
	data, err := os.ReadFile(filepath.Join(dir, "t_order.csv"))
	if err != nil || strings.Count(string(data), "\n") != 1203+1+1203/2+1 { // the header, a row each and the lines of a note that has two
		t.Fatalf("csv: %d lines, %v", strings.Count(string(data), "\n"), err)
	}
	if want := `select * from "public"."t_order" where ( id > 0 ) order by "status" desc, "id"`; len(d.sqls) != 1 || !strings.Contains(strings.ReplaceAll(d.sqls[0], "\n", " "), "order by \"status\" desc, \"id\"") || strings.Contains(d.sqls[0], "limit") || strings.Contains(d.sqls[0], "offset") {
		t.Errorf("asked %q, want like %q", d.sqls, want)
	}
	if !strings.HasPrefix(string(data), "id,status,amount,paid,note,created_at\n1,done,0.5,t,") || !strings.HasSuffix(a.toast, " · 1203 行") || tab.conn.busy != 0 || len(a.exporting) != 0 {
		t.Errorf("head of file %q, toast %q, busy %d", strings.SplitN(string(data), "\n", 2)[0], a.toast, tab.conn.busy)
	}
	exportAt(t, a, exportJSONAll)
	var rows []map[string]any
	if data, _ := os.ReadFile(filepath.Join(dir, "t_order.json")); json.Unmarshal(data, &rows) != nil || len(rows) != 1203 || rows[1202]["id"] != float64(1203) {
		t.Errorf("json: %d rows", len(rows))
	}
}

// C-c in the middle of an export takes the file away and says so; a failure
// of the statement does the same, with its message; the same table cannot
// be exported twice at once (F12.4).
func TestExportCancelAndFail(t *testing.T) {
	dir := inDir(t)
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	d := ordersStream(50)
	d.gate, d.started = make(chan struct{}), make(chan struct{})
	tab.conn.Meta = db.NewWorker(d)
	a.run("grid.export", 0)
	a.Update(teaKey("<C-n>")) // CSV · 全部
	_, cmd := a.Update(teaKey("<CR>"))
	done := make(chan tea.Msg)
	go func() { done <- cmd() }()
	<-d.started
	if tab.conn.busy != 1 {
		t.Errorf("busy %d during an export", tab.conn.busy)
	}
	a.run("grid.export", 0)
	a.Update(teaKey("<CR>"))
	if a.toast != "正在导出" {
		t.Errorf("a second export: toast %q", a.toast)
	}
	a.run("cancel", 0)
	a.Update(<-done)
	if _, err := os.Stat(filepath.Join(dir, "t_order.csv")); !os.IsNotExist(err) || a.toast != "导出已取消" || tab.conn.busy != 0 || len(a.exporting) != 0 {
		t.Errorf("cancelled: file %v, toast %q, busy %d", err, a.toast, tab.conn.busy)
	}

	d = ordersStream(5)
	tab.conn.Meta = db.NewWorker(&failDB{streamDB: d, after: 2})
	exportAt(t, a, exportJSONAll)
	if _, err := os.Stat(filepath.Join(dir, "t_order.json")); !os.IsNotExist(err) || a.toast != "boom" {
		t.Errorf("failed: file %v, toast %q", err, a.toast)
	}
}

// failDB fails its stream after the rows.
type failDB struct {
	*streamDB
	after int
}

func (d *failDB) Stream(ctx context.Context, sql string, head func([]db.Col) error, row func([]db.Val) error) error {
	d.n = d.after
	if err := d.streamDB.Stream(ctx, sql, head, row); err != nil {
		return err
	}
	return errors.New("boom")
}

func (d *failDB) Close() error { return nil }

// A request that runs again on a connection dialed anew gives the head again:
// the file starts over, not with its rows twice (F12.4).
func TestExportOutStartsOver(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "x.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	o := &exportOut{f: f, w: bufio.NewWriter(f), want: []exportCol{{name: "n"}}}
	cols := []db.Col{{Name: "n"}}
	for range 2 {
		if err := o.head(cols); err != nil {
			t.Fatal(err)
		}
		_ = o.row([]db.Val{{S: "1"}})
	}
	if err := o.finish(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(f.Name()); string(data) != "n\n1\n" || o.n != 1 {
		t.Errorf("file %q, n %d", data, o.n)
	}
}

// A result's all is its statement as it was written, no LIMIT, on its
// connection's Main; one that cannot be paged gives its page (F12.4).
func TestExportResult(t *testing.T) {
	dir := inDir(t)
	d := &streamDB{execDB: &execDB{res: map[string]db.Result{"select 1": rows(2)}}, n: 5, cols: []db.Col{{Name: "n", Type: "int4"}}}
	d.row = func(i int) []db.Val { return []db.Val{{S: strconv.Itoa(i)}} }
	a, _ := inRun(t, d.execDB, "select 1")
	a.sess.Conns[0].Main = db.NewWorker(d)
	press(t, a, "<CR>")
	a.win().focus(a.win().Result.ID)
	rt := resultOf(a.focused())
	name := "query_1-" + strconv.Itoa(rt.run.seq)
	if !rt.pageable {
		t.Fatal("select 1 is not paged")
	}
	exportAt(t, a, exportCSVAll)
	if data, err := os.ReadFile(filepath.Join(dir, name+".csv")); err != nil || string(data) != "n\n0\n1\n2\n3\n4\n" || len(d.sqls) != 1 || d.sqls[0] != "select 1" {
		t.Errorf("all: %q %v, asked %q", data, err, d.sqls)
	}
	exportAt(t, a, exportJSONPage)
	if data, err := os.ReadFile(filepath.Join(dir, name+".json")); err != nil || !strings.Contains(string(data), `"n": "a"`) {
		t.Errorf("page: %q %v", data, err)
	}
	rt.pageable = false // its own LIMIT, say: all is the page
	exportAt(t, a, exportCSVAll)
	if data, err := os.ReadFile(filepath.Join(dir, name+"-2.csv")); err != nil || string(data) != "n\na\nb\n" || len(d.sqls) != 1 {
		t.Errorf("all of one not paged: %q %v", data, err)
	}
}

// In a transaction of the user's the rows are read in a savepoint, as a page
// is: a read that fails would fail the transaction and lose what is in it (F12.4).
func TestExportAllInTransaction(t *testing.T) {
	inDir(t)
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	d := ordersStream(5)
	d.tx = 'T'
	tab.conn.Meta, tab.conn.Main = db.NewWorker(d), db.NewWorker(d)
	tab.conn.tx = 'T'
	exportAt(t, a, exportCSVAll)
	if got := strings.Join(d.saved, "|"); got != "savepoint sqlmux_export|release savepoint sqlmux_export" {
		t.Errorf("asked %q, toast %q", got, a.toast)
	}
}

// Two columns of one name, a join's id and id, are two in the file, each with
// its own values; a table named as a path is a file of the current directory.
func TestExportDuplicateColumnsAndPathNames(t *testing.T) {
	var buf bytes.Buffer
	o := &exportOut{w: bufio.NewWriter(&buf), want: []exportCol{{name: "id"}, {name: "id"}, {name: "x"}}}
	if err := o.head([]db.Col{{Name: "id"}, {Name: "id"}, {Name: "x"}}); err != nil {
		t.Fatal(err)
	}
	_ = o.row([]db.Val{{S: "1"}, {S: "2"}, {S: "3"}})
	_ = o.finish()
	if buf.String() != "id,id,x\n1,2,3\n" {
		t.Errorf("%q", buf.String())
	}
	dir := inDir(t)
	a := wide(160, 45)
	tab := loadOrders(t, a, 2)
	tab.table.Name = "../up/t"
	exportAt(t, a, exportCSVPage)
	if _, err := os.Stat(filepath.Join(dir, ".._up_t.csv")); err != nil {
		t.Errorf("not in the directory: %v, toast %q", err, a.toast)
	}
}

// A page with bytes in it is read again, its own LIMIT and OFFSET, the way
// "all" is, so the file is the same: a column of bytes whose value is text
// is hex too, and a value the grid has only a placeholder for is there (F12.4).
func TestExportPageOfBytes(t *testing.T) {
	dir := inDir(t)
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	d := ordersStream(3)
	d.row = func(i int) []db.Val {
		return []db.Val{{S: strconv.Itoa(i + 1)}, {S: "done"}, {S: "1"}, {S: "t"}, {S: "{}"}, {S: "0xff0" + strconv.Itoa(i)}, {S: "2026-09-01 00:00:00+00"}}
	}
	tab.conn.Meta = db.NewWorker(d)
	tab.limit, tab.pageNo = 100, 2
	tab.shown = tab.request
	tab.cols.Cols[tab.fieldAt(5)].Type = "varbinary(16)" // note: the page has abc, which is text
	exportAt(t, a, exportCSVPage)
	page, err := os.ReadFile(filepath.Join(dir, "t_order.csv"))
	if err != nil || len(d.sqls) != 1 || !strings.HasSuffix(d.sqls[0], " limit 100 offset 200") {
		t.Fatalf("page of bytes: %q %v, asked %q", page, err, d.sqls)
	}
	exportAt(t, a, exportCSVAll)
	if all, _ := os.ReadFile(filepath.Join(dir, "t_order-2.csv")); !bytes.Equal(all, page) || !strings.Contains(string(page), "0xff0") {
		t.Errorf("page %q, all %q", page, all)
	}
	// a placeholder for bytes of a column that is no byte column, latin1 text
	tab.cols.Cols[tab.fieldAt(5)].Type = "text"
	tab.page.Rows[0][5] = db.Val{S: db.BinaryShown(3)}
	exportAt(t, a, exportJSONPage)
	if len(d.sqls) != 3 {
		t.Errorf("a placeholder: asked %q", d.sqls)
	}
	// nothing of the kind: the page in memory is written, nothing asked
	tab.page.Rows[0][5] = db.Val{S: "text"}
	exportAt(t, a, exportCSVPage)
	if len(d.sqls) != 3 {
		t.Errorf("a plain page: asked %q", d.sqls)
	}
}

// The rows of a user's transaction are read on Main, where they are, not on
// Meta; the read is in a savepoint there (F12.4).
func TestExportAllInTransactionOnMain(t *testing.T) {
	inDir(t)
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	meta, main := ordersStream(5), ordersStream(5)
	main.tx = 'T'
	tab.conn.Meta, tab.conn.Main, tab.conn.tx = db.NewWorker(meta), db.NewWorker(main), 'T'
	exportAt(t, a, exportCSVAll)
	if len(main.sqls) != 1 || len(meta.sqls) != 0 || len(main.saved) != 2 {
		t.Errorf("main %q %q, meta %q", main.sqls, main.saved, meta.sqls)
	}
}

// What a result's "all" runs, it runs where the result ran: the schema its
// query was on is set first, though another statement has put Main on a
// schema of its own since (F12.4).
func TestExportResultSetsItsSchema(t *testing.T) {
	inDir(t)
	d := &streamDB{execDB: &execDB{res: map[string]db.Result{"select 1": rows(2)}}, n: 2, cols: []db.Col{{Name: "n", Type: "int4"}}}
	d.row = func(i int) []db.Val { return []db.Val{{S: strconv.Itoa(i)}} }
	a, _ := inRun(t, d.execDB, "select 1")
	conn := a.sess.Conns[0]
	conn.Main = db.NewWorker(d)
	press(t, a, "<CR>")
	a.win().focus(a.win().Result.ID)
	rt := resultOf(a.focused())
	rt.run.schema = "zz_s1"
	conn.mainPath = "zz_s2" // a later statement of another query moved Main
	d.ran = nil
	exportAt(t, a, exportCSVAll)
	if len(d.ran) == 0 || !strings.Contains(d.ran[0], "search_path") || !strings.Contains(d.ran[0], "zz_s1") || len(d.sqls) != 1 {
		t.Errorf("the schema first: ran %q, asked %q", d.ran, d.sqls)
	}
}

// The type the catalog gives a column is the one the file follows, not the
// stream's: MySQL's tinyint(1) is a boolean in the table, a number from the
// driver; a column of the page that the stream does not have is said (F12.4).
func TestExportCatalogTypeAndMissingColumn(t *testing.T) {
	dir := inDir(t)
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	tab.cols.Cols[tab.fieldAt(3)].Type = "tinyint(1)"
	d := ordersStream(2)
	d.cols[3].Type = "tinyint"
	d.row = func(int) []db.Val {
		return []db.Val{{S: "1"}, {S: "done"}, {S: "1.5"}, {S: "1"}, {S: "{}"}, {Null: true}, {S: "x"}}
	}
	tab.conn.Meta = db.NewWorker(d)
	exportAt(t, a, exportJSONAll)
	var rows []map[string]any
	data, _ := os.ReadFile(filepath.Join(dir, "t_order.json"))
	if err := json.Unmarshal(data, &rows); err != nil || rows[0]["paid"] != true {
		t.Errorf("paid: %s %v", data, err)
	}
	d.cols = d.cols[:3] // a stream with fewer columns than are asked for
	exportAt(t, a, exportCSVAll)
	if !strings.Contains(a.toast, "没有这一列") {
		t.Errorf("a missing column: toast %q", a.toast)
	}
}

// A page of a result that is not the first has -p and its number in the file's
// name; "all" is no page of it (F10.4, F12.4).
func TestExportAllIsNoPage(t *testing.T) {
	dir := inDir(t)
	d := &streamDB{execDB: &execDB{res: map[string]db.Result{"select 1": rows(2)}}, n: 2, cols: []db.Col{{Name: "n", Type: "int4"}}}
	d.row = func(i int) []db.Val { return []db.Val{{S: strconv.Itoa(i)}} }
	a, _ := inRun(t, d.execDB, "select 1")
	a.sess.Conns[0].Main = db.NewWorker(d)
	press(t, a, "<CR>")
	a.win().focus(a.win().Result.ID)
	rt := resultOf(a.focused())
	rt.offset = a.maxRows
	name := "query_1-" + strconv.Itoa(rt.run.seq)
	exportAt(t, a, exportCSVAll)
	exportAt(t, a, exportCSVPage)
	for _, f := range []string{name + ".csv", name + "-p2.csv"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

// A connection whose Conn has no Stream gives "all" through its Query, the
// rows read at once (F12.4).
func TestExportAllWithoutStream(t *testing.T) {
	dir := inDir(t)
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	_, cols, page := ordersTable(4)
	_ = cols
	d := &execDB{res: map[string]db.Result{}}
	tab.conn.Meta = db.NewWorker(&queryOnly{execDB: d, res: page})
	exportAt(t, a, exportCSVAll)
	data, err := os.ReadFile(filepath.Join(dir, "t_order.csv"))
	if err != nil || strings.Count(string(data), "\n") < 5 || !strings.HasSuffix(a.toast, " · 4 行") {
		t.Errorf("%q %v, toast %q", data, err, a.toast)
	}
}

// queryOnly answers a SELECT with res and has no Stream: a Conn of the kind
// a fake is.
type queryOnly struct {
	*execDB
	res db.Result
}

func (q *queryOnly) Query(context.Context, string, ...db.Val) (db.Result, error) {
	return q.res, nil
}

// A name holding a character a windows file name cannot hold exports with
// each of them an underscore, on every platform (§7.6, F18.2e).
func TestFileSafe(t *testing.T) {
	for name, want := range map[string]string{
		"hash:big":      "hash_big",
		"a/b":           "a_b",
		"a*b":           "a_b",
		`a<b>c"d|e?f\g`: "a_b_c_d_e_f_g",
		"ctrl\x00x":     "ctrl_x",
		"del\x7fx":      "del_x",
		"c1\u0085x":     "c1_x",
	} {
		if got := fileSafe(name); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
}
