package ui

import (
	"reflect"
	"strings"
	"testing"
)

// Every icon and color token can be set from a theme file (§7.3, §7.7).
func TestThemeFileNamesEverything(t *testing.T) {
	if n, want := len(NerdIcons.byName()), reflect.TypeOf(Icons{}).NumField()-1; n != want { // but Labeled
		t.Errorf("[icon] names %d of %d icons", n, want)
	}
	if n, want := len(TokyonightStorm.tokens()), reflect.TypeOf(Theme{}).NumField(); n != want {
		t.Errorf("theme files name %d of %d tokens", n, want)
	}
}

func TestParseTheme(t *testing.T) {
	th, ic, err := ParseTheme(`
row = "#6c6a6d"
string = "#FFD866"
[icon]
console = { text = "C", fg = "#ff0000" }
table = { fg = "#a9dc76" }
`, ASCIIIcons)
	if err != nil {
		t.Fatal(err)
	}
	if th.Row != c("#6c6a6d") || th.String != c("#ffd866") {
		t.Errorf("tokens not set: row %v string %v", th.Row, th.String)
	}
	if th.Bar != TokyonightStorm.Bar || th.Fg != TokyonightStorm.Fg {
		t.Error("tokens the file doesn't name must stay")
	}
	if ic.Console != (Icon{"C", c("#ff0000")}) || ic.Table != (Icon{"+", c("#a9dc76")}) || ic.Data != ASCIIIcons.Data {
		t.Errorf("icons: console %v table %v data %v", ic.Console, ic.Table, ic.Data)
	}
	if ASCIIIcons.Console.Text != ">" || TokyonightStorm.Row == th.Row {
		t.Error("the built-in sets must not change")
	}
}

func TestParseThemeErrors(t *testing.T) {
	for data, want := range map[string]string{
		`rwo = "#000000"`:                        "rwo",
		`row = "red"`:                            "row",
		`row = 1`:                                "row",
		"[icon]\nconsol = { text = \"C\" }":      "consol",
		"[icon]\nconsole = { fg = \"#f00\" }":    "icon.console.fg",
		"[icon]\nconsole = { bg = \"#ff0000\" }": "icon.console.bg",
	} {
		if _, _, err := ParseTheme(data, NerdIcons); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v should name %s", data, err, want)
		}
	}
}
