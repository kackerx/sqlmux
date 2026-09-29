package sqlkit

import (
	_ "embed"
	"errors"
	"strings"
	"sync"

	"github.com/dop251/goja"
)

// sql-formatter 15.9.0's standalone bundle (MIT, sql-formatter.LICENSE),
// run in goja (§9.5).
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
	KeywordCase string // lower, upper or preserve
	TabWidth    int
}

// Format is sql as sql-formatter lays it out in d's language (§9.5).
func Format(sql string, d Dialect, o Options) (string, error) {
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
	opts.Set("language", map[Dialect]string{PG: "postgresql", MySQL: "mysql"}[d])
	opts.Set("keywordCase", o.KeywordCase)
	opts.Set("tabWidth", o.TabWidth)
	v, err := f.format(goja.Undefined(), f.vm.ToValue(sql), opts)
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
