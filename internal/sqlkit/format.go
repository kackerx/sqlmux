package sqlkit

import (
	_ "embed"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/dop251/goja"
)

// sql-formatter 15.9.0's standalone bundle, nearley's runtime in it (MIT,
// both in sql-formatter.LICENSE), run in goja (§9.5).
//
//go:embed sql-formatter.min.js
var formatterJS string

// unicodeNames are the long Unicode property names in the bundle goja's
// regexps cannot read, what stands for them, and how often each is there:
// a new bundle with other counts fails the test (§9.5).
var unicodeNames = []struct {
	from, to string
	n        int
}{{`\p{Alphabetic}`, `\p{L}`, 3}, {`\p{Mark}`, `\p{M}`, 1}, {`\p{Decimal_Number}`, `\p{Nd}`, 1}}

// formatter is the VM, made the first time it is needed and kept; goja
// runs one call at a time.
var formatter struct {
	sync.Mutex
	vm     *goja.Runtime
	format goja.Callable
	err    error
}

// Options are what sql-formatter is told besides the language.
type Options struct {
	KeywordCase string // lower, upper or preserve; data types' too (§9.5)
	TabWidth    int
}

// FormatTimeout bounds a format, here or by formatprg (§9.5): goja takes
// seconds over a few KB of SQL. A var for the tests.
var FormatTimeout = 5 * time.Second

// ErrTimeout is a format that took longer than FormatTimeout.
var ErrTimeout = errors.New("timeout")

// Format is sql as sql-formatter lays it out in d's language (§9.5). Past
// FormatTimeout the VM is interrupted, to be used again.
func Format(sql string, d Dialect, o Options) (string, error) {
	return format(sql, d, o, time.Now().Add(FormatTimeout))
}

// format is Format that has until deadline.
func format(sql string, d Dialect, o Options, deadline time.Time) (string, error) {
	f := &formatter
	f.Lock()
	defer f.Unlock()
	if f.vm == nil && f.err == nil {
		f.vm, f.format, f.err = load()
	}
	if f.err != nil {
		return "", f.err
	}
	opts := f.vm.NewObject()
	_ = opts.Set("language", map[Dialect]string{PG: "postgresql", MySQL: "mysql", SQLite: "sqlite", MSSQL: "transactsql", ClickHouse: "clickhouse", Trino: "trino"}[d])
	_ = opts.Set("keywordCase", o.KeywordCase)
	_ = opts.Set("dataTypeCase", o.KeywordCase) // PG's type names are keywords (15.x has them apart)
	_ = opts.Set("tabWidth", o.TabWidth)
	left := time.Until(deadline)
	if left <= 0 {
		return "", ErrTimeout
	}
	vm, fired := f.vm, make(chan struct{})
	timer := time.AfterFunc(left, func() { vm.Interrupt(ErrTimeout); close(fired) })
	v, err := f.format(goja.Undefined(), vm.ToValue(sql), opts)
	if !timer.Stop() {
		<-fired // it may be interrupting still
	}
	vm.ClearInterrupt() // an interrupt that came as the format ended waits for the next one
	if ie := (*goja.InterruptedError)(nil); errors.As(err, &ie) {
		return "", ErrTimeout
	}
	if ex := (*goja.Exception)(nil); errors.As(err, &ex) { // what the formatter said, its first line
		msg := ex.Value().String()
		if o, ok := ex.Value().(*goja.Object); ok {
			msg = o.Get("message").String()
		}
		first, _, _ := strings.Cut(msg, "\n")
		return "", errors.New(first)
	}
	if err != nil {
		return "", err
	}
	return v.String(), nil
}

// FormatAll is Format of a text of several statements, each by a call of its
// own, the lot under one FormatTimeout: the VM's time grows faster than the
// text (4 KB of statements take 4 s at once, 7 KB time out, and one by one
// 48 of them take 1.4 s). Each runs from after the ; before it to its own, so
// the comments above it go to sql-formatter with it, and the output is what
// one call lays out of all of them. One statement, and MySQL's /*! … */, which
// may hold a ; of its own, go in one call (§9.5).
func FormatAll(s string, d Dialect, o Options) (string, error) {
	deadline := time.Now().Add(FormatTimeout)
	stmts := Statements(s, d)
	if len(stmts) < 2 || d == MySQL && strings.Contains(s, "/*!") {
		return format(s, d, o, deadline)
	}
	var units []string
	from := 0
	for i, st := range stmts {
		cut := len(s)
		if i+1 < len(stmts) {
			cut = semicolonAfter(s, st.End, stmts[i+1].Start, d)
		}
		out, err := format(s[from:cut], d, o, deadline)
		if err != nil {
			if from > 0 && !errors.Is(err, ErrTimeout) { // where it is in the text, not in the unit
				if _, here := format(positioned(s, from)+s[from:cut], d, o, deadline); here != nil && !errors.Is(here, ErrTimeout) {
					err = here
				}
			}
			return "", err
		}
		units, from = append(units, out), cut
	}
	return strings.Join(units, "\n\n"), nil
}

// semicolonAfter is where the first ; of s[from:to], the gap between two
// statements, ends: the one that ends the statement before it. Comments before
// it, and any more ;s, go with the next unit.
func semicolonAfter(s string, from, to int, d Dialect) int {
	gap := s[from:to]
	for _, t := range Scan(gap, d) {
		if t.Kind == Punct && gap[t.Start] == ';' {
			return from + t.End
		}
	}
	return to // Statements cuts at a ;, so there is one
}

// positioned is the blank text that puts what follows s[:at] where it is in s:
// its lines, and the columns of the last, counted as JavaScript does.
func positioned(s string, at int) string {
	line := s[strings.LastIndexByte(s[:at], '\n')+1 : at]
	return strings.Repeat("\n", strings.Count(s[:at], "\n")) + strings.Repeat(" ", len(utf16.Encode([]rune(line))))
}

func load() (*goja.Runtime, goja.Callable, error) {
	js := formatterJS
	for _, u := range unicodeNames {
		js = strings.ReplaceAll(js, u.from, u.to)
	}
	vm := goja.New()
	if _, err := vm.RunString(js); err != nil { // the UMD bundle sets the global sqlFormatter
		return nil, nil, err
	}
	format, ok := goja.AssertFunction(vm.Get("sqlFormatter").ToObject(vm).Get("format"))
	if !ok {
		return nil, nil, errors.New("sql-formatter.min.js: no sqlFormatter.format")
	}
	return vm, format, nil
}
