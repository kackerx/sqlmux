# e2e/palette.sh — command palette helpers (F0.13+). Source after lib.sh; expects H().
# Layout (F0.17): border · scope tabs · <search icon> input · ─ · list · ─ · [DDL preview · ─ (F4.2)] · footer.
top()  { e2e_plain | python3 -c 'import sys; print(next((i + 1 for i, l in enumerate(sys.stdin) if "┌─ 命令面板" in l), ""))'; }
left() { local t; t=$(top); [[ -n $t ]] && e2e_find "┌─ 命令面板" "$t" | cut -d' ' -f1; }
right() { local t; t=$(top); e2e_find "┐" "$t" | tr ' ' '\n' | awk -v l="$(left)" '$1 > l { print; exit }'; }   # the box's right border column
tabs_y()  { echo $(( $(top) + 1 )); }
input_y() { echo $(( $(top) + 2 )); }
is_open() { [[ -n $(top) ]]; }
closed()  { ! is_open || { echo "  palette still open"; false; }; }
input_is() { local l; l=$(left); local got; got=$(e2e_text $((l + 4)) $(( $(right) - 2 )) "$(input_y)" | sed 's/ *$//'); [[ $got == "$1" ]] || { echo "  input: '$got', want '$1'"; false; }; }
# the list sits between the first two ─ separators under the input (F0.14 adds a scope-tab row above it)
seps() { local t l; t=$(top); l=$(left); e2e_rows $((l + 1)) $((l + 2)) $((t + 1)) $(H) $((l + 1)) | awk -F'|' -v t=$t '$1 ~ /^─/ { print NR + t }' | head -2 | tr '\n' ' '; }
list() {  # one line per list row: "<text after the icon>|<selected 0/1>" (one capture)
  local t l s1 s2; t=$(top); l=$(left); read s1 s2 <<<"$(seps)"
  ((s2 > s1 + 1)) || return 0
  # columns are padded to line up (F0.17); runs of spaces are squeezed to two so rows read "name  location  …"
  e2e_rows $((l + 4)) $(( $(right) - 1 )) $((s1 + 1)) $((s2 - 1)) $((l + 2)) |
    awk -F'|' '{ sub(/ +$/, "", $1); gsub(/   +/, "  ", $1); print $1 "|" ($2 == "#364a82" ? 1 : 0) }'
}
nrows() { list | grep -c .; }
row_has() { list | cut -d'|' -f1 | grep -qF -- "$1"; }
selected() { list | awk -F'|' '$2 == 1 { print $1 }'; }
row_y() { local s1; read s1 _ <<<"$(seps)"; echo $((s1 + $1)); }   # screen row of list row N (1-based)
bot() { local t l; t=$(top); l=$(left); e2e_rows $l $l $t $(H) $l | awk -F'|' -v t=$t '$1 ~ /^└/ { print NR + t - 1; exit }'; }   # the bottom border's row
foot_y() { echo $(( $(bot) - 1 )); }   # the footer sits on the bottom border; a DDL preview (F4.2) comes between it and the list
# preview: the DDL preview's rows (F4.2) — between the list's closing ─ and the ─ over the footer, trailing blanks cut; nothing without one
preview() { local s1 s2 b l; read s1 s2 <<<"$(seps)"; b=$(bot); l=$(left); ((b - 2 > s2)) || return 0
  e2e_rows $((l + 2)) $(( $(right) - 1 )) $((s2 + 1)) $((b - 3)) $((l + 2)) | cut -d'|' -f1 | sed 's/ *$//'; }
footer() { e2e_text $(($(left) + 1)) $(( $(right) - 1 )) "$(foot_y)"; }
clear_input() { local i; for ((i = 0; i < 40; i++)); do e2e_keys BSpace; done; sleep 0.2; }
pal() { e2e_keys C-p; sleep 0.3; [[ -n $1 ]] && { e2e_type "$1"; sleep 0.3; }; true; }

# ---- quick SQL (F1.7): the SQL scope's result area under the input
pbox() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }   # 面板的 X Y W H
# prows：面板内每一行的文字（第一行是上边框）；结果区的标题行以「只读」收尾
prows() { local g; g=($(pbox)); e2e_rows $((g[0] + 1)) $((g[0] + g[2] - 2)) ${g[1]} $((g[1] + g[3] - 1)) $((g[0] + 1)) | cut -d'|' -f1; }
title() { prows | grep -m1 ' 只读' | tr -s ' ' | sed 's/^ //; s/ $//'; }
title_like() { [[ $(title) =~ $1 ]] || { echo "  title: '$(title)', want /$1/"; false; }; }
line1() { prows | awk '/ 只读/ { getline; print; exit }' | sed 's/^ *//; s/ *$//'; }   # 标题下面第一行（报错时就是错误）
# grid：结果区表格的数据行，每行「列1|列2…」
grid() { prows | awk '/ 只读/ { f = 1; next } f && /┼/ { g = 1; next } g && /│/' | sed 's/^ *[0-9]* │//; s/ *│ */|/g; s/^ *//; s/ *$//'; }
pfoot() { prows | tail -2 | head -1 | sed 's/ *$//'; }                     # 底栏
modified() { [[ $(pfoot) == *"已修改，↵ 重新执行" ]]; }
input() { local g; g=($(pbox)); e2e_text $((g[0] + 3)) $((g[0] + g[2] - 2)) $((g[1] + 2)) | sed 's/^ *//; s/ *$//'; }
clear_all() { local i; for ((i = 0; i < 120; i++)); do e2e_keys BSpace; done; sleep 0.3; }
# sql TEXT：在已打开的面板里清空输入、输入 ;TEXT、↵，等结果回来
sql() { clear_all; e2e_type ";$1"; sleep 0.4; e2e_keys Enter; wait_for 8 eval '[[ -n $(title) && $(title) != "… 行"* ]]'; sleep 0.2; }
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | sed -n 2p; }   # 补全列表：面板之后的第二个浮层
items() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | tr -s ' ' | sed 's/^ //; s/ $//'; done; }
