#!/usr/bin/env bash
# F0.14 Command palette: table, pane and window scopes (specs/m0-skeleton/task.md F0.14; tech-design §12, §5 zoom)
# F1.1 starts with one empty data pane: two_tabs / two_panes build what M0's default layout had;
# the second window (%report) comes back in M5.
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
. "$(dirname "$0")/palette.sh"
data_tabs() { e2e_text 34 160 43 | noicon; }
kinds() { list | cut -d'|' -f1 | awk '{ print $NF }' | sort -u | tr '\n' ' '; }   # type labels in the list
# which scope tab is highlighted (focus background) on the tab row
current_scope() {
  local y x; y=$(tabs_y)
  for tab in 所有 窗口·Pane 表 命令 SQL; do x=$(e2e_find "$tab" $y | cut -d' ' -f1); [[ -n $x ]] && style_has $x $y bg=#9ece6a >/dev/null && { echo "$tab"; return; }; done
}
icon_of() { local y; y=$(row_y "$1"); e2e_text $(($(left) + 2)) $(($(left) + 2)) $y; }
NF_WINDOW=$(printf '\xef\x8b\x92') NF_COMMAND=$(printf '\xef\x83\xa7')   # U+F2D2 U+F0E7

# ---- scopes: Tab / S-Tab cycle by rewriting the prefix only
start; two_tabs; two_panes; pal   # ① t_user / t_order, ② empty
check "the tab row (above the input, F0.17) reads 所有 · 窗口·Pane % · 表 @ · 命令 >, 所有 highlighted" eval '[[ $(e2e_text $(($(left) + 2)) $(($(left) + 40)) $(tabs_y) | tr -s " ") == " 所有 窗口·Pane % 表 @ 命令 >"* && $(current_scope) == 所有 ]]'
e2e_type "abc"; e2e_keys Left; sleep 0.2; cx=$(e2e_flag cursor_x)
e2e_keys Tab; sleep 0.3
check "Tab → 窗口·Pane: prefix %, the rest kept, cursor keeps its place in the text" eval 'input_is "%abc" && [[ $(current_scope) == 窗口·Pane && $(e2e_flag cursor_x) == $((cx + 1)) ]]'
e2e_keys Tab; sleep 0.3; check "Tab → 表: prefix @" eval 'input_is "@abc" && [[ $(current_scope) == 表 ]]'
e2e_keys Tab; sleep 0.3; check "Tab → 命令: prefix >" eval 'input_is ">abc" && [[ $(current_scope) == 命令 ]]'
e2e_keys Tab; sleep 0.3; check "Tab → SQL (F1.7): prefix ;" eval 'input_is ";abc" && [[ $(current_scope) == SQL ]]'
e2e_keys Tab; sleep 0.3; check "Tab → 所有: prefix removed" eval 'input_is "abc" && [[ $(current_scope) == 所有 ]]'
e2e_keys BTab; sleep 0.3; check "S-Tab goes back: SQL" eval 'input_is ";abc" && [[ $(current_scope) == SQL ]]'
e2e_keys BTab; sleep 0.3; check "S-Tab again: 命令" eval 'input_is ">abc" && [[ $(current_scope) == 命令 ]]'
x=$(e2e_find "表" $(tabs_y) | cut -d' ' -f1); e2e_click $x $(tabs_y); sleep 0.3
check "clicking a scope tab switches to it" eval 'input_is "@abc" && [[ $(current_scope) == 表 ]]'
e2e_keys Escape; sleep 0.2

# ---- prefixes restrict the search
pal "@ord"; check "@ord searches tables only" eval '[[ $(kinds) == "表 " ]] && row_has t_order && row_has t_order_item'
e2e_keys Escape; sleep 0.2
pal "%t_ord"; check "%t_ord searches windows and panes only (finds ① on t_order)" eval '[[ $(kinds) == "Pane " ]] && row_has "table · t_order"'
e2e_keys Escape; sleep 0.2

# ---- rows: icon, name, dim location, key / ON-OFF, type label
pal
wrow() { list | grep -n "^0: data  doraemon .*窗口|" | cut -d: -f1; }   # the window row's number (the recent tables come first)
check "window row: 0: data, located in doraemon, labelled 窗口, window icon" eval 'n=$(wrow); [[ -n $n && $(icon_of $n) == "$NF_WINDOW" ]]'
check "pane row: ⟨1⟩ table · t_order (F3.7: the type of its tab), located in 0: data, labelled Pane" eval 'row_has "table · t_order  0: data" && list | grep -q "table · t_order  0: data .*Pane|"'
check "table row: located in doraemon.public, labelled 表" eval 'list | grep -q "^mv_order_by_status  doraemon.public .*表|"'
check "the location is dim; the type label is dim and right-aligned (ends one column before │)" eval 'y=$(row_y $(wrow)); c=$(e2e_find doraemon $y); style_has $c $y fg=#565f89 && text_is $(( $(right) - 5 )) $(right) $y "窗口 │" && style_has $(( $(right) - 5 )) $y fg=#565f89'
check "所有, empty input: recent first (t_order, t_user), then window → pane → table → command" eval 'clear_input; o=$(list | cut -d"|" -f1 | awk "{print \$NF}" | uniq | tr "\n" " "); [[ $o == "表 窗口 Pane 表 "* && $(list | head -2 | cut -d" " -f1 | tr "\n" " ") == "t_order t_user " ]] || { echo "  order: $o"; false; }'
seen=""; clear_input; for i in $(seq 24); do seen+=" $(kinds)"; e2e_keys Down; sleep 0.05; done
check "所有 has all four kinds (scrolling through the list)" eval '[[ $seen == *窗口* && $seen == *Pane* && $seen == *表* && $seen == *命令* ]] || { echo "  kinds: $(tr " " "\n" <<<"$seen" | sort -u | tr "\n" " ")"; false; }'
clear_input; e2e_type '>'; sleep 0.3
check "command rows carry the command icon U+F0E7 and label 命令" eval '[[ $(icon_of 1) == "$NF_COMMAND" ]] && list | head -1 | grep -q "命令|"'
e2e_keys Escape; sleep 0.2

# ---- tables: ↵ and C-t both open a new tab (F3.18: ↵ no longer replaces the current one; F3.37: C-t is the same as ↵); footer; C-t on a non-table does nothing
start; two_tabs; pal "@t_sku"
check "a table selected: footer says only ↵ 打开 (F3.37)" eval '[[ $(footer) == *"↵ 打开 " && $(footer) != *C-t* ]] || { echo "  footer: $(footer)"; false; }'
e2e_keys Enter; sleep 0.3
check "↵ opens t_sku in a new tab (F3.18), t_order stays" eval 'closed && text_has 34 160 1 " t_sku ─" && [[ $(data_tabs) == "│ 1:t_user │ 2:t_order- │ 3:t_sku* │ +"* ]]'
pal "@goal"; e2e_keys C-t; sleep 0.3
check "C-t on a table not open in this pane: a new tab, the old one marked -" eval 'closed && [[ $(data_tabs) == "│ 1:t_user │ 2:t_order │ 3:t_sku- │ 4:goal* │ +"* ]]'
pal ">split"; e2e_keys C-t; sleep 0.3
check "C-t with a command selected: nothing happens, the palette stays" eval 'is_open && (( $(e2e_panes | awk "\$1 != \"-\"" | wc -l) == 2 ))'
e2e_keys Escape; sleep 0.2
e2e_keys C-h; sleep 0.2; pal "@agent"; e2e_keys Enter; sleep 0.3
check "focus on the sidebar → the table opens in the first data pane, focus moves there" eval '[[ $(focused) == 1 ]] && text_has 34 160 1 " agent ─"'

# ---- panes
start; two_panes; e2e_keys C-l; sleep 0.2; pal "@goal"; e2e_keys Enter; sleep 0.3; e2e_keys C-h; sleep 0.2   # ① empty, ② goal
pal "%goal"; e2e_keys Enter; sleep 0.3
check "pane scope: ↵ on ② (table · goal) focuses it" eval 'closed && [[ $(focused) == 2 ]]'

# ---- zoom (§5): a jump to another pane ends the zoom; the zoomed pane itself keeps it
start; two_tabs; two_panes
e2e_keys C-l; sleep 0.2; e2e_keys Space; e2e_type z; sleep 0.3
pal "%t_order"; e2e_keys Enter; sleep 0.3
check "② zoomed, %t_order ↵: zoom ends, focus on ①" eval '(( $(e2e_panes | awk "\$1 != \"-\"" | wc -l) == 3 )) && [[ $(focused) == 1 ]]'
e2e_keys Space; e2e_type z; sleep 0.3
pal "@goal"; e2e_keys Enter; sleep 0.3
check "① zoomed, open a table: stays zoomed (it is the target)" eval '(( $(e2e_panes | awk "\$1 != \"-\"" | wc -l) == 1 )) && [[ $(focused) == 1 ]] && text_has 1 160 1 " goal ─"'
e2e_keys Space; e2e_type z; sleep 0.3

# ---- an emptied data pane: opening a table gives one tab, no - mark
start
e2e_keys Space; e2e_type '"'; sleep 0.3   # the new empty data pane is ⟨2⟩
pal "@goal"; e2e_keys Enter; sleep 0.3
check "opening into an empty data pane: one tab, no - mark" eval 'g=($(e2e_panes | awk "\$6 == 1")); [[ $(e2e_text $((g[1] + 1)) $((g[1] + g[3] - 2)) $((g[2] + g[4] - 2)) | noicon) == " 1:goal* │ +"* ]]'

# ---- icons: ascii and [icon] overrides
printf 'icons = "ascii"\n' > "$D/config.toml"; start -C "$D"; pal
check "ascii: window icon [] and command icon :" eval '[[ $(e2e_text $(($(left) + 2)) $(($(left) + 3)) $(row_y 1)) == "[]" ]] && { clear_input; e2e_type ">"; sleep 0.3; [[ $(icon_of 1) == ":" ]]; }'
rm -rf "$D"/*; mkdir -p "$D/themes"; printf 'theme = "x"\n' > "$D/config.toml"
printf '[icon]\nwindow = { text = "W", fg = "#ff0000" }\ncommand = { text = "K" }\n' > "$D/themes/x.toml"
start -C "$D"; pal
check "[icon] window / command overrides show in the palette" eval '[[ $(icon_of 1) == W ]] && style_has $(($(left) + 2)) $(row_y 1) fg=#ff0000 && { clear_input; e2e_type ">"; sleep 0.3; [[ $(icon_of 1) == K ]]; }'

e2e_done
