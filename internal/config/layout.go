package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
)

// Layout is the working set as it is saved (§14「布局文件」): the sessions,
// each with its windows, their split trees and panes, and the tabs in them;
// what is not saved is what cannot be: results, queries running, edits not
// saved. Plain values, a TOML file a person can write.
type Layout struct {
	Version  int             `toml:"version"`
	Active   int             `toml:"active"` // the session attached
	Sessions []LayoutSession `toml:"session"`
}

// LayoutVersion is the one format there is.
const LayoutVersion = 1

type LayoutSession struct {
	Name       string         `toml:"name"`
	Connection string         `toml:"connection"` // the one it starts on
	Active     int            `toml:"active"`     // the window shown
	Windows    []LayoutWindow `toml:"window"`
}

type LayoutWindow struct {
	Name         string       `toml:"name"`
	Layout       *LayoutNode  `toml:"layout"`
	Focus        int          `toml:"focus,omitempty"` // a pane ID; 0 for the sidebar, or none: the result area is not saved
	Zoom         int          `toml:"zoom,omitempty"`
	SidebarOpen  bool         `toml:"sidebar_open"`
	SidebarWidth int          `toml:"sidebar_width,omitempty"`
	ResultHeight float64      `toml:"result_height,omitempty"` // the share the rest above the result area keeps
	Panes        []LayoutPane `toml:"pane"`
}

// LayoutNode is the split tree: a split of two, or a leaf, a pane by ID.
type LayoutNode struct {
	Split string      `toml:"split,omitempty"` // "h" side by side, "v" one over the other; "" for a leaf
	Ratio float64     `toml:"ratio,omitempty"` // the share of a
	A     *LayoutNode `toml:"a,omitempty"`
	B     *LayoutNode `toml:"b,omitempty"`
	Pane  int         `toml:"pane,omitempty"`
}

type LayoutPane struct {
	ID      int         `toml:"id"`
	Current int         `toml:"current,omitempty"` // the tab shown, from 0
	Tabs    []LayoutTab `toml:"tab"`
}

// LayoutTab is a table's, a Redis key's or a query's: Kind says which.
type LayoutTab struct {
	Kind       string `toml:"kind"` // "table" | "key" | "query"
	Connection string `toml:"connection,omitempty"`
	DB         string `toml:"db,omitempty"` // not the DSN's database (F15.1)
	Schema     string `toml:"schema,omitempty"`
	// a table's
	Table     string         `toml:"table,omitempty"`
	Where     string         `toml:"where,omitempty"`
	Order     LayoutOrder    `toml:"order,omitempty"`
	Desc      bool           `toml:"desc,omitempty"` // the default order turned; a layout of before F12.1 has the direction of its one column here
	Limit     int            `toml:"limit,omitempty"`
	Page      int            `toml:"page,omitempty"`       // from 0
	Cols      []string       `toml:"cols,omitempty"`       // the columns COLS hides
	OrderCols []string       `toml:"order_cols,omitempty"` // the columns in the order COLS puts them (F10.3)
	ColWidths map[string]int `toml:"col_widths,omitempty"` // the columns set to a width by hand (F17.14)
	// TransposeWidths is the width of the records set by hand when the table is turned, by
	// number on the page from 1 (F17.15); a key of TOML is text
	TransposeWidths map[string]int `toml:"transpose_widths,omitempty"`
	Transpose       bool           `toml:"transpose,omitempty"`
	Pinned          bool           `toml:"pinned,omitempty"` // any kind's, F22.6; the order of the tabs is the file's
	Key             string         `toml:"key,omitempty"`    // a key's name, Kind "key"; its db is DB (F16.3)
	// a query's: the name of its file; N is what layouts of before
	// M8 F8.11 say, query_<n>
	Name string `toml:"name,omitempty"`
	N    int    `toml:"n,omitempty"`
}

// LayoutOrder is a table tab's ORDER, [{col = "source", desc = true}, …]; a
// layout of before F12.1 has order = "col" and desc beside it, which reads as
// one column (LoadLayout puts the direction on it).
type LayoutOrder []db.OrderCol

// UnmarshalTOML reads the array, written as tables or inline, or the string.
func (o *LayoutOrder) UnmarshalTOML(data any) error {
	*o = nil
	switch v := data.(type) {
	case string:
		if v != "" {
			*o = LayoutOrder{{Col: v}}
		}
	case []map[string]any:
		for _, m := range v {
			c, err := orderOf(m)
			if err != nil {
				return err
			}
			*o = append(*o, c)
		}
	case []any:
		for _, e := range v {
			m, ok := e.(map[string]any)
			if !ok {
				return fmt.Errorf("order: %v is no {col, desc} table", e)
			}
			c, err := orderOf(m)
			if err != nil {
				return err
			}
			*o = append(*o, c)
		}
	default:
		return fmt.Errorf("order: %v is no string or array of tables", data)
	}
	return nil
}

// orderOf is one {col, desc} table: a layout is written by hand, so a col that
// is no name or a desc that is no boolean is an error, as limit = "x" is.
func orderOf(m map[string]any) (db.OrderCol, error) {
	col, ok := m["col"].(string)
	if !ok || col == "" {
		return db.OrderCol{}, fmt.Errorf("order: col = %v is no column name", m["col"])
	}
	desc, ok := m["desc"].(bool)
	if _, set := m["desc"]; set && !ok {
		return db.OrderCol{}, fmt.Errorf("order: desc = %v is no boolean", m["desc"])
	}
	return db.OrderCol{Col: col, Desc: desc}, nil
}

// QueryName is the name of a query tab's file: Name, or query_<N> in
// a layout of before the names (no migration: it is read, and written as a
// name).
func (t LayoutTab) QueryName() string {
	if t.Name == "" && t.N > 0 {
		return QueryName(t.N)
	}
	return t.Name
}

// LayoutsDir is where the named layouts are.
func LayoutsDir() string { return filepath.Join(Dir(), "layouts") }

// LastPath is where the working set is kept as the program ends.
func LastPath() string { return filepath.Join(StateDir(), "last.toml") }

// LayoutNames are the layouts of LayoutsDir, by name, sorted.
func LayoutNames() []string {
	files, _ := filepath.Glob(filepath.Join(LayoutsDir(), "*.toml"))
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = strings.TrimSuffix(filepath.Base(f), ".toml")
	}
	slices.Sort(names)
	return names
}

// LayoutPath is the file of the layout called name: layouts/<name>.toml.
func LayoutPath(name string) string { return filepath.Join(LayoutsDir(), name+".toml") }

// LayoutArg is the file --layout names: a path as it is (it has a / or ends
// in .toml), else a layout's by name. Only the command line takes both; the
// commands go by name (§14「命令」).
func LayoutArg(nameOrPath string) string {
	if nameOrPath == "last" { // where the program left off, as layout.open's
		return LastPath()
	}
	if strings.ContainsRune(nameOrPath, '/') || strings.HasSuffix(nameOrPath, ".toml") {
		return nameOrPath
	}
	return LayoutPath(nameOrPath)
}

// CheckLayoutName says why name cannot name a layout: it is a file of
// LayoutsDir, and last is last.toml's. The caller has trimmed it, and not
// asked for a blank one.
func CheckLayoutName(name string) error {
	if err := checkFileName(name); err != nil {
		return err
	}
	if name == "last" {
		return errors.New(i18n.T("last 是保留名，指上次退出时的布局"))
	}
	return nil
}

// checkFileName is the rule of a name that is a file's: none of / or \, no
// leading . (a connection's, a layout's, a query's).
func checkFileName(name string) error {
	if strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return errors.New(i18n.T("名字不能含 / 或 \\，也不能以 . 开头"))
	}
	return nil
}

// LoadLayout reads the layout at path. A file that is not there is
// fs.ErrNotExist in the error; one that does not read, or is of another
// version, says so with its path.
func LoadLayout(path string) (*Layout, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	l := &Layout{}
	if err := toml.Unmarshal(data, l); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if l.Version != LayoutVersion {
		return nil, fmt.Errorf(i18n.T("%s: version = %d，只认识 %d"), path, l.Version, LayoutVersion)
	}
	if len(l.Sessions) == 0 {
		return nil, fmt.Errorf(i18n.T("%s: 里面没有 [[session]]"), path)
	}
	for _, s := range l.Sessions { // the queries were consoles before F10.11: read as queries, written as such
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				for i, t := range p.Tabs {
					if len(t.Order) == 1 && t.Desc { // before F12.1 the direction was beside the column
						p.Tabs[i].Order = LayoutOrder{{Col: t.Order[0].Col, Desc: true}}
						p.Tabs[i].Desc = false
					}
					if t.Kind == "console" {
						p.Tabs[i].Kind = "query"
					}
					p.Tabs[i].Name = currentName(t.Name)
				}
			}
		}
	}
	return l, nil
}

// SaveLayout writes l to path whole, through a file renamed into place, the
// directories made for the user only.
func SaveLayout(path string, l *Layout) error {
	l.Version = LayoutVersion
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(l); err != nil {
		return err
	}
	return writeFile(path, b.Bytes())
}

// BrokenLast moves last.toml aside, as state.json's is when it cannot be
// read, so the next exit does not write over what the user had.
func BrokenLast() error {
	err := os.Rename(LastPath(), LastPath()+".broken")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
