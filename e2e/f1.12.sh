#!/usr/bin/env bash
# F1.12 层级目录树（specs/m1-browse/task.md F1.12；tech-design §7.8「schema 侧栏」）
# 默认展开、列节点、过滤后的样子由 golden 覆盖；这里在真实 PG 上按键、点击，看光标去哪、打开了什么。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
SELECT=#364a82
psql_n() { psql "$SQLMUX_TEST_PG" -At -c "$1"; }
# tree：树的每一行「文字|底色」（第 4 行起，到下面的分隔线为止）；cur：光标行（树聚焦时 select 底）
tree() { e2e_rows 2 31 4 $(( $(H) - 4 )) 3 | sed 's/ *|/|/'; }
cur() { tree | awk -F'|' -v s=$SELECT '$2 == s { print $1 }' | sed 's/^ *//'; }
cur_is() { [[ $(cur) =~ (^|\ )$1(\ |$) ]] || { echo "  cursor on '$(cur)', want $1"; false; }; }
row_of() { tree | cut -d'|' -f1 | grep -E "(^| )$1( |$)" | head -1; }      # NAME：这个节点所在行
shown() { [[ -n $(row_of "$1") ]]; }
open_mark() { row_of "$1" | grep -qF "▾"; }                                   # NAME 已展开
y_of() { tree | cut -d'|' -f1 | grep -nE "(^| )$1( |$)" | head -1 | cut -d: -f1 | awk '{ print $1 + 3 }'; }
x_of() { e2e_find "$1" "$(y_of "$1")" | cut -d' ' -f1; }
goto() { key g g; local i; for ((i = 0; i < 60; i++)); do [[ $(cur) =~ (^|\ )$1(\ |$) ]] && return; e2e_keys j; sleep 0.15; done; }   # NAME：光标移到这个节点
title() { e2e_plain | head -1; }
mclick() { _sgr 1 "$1" "$2" M; _sgr 1 "$1" "$2" m; }                          # 中键

start; key C-h
check "聚焦树：光标在 session 节点 doraemon" cur_is doraemon

# ---- l / h：展开、移到第一个子节点、移到父节点、折叠
key j
check "j：agentable（折叠着，下面的表不显示）" eval 'cur_is agentable && ! open_mark agentable && ! shown agent'
key l
check "l：展开 agentable，出现 Tables (3)、Views (0)，光标不动" eval 'cur_is agentable && open_mark agentable && shown "Tables \(3\)" && shown "Views \(0\)"'
key l
check "已展开时 l：移到第一个子节点 Tables (3)" cur_is "Tables \(3\)"
key h
check "Tables (3) 是折叠的，h：移到父节点 agentable" cur_is agentable
key h
check "agentable 已展开，h：折叠" eval 'cur_is agentable && ! open_mark agentable && ! shown "Tables \(3\)"'
goto "Tables \(6\)"; key Enter
check "分组节点 ↵：折叠 public 的 Tables (6)，下面的表隐藏" eval '! open_mark "Tables \(6\)" && ! shown t_order'
key Enter
check "再 ↵：展开回来" eval 'open_mark "Tables \(6\)" && shown t_order'

# ---- agentable 下的表：不切 schema 就能展开、打开
goto agentable; key l; key l; key l; key l                                  # 展开 agentable、到 Tables (3)、展开它、到第一张表
check "agentable → Tables (3) → l：展开，光标到第一张表 agent" cur_is agent
key Enter; wait_for 8 settled
check "↵ 打开 agentable.agent：焦点到 ①，行数和 psql 一致" eval '[[ $(title) == *" agent ─"* && $(focused) == 1 && $(cnt) == $(psql_n "select count(*) from agentable.agent") ]] || { echo "  $(title) / $(cnt)"; false; }'

# ---- 树当前的 schema：光标所在节点所属的 schema，快速 SQL 按它设 search_path
key C-h; goto "Tables \(3\)"
pal ";select count(*) from agent"; key Enter
check "光标在 agentable 下：快速 SQL 的 select count(*) from agent 能找到表" eval 'wait_for 8 eval "e2e_plain | grep -q \"1 行 · .* · 只读\""'
key Escape; goto "Tables \(6\)"
pal ";select count(*) from agent"; key Enter
check "光标回到 public 下：同一句找不到 agent（relation does not exist）" eval 'wait_for 8 eval "e2e_plain | grep -q \"relation \\\"agent\\\" does not exist\""'
key Escape

# ---- 列节点：展开表时才取列；↵ 打开表，光标落在这一列
goto t_order; key l
check "l 展开 t_order：列出列（id、user_id、status、amount…）" eval 'wait_for 3 shown amount && shown user_id && shown status'
goto amount; key Enter; wait_for 8 settled
check "列节点 amount ↵：打开 t_order，光标在 amount（第 4 列），焦点到 ①" eval '[[ $(title) == *" t_order ─"* && $(focused) == 1 ]] && pos_is 1,4'
key g c; key j j j Space Escape; key h                                        # COLS 隐藏 amount，光标挪到最近的可见列
p=$(pos); key C-h; goto amount; key Enter
check "打开已有的 tab、这一列被 COLS 隐藏了：光标不动，也不取消隐藏" eval 'pos_is $p && qb_has "COLS 9/10"'
key g c; key a Escape
e2e_keys C-p; sleep 0.3; e2e_type "@t_order"; sleep 0.3; key C-t; wait_for 8 settled   # t_order 再开一个 tab（tab 2，光标 1,1）
key C-h; goto amount; key Enter
check "t_order 开着两个 tab 时列节点 ↵：面板进入「选择 tab」" eval 'is_open && [[ $(footer) == *"↵ 切过去"* ]]'
key Enter; wait_for 8 settled
check "选定 ① · 2（F3.18 起 agent 在 tab 1）：切过去，光标也移到 amount（1,4）（§7.8，docs cc95d0d）" eval '[[ $(e2e_text 34 160 43 | noicon) == *"2:t_order*"* ]] && pos_is 1,4'
key g t; key x                                                                   # 关掉 tab 3，回到 tab 2

# ---- 鼠标：单击 ▸ / ▾ 展开折叠；单击表与 ↵ 相同；中键与 t 相同
key C-h; y=$(y_of t_sku); x=$(e2e_find ▸ "$y" | cut -d' ' -f1)
e2e_click "$x" "$y"; sleep 0.5
check "单击 t_sku 的 ▸：展开（列出 code、title）" eval 'open_mark t_sku && shown code'
e2e_click "$x" "$y"; sleep 0.3
check "再单击 ▾：折叠" eval '! open_mark t_sku && ! shown code'
mclick "$(x_of t_log)" "$(y_of t_log)"; wait_for 8 settled
check "中键 t_log：在 ① 新开 tab，焦点到 ①" eval '[[ $(title) == *" t_log ─"* && $(focused) == 1 && $(e2e_text 34 160 43 | noicon) == *"3:t_log*"* ]] || { e2e_text 34 160 43; false; }'

# ---- 工作区：tab 节点跟着变；单击或 ↵ 切到对应的 pane 和 tab
key C-h
ws_tabs() { tree | cut -d"|" -f1 | sed -n "/工作区/,\$p" | grep -cE " (agent|t_order|t_log)( |$)"; }   # 工作区里 tab 节点的个数
check "工作区的 pane-1 下有三个 tab 节点 agent、t_order、t_log" eval '[[ $(ws_tabs) == 3 ]] || tree'
wsy() { tree | cut -d'|' -f1 | grep -n '' | sed -n '/工作区/,$p' | grep -E " $1( |$)" | head -1 | cut -d: -f1 | awk '{ print $1 + 3 }'; }
# F3.19：pane 节点叫 pane-1，单击 / ↵ 只展开折叠；单击时焦点和点别的节点一样到树上，不跳到 ①
key C-l; e2e_click "$(x_of pane-1)" "$(y_of pane-1)"; sleep 0.4
check "焦点在 ① 时单击 pane-1：折叠（tab 节点隐藏），焦点到树上，不跳到 ①" eval '[[ $(focused) == 0 && $(ws_tabs) == 0 ]] && ! open_mark pane-1'
e2e_click "$(x_of pane-1)" "$(y_of pane-1)"; sleep 0.4
check "再单击 pane-1：展开，焦点仍在树上" eval '[[ $(focused) == 0 && $(ws_tabs) == 3 ]] && open_mark pane-1'
goto pane-1; key Enter
check "↵ pane-1：折叠，焦点不动" eval '[[ $(focused) == 0 && $(ws_tabs) == 0 ]] && cur_is pane-1'
key Enter
check "再 ↵：展开" eval '[[ $(focused) == 0 && $(ws_tabs) == 3 ]] && cur_is pane-1'
e2e_click "$(e2e_find t_order "$(wsy t_order)" | cut -d' ' -f1)" "$(wsy t_order)"; sleep 0.4
check "单击工作区的 t_order 节点：焦点到 ①，切到 tab 2" eval '[[ $(focused) == 1 && $(e2e_text 34 160 43 | noicon) == *"2:t_order*"* ]]'
key C-h; goto "工作区"; for ((i = 0; i < 6; i++)); do [[ $(cur) == *t_log* ]] && break; key j; done; key Enter
check "↵ 工作区的 t_log 节点：焦点到 ①，切到 tab 3" eval '[[ $(focused) == 1 && $(e2e_text 34 160 43 | noicon) == *"3:t_log*"* ]]'
key x; key C-h
check "x 关掉 t_log 之后：工作区只剩 agent、t_order 两个 tab 节点" eval '[[ $(ws_tabs) == 2 ]] || tree'

# ---- 过滤：只匹配表和视图，能跨 schema；匹配项的上级展开，其余隐藏
goto agentable; key h                                                          # 先把 agentable 折起来
key /; e2e_type ent; sleep 0.5
check "过滤 ent：agentable 的 agent、agent_version 和 public 的 t_event 都在，3/11" eval 'shown agent && shown agent_version && shown t_event && ! shown t_order && [[ $(e2e_text 2 31 2) == *"3/11"* ]] || tree'
check "匹配项的上级展开（agentable 也展开了），工作区整块隐藏" eval 'open_mark agentable && ! shown 工作区'
key Escape
check "esc 清空过滤：回到原来的展开状态（agentable 仍折叠），工作区回来" eval '! open_mark agentable && shown 工作区 && shown t_order'

# ---- R：重新加载，展开状态保留
goto t_user; key l; wait_for 3 shown email
key R; sleep 1
check "R 之后展开状态保留：t_user 仍展开，列还在" eval 'open_mark t_user && wait_for 3 shown email'

# ---- 焦点不在 data pane 上时，打开到最近聚焦过的 data pane（§12，docs 2d643e2）
FOCUS=#9ece6a
key C-l; key Space '"'; key C-h                                              # ① 下面分出 ②、聚焦它，再 C-h 回到树
check "从 ② 按 C-h：焦点到树" eval '[[ $(focused) == 0 ]]'
goto t_sku; key Enter; wait_for 8 settled
check "树上 ↵ t_sku：开在最近聚焦过的 ②，不是第一个 ①" eval '[[ $(focused) == 2 ]] && e2e_plain | grep -q "② .*t_sku ─" && [[ $(e2e_plain | head -1) == *" t_order ─"* ]]'
key C-h
check "回到树：「当前打开的表」高亮 ② 的 t_sku（focus 色），不是 ① 的 t_order" eval 'style_has $(x_of t_sku) $(y_of t_sku) fg=$FOCUS && ! style_has $(x_of t_order) $(y_of t_order) fg=$FOCUS >/dev/null'
e2e_done
