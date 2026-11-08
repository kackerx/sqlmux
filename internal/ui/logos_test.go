package ui

import (
	"bytes"
	"encoding/base64"
	"regexp"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// Images in kitty and Ghostty, not under tmux or screen, nor elsewhere (§7.7).
func TestKittyGraphics(t *testing.T) {
	for env, want := range map[string]bool{
		"TERM=xterm-kitty":                      true,
		"KITTY_WINDOW_ID=1 TERM=xterm-256color": true,
		"TERM_PROGRAM=ghostty":                  true,
		"TERM=xterm-kitty TMUX=/tmp/t,1,0":      false,
		"TERM=xterm-kitty STY=1.pts":            false,
		"TERM=xterm-256color":                   false,
		"TERM_PROGRAM=WezTerm":                  false,
	} {
		vars := map[string]string{}
		for _, kv := range strings.Fields(env) {
			k, v, _ := strings.Cut(kv, "=")
			vars[k] = v
		}
		if got := KittyGraphics(func(k string) string { return vars[k] }); got != want {
			t.Errorf("%s: %v", env, got)
		}
	}
}

// Each engine with a logo gets its image's two cells, in its id's color; Redis
// and the rest keep their glyphs. setup sends each PNG whole, in chunks a
// terminal takes, and cleanup takes each one back.
func TestLogos(t *testing.T) {
	ic, setup, cleanup := Logos(NerdIcons)
	for i, name := range logoEngines {
		icon := *ic.byName()[name]
		if icon.Text != placeholder || icon.Fg != logoColor(logoID+i) || Width(icon.Text) != 2 {
			t.Errorf("%s: %+v", name, icon)
		}
	}
	if ic.Redis != NerdIcons.Redis || ic.Table != NerdIcons.Table || NerdIcons.Postgres.Text == placeholder {
		t.Error("an icon with no logo changed, or the glyphs themselves did")
	}
	apc := regexp.MustCompile("\x1b_G([^;\x1b]*)(?:;([^\x1b]*))?\x1b\\\\")
	images := map[string][]byte{}
	cur := ""
	for _, m := range apc.FindAllStringSubmatch(setup, -1) {
		keys := m[1]
		if strings.HasPrefix(keys, "a=T,") {
			if !strings.Contains(keys, "U=1,f=100,") || !strings.Contains(keys, ",c=2,r=1,q=2,") {
				t.Errorf("keys %q", keys)
			}
			cur = regexp.MustCompile(`i=(\d+)`).FindStringSubmatch(keys)[1]
		}
		if len(m[2]) > 4096 {
			t.Errorf("a chunk of %d", len(m[2]))
		}
		b, err := base64.StdEncoding.DecodeString(m[2])
		if err != nil {
			t.Fatal(err)
		}
		images[cur] = append(images[cur], b...)
	}
	if len(images) != len(logoEngines) {
		t.Fatalf("%d images sent", len(images))
	}
	png, _ := logoFiles.ReadFile("logos/postgres.png")
	if !bytes.Equal(images["5917696"], png) { // 0x5a4c00, the first: postgres
		t.Error("postgres.png did not arrive whole")
	}
	if strings.Count(cleanup, "a=d,d=I,") != len(logoEngines) {
		t.Errorf("cleanup %q", cleanup)
	}
}

// An overlay's dimming leaves an image's cells as they are: their color is
// the image (§7.5).
func TestDimKeepsImages(t *testing.T) {
	ic, _, _ := Logos(NerdIcons)
	f := NewFrame(4, 1, TokyonightStorm)
	f.Text(0, 0, 4, ic.Oracle.Text+"x", ic.Oracle.On(uv.Style{Bg: TokyonightStorm.PaneBg}))
	f.Dim()
	if fg := f.Buf.CellAt(1, 0).Style.Fg; fg != ic.Oracle.Fg {
		t.Errorf("the image's cell: %v", fg)
	}
	if fg := f.Buf.CellAt(2, 0).Style.Fg; fg == ic.Oracle.Fg {
		t.Error("the text after it was not dimmed")
	}
}
