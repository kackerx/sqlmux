#!/usr/bin/env bash
# F3.39 WHERE 输入框的 vim 模式（specs/m3-console/task.md F3.39；tech-design §7.8「WHERE」、§6.4 wherenormal）
# 编辑器本身的行为有 nvim 差分兜底；这里在真实终端里走 WHERE 的 INSERT / NORMAL / VISUAL 切换、执行、回表格、历史下拉，
# 以及单行的规则（j / k / o / : 不做事，yy p 当字符粘）。只读，用共用库的 t_order。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

editing() { [[ $(bar) == *"-- editing WHERE --"* ]]; }
in_where() { mode_is "$1" && editing || { echo "  not in WHERE $1: $(e2e_text 60 160 "$(H)")"; false; }; }   # MODE：在 WHERE 里，模式块是 MODE
on_grid() { mode_is NORMAL && ! editing; }
where_is() { [[ $(where_in) == "$1" ]] || { echo "  WHERE: '$(where_in)', want '$1'"; false; }; }
cx() { echo $(( $(e2e_flag cursor_x) - 41 )); }                                  # 光标前有几个字符（输入框从第 42 列起，cursor_x 从 0 数）
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }'; }

start; open_table t_order; wait_for 8 settled

# ---- / 进 INSERT，esc 到 NORMAL，dd 清空，i 输入新条件，↵ 执行（task.md 验收）
key /; e2e_type "status = 'done'"; sleep 0.2
check "/：WHERE 的 INSERT，状态栏 -- editing WHERE --" in_where INSERT
key Escape
check "INSERT 下 esc：到 WHERE 的 NORMAL（模式块 NORMAL，还在编辑 WHERE），光标退一格" eval 'in_where NORMAL && where_is "status = '"'done'"'" && [[ $(cx) == 14 ]]'
key d d
check "NORMAL 下 dd：清空这一行" eval 'in_where NORMAL && where_is ""'
key i; e2e_type "id < 5"; sleep 0.2; key Enter; wait_for 8 settled
check "i 输入 id < 5、↵：执行，回到表格，4 行" eval 'on_grid && [[ $(cnt) == 4 ]] && where_is "id < 5"'

# ---- NORMAL 下 ciw 改一个词；↵ 在 NORMAL 下也执行
key /; key Escape; key 0; key c i w; e2e_type user_id; key Escape
check "NORMAL 下 ciw：id 换成 user_id" eval 'in_where NORMAL && where_is "user_id < 5"'
key Enter; wait_for 8 settled
check "NORMAL 下 ↵：执行（user_id < 5 有 $(psql "$SQLMUX_TEST_PG" -At -c "select count(*) from t_order where user_id < 5") 行）" eval 'on_grid && [[ $(cnt) == $(psql "$SQLMUX_TEST_PG" -At -c "select count(*) from t_order where user_id < 5") ]]'

# ---- NORMAL 下 esc 回表格，没 ↵ 的改动丢掉；C-c 等同 esc
key /; key Escape; key d d; key Escape
check "NORMAL 下 esc：回到表格，没执行的 dd 丢掉，输入框恢复成生效的条件" eval 'on_grid && where_is "user_id < 5" && [[ $(cnt) != 6000 ]]'
key /; e2e_type " and x"; key C-c
check "INSERT 下 C-c：同 esc，到 WHERE 的 NORMAL" in_where NORMAL
key C-c
check "NORMAL 下 C-c：回到表格，改动丢掉，不弹退出提示" eval 'on_grid && where_is "user_id < 5" && ! screen_has "再按一次"'

# ---- NORMAL 下 / 打开历史 / 收藏下拉，同时进 INSERT、光标到末尾；下拉里 esc 只关下拉
key /; key Escape; key 0; key /; sleep 0.3
check "NORMAL 下 /：打开历史下拉（列出 user_id < 5 和 id < 5），进 INSERT，光标在末尾" eval '[[ -n $(pop) ]] && e2e_plain | grep -q "user_id < 5 .*[0-9][0-9]:[0-9][0-9]" && e2e_plain | grep -qE "│ +. id < 5 .*[0-9][0-9]:[0-9][0-9]" && [[ $(cx) == 11 ]] && editing'
key Escape
check "下拉里 esc：只关下拉，停在 INSERT" eval '[[ -z $(pop) ]] && in_where INSERT'
key Escape; key Escape

# ---- 单行的规则：j / k 不动，o / O / J / : 不做事；yy 之后 p / P 把整行当字符放在光标后 / 前；u / C-r；VISUAL
key /; key Escape; key 0
key j; key k; key o; key O; key J; key :
check "j / k / o / O / J / :：都不做事，还是一行，还在 NORMAL" eval 'in_where NORMAL && where_is "user_id < 5" && ! palette_open'
key y y; key '$'; key p
check "yy 再 p：整行当字符粘在光标后面" eval 'where_is "user_id < 5user_id < 5"'
key u
check "u：撤销这次粘贴" where_is "user_id < 5"
key C-r
check "C-r：重做" where_is "user_id < 5user_id < 5"
key u; key 0; key v; key e
check "v e：VISUAL，模式块 VISUAL" in_where VISUAL
key d
check "VISUAL 下 d：删掉 user_id，回到 NORMAL" eval 'in_where NORMAL && where_is " < 5"'
key i; e2e_type id; key Escape; key Enter; wait_for 8 settled
check "改回 id < 5 执行" eval 'on_grid && [[ $(cnt) == 4 ]]'

# ---- NORMAL 下点击只移光标；[keys.normal] 的键照常：C-h 换焦点放弃这次编辑
key /; key Escape; key 0
e2e_click 46 2; sleep 0.3
check "WHERE 的 NORMAL 下点击：只移光标（到第 5 个字符），还在 NORMAL" eval 'in_where NORMAL && [[ $(cx) == 4 ]]'
key x; key C-h
check "C-h：焦点到树，这次的编辑放弃（输入框恢复成 id < 5）" eval 'focus_is 0 && [[ $(bar) != *"editing WHERE"* ]] && where_is "id < 5"'

# ---- INSERT 下 C-u / C-w 照 nvim：先删到这次 INSERT 的起点，再按一次才继续删（5514975 §7.9）
key Escape; key C-l; key /; e2e_type " and x"; sleep 0.2; key C-u
check "/ 进 INSERT（光标在 id < 5 后面），输入 and x 再 C-u：只删到这次 INSERT 的起点" eval 'in_where INSERT && where_is "id < 5"'
key C-u
check "再 C-u：删到行首" where_is ""
key Escape; key Escape
key /; e2e_type " and"; key C-w
check "输入 and 再 C-w：删掉 and，光标在留下的空格后面" eval 'where_is "id < 5" && [[ $(cx) == 7 ]]'
key C-w
check "再 C-w：删掉空格，停在这次 INSERT 的起点（5 后面）" eval 'where_is "id < 5" && [[ $(cx) == 6 ]]'
key C-w
check "第三次 C-w：越过起点，接着删前面的 5" where_is "id <"
key Escape; key Escape

# ---- REPLACE 下 C-r 不会把 A 打进文字；历史下拉开着时 REPLACE 下打字同样过滤（reviewer 在 F3.39 实测）
key /; key Escape; key 0; key R; key C-r; sleep 0.3
check "REPLACE 下 C-r：打开历史下拉，文字里没有多出 A" eval '[[ -n $(pop) ]] && where_is "id < 5"'
e2e_type us; sleep 0.3
check "下拉开着、REPLACE 下打字：照样过滤（us 只剩 user_id < 5）" eval 'rows=$(e2e_plain | grep -cE "│ +. (user_id|id) < 5 "); (( rows == 1 )) && e2e_plain | grep -q "user_id < 5 " || { e2e_plain | grep -E "< 5 "; false; }'
key Escape; key Escape; key Escape

# ---- VISUAL 下、操作符待定时点 ▾：先回 NORMAL 再进 INSERT，之后打的字过滤下拉，不当成 vim 命令
vx=$(e2e_find ▾ 2 | cut -d' ' -f1)
key /; key Escape; key 0; key v; e2e_click "$vx" 2; sleep 0.4
check "VISUAL 下点 ▾：打开历史下拉，进 INSERT" eval '[[ -n $(pop) ]] && editing && ! mode_is VISUAL >/dev/null'
e2e_type xyz; sleep 0.3
check "接着打字 xyz：是过滤（下拉里没有匹配的就不画），不是 vim 命令（文字没被删改，只是追加）" eval '[[ $(where_in) == "id < 5xyz" ]] || { echo "  [$(where_in)]"; false; }'
key Escape; key Escape; key Escape
key /; key Escape; key 0; key d; e2e_click "$vx" 2; sleep 0.4; e2e_type w; sleep 0.3
check "d 待定时点 ▾ 再打 w：w 进了输入框（不是 dw 删词）" eval '[[ $(where_in) == "id < 5w" ]] && editing || { echo "  [$(where_in)]"; false; }'
key Escape; key Escape; key Escape

e2e_done
