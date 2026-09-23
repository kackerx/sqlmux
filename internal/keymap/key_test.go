package keymap

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestParse(t *testing.T) {
	for in, want := range map[string][]Key{
		"<C-p>":     {"<C-p>"},
		"<c-P>":     {"<C-p>"},
		"<Space>s":  {"<Space>", "s"},
		"<S-Tab>":   {"<S-Tab>"},
		"<s-tab>":   {"<S-Tab>"},
		"<Enter>":   {"<CR>"},
		"<esc>":     {"<Esc>"},
		"gT":        {"g", "T"},
		"<C-w>v":    {"<C-w>", "v"},
		"<Leader>%": {"<Leader>", "%"},
		"<<":        {"<lt>", "<lt>"},
		"<lt>":      {"<lt>"},
		"<=":        {"<lt>", "="},
		"<M-S-a>":   {"<M-A>"},
		"<A-x>":     {"<M-x>"},
		"<S-a>":     {"A"},
		"<bar>":     {"|"},
		"中":         {"中"},
		" ":         {"<Space>"},
		"<Space>":   {"<Space>"},
	} {
		got, err := Parse(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("Parse(%q) = %q, %v; want %q", in, got, err, want)
		}
		if again, _ := Parse(String(got)); !reflect.DeepEqual(again, got) {
			t.Errorf("%q does not round-trip: %q", in, again)
		}
	}
	for _, bad := range []string{"<Nope>", "<X-p>", "<C-Leader>"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q): expected an error", bad)
		}
	}
}

func TestDisplay(t *testing.T) {
	for in, want := range map[string]string{
		"<Space>s": "SPC s", "gt": "gt", "<C-p>": "C-p", "<CR>": "↵", "<Esc>": "esc",
		"<C-w>v": "C-w v", "<Space>%": "SPC %", "<S-Tab>": "S-Tab", "yy": "yy", "<lt>": "<",
	} {
		ks, _ := Parse(in)
		if got := Display(ks); got != want {
			t.Errorf("Display(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFromTea(t *testing.T) {
	for _, c := range []struct {
		k    tea.Key
		want Key
	}{
		{tea.Key{Code: 'a', Text: "a"}, "a"},
		{tea.Key{Code: 'a', Text: "A", Mod: tea.ModShift}, "A"},
		{tea.Key{Code: 'p', Mod: tea.ModCtrl}, "<C-p>"},
		{tea.Key{Code: 'h', Mod: tea.ModCtrl}, "<C-h>"}, // legacy 0x08
		{tea.Key{Code: tea.KeySpace, Text: " "}, "<Space>"},
		{tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl}, "<C-Space>"},
		{tea.Key{Code: tea.KeyEnter}, "<CR>"},
		{tea.Key{Code: tea.KeyEscape}, "<Esc>"},
		{tea.Key{Code: tea.KeyTab, Mod: tea.ModShift}, "<S-Tab>"},
		{tea.Key{Code: '%', Text: "%"}, "%"},
		{tea.Key{Code: '<', Text: "<"}, "<lt>"},
		{tea.Key{Code: 'x', Mod: tea.ModAlt}, "<M-x>"},
		{tea.Key{Code: tea.KeyUp}, "<Up>"},
		{tea.Key{Code: 'a', Mod: tea.ModCtrl | tea.ModShift}, "<C-S-a>"}, // kitty only, but must match what Parse gives
		{tea.Key{Code: 'a', Mod: tea.ModAlt | tea.ModShift}, "<M-A>"},
	} {
		if got := FromTea(c.k); got != c.want {
			t.Errorf("FromTea(%v) = %q, want %q", c.k, got, c.want)
		}
	}
}
