package ui

import (
	"fmt"
	"image/color"
	"maps"
	"regexp"
	"slices"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/sqlmux/internal/i18n"
)

// Theme holds the semantic color tokens (tech-design §7.3).
type Theme struct {
	Bg, PaneBg               color.Color
	Fg, FgMuted, Dim         color.Color
	Border, Focus, Warn      color.Color
	Cursor, CursorBlur       color.Color
	Select, Row, RowAlt      color.Color // RowAlt: the grid's zebra rows (§7.6)
	Visual                   color.Color // a query's VISUAL, an input all selected, the range being selected on a table
	Selected                 color.Color // a table's records selected for good (F12.12); a theme that has none gets its visual
	EditedBg                 color.Color
	Keyword, Info            color.Color
	Error, ErrorBg           color.Color // ErrorBg: the error bar's (§7.8「错误栏」)
	Number, PK, Func         color.Color
	String, Time, Bool, JSON color.Color // grid values by column type (§7.6)
	SQLString, Comment       color.Color // the query's SQL (§7.3)
	Bar                      color.Color // status bar and toast background
	Sep                      color.Color // separators and tab dividers (§7.8)
	Match                    color.Color // fuzzy-matched characters' foreground, on the row's own background

	SQLTable, SQLColumn, SQLOperator color.Color // SQL's names the catalog knows, and its operators (§7.3)
	Yank                             color.Color // a yank's flash, bg text on it (§7.3)
	Search, SearchCurrent            color.Color // the matches of an editor's search, and the one the cursor is in, bg text on them (F15.9); a theme that has none gets its visual and its yank
	Zoom                             color.Color // the border of the pane zoomed (F9.6); a theme that has none gets its warn
	BorderFocus                      color.Color // the border, and the title on it, of the focused pane and the overlays (F20.6); a theme that has none gets its focus
	Gap                              color.Color // the blank columns between the panes (F20.6); a theme that has none gets its pane_bg
	Production                       color.Color // a production connection's border, status block and tab titles (F17.9); a theme that has none gets its error
}

var TokyonightStorm = &Theme{
	Bg: c("#1f2335"), PaneBg: c("#24283b"),
	Fg: c("#c0caf5"), FgMuted: c("#a9b1d6"), Dim: c("#565f89"),
	Border: c("#565f89"), Focus: c("#9ece6a"), Warn: c("#e0af68"),
	Cursor: c("#3d59a1"), CursorBlur: c("#2f3549"),
	Select: c("#364a82"), Row: c("#292e42"), RowAlt: c("#1f2335"),
	Visual:   c("#2d3f76"),
	Selected: c("#5c4a1e"),
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
	Yank:   c("#e0af68"),
	Search: c("#c8793a"), SearchCurrent: c("#e0af68"),
	Zoom:        c("#e0af68"),
	BorderFocus: c("#9ece6a"), Gap: c("#24283b"),

	Production: c("#f7768e"),
}

// Themes are the built-in themes by name.
var Themes = map[string]*Theme{"tokyonight-storm": TokyonightStorm}

func c(s string) color.Color { return ansi.XParseColor(s) }

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ParseTheme reads a theme file over tokyonight-storm and the icon set that
// `icons` chose: top-level keys are color tokens (§7.3), [icon] entries change
// a glyph, its color or both (§7.7). Anything the file doesn't name stays.
// A key it doesn't know, a token or an icon of a later version, is left out and
// told in unknown, not an error (F14.7); a color that is no #rrggbb is.
func ParseTheme(data string, icons *Icons) (th *Theme, ic *Icons, unknown []string, err error) {
	var raw map[string]toml.Primitive
	md, err := toml.Decode(data, &raw)
	if err != nil {
		return nil, nil, nil, err
	}
	t, i := *TokyonightStorm, *icons
	th, ic = &t, &i
	colors, glyphs := th.tokens(), ic.byName()
	for _, key := range slices.Sorted(maps.Keys(raw)) { // the same first error every run
		if key == "icon" {
			var specs map[string]toml.Primitive // not decoded till the name is one: a later version's entry may be of another shape
			if err := md.PrimitiveDecode(raw[key], &specs); err != nil {
				return nil, nil, nil, fmt.Errorf("[icon]: %w", err)
			}
			for _, name := range slices.Sorted(maps.Keys(specs)) {
				icon, ok := glyphs[name]
				if !ok {
					unknown = append(unknown, "icon."+name)
					continue
				}
				var s struct {
					Text *string `toml:"text"`
					Fg   *string `toml:"fg"`
				}
				if err := md.PrimitiveDecode(specs[name], &s); err != nil {
					return nil, nil, nil, fmt.Errorf("[icon] %s: %w", name, err)
				}
				if s.Text != nil {
					icon.Text = *s.Text
				}
				if s.Fg != nil {
					if icon.Fg, err = hex("icon."+name+".fg", *s.Fg); err != nil {
						return nil, nil, nil, err
					}
				}
			}
			continue
		}
		field, ok := colors[key]
		if !ok {
			unknown = append(unknown, key)
			continue
		}
		var v string
		if err := md.PrimitiveDecode(raw[key], &v); err != nil {
			return nil, nil, nil, fmt.Errorf(i18n.T("%s：颜色要写成字符串 \"#rrggbb\""), key)
		}
		if *field, err = hex(key, v); err != nil {
			return nil, nil, nil, err
		}
	}
	if _, ok := raw["selected"]; !ok {
		th.Selected = th.Visual // an older theme file: the selection as it was drawn
	}
	if _, ok := raw["search"]; !ok {
		th.Search = th.Visual
	}
	if _, ok := raw["search_current"]; !ok {
		th.SearchCurrent = th.Yank
	}
	if _, ok := raw["zoom"]; !ok {
		th.Zoom = th.Warn // as the theme has it, not as tokyonight-storm's
	}
	if _, ok := raw["border_focus"]; !ok {
		th.BorderFocus = th.Focus
	}
	if _, ok := raw["gap"]; !ok {
		th.Gap = th.PaneBg
	}
	if _, ok := raw["production"]; !ok {
		th.Production = th.Error
	}
	for _, k := range md.Undecoded() { // e.g. `bg` inside an [icon] entry
		told := slices.Contains(unknown, k[0]) || len(k) > 2 && k[0] == "icon" && slices.Contains(unknown, "icon."+k[1])
		if !told { // not `section.key` of a section, nor `icon.name.key` of an icon, that is told already
			unknown = append(unknown, k.String())
		}
	}
	slices.Sort(unknown)
	return th, ic, unknown, nil
}

func hex(key, v string) (color.Color, error) {
	if !hexColor.MatchString(v) {
		return nil, fmt.Errorf(i18n.T("%s = %q：颜色要写成 #rrggbb"), key, v)
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
		"selected":  &th.Selected,
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
		"yank": &th.Yank, "zoom": &th.Zoom, "production": &th.Production,
		"border_focus": &th.BorderFocus, "gap": &th.Gap,
		"search": &th.Search, "search_current": &th.SearchCurrent,
	}
}
