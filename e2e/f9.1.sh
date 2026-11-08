#!/usr/bin/env bash
# F9.1 / F9.2 / F9.4 WHERE 的几处顺手操作（specs/m9-feedback2/task.md；tech-design §9.6）
# F9.1 历史 / 收藏下拉里 C-e 只填进输入框、进 INSERT、不执行；F9.2 输入框里 C-f 收藏 / 取消收藏当前条件（toast）；
# F9.4 表格上 C-r 直接进 WHERE 并打开历史，esc 一层层退回。只读，用共用库；state 放临时目录。
. "$(dirname "$0")/lib.sh"
SOLO=1
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); ST=$D/state; trap 'e2e_stop; rm -rf "$D"' EXIT
typ() { e2e_type "$1"; sleep 0.4; }
edit() { key /; clear_in; }
run() { edit; typ "$1"; key Enter; wait_for 8 settled; sleep 0.2; }
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }'; }
items() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | noicon | tr -s ' ' | sed 's/^ //; s/ $//'; done; }
toasted() { e2e_recorded "$1"; }                                    # toast 只显示 3 秒：看录下来的输出
fresh_rec() { : >|"$D/rec"; }
wcur() { echo $(( $(e2e_flag cursor_x) )); }

start -S "$ST"; e2e_record "$D/rec"; open_table t_order; wait_for 8 settled
run "id < 50"; run "id < 5"
check "（准备）WHERE id < 5：4 行" eval '[[ $(cnt) == 4 ]]'

# ---- F9.1：C-e 填入，不执行
edit; key C-r; sleep 0.5
check "历史下拉：两项（id < 5 在前），底部提示里「应用」后面是「C-e 填入」" eval '[[ $(items | head -1) == "id < 5"* ]] && items | tail -1 | grep -q "↵ 应用 · C-e 填入" || { items; false; }'
key C-e; sleep 0.5
check "C-e：下拉关了，输入框是选中项的文字「id < 5」，INSERT 模式，没有执行（行数还是 4）" eval '[[ -z $(pop) && $(where_in) == "id < 5" ]] && mode_is INSERT && [[ $(cnt) == 4 ]] || { echo "  [$(where_in)] cnt=$(cnt) pop=[$(pop)]"; false; }'
check "光标在文字末尾" eval '[[ $(e2e_flag cursor_x) == $(( 41 + 6 )) ]] || { echo "  cursor_x=$(e2e_flag cursor_x)"; false; }'
key BSpace; typ 7; key Enter; wait_for 8 settled; sleep 0.3
check "改一个值（id < 7）↵：执行新条件，6 行" eval '[[ $(cnt) == 6 && $(where_in) == "id < 7" ]] || { echo "  cnt=$(cnt) [$(where_in)]"; false; }'

# ---- F9.2：输入框里 C-f 收藏 / 取消收藏
edit; typ "status = 'done'"; fresh_rec; key C-f; sleep 0.6
check "输入框里 C-f：toast「已收藏」" toasted "已收藏"
edit; typ "status = 'done'"; key C-r; sleep 0.5
check "C-r 打开下拉：收藏组里有 status = 'done'（分隔线下面）" eval 'it=$(items); n=$(grep -n "^─" <<<"$it" | cut -d: -f1); [[ -n $n ]] && sed -n "$((n + 1)),\$p" <<<"$it" | grep -q "status = '"'"'done'"'"'" || { echo "$it"; false; }'
key Escape; key Escape; key Escape
edit; typ "status = 'done'"; fresh_rec; key C-f; sleep 0.6
check "再按一次 C-f（同样的条件）：toast「已取消收藏」" toasted "已取消收藏"
edit; fresh_rec; key C-f; sleep 0.6
check "输入框为空 C-f：无事，没有 toast" eval '! toasted "已收藏" && ! toasted "已取消收藏"'
edit; typ "id < 9"; key C-r; sleep 0.5; fresh_rec; key C-f; sleep 0.6
check "下拉开着时 C-f：照旧收藏选中项，不 toast" eval '! toasted "已收藏" && ! toasted "已取消收藏" && [[ -n $(pop) ]]'
key Escape; key Escape; key Escape

# ---- F9.4：表格上 C-r 直接进 WHERE 并打开历史
key Escape; sleep 0.3
check "（准备）焦点在表格上，NORMAL" eval 'mode_is NORMAL && [[ -z $(pop) ]]'
key C-r; sleep 0.6
check "表格上 C-r：WHERE 聚焦（输入框里有光标），历史下拉打开，状态栏 COMMAND" eval '[[ -n $(pop) ]] && mode_is COMMAND'
key Escape; sleep 0.4
check "esc：下拉关掉，回到 WHERE 输入框（INSERT）" eval '[[ -z $(pop) ]] && mode_is INSERT'
key Escape; sleep 0.3; key Escape; sleep 0.3
check "再 esc 回到表格（NORMAL，没有输入框的光标行）" eval 'mode_is NORMAL && [[ -z $(pop) ]]'
e2e_done
