package ui

import (
	"embed"
	"encoding/base64"
	"fmt"
	"image/color"
	"strings"
)

// logoFiles are DBeaver's icons of the engines (logos/NOTICE), the ones its
// navigator tree draws, at 32×32 (its *_icon_big are wordmarks for a dialog): what a terminal
// that shows images draws for them, as DBeaver does (§7.7). Redis has none of
// its own there, only Pro's badge, and keeps its glyph.
//
//go:embed logos/*.png
var logoFiles embed.FS

// logoEngines are the engines with a logo, in the order their image ids go.
var logoEngines = []string{"postgres", "mysql", "sqlite", "mssql", "clickhouse", "trino", "oracle"}

// logoID is the first image id: the terminal's ids are its window's, shared
// with whatever else runs in it, so they are kept off the low ones others take.
const logoID = 0x5a4c00

// KittyGraphics is whether the terminal, as getenv tells it, draws images by
// kitty's Unicode placeholders: kitty and Ghostty, not under tmux, which
// passes the images through only when told to (§7.7).
func KittyGraphics(getenv func(string) string) bool {
	if getenv("TMUX") != "" || getenv("STY") != "" {
		return false
	}
	return getenv("TERM") == "xterm-kitty" || getenv("KITTY_WINDOW_ID") != "" ||
		getenv("TERM_PROGRAM") == "ghostty" || getenv("GHOSTTY_RESOURCES_DIR") != ""
}

// Logos is ic with each engine's icon its logo, and the escapes that put the
// images in the terminal (setup, before the program draws) and take them out
// (cleanup, as it ends). A logo is drawn by kitty's Unicode placeholders: two
// cells of U+10EEEE, a diacritic each for its row and column of the image,
// the image's id the cells' color, which the terminal swaps for the picture,
// scaled into the two cells (§7.7). The cells are text to everything else.
func Logos(ic *Icons) (out *Icons, setup, cleanup string) {
	c := *ic
	byName := c.byName()
	var s, d strings.Builder
	for i, name := range logoEngines {
		png, err := logoFiles.ReadFile("logos/" + name + ".png")
		if err != nil { // embedded: there is no such file only in a broken build
			continue
		}
		id := logoID + i
		s.WriteString(transmit(id, png))
		fmt.Fprintf(&d, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
		*byName[name] = Icon{Text: placeholder, Fg: logoColor(id)}
	}
	return &c, s.String(), d.String()
}

// placeholder is an image's two cells: row 0, columns 0 and 1, by kitty's
// table of diacritics (U+0305 is 0, U+030D is 1).
const placeholder = "\U0010EEEE̅̅\U0010EEEE̅̍"

// IsPlaceholder is whether a cell's content is one of an image's cells, whose
// color is the image's id and no color to change (Frame.Dim).
func IsPlaceholder(content string) bool { return strings.HasPrefix(content, "\U0010EEEE") }

// logoColor is image id as the 24-bit color of its cells.
func logoColor(id int) color.Color {
	return color.RGBA{R: uint8(id >> 16), G: uint8(id >> 8), B: uint8(id), A: 0xff}
}

// transmit is the escapes that send png as image id and make it a virtual
// placement of two columns and a row, which placeholder cells show: base64 in
// chunks of 4096, the first with the keys, quiet (q=2) so that the terminal
// answers nothing the program would read as keys.
func transmit(id int, png []byte) string {
	enc := base64.StdEncoding.EncodeToString(png)
	var b strings.Builder
	for i := 0; i < len(enc); i += 4096 {
		chunk, more := enc[i:min(i+4096, len(enc))], 0
		if i+4096 < len(enc) {
			more = 1
		}
		if i == 0 {
			fmt.Fprintf(&b, "\x1b_Ga=T,U=1,f=100,t=d,i=%d,c=2,r=1,q=2,m=%d;%s\x1b\\", id, more, chunk)
		} else {
			fmt.Fprintf(&b, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}
	return b.String()
}
