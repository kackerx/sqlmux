// Package config reads config.toml and keymaps.toml from the XDG config dir
// (tech-design §14).
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

type Config struct {
	Theme      *ui.Theme // `theme = "<name>"` (§7.3), resolved by Load
	Icons      *ui.Icons // `icons = "nerd" | "ascii"`, with the theme file's [icon] on top (§7.7)
	BaseIcons  *ui.Icons // what `icons` chose, a theme's [icon] not on it: a theme switched to goes on this
	ThemeName  string    // the theme in use: `theme`, or tokyonight-storm
	Language   string    // `language = "en" | "zh"` (§14「界面语言」)
	Border     uv.Border // `border = "single" | "rounded" | "double" | "bold" | "hidden"`: the boxes' lines (§7.1)
	Vim        bool      // `vim = false`: query and WHERE are plain editors, the grid and tree get arrow keys too (§6.9)
	Timeoutlen int       // ms to wait on an ambiguous key sequence
	TabWidth   int       // the query's indent, and the formatter's (§14)
	// ResultHeight is the share of the window the result area first takes;
	// MaxRows the rows a query's run keeps of each result (§11).
	ResultHeight float64
	MaxRows      int
	// Hints is whether a query shows the field its cursor's value goes in
	// (`[query] hints`, §11, F15.3).
	Hints bool
	// Tips is whether the status bar carries a line about a feature (`[ui]
	// tips`, §7.8, F15.4).
	Tips bool
	// RedisDelimiter, RedisScanKeys and RedisLoadAll are `[redis]`'s: what the
	// tree splits a key's name by, how many keys a db's scan reads, and the
	// length up to which a hash, set or zset is read whole (§8.7, F16.1).
	RedisDelimiter              string
	RedisScanKeys, RedisLoadAll int
	// HistoryMax is how many statements history.jsonl keeps, the oldest cut
	// at the start (`[query] history_max`, §11).
	HistoryMax int
	// KeywordCase and FormatPrg are gq's (§9.5): sql-formatter's keywordCase,
	// or a command to format with instead of it.
	KeywordCase string
	FormatPrg   string
	AutoPairs   bool // brackets and quotes typed get their other half (§7.9)
	// ConfirmDangerous asks before a run of a DELETE or UPDATE with no WHERE,
	// a DROP, a TRUNCATE, and a save that deletes rows (§13).
	ConfirmDangerous bool
	// GridFullWidth is the width up to which a column of the grid is never
	// squeezed (`[grid] full_width`, §7.6).
	GridFullWidth int
	// TreeWidth is the sidebar's width in columns, borders included, for a
	// window whose sidebar has not been dragged (§7.8).
	TreeWidth int
	// GridDesc is `[grid] order = "desc"` and GridTranspose `[grid]
	// transpose`: what a table opened anew starts with (§14).
	GridDesc, GridTranspose bool
	// PreviewSave is `[grid] preview_save`: a save shows the statements first
	// (§13, F17.10); a production connection's does whatever it says.
	PreviewSave bool
	theme       string // the name Parse read; Load finds the theme, as it touches the disk

	// Leader and Bindings are keymaps.toml's raw [keys] and [map.*]
	// entries; the keymap package gives them meaning.
	Leader   string
	Bindings []keymap.Line

	// ThemeUnknown are the keys of the theme file nothing reads; UnknownKeys
	// says them, when the language is known (F14.7).
	ThemeUnknown []string

	// Unknown are the keys of config.toml nothing reads, a misspelt one among
	// them, as TOML names them (`query.max_rowz`); said at the start, not an
	// error (F13.5).
	Unknown []string
}

func Default() *Config {
	return &Config{Theme: ui.TokyonightStorm, Icons: ui.NerdIcons, BaseIcons: ui.NerdIcons, ThemeName: "tokyonight-storm", Language: "en", Border: uv.NormalBorder(), Vim: true, Timeoutlen: 1000, TabWidth: 2, ResultHeight: 0.4, MaxRows: 500, HistoryMax: 1000, Hints: true, Tips: true, RedisDelimiter: ":", RedisScanKeys: 10000, RedisLoadAll: 5000, PreviewSave: true, KeywordCase: "lower", AutoPairs: true, ConfirmDangerous: true, GridFullWidth: 22, TreeWidth: 32}
}

// Dir is $XDG_CONFIG_HOME/sqlmux, falling back to ~/.config/sqlmux on every
// Unix, macOS included, and %APPDATA%\sqlmux on windows (§14).
func Dir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "sqlmux")
	}
	if runtime.GOOS == "windows" {
		if d, err := os.UserConfigDir(); err == nil {
			return filepath.Join(d, "sqlmux")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "sqlmux")
}

// Load reads config.toml, then keymaps.toml; a missing file means its
// defaults.
func Load() (*Config, error) {
	c := Default()
	path := filepath.Join(Dir(), "config.toml")
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if c, err = Parse(string(data)); err == nil && c.theme != "" {
			if c.Theme, c.Icons, c.ThemeUnknown, err = LoadTheme(c.theme, c.BaseIcons); err != nil {
				err = fmt.Errorf("theme = %q: %w", c.theme, err)
			}
			c.ThemeName = c.theme
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	path = filepath.Join(Dir(), "keymaps.toml")
	data, err = os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if c.Leader, c.Bindings, err = keymap.ParseKeys(string(data)); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return c, nil
}

// Parse reads config.toml's TOML over the defaults. It reads no other
// file: Load finds the theme it names, and the keys in keymaps.toml.
func Parse(data string) (*Config, error) {
	c := Default()
	raw := struct {
		Theme      *string  `toml:"theme"`
		Icons      *string  `toml:"icons"`
		Language   *string  `toml:"language"`
		Border     *string  `toml:"border"`
		Vim        *bool    `toml:"vim"`
		Timeoutlen *int     `toml:"timeoutlen"`
		TabWidth   *int     `toml:"tab_width"`
		Height     *float64 `toml:"result_height"`
		Case       *string  `toml:"keyword_case"`
		FormatPrg  string   `toml:"formatprg"`
		AutoPairs  *bool    `toml:"autopairs"`
		Dangerous  *bool    `toml:"confirm_dangerous"`
		Query      struct {
			MaxRows    *int  `toml:"max_rows"`
			HistoryMax *int  `toml:"history_max"`
			Hints      *bool `toml:"hints"`
		} `toml:"query"`
		UI struct {
			Tips *bool `toml:"tips"`
		} `toml:"ui"`
		Redis struct {
			Delimiter *string `toml:"delimiter"`
			ScanKeys  *int    `toml:"scan_keys"`
			LoadAll   *int    `toml:"load_all"`
		} `toml:"redis"`
		Tree struct {
			Width *int `toml:"width"`
		} `toml:"tree"`
		Grid struct {
			FullWidth *int    `toml:"full_width"`
			Order     *string `toml:"order"`
			Transpose *bool   `toml:"transpose"`
			Preview   *bool   `toml:"preview_save"`
		} `toml:"grid"`
		Keys any `toml:"keys"`
		Map  any `toml:"map"`
	}{}
	md, err := toml.Decode(data, &raw)
	if err != nil {
		return nil, err
	}
	for _, k := range md.Undecoded() {
		if s := k.String(); !slices.Contains(c.Unknown, s) { // a [[table]] twice says each key twice
			c.Unknown = append(c.Unknown, s)
		}
	}
	if raw.Keys != nil || raw.Map != nil { // F5.9 moved them
		return nil, errors.New("[keys] / [map] belong in keymaps.toml (sqlmux keys --format toml writes a complete reference)")
	}
	if raw.Icons != nil {
		if *raw.Icons != "nerd" && *raw.Icons != "ascii" {
			return nil, fmt.Errorf(`icons = %q: must be "nerd" or "ascii"`, *raw.Icons)
		}
		c.Icons = ui.IconSet(*raw.Icons)
		c.BaseIcons = c.Icons
	}
	if raw.Language != nil {
		if *raw.Language != "zh" && *raw.Language != "en" {
			return nil, fmt.Errorf(`language = %q: must be "en" or "zh"`, *raw.Language)
		}
		c.Language = *raw.Language
	}
	if raw.Border != nil {
		b, ok := ui.BorderOf(*raw.Border)
		if !ok {
			return nil, fmt.Errorf(`border = %q: must be "single", "rounded", "double", "bold" or "hidden"`, *raw.Border)
		}
		c.Border = b
	}
	if raw.Vim != nil {
		c.Vim = *raw.Vim
	}
	if raw.Theme != nil {
		c.theme = *raw.Theme
	}
	if raw.Timeoutlen != nil {
		if *raw.Timeoutlen <= 0 {
			return nil, fmt.Errorf("timeoutlen = %d: must be greater than 0", *raw.Timeoutlen)
		}
		c.Timeoutlen = *raw.Timeoutlen
	}
	if raw.TabWidth != nil {
		if *raw.TabWidth <= 0 {
			return nil, fmt.Errorf("tab_width = %d: must be greater than 0", *raw.TabWidth)
		}
		c.TabWidth = *raw.TabWidth
	}
	if h := raw.Height; h != nil {
		if *h <= 0 || *h >= 1 {
			return nil, fmt.Errorf("result_height = %v: must be between 0 and 1", *h)
		}
		c.ResultHeight = *h
	}
	if k := raw.Case; k != nil {
		if *k != "lower" && *k != "upper" && *k != "preserve" {
			return nil, fmt.Errorf(`keyword_case = %q: must be "lower", "upper" or "preserve"`, *k)
		}
		c.KeywordCase = *k
	}
	c.FormatPrg = raw.FormatPrg
	if raw.AutoPairs != nil {
		c.AutoPairs = *raw.AutoPairs
	}
	if raw.Dangerous != nil {
		c.ConfirmDangerous = *raw.Dangerous
	}
	if n := raw.Query.MaxRows; n != nil {
		if *n <= 0 {
			return nil, fmt.Errorf("[query] max_rows = %d: must be greater than 0", *n)
		}
		c.MaxRows = *n
	}
	if raw.UI.Tips != nil {
		c.Tips = *raw.UI.Tips
	}
	if d := raw.Redis.Delimiter; d != nil {
		if *d == "" {
			return nil, errors.New(`[redis] delimiter = "": must not be empty`)
		}
		c.RedisDelimiter = *d
	}
	if n := raw.Redis.ScanKeys; n != nil {
		if *n <= 0 {
			return nil, fmt.Errorf("[redis] scan_keys = %d: must be greater than 0", *n)
		}
		c.RedisScanKeys = *n
	}
	if n := raw.Redis.LoadAll; n != nil {
		if *n <= 0 {
			return nil, fmt.Errorf("[redis] load_all = %d: must be greater than 0", *n)
		}
		c.RedisLoadAll = *n
	}
	if raw.Query.Hints != nil {
		c.Hints = *raw.Query.Hints
	}
	if n := raw.Query.HistoryMax; n != nil {
		if *n <= 0 {
			return nil, fmt.Errorf("[query] history_max = %d: must be greater than 0", *n)
		}
		c.HistoryMax = *n
	}
	if n := raw.Tree.Width; n != nil { // held to 16 and half the window where it is used, whatever it is
		c.TreeWidth = *n
	}
	if n := raw.Grid.FullWidth; n != nil {
		if *n <= 0 {
			return nil, fmt.Errorf("[grid] full_width = %d: must be greater than 0", *n)
		}
		c.GridFullWidth = *n
	}
	if o := raw.Grid.Order; o != nil {
		if *o != "asc" && *o != "desc" {
			return nil, fmt.Errorf(`[grid] order = %q: must be "asc" or "desc"`, *o)
		}
		c.GridDesc = *o == "desc"
	}
	if raw.Grid.Transpose != nil {
		c.GridTranspose = *raw.Grid.Transpose
	}
	if raw.Grid.Preview != nil {
		c.PreviewSave = *raw.Grid.Preview
	}
	return c, nil
}

// ThemeNames are the themes there are: the files of themes/ and the
// built-in ones, by name.
func ThemeNames() []string {
	names := slices.Collect(maps.Keys(ui.Themes))
	files, _ := filepath.Glob(filepath.Join(Dir(), "themes", "*.toml"))
	for _, f := range files {
		names = append(names, strings.TrimSuffix(filepath.Base(f), ".toml"))
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// LoadTheme finds a theme by name (§7.3): themes/<name>.toml first, then a
// built-in theme; with the icons that `icons` chose, a theme's [icon] on
// them. unknown are the keys it has that nothing reads (F14.7).
func LoadTheme(name string, icons *ui.Icons) (th *ui.Theme, ic *ui.Icons, unknown []string, err error) {
	path := filepath.Join(Dir(), "themes", name+".toml")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if th, ok := ui.Themes[name]; ok {
			return th, icons, nil, nil
		}
		return nil, nil, nil, fmt.Errorf(i18n.T("找不到 %s，也没有这个内置主题"), path)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	th, ic, unknown, err = ui.ParseTheme(string(data), icons) // a key of a later version: the theme goes on without it (F14.7)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	return th, ic, unknown, nil
}

// UnknownThemeKeys is what a toast says of the keys of theme name that nothing
// reads, "" with none; the one sentence of config.toml's and connections.toml's.
func UnknownThemeKeys(name string, unknown []string) string {
	if len(unknown) == 0 {
		return ""
	}
	return unknownKeys("themes/"+name+".toml", unknown)
}

func unknownKeys(file string, keys []string) string {
	return i18n.T("%s: 不认识的键 %s", file, strings.Join(keys, ", "))
}
