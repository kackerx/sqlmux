package ui

import (
	"fmt"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
)

// Icon is a glyph and, when a theme file gives one, the color it keeps
// wherever it is drawn (§7.7). A nil Fg follows the text around it.
type Icon struct {
	Text string
	Fg   color.Color
}

// On is st with the icon's own color, if it has one.
func (i Icon) On(st uv.Style) uv.Style {
	if i.Fg != nil {
		st.Fg = i.Fg
	}
	return st
}

// Icons are the glyphs used in titles and the sidebar (§7.7).
type Icons struct {
	Schema, Table Icon
	Data, Console Icon
	Filter        Icon
	Postgres      Icon
	Search, Keys  Icon // status bar: palette entry, pending keys
	Conn          Icon
	Key           Icon // primary key columns
	Command       Icon // palette rows (K-03)
	Window        Icon
	// Labeled sets (ascii) say nothing by themselves, so the words beside
	// the icons stay: pane types in titles, C-p, the filter's / (§7.7).
	Labeled bool
}

// Nerd Font glyphs, all from the BMP private use area.
var NerdIcons = &Icons{
	Schema:   Icon{Text: "\uf0e8"}, // nf-fa-sitemap
	Table:    Icon{Text: "\uf0ce"}, // nf-fa-table
	Data:     Icon{Text: "\uf1c0"}, // nf-fa-database
	Console:  Icon{Text: "\uf489"}, // nf-oct-terminal
	Filter:   Icon{Text: "\uf0b0"}, // nf-fa-filter
	Postgres: Icon{Text: "\ue76e"}, // nf-dev-postgresql
	Search:   Icon{Text: "\uf002"}, // nf-fa-search
	Keys:     Icon{Text: "\uf11c"}, // nf-fa-keyboard_o
	Conn:     Icon{Text: "\uf1e6"}, // nf-fa-plug
	Key:      Icon{Text: "\uf084"}, // nf-fa-key
	Command:  Icon{Text: "\uf0e7"}, // nf-fa-bolt
	Window:   Icon{Text: "\uf2d2"}, // nf-fa-window_restore
}

var ASCIIIcons = &Icons{
	Schema: Icon{Text: "#"}, Table: Icon{Text: "+"},
	Data: Icon{Text: "="}, Console: Icon{Text: ">"},
	Filter:   Icon{Text: "?"},
	Postgres: Icon{Text: "pg"},
	Search:   Icon{Text: "~"},
	Keys:     Icon{Text: "kb"},
	Conn:     Icon{Text: "@"},
	Key:      Icon{Text: "*"},
	Command:  Icon{Text: ":"},
	Window:   Icon{Text: "[]"},
	Labeled:  true,
}

// IconSet maps the config value `icons = "nerd" | "ascii"`.
func IconSet(name string) *Icons {
	if name == "ascii" {
		return ASCIIIcons
	}
	return NerdIcons
}

// Number writes pane n's ⟨n⟩ (§7.8): beside Nerd icons ⓪ ① … ⑳, one
// column each; with ascii icons, or past 20, ⟨n⟩.
func (ic *Icons) Number(n int) string {
	switch {
	case ic.Labeled || n < 0 || n > 20:
		return fmt.Sprintf("⟨%d⟩", n)
	case n == 0:
		return "\u24ea" // ⓪
	}
	return string(rune(0x2460 + n - 1)) // ① is U+2460
}

// byName names the icons the way a theme file's [icon] table does (§7.7).
func (ic *Icons) byName() map[string]*Icon {
	return map[string]*Icon{
		"schema": &ic.Schema, "table": &ic.Table, "data": &ic.Data, "console": &ic.Console,
		"filter": &ic.Filter, "search": &ic.Search, "keys": &ic.Keys, "conn": &ic.Conn,
		"key": &ic.Key, "postgres": &ic.Postgres, "command": &ic.Command, "window": &ic.Window,
	}
}
