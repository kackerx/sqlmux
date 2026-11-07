package app

import (
	"cmp"
	"context"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/redis"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// connForm is the open connection manager (§7.8「连接管理」): the list of
// connections on its left, and the form of the selected one on its right:
// its fields in the order below, the one with the keys, why ↵ did not save.
type connForm struct {
	fields []ui.FormField
	sel    int
	err    string
	cur    int                // the list's selected row: a connection, or len(specs) for the one that makes a new one
	top    int                // the list's first row showing
	list   bool               // the list has the keys, not the form
	direct bool               // opened on a new connection by conn.new, not by going down the list to it: esc closes it
	was    *config.Connection // the connection the form edits, as connections.toml said it when it was selected; nil for a new one
	// the group field's list of the groups there are (F10.6): the one picked
	// with ↓ and ↑, -1 for none; esc closes it till something is typed
	groupSel int
	groupTop int // the first group shown, held while the selection is in view (F13.10)
	groupOff bool
	// testing is the number of the connection test out, 0 for none; result is
	// what a test found, a connection made (resultOK) or why not, shown while
	// err, why a save or a field did not do, is not (F12.5)
	testing  int
	result   string
	resultOK bool
}

// The form's fields, in order; the last three are in the 高级 fold.
const (
	fieldName = iota
	fieldGroup
	fieldEngine
	fieldHost
	fieldPort
	fieldUser
	fieldPassword
	fieldDatabase
	fieldReadOnly
	fieldProduction
	fieldAdvanced
	fieldDSN
	fieldPasswordCmd
	fieldSSH
)

// connFields are the form's rows by the names they carry, which are the
// toml keys they write (§14).
var connFields = []string{fieldName: "name", fieldGroup: "group", fieldEngine: "engine", fieldHost: "host", fieldPort: "port", fieldUser: "user",
	fieldPassword: "password", fieldDatabase: "database", fieldReadOnly: "read_only", fieldProduction: "production", fieldAdvanced: "", fieldDSN: "dsn", fieldPasswordCmd: "password_cmd", fieldSSH: "ssh"}

// defaultPort is the port of an engine's server.
var defaultPort = map[string]string{"postgres": "5432", "mysql": "3306", "redis": "6379", "mssql": "1433", "clickhouse": "8123", "trino": "8080", "oracle": "1521"} // none for SQLite

// dsnExample is what an empty dsn field shows of each engine, the URL form
// of both (§8): the driver's own form of MySQL is read as well, not shown.
var dsnExample = map[string]string{"postgres": "postgres://user:pass@host:5432/db?sslmode=prefer", "mysql": "mysql://user:pass@host:3306/db", "sqlite": "~/data.db?mode=ro", "redis": "redis://user:pass@host:6379/0", "mssql": "sqlserver://user:pass@host:1433?database=db&TrustServerCertificate=true",
	"clickhouse": "clickhouse://user:pass@host:8123/db?secure=false", "trino": "trino://user@host:8080/catalog/schema?ssl=false",
	"oracle": "oracle://user:pass@host:1521/SERVICE_NAME"}

// managed are the connections the manager lists: those of connections.toml,
// not one of the command line, which is nowhere to find, edit or delete (F14.1).
func (a *App) managed() []*Conn {
	// ponytail: an exec connection is written in connections.toml; the form has no
	// field for its command, so the manager does not list it (F17.7)
	return slices.DeleteFunc(slices.Clone(a.sess.Conns), func(c *Conn) bool { return c.Spec.Temp || c.Spec.Engine == "exec" })
}

// specs are the connections the manager lists, as connections.toml says them.
func (a *App) specs() []config.Connection {
	var out []config.Connection
	for _, c := range a.managed() {
		out = append(out, c.Spec)
	}
	return out
}

// blankFields is a form's rows, empty but for the port.
func blankFields() []ui.FormField {
	fs := make([]ui.FormField, len(connFields))
	for i, name := range connFields {
		fs[i].Label = name
	}
	fs[fieldAdvanced].Label = i18n.T("高级")
	fs[fieldEngine].Kind, fs[fieldEngine].Choices = ui.FormSegments, []string{"postgres", "mysql", "sqlite", "redis", "mssql", "clickhouse", "trino", "oracle"}
	fs[fieldReadOnly].Kind = ui.FormCheck
	fs[fieldProduction].Kind = ui.FormCheck
	fs[fieldAdvanced].Kind = ui.FormFold
	fs[fieldPassword].Mask = true
	fs[fieldPort].Input = ui.Input{Text: defaultPort["postgres"], Pos: len(defaultPort["postgres"])}
	return fs
}

// openConnForm is conn.new: the manager on its last row, a new connection,
// the form having the keys on name.
func (a *App) openConnForm() {
	a.form = &connForm{fields: blankFields(), cur: len(a.managed()), direct: true, groupSel: -1}
	a.keepConnInSight()
}

// openConnManager is conn.manage: the manager on the connection in use, the
// list having the keys (F8.16).
func (a *App) openConnManager() { a.manage(a.conn()) }

// manage opens the manager on connection c.
func (a *App) manage(c *Conn) {
	a.form = &connForm{list: true, groupSel: -1}
	a.selectConn(max(slices.Index(a.managed(), c), 0))
}

// selectConn selects row i of the list and fills the form from it: for a
// connection that is there its values as connections.toml says them now, the
// dsn written in the fold, which is open, so the host, port, user and
// database, split from it for the eye, are dim and not to edit (F8.16).
// ponytail: edited in the dsn only; a form that rewrites parts of a dsn is
// what a dsn with its own parameters (?sslmode=…) does not survive.
func (a *App) selectConn(i int) {
	f, specs := a.form, a.specs()
	f.cur, f.err, f.sel, f.direct, f.groupSel, f.groupOff = i, "", 0, false, -1, false
	f.untest()
	defer a.keepConnInSight()
	if i >= len(specs) {
		f.fields, f.was = blankFields(), nil
		return
	}
	c, err := config.ReadConnection(specs[i].Name) // the user may have edited the file since
	if err != nil {
		c, f.err = specs[i], err.Error()
	}
	f.was, f.fields = &c, blankFields()
	text := func(field int, s string) { f.fields[field].Input = ui.Input{Text: s, Pos: len(s)} }
	text(fieldName, c.Name)
	text(fieldGroup, c.Group)
	f.fields[fieldEngine].On = slices.Index(f.fields[fieldEngine].Choices, c.Engine)
	host, port, user, database := splitDSN(c.Engine, c.DSN)
	text(fieldHost, host)
	text(fieldPort, port)
	text(fieldUser, user)
	text(fieldDatabase, database)
	if c.ReadOnly {
		f.fields[fieldReadOnly].On = 1
	}
	if c.Production {
		f.fields[fieldProduction].On = 1
	}
	f.fields[fieldAdvanced].On = 1
	text(fieldDSN, c.DSN)
	text(fieldPasswordCmd, c.PasswordCmd)
	text(fieldSSH, c.SSH)
}

// mysqlDSN is the go-sql-driver's `user:password@tcp(host:port)/database?…`.
var mysqlDSN = regexp.MustCompile(`^(?:([^:@/]*)(?::[^@]*)?@)?tcp\(([^)]*)\)/([^?]*)`)

// splitDSN is the host, port, user and database a dsn says, as far as it
// says them in a form it knows: a URL, which is what the form writes for
// both engines, or the go-sql-driver's; "" for what it cannot read.
func splitDSN(engine, dsn string) (host, port, user, database string) {
	if engine == "sqlite" { // the file is all there is
		return "", "", "", dsn
	}
	if engine == "mysql" && !strings.Contains(dsn, "://") {
		if m := mysqlDSN.FindStringSubmatch(dsn); m != nil {
			host, port, err := net.SplitHostPort(m[2])
			if err != nil { // no port written
				host, port = m[2], ""
			}
			return host, port, m[1], m[3]
		}
		return
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return
	}
	return u.Hostname(), u.Port(), u.User.Username(), strings.TrimPrefix(u.Path, "/")
}

// advanced is whether the fold is open; dsnGiven whether a dsn is typed in
// it: then host, port, user and database take no part.
func (f *connForm) advanced() bool { return f.fields[fieldAdvanced].On == 1 }
func (f *connForm) dsnGiven() bool {
	return f.advanced() && strings.TrimSpace(f.fields[fieldDSN].Input.Text) != ""
}

// skipped is whether Tab goes past field i.
func (f *connForm) skipped(i int) bool {
	switch i {
	case fieldDSN, fieldPasswordCmd:
		return !f.advanced()
	case fieldSSH: // a file has no network to go through (F15.2)
		return !f.advanced() || f.sqlite()
	case fieldHost, fieldPort, fieldUser:
		return f.dsnGiven() || f.sqlite()
	case fieldPassword:
		return f.sqlite()
	case fieldDatabase:
		return f.dsnGiven()
	}
	return false
}

// password is the password typed, "" with SQLite on, whose password field is
// hidden: what it held when the engine was another's is no password of a file.
func (f *connForm) password() string {
	if f.skipped(fieldPassword) {
		return ""
	}
	return f.fields[fieldPassword].Input.Text
}

// sqlite is whether the engine on is SQLite, whose form is a file, its
// database field, and no more (F14.1).
func (f *connForm) sqlite() bool {
	e := f.fields[fieldEngine]
	return e.Choices[e.On] == "sqlite"
}

// redis is whether the engine on is Redis, which has no user that must be
// named, and a db that is a number (F16.1).
func (f *connForm) redis() bool {
	e := f.fields[fieldEngine]
	return e.Choices[e.On] == "redis"
}

// clearErr forgets why ↵ did not save, as the form is touched.
func (f *connForm) clearErr() {
	f.err = ""
	for i := range f.fields {
		f.fields[i].Bad = false
	}
}

// formMove is Tab (1) and S-Tab (-1): the next or previous field, around
// the ends.
func (a *App) formMove(d int) {
	f := a.form
	f.clearErr()
	f.groupSel, f.groupOff = -1, false
	for range f.fields {
		f.sel = (f.sel + d + len(f.fields)) % len(f.fields)
		if !f.skipped(f.sel) {
			return
		}
	}
}

// formKey is a key no binding takes: text for an input, space (and the
// arrows for the engine) for a switch.
func (a *App) formKey(k keymap.Key) {
	f := a.form
	f.clearErr()
	f.untest()
	fd := &f.fields[f.sel]
	switch fd.Kind {
	case ui.FormInput:
		if editInput(&fd.Input, k) && f.sel == fieldGroup {
			f.groupSel, f.groupOff = -1, false
		}
	case ui.FormSegments:
		switch k {
		case "<Space>", "<Right>":
			a.formSegment(f.sel, (fd.On+1)%len(fd.Choices))
		case "<Left>":
			a.formSegment(f.sel, (fd.On+len(fd.Choices)-1)%len(fd.Choices))
		}
	case ui.FormCheck, ui.FormFold:
		if k == "<Space>" {
			fd.On = 1 - fd.On
			if f.sel == fieldProduction && fd.On == 1 { // a production connection is read only to begin with (§13)
				f.fields[fieldReadOnly].On = 1
			}
		}
	}
}

// formSegment makes segment seg of field i, the engine, the one on; the port
// goes with it while it is empty or the engine's default.
func (a *App) formSegment(i, seg int) {
	f := a.form
	f.clearErr()
	f.untest()
	f.sel = i
	fd := &f.fields[i]
	if i == fieldEngine && seg != fd.On {
		port := &f.fields[fieldPort].Input
		if port.Text == "" || port.Text == defaultPort[fd.Choices[fd.On]] {
			p := defaultPort[fd.Choices[seg]]
			*port = ui.Input{Text: p, Pos: len(p)}
		}
	}
	fd.On = seg
}

// untest drops a test's result, one still out too: the fields are not the
// ones it tried.
func (f *connForm) untest() { f.testing, f.result, f.resultOK = 0, "", false }

// formClick is a click on field i: the keys go to it; a check and the fold
// also turn over (a segment is picked by formSegment).
func (a *App) formClick(i int) {
	f := a.form
	if f.skipped(i) {
		return
	}
	f.list = false
	f.clearErr()
	f.sel, f.groupSel, f.groupOff = i, -1, false
	if k := f.fields[i].Kind; k == ui.FormCheck || k == ui.FormFold {
		a.formKey("<Space>")
	}
}

// fail says why ↵ did not save, field i at fault and having the keys.
func (a *App) fail(i int, why string) tea.Cmd {
	a.form.err, a.form.sel = why, i
	a.form.fields[i].Bad = true
	return nil
}

// composeDSN is the dsn of host, port, user and database as a URL, the
// user's and the database's characters escaped; no password in it (§13).
func composeDSN(engine, host, port, user, database string) string {
	u := url.URL{Scheme: engine, Host: net.JoinHostPort(host, port)}
	if engine == "mssql" { // the driver's URL start; any other it reads as keywords (F18.13)
		u.Scheme = "sqlserver"
	}
	if user != "" { // a Redis server may have no user to name
		u.User = url.User(user)
	}
	switch engine {
	case "mssql": // the database is a query's: the driver's URL form has no path (F18.6)
		if database != "" {
			u.RawQuery = "database=" + url.QueryEscape(database)
		}
	case "postgres":
		u.Path = "/" + database
		u.RawQuery = "sslmode=prefer"
	case "trino": // the database field is the catalog, the schema after it a /: catalog/schema
		u.Path = "/" + strings.TrimPrefix(database, "/")
	default:
		u.Path = "/" + database
	}
	return u.String()
}

// formCancel is esc in the form: it goes back to the list, unless the form
// was opened on its own, by conn.new, the sidebar's ＋ or a start with no
// connection, which closes the manager (F8.16).
func (a *App) formCancel() {
	if f := a.form; !f.list && !f.direct {
		f.list = true
		return
	}
	a.form = nil
}

// connsMove moves the list's selection by d, the form following; it stops at
// the ends.
func (a *App) connsMove(d int) {
	if i := a.form.cur + d; i >= 0 && i <= len(a.managed()) {
		a.selectConn(i)
	}
}

// keepConnInSight scrolls the list to its selection.
func (a *App) keepConnInSight() {
	f := a.form
	f.top = a.managerView().ScrollTop(a.w)
}

// connTab is a tab of a session on a connection: pane p of window win, its
// i-th.
type connTab struct {
	win *Window
	p   *Pane
	i   int
}

// tabsOn are the tabs of every session that are on the connection called
// name: a table of it, a query that runs on it or goes to it once connected,
// the result of a run on it (F18.12).
func (a *App) tabsOn(name string) (out []connTab) {
	on := func(c *Conn) bool { return c != nil && c.Spec.Name == name }
	for _, s := range a.sessions {
		for _, w := range s.Windows {
			if w.Root == nil {
				continue
			}
			for _, p := range w.Root.Leaves() {
				for i, tb := range p.Tabs {
					switch {
					case tb.Data != nil && on(tb.Data.conn), tb.Query != nil && (on(tb.Query.conn) || on(tb.Query.want)),
						tb.Result != nil && tb.Result.run != nil && on(tb.Result.run.conn):
						out = append(out, connTab{w, p, i})
					}
				}
			}
		}
	}
	return out
}

// deleteConn is x on the list, or the form's delete: asked first, saying how
// many tabs close with it; the queries on it are written, the block goes from
// connections.toml, its password from the keychain when it is the one this
// program put there, the connection from every session, with the tabs closed
// of it as closeTab closes them, but not kept for tab.reopen, and the password
// kept for it (F8.16, F18.12).
func (a *App) deleteConn() tea.Cmd {
	if f := a.form; f.was != nil {
		return a.deleteConnection(*f.was)
	}
	return nil
}

// deleteConnection is deleteConn on c, as connections.toml has it: the
// manager's, or the tree's, where tree.delete and the menu are on a
// connection (F18.14).
func (a *App) deleteConnection(c config.Connection) tea.Cmd {
	text := i18n.T("删除连接 %s？", c.Name)
	if tabs := a.tabsOn(c.Name); len(tabs) > 0 {
		text = i18n.T("删除连接 %s？它开着的 %d 个 tab 会一起关掉", c.Name, len(tabs))
		if n := changed(tabs); n > 0 {
			text += i18n.T("，其中 %d 个有没保存的修改，会丢", n)
		}
	}
	if line := rollbackLine(a.connsOf(c.Name, (*Conn).inTx), verbClose); line != "" {
		text += "\n" + line
	}
	a.confirm = &confirmBox{text: text, yes: i18n.T("删除"), then: func() tea.Cmd {
		tabs := a.tabsOn(c.Name)
		for _, t := range tabs {
			if q := t.p.Tabs[t.i].Query; q != nil { // before anything goes: a query's text is not lost (§11)
				if err := q.flush(); err != nil {
					return a.saveFailed(err)
				}
			}
		}
		if err := config.DeleteConnection(c.Name); err != nil {
			return a.showToast(err.Error())
		}
		var toast tea.Cmd
		if err := config.DeletePassword(c.PasswordCmd); err != nil {
			toast = a.showToast(err.Error())
		}
		secrets.Delete(c.Name)                // a connection of the name made next asks again
		for k := len(tabs) - 1; k >= 0; k-- { // a pane's last tab first: the ones before keep their places
			t := tabs[k]
			dropTab(t.p, t.i)
			if len(t.p.Tabs) == 0 {
				t.win.removePane(t.p.ID)
			}
		}
		var ws []*db.Worker // closed in a Cmd: Close waits on what runs
		for _, o := range a.connsOf(c.Name, func(o *Conn) bool { return o.State == connReady }) {
			ws = append(ws, o.Main, o.Meta)
		}
		for _, s := range a.sessions {
			s.Conns = slices.DeleteFunc(s.Conns, func(o *Conn) bool { return o.Spec.Name == c.Name })
			s.closed = slices.DeleteFunc(s.closed, func(t closedTab) bool { return t.conn != nil && t.conn.Spec.Name == c.Name })
			if s.start != nil && s.start.Spec.Name == c.Name {
				s.start = nil
				if len(s.Conns) > 0 {
					s.start = s.Conns[0]
				}
			}
			if s.last != nil && s.last.Spec.Name == c.Name {
				s.last = s.start
			}
		}
		a.regroup() // the first of a group gone: the next one is where the group begins
		a.clampTree()
		if a.form != nil {
			a.selectConn(min(a.form.cur, len(a.managed())))
			a.form.list = true
		}
		return tea.Batch(toast, closeWorkers(ws))
	}}
	return nil
}

// connsOf are the connections called name in every session, and the databases
// of them made so far, that are what is: a transaction going, connected.
func (a *App) connsOf(name string, is func(*Conn) bool) (out []*Conn) {
	for _, s := range a.sessions {
		for _, o := range s.conns() {
			if o.root().Spec.Name == name && is(o) {
				out = append(out, o)
			}
		}
	}
	return out
}

// changed is how many of tabs are tables with changes not saved.
func changed(tabs []connTab) (n int) {
	for _, t := range tabs {
		if d := t.p.Tabs[t.i].Data; d != nil && d.changes() > 0 {
			n++
		}
	}
	return n
}

// formConn is the connection the form says, checked as saving checks the
// fields: the new one's dsn composed from the parts, or the one that is
// typed; one that does not check says why, its field getting the keys (§7.8),
// and is not returned.
func (a *App) formConn() (config.Connection, bool) {
	f := a.form
	fs := f.fields
	text := func(i int) string { return strings.TrimSpace(fs[i].Input.Text) }
	var c config.Connection
	var names []string // the other connections'
	if f.was != nil {
		c = *f.was
		c.Name, c.Group, c.Engine, c.DSN = text(fieldName), text(fieldGroup), fs[fieldEngine].Choices[fs[fieldEngine].On], text(fieldDSN)
		c.PasswordCmd, c.ReadOnly, c.SSH = text(fieldPasswordCmd), fs[fieldReadOnly].On == 1, text(fieldSSH)
		c.Production = fs[fieldProduction].On == 1
		for _, s := range a.specs() {
			if s.Name != f.was.Name {
				names = append(names, s.Name)
			}
		}
	} else {
		c = config.Connection{Name: text(fieldName), Group: text(fieldGroup), Engine: fs[fieldEngine].Choices[fs[fieldEngine].On], DSN: "-", ReadOnly: fs[fieldReadOnly].On == 1, Production: fs[fieldProduction].On == 1}
		if f.advanced() {
			c.PasswordCmd, c.SSH = text(fieldPasswordCmd), text(fieldSSH)
		}
		for _, conn := range a.sess.Conns {
			names = append(names, conn.Spec.Name)
		}
	}
	if field, problem := c.Check(names); problem != "" { // a new one's dsn is "-" here: composed or typed below
		a.fail(slices.Index(connFields, field), problem)
		return c, false
	}
	if f.was != nil {
		return c, true
	}
	if f.dsnGiven() {
		c.DSN = text(fieldDSN)
		return c, true
	}
	if f.sqlite() { // the file is the dsn
		if text(fieldDatabase) == "" {
			a.fail(fieldDatabase, i18n.T("缺少 %s", "file"))
			return c, false
		}
		c.DSN = text(fieldDatabase)
		return c, true
	}
	required := []int{fieldHost, fieldUser, fieldDatabase}
	if f.redis() { // the default user, and db 0
		required = []int{fieldHost}
	}
	for _, i := range required {
		if text(i) == "" {
			a.fail(i, i18n.T("缺少 %s", connFields[i]))
			return c, false
		}
	}
	database := text(fieldDatabase)
	if f.redis() {
		if database == "" {
			database = "0"
		}
		if n, err := strconv.Atoi(database); err != nil || n < 0 {
			a.fail(fieldDatabase, i18n.T("db 要是从 0 起的数字"))
			return c, false
		}
	}
	host := strings.Trim(text(fieldHost), "[]") // an IPv6 address may be written with them
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		a.fail(fieldHost, i18n.T("host 里不要带端口，端口在下面"))
		return c, false
	}
	p, err := strconv.Atoi(text(fieldPort))
	if err != nil || p < 1 || p > 65535 {
		a.fail(fieldPort, i18n.T("port 要是 1–65535 的数字"))
		return c, false
	}
	c.DSN = composeDSN(c.Engine, host, strconv.Itoa(p), text(fieldUser), database) // +5432 is 5432
	return c, true
}

// testTimeout is how long a connection test waits, as a dial does (§8).
const testTimeout = 10 * time.Second

// connTested is what testConn found, of the test numbered seq.
type connTested struct {
	seq  int
	note string
	err  error
}

// testConnForm is C-t: the form checked as saving checks it, then what it
// says connected to once, and closed (F12.5). Nothing is saved or put in
// the tree. A password typed is the one tried, else the connection's own
// source, as a dial reads it.
func (a *App) testConnForm() tea.Cmd {
	f := a.form
	f.clearErr()
	f.untest()
	c, ok := a.formConn()
	if !ok {
		return nil
	}
	a.formTests++
	seq, pw := a.formTests, f.password()
	f.testing = seq
	return func() tea.Msg {
		note, err := testConn(c, pw)
		return connTested{seq, note, err}
	}
}

// testConn connects to spec once, read only, and says what it reached:
// `已连接 · PostgreSQL 17.7 · 120ms`. password "" is the spec's own.
func testConn(spec config.Connection, password string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	start := time.Now()
	spec, err := spec.Expanded()
	if err != nil {
		return "", err
	}
	if password == "" {
		if password, err = spec.Secret(); err != nil {
			return "", err
		}
	}
	conn, _, err := driverOf(spec).Connect(tunneled(ctx, spec), spec.DSN, password, true)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	version, args := "select version()", []db.Val(nil)
	switch spec.Engine {
	case "sqlite":
		version = "select sqlite_version()"
	case "mssql": // no version(): the server's number is here (F18.6)
		version = "select convert(nvarchar(max), serverproperty('ProductVersion'))"
	case "trino": // the coordinator's version
		version = "select node_version from system.runtime.nodes where coordinator"
	case "oracle": // v$version wants a grant; this one is everyone's
		version = "select version_full from product_component_version where product like 'Oracle%'"
	case "redis": // PING, then what the server says it is
		if _, err := conn.Query(ctx, "PING"); err != nil {
			return "", err
		}
		version, args = "INFO", []db.Val{{S: "server"}}
	}
	r, err := conn.Query(ctx, version, args...)
	if err != nil {
		return "", err
	}
	return i18n.T("已连接 · %s · %s", versionText(spec.Engine, r.Rows[0][0].S), time.Since(start).Round(time.Millisecond)), nil
}

// versionText is what a server's version() says, as far as news: PG's
// `PostgreSQL 17.11 (Debian 17.11-1.pgdg13+2) on aarch64…` is PostgreSQL
// 17.11; MySQL's is the number, with its name before it.
func versionText(engine, v string) string {
	switch engine {
	case "mssql":
		return "SQL Server " + v
	case "clickhouse":
		return "ClickHouse " + v
	case "trino":
		return "Trino " + v
	case "oracle":
		return "Oracle " + v
	case "mysql":
		return "MySQL " + v
	case "sqlite":
		return "SQLite " + v
	case "redis": // INFO server's text
		return "Redis " + redis.Version(v)
	}
	v, _, _ = strings.Cut(v, " on ")
	v, _, _ = strings.Cut(v, " (")
	return v
}

// gotConnTested shows what a test found, unless the form it was for is gone
// or its fields changed since (F12.5).
func (a *App) gotConnTested(m connTested) {
	f := a.form
	if f == nil || f.testing != m.seq {
		return
	}
	f.testing, f.resultOK = 0, m.err == nil
	f.result = m.note
	if m.err != nil {
		f.result = toastLine(m.err.Error())
	}
}

// saveEditedConn is ↵ on the form of a connection that is there: what the
// form changed is rewritten in its block of connections.toml, nothing else
// of the file. A password typed, or the one of a connection renamed that
// this program keeps in the keychain, goes into the keychain under the new
// name once the file is known to take the edit, and the old item goes
// after the file is written. The sessions' connections take the block as
// the file reads back, those connected when they connect again (F8.16).
func (a *App) saveEditedConn() tea.Cmd {
	f := a.form
	was := *f.was
	c, ok := a.formConn()
	if !ok {
		return nil
	}
	pw, typed := f.password(), true
	if pw == "" && c.Name != was.Name && c.PasswordCmd == was.PasswordCmd && config.OwnPassword(was.PasswordCmd) {
		// its item is named after it: the password goes to one under the new name
		var err error
		if pw, err = (config.Connection{PasswordCmd: was.PasswordCmd}).Secret(); err != nil {
			return a.fail(fieldPassword, i18n.T("钥匙串里读不到 %s 的密码：在 password 里重填，或清空 password_cmd 再保存", was.Name))
		}
		typed = false
	}
	if typed && pw != "" && c.PasswordCmd != "" && c.PasswordCmd != was.PasswordCmd { // two sources: which one is wanted is not for us to say
		return a.fail(fieldPassword, i18n.T("password 和 password_cmd 填一个就够了"))
	}
	if pw != "" { // the one place that has the keychain's command and clears the other sources; asked of the file before anything is stored
		c.PasswordCmd, c.PasswordEnv, c.Password = config.KeychainCmd(c.Name, pw), "", ""
	}
	if err := config.CheckUpdate(was.Name, was, c); err != nil {
		f.err = err.Error()
		return nil
	}
	var toast tea.Cmd
	if pw != "" {
		_, ok, err := config.StorePassword(c.Name, pw)
		switch {
		case err != nil:
			return a.fail(fieldPassword, err.Error())
		case ok:
		case !typed: // no keychain tool now: the password stays where it was
			c.PasswordCmd, c.PasswordEnv, c.Password = was.PasswordCmd, was.PasswordEnv, was.Password
		case strings.Contains(pw, "${"):
			return a.fail(fieldPassword, i18n.T("没有钥匙串工具：password 里的 ${ 会被当成环境变量"))
		default: // the file is read password_cmd first: the plaintext would never be used beside one
			c.PasswordCmd, c.Password = "", pw
			toast = a.showToast(i18n.T("没有钥匙串工具，密码以明文存进了 connections.toml"))
		}
	}
	saved, err := config.UpdateConnection(was.Name, was, c)
	if err != nil { // ponytail: past the check a write can still fail (a full disk, the file changed meanwhile), leaving the item stored; take it out then if it comes up
		f.err = err.Error()
		return nil
	}
	if !config.SamePassword(was.PasswordCmd, saved.PasswordCmd) { // the old item, of a name or a source left, is no one's now
		if err := config.DeletePassword(was.PasswordCmd); err != nil {
			toast = tea.Batch(toast, a.showToast(err.Error()))
		}
	}
	secrets.Delete(was.Name) // a changed password, or a name's, is asked for again
	secrets.Delete(saved.Name)
	rest := saved // the name and the group are the tree's at once, they are no news
	rest.Name, rest.Group = was.Name, was.Group
	changed, told := !reflect.DeepEqual(rest, was), false
	for _, s := range a.sessions {
		for _, conn := range s.conns() {
			if conn.Spec.Name != was.Name {
				continue
			}
			edited := saved
			edited.Database = conn.Spec.Database // a database of the connection stays the one
			if conn.State == connIdle || conn.State == connFailed {
				conn.setSpec(edited)
			} else { // dialed, or being: its engine and lock are the dial's
				conn.setConfig(edited)
				told = changed
			}
		}
	}
	if saved.Name != was.Name {
		a.state.RenameConnection(was.Name, saved.Name)
		toast = tea.Batch(toast, a.saveState())
	}
	if told {
		toast = tea.Batch(toast, a.showToast(i18n.T("下次连接时生效")))
	}
	a.regroup()
	a.selectConn(slices.IndexFunc(a.managed(), func(c *Conn) bool { return c.Spec.Name == saved.Name }))
	return toast
}

// saveConnForm is ↵: the connection, checked as connections.toml is when
// read, goes at its end and into every session's tree, not connected; one
// that doesn't check says why, its field getting the keys (§7.8). The first
// connection of a start with none is connected, shown and given a query
// (F7.4).
func (a *App) saveConnForm() tea.Cmd {
	f := a.form
	if f.was != nil {
		return a.saveEditedConn()
	}
	c, ok := a.formConn()
	if !ok {
		return nil
	}
	if err := config.CheckAppend(c); err != nil { // before a password goes into the keychain for it
		f.err = err.Error()
		return nil
	}
	var toast tea.Cmd
	if pw := f.password(); pw != "" {
		if c.PasswordCmd != "" { // two sources: which one is wanted is not for us to say
			return a.fail(fieldPassword, i18n.T("password 和 password_cmd 填一个就够了"))
		}
		cmd, stored, err := config.StorePassword(c.Name, pw)
		switch {
		case err != nil: // not a keychain without a tool: a locked one the user can unlock and ↵ again
			return a.fail(fieldPassword, err.Error())
		case stored:
			c.PasswordCmd = cmd
		case strings.Contains(pw, "${"): // dial would take it for a variable (§13)
			return a.fail(fieldPassword, i18n.T("没有钥匙串工具：password 里的 ${ 会被当成环境变量"))
		default: // plaintext, the user's call (§13): a file AppendConnection makes is 0600, one there is keeps its permissions
			c.Password = pw
			toast = a.showToast(i18n.T("没有钥匙串工具，密码以明文存进了 connections.toml"))
		}
	}
	if err := config.AppendConnection(c); err != nil {
		f.err = err.Error()
		return nil
	}
	first := len(a.sess.Conns) == 0
	for _, s := range a.sessions {
		s.Conns = append(s.Conns, newConn(c))
		groupOrder(s.Conns)
	}
	a.form = nil
	if first {
		return tea.Batch(toast, a.startFirst(a.sess.Conns[0]))
	}
	return toast
}

// startFirst makes conn, the first connection of a start with none, what
// the session started with, as if it had been named on the command line: it
// connects, and query_1 opens beside the empty pane on it (F7.4).
func (a *App) startFirst(conn *Conn) tea.Cmd {
	s := a.sess
	s.start, s.last, s.Name = conn, conn, conn.Spec.Name
	cmds := []tea.Cmd{a.connect(conn)}
	for _, w := range s.Windows { // each window made meanwhile had none either
		if p := w.pane(2); p != nil && len(p.Tabs) == 0 {
			cons, err := openQuery(conn, a.freeQuery())
			if err != nil {
				cmds = append(cmds, a.showToast(err.Error()))
				break
			}
			putTab(p, Tab{Name: cons.name, Query: cons}, false)
			cmds = append(cmds, a.keepQuery(cons))
		}
	}
	return tea.Batch(cmds...)
}

// managerView is the manager as it is drawn: the connections in the
// session's order, then the row that makes a new one, and the form (F8.16).
func (a *App) managerView() ui.ConnManager {
	ic, f := a.icons, a.form
	m := ui.ConnManager{Title: i18n.T("连接"), Sel: f.cur, Top: f.top, ListFocus: f.list, Form: a.formView()}
	group := ""
	for _, c := range a.managed() {
		row := ui.ConnRow{Icon: ic.Engine(c.Spec.Kind()), Name: c.Spec.Name}
		if g := c.Spec.Group; g != group { // a rule where a group begins, as in the tree (F10.6)
			group, row.Rule, row.Group = g, true, g
		}
		if c.readOnly {
			row.Lock = ic.Lock
		}
		m.Rows = append(m.Rows, row)
	}
	m.Rows = append(m.Rows, ui.ConnRow{Icon: ic.RowAdd, Name: i18n.T("新建"), Rule: len(a.groupNames()) > 0})
	return m
}

// formView is the form as it is drawn: each row's icon, the rows of the
// closed fold hidden, those that take no part dim; the keys under it are
// the list's while it has them.
func (a *App) formView() ui.Form {
	f := a.form
	fields := slices.Clone(f.fields)
	ic := a.icons
	del := ""
	if f.was != nil {
		del = i18n.T("删除")
	}
	icons := []ui.Icon{fieldName: ic.Tag, fieldGroup: ic.Group, fieldEngine: ic.Engine(fields[fieldEngine].Choices[fields[fieldEngine].On]), fieldHost: ic.Host,
		fieldPort: ic.Port, fieldUser: ic.User, fieldPassword: ic.Key, fieldDatabase: ic.Database, fieldReadOnly: ic.Lock, fieldProduction: ic.Stop,
		fieldAdvanced: ic.Advanced, fieldDSN: ic.Link, fieldPasswordCmd: ic.Query, fieldSSH: ic.Link}
	for i := range fields {
		fields[i].Icon = icons[i]
		fields[i].Hidden = (i == fieldDSN || i == fieldPasswordCmd || i == fieldSSH) && !f.advanced() || f.sqlite() && (i == fieldHost || i == fieldPort || i == fieldUser || i == fieldPassword || i == fieldSSH)
		fields[i].Dim = f.skipped(i) && !fields[i].Hidden
	}
	if f.sqlite() {
		fields[fieldDatabase].Label = "file"
	}
	if f.redis() {
		fields[fieldDatabase].Label = "db"
	}
	hints := bound(
		ui.Hint{Key: a.keys.Hint("form.next", "form"), Label: i18n.T("下一项"), Action: "form.next"},
		ui.Hint{Key: a.keys.Hint("form.save", "form"), Label: i18n.T("保存"), Action: "form.save"},
		ui.Hint{Key: a.keys.Hint("form.cancel", "form"), Label: i18n.T("取消"), Action: "form.cancel"},
	)
	if f.list {
		hints = bound(
			ui.Hint{Key: a.keys.Hint("conns.form", "conns"), Label: i18n.T("编辑"), Action: "conns.form"},
			ui.Hint{Key: a.keys.Hint("form.save", "conns"), Label: i18n.T("保存"), Action: "form.save"},
			ui.Hint{Key: a.keys.Hint("conns.close", "conns"), Label: i18n.T("关闭"), Action: "conns.close"},
		)
	}
	note := cmp.Or(f.err, f.result)
	if note == "" && f.testing != 0 { // the button stays as it is: the row says
		note = i18n.T("测试中…")
	}
	fields[fieldDSN].Example = dsnExample[fields[fieldEngine].Choices[fields[fieldEngine].On]]
	form := ui.Form{Fields: fields, Sel: f.sel, Err: note, OK: f.err == "" && (f.resultOK || f.testing != 0), Save: i18n.T("保存"), Test: i18n.T("测试"), Cancel: i18n.T("取消"), Delete: del, Hints: hints, PickAt: fieldGroup}
	if cands := a.groupCands(); len(cands) > 0 { // under the group field (F10.6)
		form.Pick = &ui.Complete{Sel: f.groupPick(len(cands)), Top: f.groupTop, Act: "form.pick"}
		for _, g := range cands {
			form.Pick.Items = append(form.Pick.Items, ui.CompleteItem{Text: g})
		}
	}
	return form
}

// groupCands are the groups the group field's list offers: those that start
// as what is typed does, in either case, but not what is typed exactly; none
// before anything is typed, as a completion has none (§7.8). None either when
// the field has no keys or the list is closed (F10.6).
func (a *App) groupCands() (out []string) {
	f := a.form
	if f == nil || f.list || f.sel != fieldGroup || f.groupOff {
		return nil
	}
	text := strings.TrimSpace(f.fields[fieldGroup].Input.Text)
	if text == "" {
		return nil
	}
	typed := strings.ToLower(text)
	for _, g := range a.groupNames() {
		if strings.HasPrefix(strings.ToLower(g), typed) && g != text {
			out = append(out, g)
		}
	}
	return out
}

// groupPick is the candidate of n the list has selected: the first, till ↓
// or ↑ picks another; none (-1) when there are none.
func (f *connForm) groupPick(n int) int {
	switch {
	case f.groupSel >= 0 && f.groupSel < n:
		return f.groupSel
	case n > 0:
		return 0
	}
	return -1
}

// groupMove is ↓ (d 1) and ↑ on the group list, round the ends.
func (a *App) groupMove(d int) {
	f, n := a.form, len(a.groupCands())
	if n == 0 {
		return
	}
	f.groupSel = (f.groupPick(n) + d + n) % n
}

// groupAccept puts the candidate selected in the field, and is whether one
// was: Tab and ↵ take it, or, with no list, do what they do without it (F10.6).
func (a *App) groupAccept() bool {
	f, cands := a.form, a.groupCands()
	i := f.groupPick(len(cands))
	if i < 0 {
		return false
	}
	g := cands[i]
	f.fields[fieldGroup].Input = ui.Input{Text: g, Pos: len(g)}
	f.groupSel = -1
	f.untest()
	return true
}
