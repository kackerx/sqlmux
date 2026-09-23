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
			if v.Field(i).String() == "" {
				t.Errorf("%s icons: %s is empty", name, v.Type().Field(i).Name)
			}
		}
	}
}
