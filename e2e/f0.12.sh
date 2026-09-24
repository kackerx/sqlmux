#!/usr/bin/env bash
# F0.12 Theme files, colors by column type, [icon] overrides, console icon
# (specs/m0-skeleton/task.md F0.12; tech-design §7.3, §7.6, §7.7)
# "No theme = unchanged screen" is covered by f0.2/f0.5 passing and the golden diff (one glyph).
# F1.1 has no console: the console icon checks come back in M3; [icon] overrides use the data pane's icon.
# Column colors (F1.3, §7.6) on seed's t_order: id / amount number, status an enum, paid bool, meta jsonb,
# note text (set on every 10th row), created_at timestamptz.
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
# conf CONFIG [THEME] — a fresh config dir: config.toml, and themes/x.toml when THEME is given
conf() { rm -rf "$D"/*; printf "$1" > "$D/config.toml"; [[ -n $2 ]] && { mkdir -p "$D/themes"; printf "$2" > "$D/themes/x.toml"; }; true; }
fails() { e2e_start -C "$D" "$E2E_BIN"; wait_for 3 screen_has '[e2e-exit 1]' || return 1; screen_has "$1" || { echo "  error lacks '$1':"; e2e_plain | grep sqlmux: | sed 's/^/    /'; false; }; }
NF_DATA=$(printf '\xef\x87\x80')   # U+F1C0
ICON=39 ICON_ASCII=41             # data title icon column: nerd ┌─ ① <icon> …; ascii ┌─ ⟨1⟩ <icon> … (⟨1⟩ is 3 wide, F0.16)
# cell NAME N — "X Y" of column NAME's value on data row N (-1: the header): text starts under the header name,
# numbers end under its last letter (right-aligned)
cell() { local hy=$(( $(grid_y) - 1 )) c; c=$(e2e_find "$1" $hy); c=${c%% *}
  [[ $1 == id || $1 == amount ]] && c=$((c + ${#1} - 1)); echo "$c $(( hy + 1 + $2 ))"; }
fg_at() { style_has $(cell "$1" "$2") "fg=$3"; }   # NAME N COLOR

# ---- defaults: numbers are the number color, the other types default to fg
conf ''; start -C "$D"; open_table t_order
check "default: id / amount number, status (enum) / paid / meta / created_at / note fg" eval 'fg_at id 1 "#ff9e64" && fg_at amount 1 "#ff9e64" && fg_at status 1 "#c0caf5" && fg_at paid 1 "#c0caf5" && fg_at meta 1 "#c0caf5" && fg_at created_at 1 "#c0caf5" && fg_at note 10 "#c0caf5"'

# ---- a theme file with eight tokens: those apply, nothing else moves; bar stays #292e42
conf 'theme = "x"\n' 'pane_bg = "#101010"\nrow = "#202020"\ncursor = "#303030"\nnumber = "#404040"\nstring = "#505050"\ntime = "#606060"\nbool = "#707070"\njson = "#808080"\n'
start -C "$D"
check "pane_bg applies (pane body)" style_has 130 20 bg=#101010
open_table t_order
check "row applies (current row)" style_has $(cell user_id 1) bg=#202020
check "cursor applies (current cell)" style_has $(cell id 1) bg=#303030
check "number / string / time / bool / json apply to id / note / created_at / paid / meta" eval 'fg_at id 1 "#404040" && fg_at note 10 "#505050" && fg_at created_at 1 "#606060" && fg_at paid 1 "#707070" && fg_at meta 1 "#808080"'
check "an enum is not a string: status keeps fg" fg_at status 1 "#c0caf5"
check "other tokens unchanged: border, focus, bg gap, row_alt, header" eval 'style_has 1 1 fg=#3b4261 && style_has 34 1 fg=#9ece6a && style_has 33 5 bg=#1f2335 && style_has $(cell user_id 2) bg=#1f2335 && style_has $(cell user_id -1) fg=#7aa2f7'
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
