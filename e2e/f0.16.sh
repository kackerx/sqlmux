#!/usr/bin/env bash
# F0.16 Drop text next to icons, circled pane numbers, fewer palette candidates
# (specs/m0-skeleton/task.md F0.16; tech-design §7.7, §7.8, §12)
# ⑳ → ⟨21⟩ is unit-tested (icons_test.go); the goldens: 80×24 unchanged, 160×45 differs in 3 lines (checked by diff).
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
start() { e2e_start "$@" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3; }
H() { e2e_flag pane_height; }
. "$(dirname "$0")/palette.sh"
NF_SCHEMA=$(printf '\xef\x83\xa8') NF_DATA=$(printf '\xef\x87\x80') NF_CONSOLE=$(printf '\xef\x92\x89') NF_FILTER=$(printf '\xef\x82\xb0')
widths_are() { local got; got=$(e2e_widths | sed '/^0$/d' | sort -u | tr '\n' ' '); [[ $got == "$1 " ]] || { echo "  row widths: $got"; false; }; }

# ---- nerd: circled numbers, no type words, icon-only palette entry, no / in the filter row
start
check "titles: ⓪ <icon> public ▾ · ① <icon> t_order · ② <icon> console_1" eval 'text_has 1 32 1 "┌─ ⓪ $NF_SCHEMA public ▾ " && text_has 34 103 1 "┌─ ① $NF_DATA t_order ─" && text_has 105 160 1 "┌─ ② $NF_CONSOLE console_1 ─"'
check "no data / console words in the titles" eval '[[ $(e2e_text 34 160 1) != *" data "* && $(e2e_text 34 160 1) != *"console · "* ]]'
check "filter row: icon then the table count, no /" text_is 2 14 2 " $NF_FILTER 14 tables "
check "status bar: the palette entry is only the search icon" eval '[[ $(e2e_text 1 160 45) != *C-p* ]] && text_is $(( $(search_col) - 1 )) $(( $(search_col) + 1 )) 45 " $SEARCH_ICON "'
e2e_click $(search_col) 45; sleep 0.3
check "clicking the search icon opens the palette" is_open
check "palette pane rows keep the type: ① data · t_order" eval 'clear_input; e2e_type "%"; sleep 0.3; row_has "① data · t_order" && row_has "② console · console_1"'
e2e_keys Escape; sleep 0.2
e2e_keys Space; e2e_type '"'; sleep 0.3
check "a new pane is ② and console becomes ③" eval 'text_has 34 103 23 "┌─ ② $NF_DATA ─" && text_has 105 160 1 "┌─ ③ $NF_CONSOLE console_1"'
check "an empty pane's title is only ② <icon>" eval '[[ $(e2e_text 34 103 23) == "┌─ ② $NF_DATA ─"*"─┐" ]]'
e2e_keys Space; e2e_type q; sleep 0.4
mid() { e2e_panes | awk -v n="$1" '$1 == n { print int($2 + $4 / 2 - 1), int($3 + $5 / 2) }'; }   # a pane's centre cell
digit_at() { local x y; read x y <<<"$(mid "$1")"; [[ $(e2e_text $x $x $y) == "$1" ]] || { echo "  pane $1 centre ($x,$y): '$(e2e_text $((x - 1)) $((x + 1)) $y)'"; false; }; }
check "SPC q still shows plain digits in the middle of each pane" eval 'digit_at 0 && digit_at 1 && digit_at 2 && digit_at 3'
e2e_keys Escape; sleep 0.2

# ---- narrow windows: the shortest title is ① <icon>, nothing overflows
for w in 60 40 30; do
  start -x $w -y 16
  check "${w} wide: every row is $w columns, titles stay inside their boxes" eval 'widths_are $w && t=$(e2e_text 1 $w 1); [[ $t != *"─┐─"* && $t == *┐ ]]'
done

# ---- a dragged-narrow sidebar (§7.8): keep ⓪ <icon>, shorten the schema name (pub… ▾); drop ▾ only with no room for one letter
start
side_w() { e2e_panes | awk '$1 == 0 { print $4 }'; }
want_title() {  # W → the expected start of the sidebar title
  local room=$(( $1 - 12 ))                          # "┌─ ⓪ X " + " ▾" + " ─┐" take 12 columns
  if ((room >= 6)); then echo "┌─ ⓪ $NF_SCHEMA public ▾ "; elif ((room >= 2)); then echo "┌─ ⓪ $NF_SCHEMA ${NAME:0:room-1}… ▾ "; else echo "┌─ ⓪ $NF_SCHEMA "; fi
}
NAME=public ok=1
for w in 22 20 19 18 17 16; do
  e2e_down $(( $(side_w) + 1 )) 20; e2e_drag_to $(( w + 1 )) 20; e2e_up $(( w + 1 )) 20; sleep 0.3
  t=$(e2e_text 1 "$(side_w)" 1); exp=$(want_title "$(side_w)")
  [[ $t == "$exp"* && $t == *┐ && $t != *SPC* ]] || { echo "  width $(side_w): '$t', want it to start with '$exp' and no SPC b"; ok=0; }
done
check "sidebar 16–22 wide: ⓪ <icon> stays, the schema name shrinks to pub… ▾, SPC b gives way first" test $ok = 1

# ---- ascii: the screen is as before F0.16
printf 'icons = "ascii"\n' > "$D/config.toml"; start -C "$D"
check "ascii: titles keep ⟨n⟩ and the type words" eval 'text_has 1 32 1 "⟨0⟩ # public ▾" && text_has 34 103 1 "⟨1⟩ = data · t_order" && text_has 105 160 1 "⟨2⟩ > console · "'
check "ascii: the palette entry still reads ~ C-p, the filter row keeps /" eval 'text_has 1 160 45 " ~ C-p " && text_has 1 32 2 "? / 14 tables"'

# ---- the palette: commands only useful while editing a cell are not candidates
start; pal '>'
seen=""; for i in $(seq 90); do seen+=$'\n'"$(selected)"; e2e_keys Down; sleep 0.03; done
check "empty command scope lists no cell.* commands (确定 / 完成编辑 / 加一 / 减一 / 下一段 / 上一段)" eval '! grep -qE "cell\.|^确定|^完成编辑|^加一|^减一|^下一段|^上一段" <<<"$seen" && grep -q "pane.zoom" <<<"$seen"'
e2e_keys Escape

e2e_done
