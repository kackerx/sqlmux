#!/usr/bin/env bash
# F0.13 Command palette: frame and command scope (specs/m0-skeleton/task.md F0.13; tech-design §12, §9.7, §7.5)
# match.go ranking / highlight / extended syntax are unit-tested (TestFilterRanks, TestFilterPositions, TestTextMatch).
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

start() { e2e_start "$@" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3; }
W() { e2e_flag pane_width; }
H() { e2e_flag pane_height; }
top()  { e2e_plain | python3 -c 'import sys; print(next((i + 1 for i, l in enumerate(sys.stdin) if "┌─ 命令面板" in l), ""))'; }
left() { local t; t=$(top); [[ -n $t ]] && e2e_find "┌─ 命令面板" "$t" | cut -d' ' -f1; }
is_open() { [[ -n $(top) ]]; }
closed()  { ! is_open || { echo "  palette still open"; false; }; }
input_is() { local t l; t=$(top); l=$(left); local got; got=$(e2e_text $((l + 2)) $((l + 77)) $((t + 1)) | sed 's/ *$//'); [[ $got == "$1" ]] || { echo "  input: '$got', want '$1'"; false; }; }
list() {  # one line per list row: "<text>|<selected 0/1>" (one capture)
  local t l; t=$(top); l=$(left)
  e2e_rows $((l + 2)) $((l + 78)) $((t + 3)) $((t + 13)) $((l + 2)) |
    awk -F'|' '/^─/ { exit } { sub(/ +$/, "", $1); print $1 "|" ($2 == "#364a82" ? 1 : 0) }'
}
nrows() { list | grep -c .; }
row_has() { list | cut -d'|' -f1 | grep -qF -- "$1"; }
selected() { list | awk -F'|' '$2 == 1 { print $1 }'; }
footer() { local t; t=$(top); local b=$((t + 3 + $(nrows) + 1)); e2e_text $(($(left) + 1)) $(($(left) + 78)) $b; }
clear_input() { local i; for ((i = 0; i < 40; i++)); do e2e_keys BSpace; done; sleep 0.2; }
pal() { e2e_keys C-p; sleep 0.3; [[ -n $1 ]] && { e2e_type "$1"; sleep 0.3; }; true; }

# ---- open / dim / close (§7.5)
start
pal
check "C-p opens the palette" is_open
check "background dims 60% toward bg (pane_bg → #212537, bar → #23273a)" eval 'style_has 1 1 bg=#212537 && style_has 80 45 bg=#23273a'
check "mode block shows COMMAND, status bar has no command line" eval '[[ $(e2e_text 1 160 45) == *" doraemon ▾  0: data*"*" COMMAND " ]]'
e2e_keys Escape; sleep 0.3
check "esc closes, the screen is restored" eval 'closed && style_has 1 1 bg=#24283b && style_has 80 45 bg=#292e42 && [[ $(e2e_text 150 160 45) == *" NORMAL " ]]'
pal; e2e_keys C-c; sleep 0.3
check "C-c closes too, without the quit toast" eval 'closed && ! screen_has 再按一次'

# ---- layout: width min(80, W-4), centered, top edge at H/4
for s in "160 45" "100 30" "60 20"; do
  set -- $s; SW=$1 SH=$2; start -x $SW -y $SH; pal
  w=$(( SW - 4 < 80 ? SW - 4 : 80 ))
  # "1/4 of the window": the window is the area above the status bar, H-1 rows
  check "${SW}x$SH: width $w, centered, top at row $(( (SH - 1) / 4 + 1 ))" eval '[[ $(top) == $(( (SH - 1) / 4 + 1 )) && $(left) == $(( (SW - w) / 2 + 1 )) ]] && [[ $(e2e_text $(( (SW - w) / 2 + w )) $(( (SW - w) / 2 + w )) $(top)) == ┐ ]]'
done
start; pal
check "empty input: at most 10 rows" eval '(( $(nrows) == 10 ))'
check "footer: ↑/↓ 移动 · esc 关闭 … ↵ 执行" eval '[[ $(footer) == " ↑/↓ 移动 · esc 关闭"*"↵ 执行 " ]]'
check "palette's own actions are not candidates (palette.up/down/run/close)" eval 'clear_input; e2e_type palette; sleep 0.3; ! row_has palette.up && ! row_has palette.down && ! row_has palette.run && ! row_has palette.close'
clear_input; e2e_type 关闭; sleep 0.3
check "typing 关闭 does not list 关闭命令面板" eval 'row_has "关闭 tab" && ! row_has 命令面板'
e2e_keys Escape; sleep 0.2

# ---- search: split / resize, title + dim id, keys in the right column
pal split
check "split: 上下分割 / 左右分割 with SPC \" / SPC %" eval '(( $(nrows) == 2 )) && list | grep -q "上下分割  pane.split.below .* SPC \"|" && list | grep -q "左右分割  pane.split.right .* SPC %|"'
check "the action id is dim" eval 'y=$(( $(top) + 3 )); c=$(e2e_find pane.split.below $y); style_has $((c + 1)) $y fg=#565f89'
check "matched characters get a warn background" eval 'y=$(( $(top) + 3 )); c=$(e2e_find pane.split.below $y); style_has $((c + 5)) $y bg=#e0af68 && style_has $((c + 9)) $y bg=#e0af68 && ! style_has $((c + 1)) $y bg=#e0af68 >/dev/null'
e2e_keys Enter; sleep 0.3
check "↵ runs the selected command (split below) and closes" eval 'closed && [[ $(e2e_panes | awk "{printf \"%s \", \$1}") == "0 1 3 2 " ]]'
start; pal resize
check "resize: 4 commands, no keys in the right column" eval '(( $(nrows) == 4 )) && [[ $(list | cut -d"|" -f1 | grep -c "pane.resize.[a-z]*$") == 4 ]]'
clear_input; e2e_type resize.left; sleep 0.3; e2e_keys Enter; sleep 0.3
check "↵ runs an unbound command (resize left: data 70 → 64 columns)" eval '[[ $(e2e_panes | awk "\$1 == 1 { print \$4 }") == 64 ]]'

# ---- smartcase
pal limit; check "all-lowercase is case-insensitive: limit finds LIMIT" row_has "LIMIT  grid.limit"
clear_input; e2e_type LIMIT; sleep 0.3; check "an uppercase letter makes it case-sensitive: LIMIT still finds it" row_has "LIMIT  grid.limit"
clear_input; e2e_type split; sleep 0.3; check "split finds the two split commands" eval '(( $(nrows) == 2 ))'
clear_input; e2e_type SPLIT; sleep 0.3; check "SPLIT (has uppercase) is case-sensitive: matches nothing" eval '(( $(nrows) == 0 ))'
e2e_keys Escape; sleep 0.2

# ---- : opens the command scope; ex aliases
start
e2e_type ':'; sleep 0.3
check ": opens the palette with > already typed" input_is '>'
e2e_type q; sleep 0.3
check ":q ranks 关闭 tab first" eval '[[ $(selected) == "关闭 tab  tab.close"* ]]'
e2e_keys Enter; sleep 0.3
check ":q↵ closes the current tab (t_order → t_user)" eval 'closed && text_has 34 103 1 "data · t_user"'
e2e_type ':qa'; sleep 0.3
check ":qa ranks 退出 first" eval '[[ $(selected) == "退出  quit"* ]]'
e2e_keys Enter
check ":qa↵ quits, terminal restored" eval 'wait_for 3 screen_has "[e2e-exit 0]" && flag_is alternate_on 0 && flag_is cursor_flag 1'

# ---- toggles: ON / OFF, the palette stays open; recent first
start; pal zoom
check "pane.zoom shows OFF; footer says ↵ 切换" eval 'list | grep -q "pane.zoom .*OFF|1" && [[ $(footer) == *"↵ 切换 " ]]'
e2e_keys Enter; sleep 0.3
check "↵ toggles: ON, the palette stays open, the pane is zoomed" eval 'is_open && list | grep -q "pane.zoom .*ON|1"'
e2e_keys Enter; sleep 0.3; clear_input; e2e_type tree.toggle; sleep 0.3
check "tree.toggle shows ON while the sidebar is open" eval 'list | grep -q "tree.toggle .*ON|"'
clear_input
check "empty input lists the recently used one first" eval '[[ $(list | head -1) == *pane.zoom* ]]'
e2e_keys Escape; sleep 0.2
# scrolled list + toggle: the selection stays visible (the bug the reviewer sent back)
pal; for i in $(seq 120); do [[ $(selected) == *tree.toggle* ]] && break; e2e_keys Down; sleep 0.05; done
check "moving down scrolls the list to tree.toggle" eval '[[ $(selected) == *tree.toggle* ]]'
e2e_keys Enter; sleep 0.3
check "after toggling from a scrolled list, the selected row is still visible" eval '[[ $(selected) == *tree.toggle* ]]'
e2e_keys Enter; sleep 0.3; e2e_keys Escape; sleep 0.2

# ---- mouse: hover selects, click runs, click outside / footer
start; pal split
y=$(( $(top) + 4 )); e2e_move 60 $y; sleep 0.3
check "hover moves the selection" eval '[[ $(selected) == 左右分割* ]]'
e2e_click 60 $y; sleep 0.3
check "click runs that row (split right) and closes" eval 'closed && [[ $(e2e_panes | awk "{printf \"%s \", \$1}") == "0 1 2 3 " ]]'
pal; e2e_click 5 5; sleep 0.3
check "click outside closes" closed
pal split; c=$(e2e_find "esc 关闭" $(( $(top) + 6 ))); e2e_click $c $(( $(top) + 6 )); sleep 0.3
check "click esc 关闭 in the footer closes" closed
start; pal split; c=$(e2e_find "↵ 执行" $(( $(top) + 6 ))); e2e_click $c $(( $(top) + 6 )); sleep 0.3
check "click ↵ 执行 in the footer runs the selection" eval 'closed && (( $(e2e_panes | wc -l) == 4 ))'

# ---- the input: grapheme backspace, visible cursor, long input
E_ACUTE=$(printf 'e\xcc\x81') THUMB=$(printf '\xf0\x9f\x91\x8d\xf0\x9f\x8f\xbd')
start; pal
e2e_type "x${E_ACUTE}"; e2e_keys BSpace; sleep 0.2; check "backspace deletes all of é (e+U+0301)" input_is x
e2e_type "${THUMB}"; e2e_keys BSpace; sleep 0.2; check "backspace deletes all of 👍🏽" input_is x
check "the terminal cursor is shown right after the input" eval 'flag_is cursor_flag 1 && [[ $(e2e_flag cursor_y) == $(top) && $(e2e_flag cursor_x) == $(( $(left) + 2 )) ]]'
e2e_type "$(printf '%100s' | tr ' ' a)XYZ"; sleep 0.3
check "input longer than the box: the typing position stays visible" eval 'y=$(( $(top) + 1 )); [[ $(e2e_text $(left) $(( $(left) + 79 )) $y) == *XYZ*"│" ]] && (( $(e2e_flag cursor_x) < $(left) + 79 ))'

e2e_done
