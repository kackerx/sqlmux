// Package clickhouse is db.Conn over ClickHouse's HTTP interface (tech-design
// §8.9): every value the server's own text, TabSeparatedWithNamesAndTypes,
// one statement a request, a session of its own for what a SET leaves.
package clickhouse

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// tsv is the format a statement answers in when it names none: the names and
// the types first, each value as the server writes it, NULL as \N.
const tsv = "TabSeparatedWithNamesAndTypes"

// Conn is one session on a server. The server keeps a session's SETs and its
// temporary tables; the database every request names is the connection's own,
// moved by USE and by UseSchema (§8.6).
type Conn struct {
	client   *http.Client
	endpoint string // scheme://host:port/
	user     string
	password string
	database string
	session  string
	readOnly bool       // readonly=2: the server refuses writes, settings still go (§13)
	settings url.Values // the DSN's own, sent with every request
}

// config is what a DSN says: the endpoint, the user and password, the
// database, and the settings to send; TLS when it asks for https.
type config struct {
	endpoint, user, password, database string
	settings                           url.Values
	insecure                           bool
}

// parseDSN reads clickhouse://user:pass@host:port/db?secure=true&setting=value,
// or the http:// or https:// URL of the interface itself, its database the path
// or a database parameter. The port is the HTTP one: 8123, 8443 with TLS.
func parseDSN(dsn string) (config, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return config{}, err
	}
	q := u.Query()
	secure := u.Scheme == "https" || q.Get("secure") == "true" || q.Get("secure") == "1"
	switch u.Scheme {
	case "clickhouse", "http", "https":
	default:
		return config{}, fmt.Errorf(i18n.T("不认识的 ClickHouse 地址 %q：写成 clickhouse://user:pass@host:8123/db"), dsn)
	}
	scheme, port := "http", "8123"
	if secure {
		scheme, port = "https", "8443"
	}
	host := u.Hostname()
	if host == "" {
		host = "localhost"
	}
	if p := u.Port(); p != "" {
		port = p
	}
	if port == "9000" || port == "9440" { // the native protocol's, which sqlmux does not speak
		return config{}, fmt.Errorf(i18n.T("端口 %s 是 ClickHouse 的原生协议，sqlmux 走 HTTP 接口：一般是 8123，TLS 是 8443"), port)
	}
	c := config{endpoint: scheme + "://" + net.JoinHostPort(host, port) + "/", user: u.User.Username(), database: strings.Trim(u.Path, "/"), settings: url.Values{}}
	c.password, _ = u.User.Password()
	if c.user == "" {
		c.user = "default"
	}
	for k, vs := range q {
		switch k {
		case "secure":
		case "skip_verify":
			c.insecure = vs[0] == "true" || vs[0] == "1"
		case "database":
			c.database = vs[0]
		case "user", "username":
			c.user = vs[0]
		case "password":
			c.password = vs[0]
		default:
			c.settings[k] = vs
		}
	}
	return c, nil
}

// Connect opens a session, and says where it is: user@host:port for the
// status bar, the database the server put it in (§7.8). readOnly has the
// server refuse every write of it, readonly=2.
func Connect(ctx context.Context, dsn, password string, readOnly bool) (*Conn, db.Info, error) {
	cfg, err := parseDSN(dsn)
	if err != nil {
		return nil, db.Info{}, err
	}
	if password != "" {
		cfg.password = password
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if d, ok := db.DialerOf(ctx); ok { // through ssh (F15.2)
		transport.DialContext = d.Dial
	}
	if cfg.insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // the DSN asked for it: skip_verify=true
	}
	c := &Conn{client: &http.Client{Transport: transport}, endpoint: cfg.endpoint, user: cfg.user, password: cfg.password,
		database: cfg.database, session: newID(), readOnly: readOnly, settings: cfg.settings}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	r, err := c.Query(dialCtx, "select currentDatabase(), currentUser()")
	if err != nil {
		return nil, db.Info{}, err
	}
	c.database = r.Rows[0][0].S
	u, _ := url.Parse(c.endpoint)
	return c, db.Info{Addr: r.Rows[0][1].S + "@" + u.Host, Database: c.database}, nil
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (c *Conn) Close() error {
	c.client.CloseIdleConnections()
	return nil
}

// TxStatus is out of a transaction, always: ClickHouse has none (§10.3).
func (c *Conn) TxStatus() byte { return 'I' }

// Exec runs the statements of sql one after the other, a request each: the
// HTTP interface takes one at a time. It stops at the first that fails, with
// the results of those before it (§8.1).
func (c *Conn) Exec(ctx context.Context, sql string, maxRows int) ([]db.Result, error) {
	var out []db.Result
	for _, st := range sqlkit.Statements(sql, sqlkit.ClickHouse) {
		r, err := c.run(ctx, sql[st.Start:st.End], maxRows)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Query runs one statement, each $n of it the literal of args[n-1] (Arg).
func (c *Conn) Query(ctx context.Context, sql string, args ...db.Val) (db.Result, error) {
	text, err := db.Inline(sql, args, sqlkit.ClickHouse)
	if err != nil {
		return db.Result{}, err
	}
	return c.run(ctx, text, 0)
}

// run sends one statement and reads its answer, keeping maxRows rows of it
// (0: all), the request ended past them.
func (c *Conn) run(ctx context.Context, sql string, maxRows int) (db.Result, error) {
	start := time.Now()
	word := sqlkit.FirstWord(sql, sqlkit.ClickHouse)
	resp, stop, err := c.send(ctx, sql)
	if err != nil {
		return db.Result{}, err
	}
	defer stop()
	defer resp.Body.Close()
	if word == "use" { // the database every request names from now on
		c.database = useTarget(sql)
	}
	var r db.Result
	err = c.read(resp, func(cols []db.Col) error { r.Cols = cols; return nil }, func(vals []db.Val) error {
		if maxRows > 0 && len(r.Rows) == maxRows {
			r.Truncated = true
			return errEnough
		}
		r.Rows = append(r.Rows, vals)
		return nil
	})
	if err != nil && !errors.Is(err, errEnough) {
		return db.Result{}, err
	}
	r.Took = time.Since(start)
	r.Tag = strings.ToUpper(word)
	switch {
	case r.Cols != nil:
		r.Tag += " " + strconv.Itoa(len(r.Rows))
	case word == "insert":
		r.Affected = written(resp)
		r.Tag += " " + strconv.FormatInt(r.Affected, 10)
	case word == "alter" || word == "delete" || word == "update": // a mutation says nothing of the rows it hit
		r.Affected = -1
	}
	return r, nil
}

// errEnough stops a read that has the rows it wants.
var errEnough = errors.New("enough rows")

// written is the rows an INSERT wrote, as the summary header has it.
func written(resp *http.Response) int64 {
	var sum struct {
		Written string `json:"written_rows"`
	}
	_ = json.Unmarshal([]byte(resp.Header.Get("X-ClickHouse-Summary")), &sum)
	n, _ := strconv.ParseInt(sum.Written, 10, 64)
	return n
}

// useTarget is the database a USE names, unquoted.
func useTarget(sql string) string {
	for _, t := range sqlkit.Scan(sql, sqlkit.ClickHouse)[1:] {
		switch t.Kind {
		case sqlkit.Ident, sqlkit.Keyword:
			return sql[t.Start:t.End]
		case sqlkit.Quoted:
			return unescape(sql[t.Start+1 : t.End-1])
		}
	}
	return ""
}

// Stream gives the rows of a statement as they come, head with the columns
// first (F12.4).
func (c *Conn) Stream(ctx context.Context, sql string, head func([]db.Col) error, row func([]db.Val) error) error {
	resp, stop, err := c.send(ctx, sql)
	if err != nil {
		return err
	}
	defer stop()
	defer resp.Body.Close()
	return c.read(resp, head, row)
}

// send posts sql, the connection's database, session and settings in the URL.
// A request whose ctx is cancelled ends; the server stops a read as the client
// goes, and a write by a KILL QUERY sent after it (§8.3). stop, called once the
// answer is read, keeps the ctx's own end, which comes with every request's,
// from sending that.
func (c *Conn) send(ctx context.Context, sql string) (resp *http.Response, stop func() bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	q := url.Values{}
	for k, vs := range c.settings {
		q[k] = vs
	}
	id := newID()
	q.Set("query_id", id)
	q.Set("session_id", c.session)
	q.Set("session_timeout", "3600")
	q.Set("default_format", tsv)
	q.Set("mutations_sync", "2") // a save's UPDATE is done when it answers (§10.3)
	q.Set("cancel_http_readonly_queries_on_client_close", "1")
	if c.database != "" {
		q.Set("database", c.database)
	}
	if c.readOnly {
		q.Set("readonly", "2")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"?"+q.Encode(), strings.NewReader(sql))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("X-ClickHouse-User", c.user)
	req.Header.Set("X-ClickHouse-Key", c.password)
	stop = context.AfterFunc(ctx, func() { c.kill(id) })
	resp, err = c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		stop()
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		stop()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return nil, nil, parseError(string(body))
	}
	return resp, stop, nil
}

// kill asks the server to stop query id, in a request of its own, out of the
// session: a session runs one request at a time.
func (c *Conn) kill(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q := url.Values{"query": {"kill query where query_id = " + sqlkit.Literal(id, sqlkit.ClickHouse) + " async"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return
	}
	req.Header.Set("X-ClickHouse-User", c.user)
	req.Header.Set("X-ClickHouse-Key", c.password)
	if resp, err := c.client.Do(req); err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// read reads an answer: in TabSeparatedWithNamesAndTypes, the columns and
// then each row; in a format the statement named itself, a column of its lines.
// An error the server meets once rows have gone comes at the end of them, after
// a line of __exception__ (§8.9).
func (c *Conn) read(resp *http.Response, head func([]db.Col) error, row func([]db.Val) error) error {
	br := bufio.NewReaderSize(resp.Body, 64<<10)
	line := func() (string, bool, error) {
		s, err := br.ReadString('\n')
		if err == io.EOF && s == "" {
			return "", false, nil
		}
		if err != nil && err != io.EOF {
			return "", false, err
		}
		s = strings.TrimSuffix(s, "\n")
		if s == "__exception__\r" || s == "__exception__" { // the server's marker ends in CRLF; a value's \r is escaped, so no line of rows does
			rest, _ := io.ReadAll(io.LimitReader(br, 1<<16))
			return "", false, parseError(string(rest))
		}
		return s, true, nil
	}
	if f := resp.Header.Get("X-ClickHouse-Format"); f != tsv {
		first := true
		for {
			s, ok, err := line()
			if err != nil || !ok {
				return err
			}
			if first {
				if err := head([]db.Col{{Name: strings.ToLower(f), Type: "String"}}); err != nil {
					return err
				}
				first = false
			}
			if err := row([]db.Val{{S: s}}); err != nil {
				return err
			}
		}
	}
	names, ok, err := line()
	if err != nil || !ok { // no rows of any kind: a write, a SET
		return err
	}
	types, _, err := line()
	if err != nil {
		return err
	}
	ns, ts := strings.Split(names, "\t"), strings.Split(types, "\t")
	cols := make([]db.Col, len(ns))
	for i := range ns {
		cols[i] = db.Col{Name: unescape(ns[i])}
		if i < len(ts) {
			cols[i].Type = unescape(ts[i])
		}
	}
	if err := head(cols); err != nil {
		return err
	}
	for {
		s, ok, err := line()
		if err != nil || !ok {
			return err
		}
		fields := strings.Split(s, "\t")
		vals := make([]db.Val, len(fields))
		for i, f := range fields {
			vals[i] = cell(f)
		}
		if err := row(vals); err != nil {
			return err
		}
	}
}

// cell is one field of a TabSeparated line: \N alone is NULL, the rest
// unescaped; bytes that are no UTF-8 are shown by their size (§8.1).
// ponytail: an export writes those as the grid shows them too, not as bytes
func cell(f string) db.Val {
	if f == `\N` {
		return db.Val{Null: true}
	}
	s := unescape(f)
	if !utf8.ValidString(s) {
		return db.Val{S: db.BinaryShown(len(s))}
	}
	return db.Val{S: s}
}

// unescape undoes TabSeparated's escapes: \t \n \r \b \f \0 \\ \' and the like.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case '0':
			b.WriteByte(0)
		default: // \\ \' and any other: the character itself
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// Error is what the server said of a statement that failed: its error number,
// the name of it and the message, as "Code: 47. DB::Exception: … (NAME) (version …)".
type Error struct {
	Code    int
	Name    string
	Message string
}

func (e Error) Error() string {
	if e.Name == "" {
		return e.Message
	}
	return e.Message + " (" + e.Name + ")"
}

var errorText = regexp.MustCompile(`(?s)^Code: (\d+)\. (?:DB::Exception: )?(.*?)(?: \(([A-Z_0-9]+)\))?(?: \(version .*\))?\s*$`)

// parseError is the error of the text the server answered a failure with.
func parseError(body string) error {
	body = strings.TrimSpace(body)
	m := errorText.FindStringSubmatch(body)
	if m == nil {
		if body == "" {
			body = i18n.T("服务器没有说明原因")
		}
		return Error{Message: body}
	}
	n, _ := strconv.Atoi(m[1])
	return Error{Code: n, Name: m[3], Message: strings.TrimSuffix(m[2], ".")}
}

// Engine is ClickHouse's way with the catalog, SQL and errors (§8.9).
type Engine struct{}

func (Engine) Connect(ctx context.Context, dsn, password string, readOnly bool) (db.Conn, db.Info, error) {
	c, info, err := Connect(ctx, dsn, password, readOnly)
	if err != nil {
		return nil, db.Info{}, err
	}
	return c, info, nil
}

// Broken says whether err is a lost connection (§8.2「断线重连」): a dial that
// failed sent nothing; the rest of the network's errors may have sent the
// statement. A server's error leaves the connection as it was: each request
// is one of its own.
func (Engine) Broken(err error) db.Lost {
	var op *net.OpError
	switch {
	case errors.As(err, &op) && op.Op == "dial":
		return db.LostUnsent
	case db.NetworkError(err):
		return db.LostSent
	}
	return db.NotLost
}

// ErrorOf is err as the log and an error bar show it (§11): the error's name
// as its code, and its message.
func (Engine) ErrorOf(err error) db.ServerError {
	var e Error
	if !errors.As(err, &e) {
		return db.ServerError{Severity: "ERROR", Message: err.Error()}
	}
	code := e.Name
	if code == "" && e.Code != 0 {
		code = strconv.Itoa(e.Code)
	}
	return db.ServerError{Severity: "ERROR", Code: code, Message: e.Message}
}

func (Engine) Dialect() sqlkit.Dialect { return sqlkit.ClickHouse }

// Quote writes names in backquotes, a backquote or a backslash in one escaped.
func (Engine) Quote(names ...string) string {
	quoted := make([]string, len(names))
	r := strings.NewReplacer(`\`, `\\`, "`", "\\`")
	for i, n := range names {
		quoted[i] = "`" + r.Replace(n) + "`"
	}
	return strings.Join(quoted, ".")
}

// Arg is $n, which Query puts the literal in for: the HTTP interface's own
// parameters want a type in the statement.
func (Engine) Arg(n int) string { return "$" + strconv.Itoa(n) }

// Typed is arg as the column's type: ClickHouse compares and sets no text as a
// number or a date (§10.3).
func (Engine) Typed(arg, typ string) string { return "CAST(" + arg + " AS " + typ + ")" }

func (Engine) Eq(col, arg string) string { return col + " = " + arg }

// Same is the column holding what was read: arg is the text read, as the
// column's type (Typed), the server reading back what it wrote.
func (Engine) Same(col, arg string) string { return col + " = " + arg }

// Write is a mutation (§10.3): ALTER TABLE … UPDATE or DELETE, which the
// connection's mutations_sync = 2 waits for.
func (Engine) Write(table, set, where string) string {
	if set == "" {
		return "alter table " + table + " delete where " + where
	}
	return "alter table " + table + " update " + set + " where " + where
}

// UseSchema makes schema the database of the connection's requests (§8.6).
func (Engine) UseSchema(_ context.Context, c db.Conn, schema, _ string) error {
	if own, ok := connOf(c); ok {
		own.database = schema
	}
	return nil
}

// connOf is c as this package's Conn, through the Worker's counting.
func connOf(c db.Conn) (*Conn, bool) {
	if u, ok := c.(interface{ Unwrap() db.Conn }); ok {
		c = u.Unwrap()
	}
	own, ok := c.(*Conn)
	return own, ok
}
