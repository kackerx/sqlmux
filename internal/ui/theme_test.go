package ui

import (
	"reflect"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// Every icon and color token can be set from a theme file (§7.3, §7.7).
func TestThemeFileNamesEverything(t *testing.T) {
	icons, want := reflect.TypeOf(Icons{}), 0
	for i := range icons.NumField() {
		if icons.Field(i).Type == reflect.TypeOf(Icon{}) {
			want++
		}
	}
	if n := len(NerdIcons.byName()); n != want {
		t.Errorf("[icon] names %d of %d icons", n, want)
	}
	if n, want := len(TokyonightStorm.tokens()), reflect.TypeOf(Theme{}).NumField(); n != want {
		t.Errorf("theme files name %d of %d tokens", n, want)
	}
}

func TestParseTheme(t *testing.T) {
	th, ic, _, err := ParseTheme(`
row = "#6c6a6d"
string = "#FFD866"
match = "#00ff00"
visual = "#6c6a6d"
sql_column = "#010203"
[icon]
query = { text = "C", fg = "#ff0000" }
table = { fg = "#a9dc76" }
sort_asc = { fg = "#ff0000" }
`, ASCIIIcons)
	if err != nil {
		t.Fatal(err)
	}
	if th.Row != c("#6c6a6d") || th.String != c("#ffd866") || th.Match != c("#00ff00") || th.Visual != c("#6c6a6d") || th.SQLColumn != c("#010203") {
		t.Errorf("tokens not set: row %v string %v", th.Row, th.String)
	}
	if th.Bar != TokyonightStorm.Bar || th.Fg != TokyonightStorm.Fg {
		t.Error("tokens the file doesn't name must stay")
	}
	if ic.Query != (Icon{"C", c("#ff0000")}) || ic.Table != (Icon{"+", c("#a9dc76")}) || ic.Schema != ASCIIIcons.Schema || ic.SortAsc != (Icon{"↑", c("#ff0000")}) {
		t.Errorf("icons: query %v table %v schema %v", ic.Query, ic.Table, ic.Schema)
	}
	if ASCIIIcons.Query.Text != ">" || TokyonightStorm.Row == th.Row {
		t.Error("the built-in sets must not change")
	}
}

// The zoomed pane's border color is the theme's zoom, which a theme without one
// takes from its own warn, not from tokyonight's (F9.6).
func TestThemeZoomDefaultsToWarn(t *testing.T) {
	for _, tc := range []struct{ file, want string }{
		{"", "#e0af68"}, // tokyonight's own
		{`warn = "#112233"`, "#112233"},
		{`warn = "#112233"` + "\n" + `zoom = "#445566"`, "#445566"},
	} {
		th, _, _, err := ParseTheme(tc.file, ASCIIIcons)
		if err != nil || th.Zoom != c(tc.want) {
			t.Errorf("%q: zoom %v, want %s (%v)", tc.file, th.Zoom, tc.want, err)
		}
	}
}

// The gaps between the panes are the theme's gap, which a theme without one takes
// from its own pane_bg, and the focused border and the overlays' the theme's
// border_focus, from its own focus: not tokyonight's (F20.6).
func TestThemeGapAndBorderFocusDefaults(t *testing.T) {
	for _, tc := range []struct{ file, gap, borderFocus string }{
		{"", "#24283b", "#9ece6a"}, // tokyonight's own
		{`pane_bg = "#112233"` + "\n" + `focus = "#445566"`, "#112233", "#445566"},
		{`pane_bg = "#112233"` + "\n" + `gap = "#778899"` + "\n" + `focus = "#445566"` + "\n" + `border_focus = "#aabbcc"`, "#778899", "#aabbcc"},
	} {
		th, _, unknown, err := ParseTheme(tc.file, ASCIIIcons)
		if err != nil || len(unknown) != 0 || th.Gap != c(tc.gap) || th.BorderFocus != c(tc.borderFocus) {
			t.Errorf("%q: gap %v, border_focus %v, want %s, %s (%v %v)", tc.file, th.Gap, th.BorderFocus, tc.gap, tc.borderFocus, unknown, err)
		}
	}
}

// The search colors of an editor are the theme's own, and a theme file that
// has none gets its visual and its yank, as it has had its selection (F15.9).
func TestThemeSearchFallsBack(t *testing.T) {
	th, _, _, err := ParseTheme(`visual = "#111111"`+"\n"+`yank = "#222222"`, ASCIIIcons)
	if err != nil || th.Search != c("#111111") || th.SearchCurrent != c("#222222") {
		t.Errorf("none written: %v %v (%v)", th.Search, th.SearchCurrent, err)
	}
	th, _, unknown, err := ParseTheme(`search = "#333333"`+"\n"+`search_current = "#444444"`, ASCIIIcons)
	if err != nil || th.Search != c("#333333") || th.SearchCurrent != c("#444444") || len(unknown) != 0 {
		t.Errorf("written: %v %v, unknown %v (%v)", th.Search, th.SearchCurrent, unknown, err)
	}
	if TokyonightStorm.Search == TokyonightStorm.Visual || TokyonightStorm.SearchCurrent != TokyonightStorm.Yank {
		t.Error("the built-in theme has its own search colors")
	}
}

// A color that is no #rrggbb is an error, naming the key.
func TestParseThemeErrors(t *testing.T) {
	for data, want := range map[string]string{
		`row = "red"`:                       "row",
		`row = 1`:                           "row",
		"[icon]\nquery = { fg = \"#f00\" }": "icon.query.fg",
	} {
		if _, _, _, err := ParseTheme(data, NerdIcons); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v should name %s", data, err, want)
		}
	}
}

// An unknown icon is told whatever its value is.
func TestParseThemeUnknownIconOfAnotherShape(t *testing.T) {
	_, _, unknown, err := ParseTheme("[icon]\nnosuchicon = \"x\"\n", NerdIcons)
	if err != nil || strings.Join(unknown, " ") != "icon.nosuchicon" {
		t.Errorf("unknown %q %v", unknown, err)
	}
	if _, _, _, err := ParseTheme("[icon]\nquery = \"x\"\n", NerdIcons); err == nil {
		t.Error("a known icon of the wrong shape is an error")
	}
}

// A section it does not know is told once, not with each key in it.
func TestParseThemeUnknownSection(t *testing.T) {
	_, _, unknown, err := ParseTheme("rwo = \"#000000\"\n[later]\nfoo = 1\n[later.deeper]\nbar = 2\n", NerdIcons)
	if err != nil || strings.Join(unknown, " ") != "later rwo" {
		t.Errorf("unknown %q %v", unknown, err)
	}
}

// A key it does not know, a token or an icon or a field of one that a later
// version has, loads and is told, sorted; the rest of the file is read (F14.7).
func TestParseThemeUnknown(t *testing.T) {
	th, ic, unknown, err := ParseTheme(`
selectd = "#ffffff"
row = "#6c6a6d"
rwo = "#000000"
[icon]
consol = { text = "C" }
query = { text = "Q", bg = "#ff0000" }
`, NerdIcons)
	if err != nil {
		t.Fatal(err)
	}
	if want := "icon.consol icon.query.bg rwo selectd"; strings.Join(unknown, " ") != want {
		t.Errorf("unknown %q, want %q", unknown, want)
	}
	if th.Row != c("#6c6a6d") || ic.Query.Text != "Q" {
		t.Errorf("the rest is read: row %v, query %v", th.Row, ic.Query)
	}
}

// A match is drawn in bg on its color; a theme with none (its visual in the
// place) keeps the text's own color, which bg would hide (F15.9).
func TestThemeHit(t *testing.T) {
	st := uv.Style{Fg: c("#aaaaaa"), Bg: c("#bbbbbb")}
	th := *TokyonightStorm
	if got := th.hit(false, st); got.Bg != th.Search || got.Fg != th.Bg {
		t.Errorf("a match: %v on %v", got.Fg, got.Bg)
	}
	if got := th.hit(true, st); got.Bg != th.SearchCurrent || got.Fg != th.Bg {
		t.Errorf("the current match: %v on %v", got.Fg, got.Bg)
	}
	th.Search = th.Visual
	if got := th.hit(false, st); got.Bg != th.Visual || got.Fg != st.Fg {
		t.Errorf("a theme with no search color: %v on %v", got.Fg, got.Bg)
	}
}

// The production color is the theme's error when it names none, its own when it does (F17.9).
func TestProductionColor(t *testing.T) {
	th, _, unknown, err := ParseTheme("error = \"#112233\"\n", NerdIcons)
	if err != nil || len(unknown) != 0 || th.Production != th.Error || th.Production == TokyonightStorm.Production {
		t.Errorf("no production: %v %v %v", th.Production, unknown, err)
	}
	th, _, _, err = ParseTheme("error = \"#112233\"\nproduction = \"#445566\"\n", NerdIcons)
	if err != nil || th.Production != c("#445566") {
		t.Errorf("production: %v %v", th.Production, err)
	}
}
