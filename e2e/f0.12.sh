#!/usr/bin/env bash
# F0.12 Theme files, colors by column type, [icon] overrides, console icon
# (specs/m0-skeleton/task.md F0.12; tech-design §7.3, §7.6, §7.7)
# "No theme = unchanged screen" is covered by f0.2/f0.5 passing and the golden diff (one glyph).
# F1.1 has no console and no grid: the console icon checks come back in M3, the grid colors
# (column types, row, cursor, row_alt, header) in F1.3; [icon] overrides use the data pane's icon.
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
# conf CONFIG [THEME] — a fresh config dir: config.toml, and themes/x.toml when THEME is given
conf() { rm -rf "$D"/*; printf "$1" > "$D/config.toml"; [[ -n $2 ]] && { mkdir -p "$D/themes"; printf "$2" > "$D/themes/x.toml"; }; true; }
fails() { e2e_start -C "$D" "$E2E_BIN"; wait_for 3 screen_has '[e2e-exit 1]' || return 1; screen_has "$1" || { echo "  error lacks '$1':"; e2e_plain | grep sqlmux: | sed 's/^/    /'; false; }; }
NF_DATA=$(printf '\xef\x87\x80')   # U+F1C0
ICON=39 ICON_ASCII=41             # data title icon column: nerd ┌─ ① <icon> …; ascii ┌─ ⟨1⟩ <icon> … (⟨1⟩ is 3 wide, F0.16)

# ---- a theme file with six tokens: those apply, nothing else moves; bar stays #292e42
conf 'theme = "x"\n' 'pane_bg = "#101010"\nrow = "#202020"\ncursor = "#303030"\nnumber = "#404040"\nstring = "#505050"\ntime = "#606060"\n'
start -C "$D"
check "pane_bg applies (pane body)" style_has 130 20 bg=#101010
check "other tokens unchanged: border, focus, bg gap" eval 'style_has 1 1 fg=#3b4261 && style_has 34 1 fg=#9ece6a && style_has 33 5 bg=#1f2335'
check "status bar keeps bar #292e42, not row" style_has 80 45 bg=#292e42
e2e_keys C-c; sleep 0.3
check "toast keeps bar #292e42" toast_is "再按一次 C-c 退出"

# ---- theme errors: exit 1 and name the item
conf 'theme = "x"\n' 'pane_bgg = "#101010"\n'; check "misspelt token → error names it" fails "pane_bgg"
conf 'theme = "x"\n' 'pane_bg = "#12345"\n';  check "bad color → error names it" fails 'pane_bg = "#12345"'
conf 'theme = "nope"\n';                        check "theme not found → error names it" fails 'theme = "nope"'

# ---- [icon] overrides (yazi-style)
conf 'theme = "x"\n' '[icon]\ndata = { text = "C", fg = "#ff0000" }\n'; start -C "$D"
check "[icon] data text+fg: red C, rest of the title unchanged" eval 'text_is $ICON $ICON 1 C && style_has $ICON 1 fg=#ff0000 && style_has $((ICON + 2)) 1 fg=#9ece6a && style_has 37 1 fg=#9ece6a   # ─, ①'
conf 'theme = "x"\n' '[icon]\ndata = { fg = "#00ff00" }\n'; start -C "$D"
check "[icon] fg only: same glyph, new color" eval 'text_is $ICON $ICON 1 "$NF_DATA" && style_has $ICON 1 fg=#00ff00'
conf 'theme = "x"\nicons = "ascii"\n' '[icon]\ndata = { text = "C", fg = "#ff0000" }\n'; start -C "$D"
check "[icon] still applies with icons = ascii" eval 'text_is $ICON_ASCII $ICON_ASCII 1 C && style_has $ICON_ASCII 1 fg=#ff0000'
conf 'theme = "x"\n' '[icon]\nconn = { fg = "#ff00ff" }\n'; start -C "$D"
check "[icon] conn: the status bar connection icon changes color" eval 'c=$(e2e_find sqlmux@localhost 45); style_has $((c - 2)) 45 fg=#ff00ff && style_has $c 45 fg=#7dcfff'
conf 'theme = "x"\n' '[icon]\nfoo = { text = "F" }\n';            check "[icon] unknown name → error" fails "foo"
conf 'theme = "x"\n' '[icon]\ndata = { bg = "#ff0000" }\n';       check "[icon] unknown sub-key (bg) → error" fails "bg"
conf 'theme = "x"\n' '[icon]\ndata = { fg = "red" }\n';           check "[icon] bad color → error" fails '"red"'

e2e_done
