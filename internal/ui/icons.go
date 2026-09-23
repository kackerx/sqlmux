package ui

// Icons are the glyphs used in titles and the sidebar (§7.7).
type Icons struct {
	Schema, Table string
	Data, Console string
	Filter        string
	Postgres      string
	Search, Keys  string // status bar: palette entry, pending keys
	Conn          string
}

// Nerd Font glyphs, all from the BMP private use area.
var NerdIcons = &Icons{
	Schema:   "\uf0e8", // nf-fa-sitemap
	Table:    "\uf0ce", // nf-fa-table
	Data:     "\uf1c0", // nf-fa-database
	Console:  "\uf120", // nf-fa-terminal
	Filter:   "\uf0b0", // nf-fa-filter
	Postgres: "\ue76e", // nf-dev-postgresql
	Search:   "\uf002", // nf-fa-search
	Keys:     "\uf11c", // nf-fa-keyboard_o
	Conn:     "\uf1e6", // nf-fa-plug
}

var ASCIIIcons = &Icons{
	Schema: "#", Table: "+",
	Data: "=", Console: ">",
	Filter:   "?",
	Postgres: "pg",
	Search:   "~",
	Keys:     "kb",
	Conn:     "@",
}

// IconSet maps the config value `icons = "nerd" | "ascii"`.
func IconSet(name string) *Icons {
	if name == "ascii" {
		return ASCIIIcons
	}
	return NerdIcons
}
