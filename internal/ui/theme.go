package ui

import (
	"fmt"
	"image/color"
	"maps"
	"regexp"
	"slices"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/x/ansi"
)

// Theme holds the semantic color tokens (tech-design §7.3).
type Theme struct {
	Bg, PaneBg               color.Color
	Fg, FgMuted, Dim         color.Color
	Border, Focus, Warn      color.Color
	Cursor, CursorBlur       color.Color
	Select, Row, RowAlt      color.Color // RowAlt: the grid's zebra rows (§7.6)
	Visual                   color.Color // a console's VISUAL, an input all selected
	EditedBg                 color.Color
	Keyword, Info            color.Color
	Error, ErrorBg           color.Color // ErrorBg: the error bar's (§7.8「错误栏」)
	Number, PK, Func         color.Color
	String, Time, Bool, JSON color.Color // grid values by column type (§7.6)
	SQLString, Comment       color.Color // the console's SQL (§7.3)
	Bar                      color.Color // status bar and toast background
	Sep                      color.Color // separators and tab dividers (§7.8)
	Match                    color.Color // fuzzy-matched characters' foreground, on the row's own background

	SQLTable, SQLColumn, SQLOperator color.Color // SQL's names the catalog knows, and its operators (§7.3)
}

var TokyonightStorm = &Theme{
	Bg: c("#1f2335"), PaneBg: c("#24283b"),
	Fg: c("#c0caf5"), FgMuted: c("#a9b1d6"), Dim: c("#565f89"),
	Border: c("#3b4261"), Focus: c("#9ece6a"), Warn: c("#e0af68"),
	Cursor: c("#3d59a1"), CursorBlur: c("#2f3549"),
	Select: c("#364a82"), Row: c("#292e42"), RowAlt: c("#1f2335"),
	Visual:   c("#2d3f76"),
	EditedBg: c("#2d2a24"),
	Keyword:  c("#bb9af7"), Info: c("#7dcfff"),
	Error: c("#f7768e"), ErrorBg: c("#3b2230"),
	Number: c("#ff9e64"), PK: c("#73daca"), Func: c("#7aa2f7"),
	String: c("#c0caf5"), Time: c("#c0caf5"), Bool: c("#c0caf5"), JSON: c("#c0caf5"), // same as Fg: uncolored by default
	SQLString: c("#9ece6a"), Comment: c("#565f89"),
	Bar:   c("#292e42"),
	Sep:   c("#2f3549"),
	Match: c("#ff9e64"),

	SQLTable: c("#2ac3de"), SQLColumn: c("#73daca"), SQLOperator: c("#89ddff"),
}

// Themes are the built-in themes by name.
var Themes = map[string]*Theme{"tokyonight-storm": TokyonightStorm}

func c(s string) color.Color { return ansi.XParseColor(s) }

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ParseTheme reads a theme file over tokyonight-storm and the icon set that
// `icons` chose: top-level keys are color tokens (§7.3), [icon] entries change
// a glyph, its color or both (§7.7). Anything the file doesn't name stays.
func ParseTheme(data string, icons *Icons) (*Theme, *Icons, error) {
	var raw map[string]toml.Primitive
	md, err := toml.Decode(data, &raw)
	if err != nil {
		return nil, nil, err
	}
	th, ic := *TokyonightStorm, *icons
	colors, glyphs := th.tokens(), ic.byName()
	for _, key := range slices.Sorted(maps.Keys(raw)) { // the same first error every run
		if key == "icon" {
			var specs map[string]struct {
				Text *string `toml:"text"`
				Fg   *string `toml:"fg"`
			}
			if err := md.PrimitiveDecode(raw[key], &specs); err != nil {
				return nil, nil, fmt.Errorf("[icon]: %w", err)
			}
			for _, name := range slices.Sorted(maps.Keys(specs)) {
				icon, ok := glyphs[name]
				if !ok {
					return nil, nil, fmt.Errorf("[icon]：没有叫 %s 的图标", name)
				}
				s := specs[name]
				if s.Text != nil {
					icon.Text = *s.Text
				}
				if s.Fg != nil {
					if icon.Fg, err = hex("icon."+name+".fg", *s.Fg); err != nil {
						return nil, nil, err
					}
				}
			}
			continue
		}
		field, ok := colors[key]
		if !ok {
			return nil, nil, fmt.Errorf("没有叫 %s 的 token", key)
		}
		var v string
		if err := md.PrimitiveDecode(raw[key], &v); err != nil {
			return nil, nil, fmt.Errorf("%s：颜色要写成字符串 \"#rrggbb\"", key)
		}
		if *field, err = hex(key, v); err != nil {
			return nil, nil, err
		}
	}
	if u := md.Undecoded(); len(u) > 0 { // e.g. `bg` inside an [icon] entry
		return nil, nil, fmt.Errorf("不认识 %s", u[0])
	}
	return &th, &ic, nil
}

func hex(key, v string) (color.Color, error) {
	if !hexColor.MatchString(v) {
		return nil, fmt.Errorf("%s = %q：颜色要写成 #rrggbb", key, v)
	}
	return c(v), nil
}

// tokens names the fields the way theme files and §7.3 do.
func (th *Theme) tokens() map[string]*color.Color {
	return map[string]*color.Color{
		"bg": &th.Bg, "pane_bg": &th.PaneBg,
		"fg": &th.Fg, "fg_muted": &th.FgMuted, "dim": &th.Dim,
		"border": &th.Border, "focus": &th.Focus, "warn": &th.Warn,
		"cursor": &th.Cursor, "cursor_blur": &th.CursorBlur,
		"select": &th.Select, "row": &th.Row, "row_alt": &th.RowAlt,
		"visual":    &th.Visual,
		"edited_bg": &th.EditedBg,
		"keyword":   &th.Keyword, "info": &th.Info,
		"error": &th.Error, "error_bg": &th.ErrorBg,
		"number": &th.Number, "pk": &th.PK, "func": &th.Func,
		"string": &th.String, "time": &th.Time, "bool": &th.Bool, "json": &th.JSON,
		"sql_string": &th.SQLString, "comment": &th.Comment,
		"bar":   &th.Bar,
		"sep":   &th.Sep,
		"match": &th.Match,

		"sql_table": &th.SQLTable, "sql_column": &th.SQLColumn, "sql_operator": &th.SQLOperator,
	}
}
