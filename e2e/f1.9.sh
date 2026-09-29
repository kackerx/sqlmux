#!/usr/bin/env bash
# F1.9 补全列表的按键（specs/m1-browse/task.md F1.9；tech-design §9.7「交互」）
# F1.14 把弱 / 强高亮换成了「弹出即选中、到头绕回、智能回车」，那部分在 f1.14.sh；这里留下没变的：
# esc 两步，快速 SQL 里 Tab 移动候选还是切换范围，select 42 as x 的 ↵ 是执行。match 色由单测覆盖。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
SELECT=#364a82
# boxN N：第 N 个浮层的 X Y W H；WHERE 的补全列表是第 1 个，面板里的是第 2 个（面板本身是第 1 个）
boxN() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | sed -n "${1}p"; }
# items N：第 N 个浮层里每一行「候选|底色」，底色取行尾的留白
items() { local g y; g=($(boxN "$1")); [[ -n ${g[0]} ]] || return 0
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do
    printf '%s|%s\n' "$(e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | awk '{ print $1 }')" "$(e2e_style $((g[0] + g[2] - 2)) $y | grep -oE 'bg=#[0-9a-f]+' | cut -c4-)"; done; }
lit_as() { items "$1" | awk -F'|' -v c="$2" '$2 == c { print $1 }'; }     # N COLOR：这种底色的候选
typ() { e2e_type "$1"; sleep 0.4; }
edit() { key /; clear_in; }

start; open_table t_order; wait_for 8 settled

# ---- esc 两步：第一次关列表（仍在输入，↵ 执行），第二次才退出输入
edit; typ sta; key Escape
check "esc 第一次：只关补全列表，仍在输入" eval '[[ -z $(boxN 1) ]] && mode_is INSERT && [[ $(where_in) == sta ]]'
key Enter; wait_for 8 settled
check "列表关了之后 ↵：执行查询" eval 'mode_is NORMAL && e2e_text 35 159 4 | grep -q "column \"sta\" does not exist"'
edit; typ sta; key Escape; key Escape
check "esc 第二次：退出输入，输入框恢复成生效的条件" eval 'mode_is NORMAL && [[ $(where_in) == sta ]]'   # 生效的就是刚才执行过的 sta

# ---- 快速 SQL：列表开着时 Tab 移动候选，关着时 Tab 切换范围；select 42 as x 的 ↵ 是执行
where() { key /; clear_in; e2e_type "$1"; sleep 0.2; key Enter; wait_for 8 settled; }; where ""
pal ";select 42 as x"; sleep 0.3
key Enter; wait_for 8 eval 'e2e_plain | grep -q " 只读"'
check "↵ 直接执行，语句不变（没有被换成 max）" eval 'input_is ";select 42 as x" && e2e_plain | grep -q "│ 1 │ 42"'
clear_input; e2e_type ";select * from t_ord"; sleep 0.5
key Tab
check "列表开着：Tab 在候选间移动，范围仍是 SQL" eval '[[ -n $(lit_as 2 $SELECT) ]] && input_is ";select * from t_ord"'
key Escape; key Tab
check "列表关着：Tab 切换范围（SQL → 所有，去掉 ;）" input_is "select * from t_ord"
key Escape

e2e_done
