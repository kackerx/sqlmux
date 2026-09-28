#!/usr/bin/env bash
# F1.9 补全列表的按键（specs/m1-browse/task.md F1.9；tech-design §9.7「交互」）
# 弱 / 强高亮和 match 色由单测和 golden 覆盖（match_test、where_test）；这里在真实终端里按键，
# 看 ↵ 是执行还是接受、Tab / S-Tab / C-p / ↑ 选到哪一项。高亮状态只能从底色读出：row 底 = 弱，select 底 = 强。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
ROW=#292e42 SELECT=#364a82
# boxN N：第 N 个浮层的 X Y W H；WHERE 的补全列表是第 1 个，面板里的是第 2 个（面板本身是第 1 个）
boxN() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | sed -n "${1}p"; }
# items N：第 N 个浮层里每一行「候选|底色」，底色取行尾的留白
items() { local g y; g=($(boxN "$1")); [[ -n ${g[0]} ]] || return 0
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do
    printf '%s|%s\n' "$(e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | awk '{ print $1 }')" "$(e2e_style $((g[0] + g[2] - 2)) $y | grep -oE 'bg=#[0-9a-f]+' | cut -c4-)"; done; }
lit_as() { items "$1" | awk -F'|' -v c="$2" '$2 == c { print $1 }'; }     # N COLOR：这种底色的候选
weak() { lit_as 1 $ROW; }
strong() { lit_as 1 $SELECT; }
nth() { items 1 | sed -n "${1}p" | cut -d'|' -f1; }
typ() { e2e_type "$1"; sleep 0.4; }
edit() { key /; clear_in; }

start; open_table t_order; wait_for 8 settled

# ---- 弹出时第一项弱高亮；↵ 只在选过（强高亮）时接受，否则照常执行
edit; typ sta
check "WHERE 输入 sta：status 弱高亮（row 底），没有强高亮" eval '[[ $(weak) == status && -z $(strong) ]] || { items 1; false; }'
key Enter; wait_for 8 settled
check "没选过时 ↵：直接执行查询（输入仍是 sta，报列不存在），回到 NORMAL" eval 'mode_is NORMAL && [[ $(where_in) == sta ]] && e2e_text 35 159 4 | grep -q "column \"sta\" does not exist"'
edit; typ sta; key Tab
check "按一次 Tab：status 强高亮（select 底）" eval '[[ $(strong) == status ]] || { items 1; false; }'
key Enter
check "选过之后 ↵：接受，输入变成 status，仍在输入" eval '[[ $(where_in) == status && -z $(boxN 1) ]] && mode_is INSERT'

# ---- Tab 选中第一项之后移到下一项，S-Tab 回来；第一次按 S-Tab / C-p / ↑ 选中最后一项
edit; typ at; first=$(nth 1); second=$(nth 2); n=$(items 1 | grep -c .)
key Tab; key Tab
check "at：Tab 两次，选到第 2 项（${second}）" eval '[[ -n $second && $(strong) == "$second" ]] || { items 1; false; }'
key BTab
check "S-Tab：回到第 1 项（${first}）" eval '[[ $(strong) == "$first" ]]'
for k in BTab C-p Up; do
  edit; typ at; key $k
  check "新弹出的列表上第一次按 ${k}：选中最后一项" eval '[[ $(strong) == "$(nth $n)" && $(strong) != "$first" ]] || { items 1; false; }'
done

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
check "快速 SQL 输入 select 42 as x：x 弹出了候选（如 max），但还没有选中" eval '[[ -n $(boxN 2) && -z $(lit_as 2 $SELECT) ]] || { items 2; false; }'
key Enter; wait_for 8 eval 'e2e_plain | grep -q " 只读"'
check "↵ 直接执行，语句不变（没有被换成 max）" eval 'input_is ";select 42 as x" && e2e_plain | grep -q "│ 1 │ 42"'
clear_input; e2e_type ";select * from t_ord"; sleep 0.5
key Tab
check "列表开着：Tab 选中候选，范围仍是 SQL" eval '[[ -n $(lit_as 2 $SELECT) ]] && input_is ";select * from t_ord"'
key Escape; key Tab
check "列表关着：Tab 切换范围（SQL → 所有，去掉 ;）" input_is "select * from t_ord"
key Escape

e2e_done
