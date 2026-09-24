#!/usr/bin/env bash
# F0.12 Theme files, colors by column type, [icon] overrides, console icon
# (specs/m0-skeleton/task.md F0.12; tech-design §7.3, §7.6, §7.7)
# "No theme = unchanged screen" is covered by f0.2/f0.5/f0.9 passing and the golden diff (one glyph).
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
# conf CONFIG [THEME] — a fresh config dir: config.toml, and themes/x.toml when THEME is given
conf() { rm -rf "$D"/*; printf "$1" > "$D/config.toml"; [[ -n $2 ]] && { mkdir -p "$D/themes"; printf "$2" > "$D/themes/x.toml"; }; true; }
start() { e2e_start -C "$D" "$@" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3; }
fails() { e2e_start -C "$D" "$E2E_BIN"; wait_for 3 screen_has '[e2e-exit 1]' || return 1; screen_has "$1" || { echo "  error lacks '$1':"; e2e_plain | grep sqlmux: | sed 's/^/    /'; false; }; }
NF_CONSOLE=$(printf '\xef\x92\x89')   # U+F489
ICON=110                              # console title: ┌─ ② <icon> console_1 … (column of the icon; F0.16)

# ---- defaults: console icon, column colors
conf ''; start
check "nerd: console icon is U+F489" text_is $ICON $ICON 1 "$NF_CONSOLE"
check "data columns: id number, biz_type string (= fg), created_at time (= fg)" eval 'style_has 42 5 fg=#ff9e64 && style_has 48 5 fg=#c0caf5 && style_has 69 5 fg=#c0caf5'
conf 'icons = "ascii"\n'; start
check "ascii: console icon is still >" text_is $ICON $ICON 1 ">"

# ---- a theme file with six tokens: those apply, nothing else moves; bar stays #292e42
conf 'theme = "x"\n' 'pane_bg = "#101010"\nrow = "#202020"\ncursor = "#303030"\nnumber = "#404040"\nstring = "#505050"\ntime = "#606060"\n'
start
check "pane_bg applies (console body)" style_has 130 20 bg=#101010
check "row applies (current row)" style_has 60 5 bg=#202020
check "cursor applies (current cell)" style_has 40 5 bg=#303030
check "number / string / time apply to id / biz_type / created_at" eval 'style_has 42 5 fg=#404040 && style_has 48 5 fg=#505050 && style_has 69 5 fg=#606060'
check "other tokens unchanged: border, focus, bg gap, row_alt, header" eval 'style_has 1 1 fg=#3b4261 && style_has 34 1 fg=#9ece6a && style_has 33 5 bg=#1f2335 && style_has 60 6 bg=#1f2335 && style_has 43 3 fg=#7aa2f7'
check "status bar keeps bar #292e42, not row" style_has 80 45 bg=#292e42
e2e_keys C-c; sleep 0.3
check "toast keeps bar #292e42" toast_is "再按一次 C-c 退出"

# ---- theme errors: exit 1 and name the item
conf 'theme = "x"\n' 'pane_bgg = "#101010"\n'; check "misspelt token → error names it" fails "pane_bgg"
conf 'theme = "x"\n' 'pane_bg = "#12345"\n';  check "bad color → error names it" fails 'pane_bg = "#12345"'
conf 'theme = "nope"\n';                        check "theme not found → error names it" fails 'theme = "nope"'

# ---- [icon] overrides (yazi-style)
conf 'theme = "x"\n' '[icon]\nconsole = { text = "C", fg = "#ff0000" }\n'; start
check "[icon] console text+fg: red C, rest of the title unchanged" eval 'text_is $ICON $ICON 1 C && style_has $ICON 1 fg=#ff0000 && style_has $((ICON + 2)) 1 fg=#565f89 && style_has 108 1 fg=#565f89   # console_1, ②'
conf 'theme = "x"\n' '[icon]\nconsole = { fg = "#00ff00" }\n'; start
check "[icon] fg only: same glyph, new color" eval 'text_is $ICON $ICON 1 "$NF_CONSOLE" && style_has $ICON 1 fg=#00ff00'
conf 'theme = "x"\nicons = "ascii"\n' '[icon]\nconsole = { text = "C", fg = "#ff0000" }\n'; start
check "[icon] still applies with icons = ascii" eval 'text_is $ICON $ICON 1 C && style_has $ICON 1 fg=#ff0000'
conf 'theme = "x"\n' '[icon]\nconn = { fg = "#ff00ff" }\n'; start
check "[icon] conn: the status bar connection icon changes color" eval 'c=$(e2e_find pg@localhost 45); style_has $((c - 2)) 45 fg=#ff00ff && style_has $c 45 fg=#7dcfff'
conf 'theme = "x"\n' '[icon]\nfoo = { text = "F" }\n';            check "[icon] unknown name → error" fails "foo"
conf 'theme = "x"\n' '[icon]\nconsole = { bg = "#ff0000" }\n';    check "[icon] unknown sub-key (bg) → error" fails "bg"
conf 'theme = "x"\n' '[icon]\nconsole = { fg = "red" }\n';        check "[icon] bad color → error" fails '"red"'

e2e_done
