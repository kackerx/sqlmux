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
			if icon, ok := v.Field(i).Interface().(Icon); ok && icon.Text == "" {
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
