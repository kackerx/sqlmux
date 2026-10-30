package ui

import (
	"reflect"
	"testing"
)

// A glyph added to one set must be added to the other.
func TestIconSetsComplete(t *testing.T) {
	for name, set := range map[string]*Icons{"nerd": NerdIcons, "ascii": ASCIIIcons} {
		v := reflect.ValueOf(*set)
		for i := range v.NumField() {
			if icon, ok := v.Field(i).Interface().(Icon); ok && icon.Text == "" && v.Type().Field(i).Name != "Pane" { // Pane is no longer drawn, a theme may still set it
				t.Errorf("%s icons: %s is empty", name, v.Type().Field(i).Name)
			}
		}
	}
}

// Pane numbers are circled beside Nerd icons, up to ⑳ (§7.8).
func TestNumber(t *testing.T) {
	for n, want := range map[int]string{0: "⓪", 1: "①", 20: "⑳", 21: "⟨21⟩"} {
		if got := NerdIcons.Number(n); got != want || Width(got) != Width(want) {
			t.Errorf("nerd %d: %q, want %q", n, got, want)
		}
	}
	if got := ASCIIIcons.Number(1); got != "⟨1⟩" {
		t.Errorf("ascii 1: %q", got)
	}
	if Width("①") != 1 || Width("⓪") != 1 {
		t.Error("circled numbers take one column, as tmux draws them")
	}
}

// A Redis key's type has its icon, by the name TYPE gives it, and each icon is
// one a theme file's [icon] may name (F16.2, §7.7).
func TestKeyTypeIcons(t *testing.T) {
	for typ, name := range map[string]string{"string": "str", "hash": "hash", "list": "list", "set": "set", "zset": "zset", "stream": "stream", "ReJSON-RL": "json"} {
		for _, set := range []*Icons{NerdIcons, ASCIIIcons} {
			ic := set.KeyType(typ)
			if ic.Text == "" || ic != *set.byName()[name] {
				t.Errorf("%s: %+v, want the icon named %s", typ, ic, name)
			}
		}
		_, ic, unknown, err := ParseTheme("[icon]\n"+name+" = { text = \"X\" }\n", NerdIcons)
		if err != nil || len(unknown) != 0 || ic.KeyType(typ).Text != "X" {
			t.Errorf("[icon] %s: %+v %v %v", name, ic.KeyType(typ), unknown, err)
		}
	}
	if NerdIcons.KeyType("somethingnew") != NerdIcons.Key {
		t.Error("a type that is not known has the key icon")
	}
	seen := map[string]string{}
	for _, typ := range []string{"string", "hash", "list", "set", "zset", "stream", "ReJSON-RL"} {
		if other, ok := seen[NerdIcons.KeyType(typ).Text]; ok {
			t.Errorf("%s and %s share an icon", typ, other)
		}
		seen[NerdIcons.KeyType(typ).Text] = typ
	}
}

// A Nerd glyph is one cell wide, those of the supplementary private use area
// too (F16.11): the completion box and the menus count their width by it.
func TestIconWidth(t *testing.T) {
	v := reflect.ValueOf(*NerdIcons)
	for i := range v.NumField() {
		if icon, ok := v.Field(i).Interface().(Icon); ok && icon.Text != "" && Width(icon.Text) != 1 {
			t.Errorf("%s is %d cells", v.Type().Field(i).Name, Width(icon.Text))
		}
	}
}

// The keyword and value glyphs are those of the user's blink.cmp kind_icons
// (§7.7, F16.11).
func TestCandidateGlyphs(t *testing.T) {
	if NerdIcons.Keyword.Text != "\U000f030b" || NerdIcons.Value.Text != "\U000f03a0" || NerdIcons.Snippet.Text != "" {
		t.Errorf("keyword %+q value %+q snippet %+q", NerdIcons.Keyword.Text, NerdIcons.Value.Text, NerdIcons.Snippet.Text)
	}
}

// The three dots go round in four frames, each as wide as the others, in
// ascii too (§7.8「转圈」).
func TestSpinnerFrames(t *testing.T) {
	for _, tc := range []struct {
		ic   *Icons
		want []string
	}{
		{NerdIcons, []string{"●••", "•●•", "••●", "•●•"}},
		{ASCIIIcons, []string{"o..", ".o.", "..o", ".o."}},
	} {
		for n, want := range tc.want {
			if got := tc.ic.Spinner(n); got != want || tc.ic.Spinner(n+4) != want {
				t.Errorf("frame %d: %q, want %q", n, got, want)
			}
			if Width(want) != 3 {
				t.Errorf("frame %q is %d wide", want, Width(want))
			}
		}
	}
}
