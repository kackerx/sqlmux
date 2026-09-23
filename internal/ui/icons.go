package ui

// Icons are the glyphs used in titles, trees and the status bar (§7.7).
type Icons struct {
	Postgres, MySQL string
	Schema, Table   string
	Data, Console   string
	Result, Key     string
	Filter          string
}

// Nerd Font glyphs, all from the BMP private use area.
var NerdIcons = &Icons{
	Postgres: "", MySQL: "",
	Schema: "", Table: "",
	Data: "", Console: "",
	Result: "", Key: "",
}

var ASCIIIcons = &Icons{
	Postgres: "pg", MySQL: "my",
	Schema: "#", Table: "+",
	Data: "=", Console: ">",
	Result: "~", Key: "*",
	Filter: "?",
}

// IconSet maps the config value `icons = "nerd" | "ascii"`.
func IconSet(name string) *Icons {
	if name == "ascii" {
		return ASCIIIcons
	}
	return NerdIcons
}
