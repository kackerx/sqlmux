package ui

// Icons are the glyphs used in titles and the sidebar (§7.7).
type Icons struct {
	Schema, Table string
	Data, Console string
	Filter        string
}

// Nerd Font glyphs, all from the BMP private use area.
var NerdIcons = &Icons{
	Schema: "", Table: "", // nf-fa-sitemap, nf-fa-table
	Data: "", Console: "", // nf-fa-database, nf-fa-terminal
	Filter: "", // nf-fa-filter
}

var ASCIIIcons = &Icons{
	Schema: "#", Table: "+",
	Data: "=", Console: ">",
	Filter: "?",
}

// IconSet maps the config value `icons = "nerd" | "ascii"`.
func IconSet(name string) *Icons {
	if name == "ascii" {
		return ASCIIIcons
	}
	return NerdIcons
}
