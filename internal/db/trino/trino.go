// Package trino is db.Conn over Trino's client protocol (tech-design §8.10):
// a statement is POSTed, its pages of rows followed by nextUri, each value
// the text the server wrote it as in JSON. The catalog is the database level
// (§5), each one a connection of its own, as PG's databases are.
package trino

import (
	"bytes"
	"cmp"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// Conn is one client session: the catalog and schema, the session
// properties a SET SESSION left, the transaction START TRANSACTION began, as
// the server's headers say them, all sent with each statement.
type Conn struct {
	client   *http.Client
	endpoint string // scheme://host:port
	user     string
	password string
	catalog  string
	schema   string
	session  map[string]string
	tx       string // the transaction id; "" for none, autocommit
}

// config is what a DSN says.
type config struct {
	endpoint, user, password, catalog, schema string
	session                                   map[string]string
	insecure                                  bool
}

// parseDSN reads trino://user[:pass]@host:port/catalog/schema?ssl=true, or the
// form the Go client takes, http[s]://user@host:port?catalog=c&schema=s;
// session_properties=a:1;b:2 either way. The port is 8080, 443 with TLS;
// port 443 is TLS unless ssl=false says not, as nothing else answers there.
func parseDSN(dsn string) (config, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return config{}, err
	}
	q := u.Query()
	sslSet := cmp.Or(q.Get("ssl"), q.Get("SSL"))
	ssl := u.Scheme == "https" || sslSet == "true" || sslSet == "" && u.Scheme == "trino" && u.Port() == "443"
	switch u.Scheme {
	case "trino", "http", "https":
	default:
		return config{}, fmt.Errorf(i18n.T("不认识的 Trino 地址 %q：写成 trino://user@host:8080/catalog/schema"), dsn)
	}
	scheme, port := "http", "8080"
	if ssl {
		scheme, port = "https", "443"
	}
	host := u.Hostname()
	if host == "" {
		host = "localhost"
	}
	if p := u.Port(); p != "" {
		port = p
	}
	c := config{endpoint: scheme + "://" + net.JoinHostPort(host, port), user: u.User.Username(), session: map[string]string{}}
	c.password, _ = u.User.Password()
	path := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(path) > 0 {
		c.catalog = path[0]
	}
	if len(path) > 1 {
		c.schema = path[1]
	}
	if v := q.Get("catalog"); v != "" {
		c.catalog = v
	}
	if v := q.Get("schema"); v != "" {
		c.schema = v
	}
	if v := q.Get("user"); v != "" {
		c.user = v
	}
	if c.user == "" {
		c.user = "sqlmux"
	}
	c.insecure = q.Get("skip_verify") == "true"
	for _, p := range strings.Split(sessionProperties(u.RawQuery), ";") {
		if k, v, ok := strings.Cut(p, ":"); ok {
			c.session[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return c, nil
}

// sessionProperties is the raw query's session_properties, a:1;b:2 as the Go
// client writes it: url.Query drops a pair that has a ; in it.
func sessionProperties(raw string) string {
	for _, kv := range strings.Split(raw, "&") {
		if v, ok := strings.CutPrefix(kv, "session_properties="); ok {
			if u, err := url.QueryUnescape(v); err == nil {
				return u
			}
			return v
		}
	}
	return ""
}

// WithDatabase is dsn naming catalog name instead of its own: a child of the
// connection in the tree (§5, F15.1). Its schema is the server's to pick.
func WithDatabase(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	q := u.Query()
	if q.Has("catalog") {
		q.Set("catalog", name)
		q.Del("schema")
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	u.Path = "/" + url.PathEscape(name)
	return u.String(), nil
}

// Connect opens a session and says where it is: user@host:port, the catalog
// (§7.8). With no catalog in the DSN it takes the first of the server's that
// is not system, so the tree has schemas to show. readOnly sets nothing on the
// server: Trino has no read-only session; a read-only connection is the app's
// interception (§13), and quick SQL's a read-only transaction (Quick).
func Connect(ctx context.Context, dsn, password string, _ bool) (*Conn, db.Info, error) {
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
		catalog: cfg.catalog, schema: cfg.schema, session: cfg.session}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if c.catalog == "" {
		r, err := c.Query(dialCtx, "show catalogs")
		if err != nil {
			return nil, db.Info{}, err
		}
		for _, row := range r.Rows {
			if row[0].S != "system" {
				c.catalog = row[0].S
				break
			}
		}
	} else if _, err := c.Query(dialCtx, "select 1"); err != nil {
		return nil, db.Info{}, err
	}
	u, _ := url.Parse(c.endpoint)
	return c, db.Info{Addr: c.user + "@" + u.Host, Database: c.catalog}, nil
}

func (c *Conn) Close() error {
	c.client.CloseIdleConnections()
	return nil
}

// TxStatus is 'T' in a transaction a START TRANSACTION began, else 'I'.
func (c *Conn) TxStatus() byte {
	if c.tx != "" {
		return 'T'
	}
	return 'I'
}

// Exec runs the statements of sql one after the other, a request each: the
// protocol takes one at a time. It stops at the first that fails (§8.1).
func (c *Conn) Exec(ctx context.Context, sql string, maxRows int) ([]db.Result, error) {
	var out []db.Result
	for _, st := range sqlkit.Statements(sql, sqlkit.Trino) {
		var r db.Result
		err := c.run(ctx, sql[st.Start:st.End], func(cols []db.Col) error { r.Cols = cols; return nil }, func(v []db.Val) error {
			if maxRows > 0 && len(r.Rows) == maxRows {
				r.Truncated = true
				return errEnough
			}
			r.Rows = append(r.Rows, v)
			return nil
		}, &r)
		if err != nil && !errors.Is(err, errEnough) {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Query runs one statement, each $n of it the literal of args[n-1].
func (c *Conn) Query(ctx context.Context, sql string, args ...db.Val) (db.Result, error) {
	text, err := db.Inline(sql, args, sqlkit.Trino)
	if err != nil {
		return db.Result{}, err
	}
	var r db.Result
	err = c.run(ctx, text, func(cols []db.Col) error { r.Cols = cols; return nil }, func(v []db.Val) error { r.Rows = append(r.Rows, v); return nil }, &r)
	return r, err
}

// Stream gives the rows of a statement page by page as the server has them,
// head with the columns first (F12.4).
func (c *Conn) Stream(ctx context.Context, sql string, head func([]db.Col) error, row func([]db.Val) error) error {
	return c.run(ctx, sql, head, row, &db.Result{})
}

// errEnough stops a read that has the rows it wants; the statement is
// cancelled on the server as run ends.
var errEnough = errors.New("enough rows")

// answer is one page of a statement's answer.
type answer struct {
	NextURI     string              `json:"nextUri"`
	Columns     []column            `json:"columns"`
	Data        [][]json.RawMessage `json:"data"`
	Error       *serverError        `json:"error"`
	UpdateType  string              `json:"updateType"`
	UpdateCount *int64              `json:"updateCount"`
}

type column struct{ Name, Type string }

type serverError struct {
	Message       string `json:"message"`
	ErrorCode     int    `json:"errorCode"`
	ErrorName     string `json:"errorName"`
	ErrorType     string `json:"errorType"`
	ErrorLocation *struct {
		Line   int `json:"lineNumber"`
		Column int `json:"columnNumber"`
	} `json:"errorLocation"`
}

// run sends sql and follows its pages, giving head the columns and row each
// row; r gets the tag, the rows a write hit and the time it took. A run that
// ends before the last page, cancelled or with what it wanted, cancels the
// statement on the server (§8.3).
func (c *Conn) run(ctx context.Context, sql string, head func([]db.Col) error, row func([]db.Val) error, r *db.Result) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	start := time.Now()
	sql = strings.TrimRight(strings.TrimSpace(sql), ";") // no ; at the end: the server's syntax error
	word := sqlkit.FirstWord(sql, sqlkit.Trino)
	a, err := c.page(ctx, http.MethodPost, c.endpoint+"/v1/statement", sql)
	var types []string
	headed, counted := false, false
	for err == nil {
		if a.Error != nil {
			err = toError(a.Error, sql)
			break
		}
		if !headed && a.Columns != nil {
			headed = true
			cols := make([]db.Col, len(a.Columns))
			types = make([]string, len(a.Columns))
			for i, col := range a.Columns {
				cols[i], types[i] = db.Col{Name: col.Name, Type: col.Type}, col.Type
			}
			if a.UpdateType == "" {
				if err = head(cols); err != nil {
					break
				}
			}
		}
		if a.UpdateCount != nil {
			r.Affected, counted = *a.UpdateCount, true
		}
		if a.UpdateType == "" {
			for _, raw := range a.Data {
				if err = row(values(raw, types)); err != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		if a.UpdateType != "" {
			r.Tag = a.UpdateType
		}
		if a.NextURI == "" {
			break
		}
		next := a.NextURI
		if a, err = c.page(ctx, http.MethodGet, next, ""); err != nil && a.NextURI == "" {
			a.NextURI = next
		}
	}
	if err != nil && a.NextURI != "" { // stopped before the end: the server is told to stop too
		go c.cancel(a.NextURI)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	switch {
	case r.Tag == "": // rows: SELECT 3
		r.Tag = strings.ToUpper(word) + " " + strconv.Itoa(len(r.Rows))
	case counted: // the server's own words for a write, and the rows it hit: UPDATE 1
		r.Tag += " " + strconv.FormatInt(r.Affected, 10)
	}
	r.Took = time.Since(start)
	return err
}

// page asks for one page of an answer: the statement's POST, or a GET of
// its nextUri; a 503, the server's "busy, ask again", is asked again. The
// headers of the answer carry what the statement did to the session.
func (c *Conn) page(ctx context.Context, method, uri, body string) (a answer, err error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, uri, strings.NewReader(body))
		if err != nil {
			return a, err
		}
		c.headers(req)
		resp, err := c.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return a, ctx.Err()
			}
			return a, err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return a, err
		}
		if resp.StatusCode == http.StatusServiceUnavailable && attempt < 10 {
			select {
			case <-ctx.Done():
				return a, ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			msg := strings.TrimSpace(string(data))
			if msg == "" {
				msg = resp.Status
			}
			return a, Error{Name: http.StatusText(resp.StatusCode), Message: msg}
		}
		c.took(resp.Header)
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		err = d.Decode(&a)
		return a, err
	}
}

// headers are the session's, sent with every request.
func (c *Conn) headers(req *http.Request) {
	req.Header.Set("X-Trino-User", c.user)
	req.Header.Set("X-Trino-Source", "sqlmux")
	if c.password != "" {
		req.SetBasicAuth(c.user, c.password)
	}
	if c.catalog != "" {
		req.Header.Set("X-Trino-Catalog", c.catalog)
	}
	if c.schema != "" {
		req.Header.Set("X-Trino-Schema", c.schema)
	}
	tx := c.tx
	if tx == "" {
		tx = "NONE" // asks the server to say when a statement starts one
	}
	req.Header.Set("X-Trino-Transaction-Id", tx)
	if len(c.session) > 0 {
		var kv []string
		for _, k := range slices.Sorted(func(yield func(string) bool) {
			for k := range c.session {
				if !yield(k) {
					return
				}
			}
		}) {
			kv = append(kv, k+"="+url.QueryEscape(c.session[k]))
		}
		req.Header.Set("X-Trino-Session", strings.Join(kv, ","))
	}
}

// took keeps what a statement did to the session: USE's catalog and schema,
// SET SESSION's and RESET SESSION's properties, a transaction begun or ended.
func (c *Conn) took(h http.Header) {
	if v := h.Get("X-Trino-Set-Catalog"); v != "" {
		c.catalog = v
	}
	if v := h.Get("X-Trino-Set-Schema"); v != "" {
		c.schema = v
	}
	for _, kv := range h.Values("X-Trino-Set-Session") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			v, _ = url.QueryUnescape(v)
			c.session[k] = v
		}
	}
	for _, k := range h.Values("X-Trino-Clear-Session") {
		delete(c.session, k)
	}
	if v := h.Get("X-Trino-Started-Transaction-Id"); v != "" {
		c.tx = v
	}
	if h.Get("X-Trino-Clear-Transaction-Id") != "" {
		c.tx = ""
	}
}

// cancel tells the server to stop the statement whose next page is at uri.
func (c *Conn) cancel(uri string) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return
	}
	c.headers(req)
	if resp, err := c.client.Do(req); err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// values is a row's JSON as text, each value as the server wrote it: a
// string's own text, a number's digits, true or false, an array, a map or a
// row as its JSON; a varbinary, in base64, by its size (§8.1).
func values(raw []json.RawMessage, types []string) []db.Val {
	out := make([]db.Val, len(raw))
	for i, v := range raw {
		s := string(v)
		switch {
		case s == "null":
			out[i] = db.Val{Null: true}
		case strings.HasPrefix(s, `"`):
			var text string
			_ = json.Unmarshal(v, &text)
			if i < len(types) && types[i] == "varbinary" {
				b, _ := base64.StdEncoding.DecodeString(text)
				text = db.BinaryShown(len(b))
			} else if !utf8.ValidString(text) {
				text = db.BinaryShown(len(text))
			}
			out[i] = db.Val{S: text}
		default:
			out[i] = db.Val{S: s}
		}
	}
	return out
}

// Error is what the server said of a statement that failed: its error's name,
// the message, and where in the statement it points, from 1, 0 for nowhere.
type Error struct {
	Name     string
	Message  string
	Position int
}

func (e Error) Error() string { return e.Message }

// toError is the server's error of statement sql, its line and column made
// the character they are.
func toError(e *serverError, sql string) Error {
	out := Error{Name: e.ErrorName, Message: e.Message}
	if loc := e.ErrorLocation; loc != nil && loc.Line > 0 && loc.Column > 0 {
		lines := strings.SplitAfter(sql, "\n")
		pos := 0
		for i := 0; i < loc.Line-1 && i < len(lines); i++ {
			pos += utf8.RuneCountInString(lines[i])
		}
		out.Position = pos + loc.Column
		// the message says it again: "line 1:8: mismatched input"
		if p := fmt.Sprintf("line %d:%d: ", loc.Line, loc.Column); strings.HasPrefix(out.Message, p) {
			out.Message = strings.TrimPrefix(out.Message, p)
		}
	}
	return out
}

// Engine is Trino's way with the catalog, SQL and errors (§8.10).
type Engine struct{}

func (Engine) Connect(ctx context.Context, dsn, password string, readOnly bool) (db.Conn, db.Info, error) {
	c, info, err := Connect(ctx, dsn, password, readOnly)
	if err != nil {
		return nil, db.Info{}, err
	}
	return c, info, nil
}

// Broken says whether err is a lost connection (§8.2「断线重连」): a dial that
// failed sent nothing; the rest of the network's errors may have sent it.
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

// ErrorOf is err as the log and an error bar show it (§11).
func (Engine) ErrorOf(err error) db.ServerError {
	var e Error
	if !errors.As(err, &e) {
		return db.ServerError{Severity: "ERROR", Message: err.Error()}
	}
	return db.ServerError{Severity: "ERROR", Code: e.Name, Message: e.Message, Position: e.Position}
}

func (Engine) Dialect() sqlkit.Dialect { return sqlkit.Trino }

func (Engine) Quote(names ...string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = `"` + strings.ReplaceAll(n, `"`, `""`) + `"`
	}
	return strings.Join(quoted, ".")
}

// Arg is $n, which Query puts the literal in for.
func (Engine) Arg(n int) string { return "$" + strconv.Itoa(n) }

// Typed is arg as the column's type: Trino compares and sets no varchar as a
// number or a date (§10.3); a json's text is parsed, which a CAST would take
// for a JSON string.
func (Engine) Typed(arg, typ string) string {
	if typ == "json" {
		return "json_parse(" + arg + ")"
	}
	return "CAST(" + arg + " AS " + typ + ")"
}

func (Engine) Eq(col, arg string) string   { return col + " = " + arg }
func (Engine) Same(col, arg string) string { return col + " = " + arg }

// Write is an UPDATE or a DELETE, which a connector runs in autocommit only
// (§10.3).
func (Engine) Write(table, set, where string) string {
	if set == "" {
		return "delete from " + table + " where " + where
	}
	return "update " + table + " set " + set + " where " + where
}

// UseSchema makes schema the one unqualified names go to (§8.6).
func (Engine) UseSchema(_ context.Context, c db.Conn, schema, _ string) error {
	if own, ok := connOf(c); ok {
		own.schema = schema
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
