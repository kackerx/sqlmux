#!/usr/bin/env bash
# F4.3 窗口·Pane 范围列出 tab 并显示层级（specs/m4-palette/task.md F4.3；tech-design §12「每一行」）
# 默认布局：① 开 t_order、t_user，② console_1 加一个引导 tab，执行一次后 ③ 结果区有 日志、console_1 #1。
# 别的 window 的 tab 在 M5 之前只有单测。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
icon_of() { e2e_text $(($(left) + 2)) $(($(left) + 2)) "$(row_y "$1")"; }       # 面板第 N 行的图标
tab_icon() { local x; x=$(tab_x "$1" "$2"); e2e_text $((x + 2)) $((x + 2)) "$(tab_y "$1")"; }   # P N:：pane P 的 tab 栏上第 N 个 tab 的图标（紧跟「N:」）
rowof() { list | cut -d'|' -f1 | grep -n -- "$1" | head -1 | cut -d: -f1; }       # 第一个含 TEXT 的行
pick() { local n; n=$(rowof "$1"); for ((i = 1; i < n; i++)); do e2e_keys Down; done; sleep 0.2; }   # TEXT：选中第一个含它的行

start
open_table t_order; open_table t_user                                            # ① t_order、t_user，当前是 t_user
key C-l; key i; e2e_type "select 1 as a"; key Escape; key Enter; wait_for 8 eval '[[ -n $(geom 3) ]]'; sleep 0.3
click_tab 2 "+"                                                                  # ② 加一个引导 tab
ICON_TABLE=$(tab_icon 1 1:) ICON_CONSOLE=$(tab_icon 2 1:) ICON_LOG=$(tab_icon 3 1:) ICON_RESULT=$(tab_icon 3 2:)
check "准备好了：① t_order │ t_user，② console_1 │ 新 tab，③ 日志 │ console_1 #1" eval 'tabs_are 1 "1:t_order- │ 2:t_user*" && tabs_are 2 "1:console_1- │ 2:新 tab*" && [[ $(tabbar 3 | sed -E "s/ {3,}.*//") == "1:日志- │ 2:console_1 #1*" ]]'

# ---- % 列出 window → pane（按 ⟨n⟩）→ 各自的 tab，位置写全层级（§12、F4.3 细节）
pal "%"
want="0: data  doraemon  窗口
⓪ schema  doraemon › 0: data  Pane
① table · t_user  doraemon › 0: data  Pane
t_order  doraemon › 0: data › pane-1  Tab
t_user  doraemon › 0: data › pane-1  Tab
② 新 tab  doraemon › 0: data  Pane
console_1  doraemon › 0: data › pane-2  Tab
新 tab  doraemon › 0: data › pane-2  Tab
③ result · console_1 #1  doraemon › 0: data  Pane
日志  doraemon › 0: data › pane-3  Tab
console_1 #1  doraemon › 0: data › pane-3  Tab"
check "% 输入为空：window、pane 按 ⟨n⟩，每个 pane 后面紧跟它的 tab（结果区、引导 tab 也列）；tab 的位置是 doraemon › 0: data › pane-N，pane 的是 doraemon › 0: data，window 的是 doraemon" eval '[[ $(list | cut -d"|" -f1) == "$want" ]] || { list; false; }'
check "tab 的图标照 tab 的类型（和 tab 栏上的一样）：表、console、日志、结果；引导 tab 留空" eval '[[ $(icon_of 4) == "$ICON_TABLE" && $(icon_of 7) == "$ICON_CONSOLE" && $(icon_of 8) == " " && $(icon_of 10) == "$ICON_LOG" && $(icon_of 11) == "$ICON_RESULT" && $ICON_TABLE != "$ICON_CONSOLE" ]] || { for i in 4 7 8 10 11; do icon_of $i | od -An -tx1; done; false; }'
e2e_keys Escape; sleep 0.3

# ---- ↵ 切到那个 tab、聚焦它的 pane（验收）
pal "%t_or"
check "%t_or：第一行就是 tab t_order，位置 doraemon › 0: data › pane-1" eval '[[ $(selected) == "t_order  doraemon › 0: data › pane-1  Tab" ]] || { list; false; }'
check "底栏 ↵ 切换" eval '[[ $(footer) == *"↵ 切换 " ]]'
e2e_keys Enter; sleep 0.4
check "↵：面板关掉，焦点到 ①，① 的当前 tab 换成 t_order" eval 'closed && focus_is 1 && tabs_are 1 "1:t_order* │ 2:t_user-"'
pal "%console_1"; pick "^console_1  "; e2e_keys Enter; sleep 0.4
check "② 的 console_1（当前是引导 tab）：焦点到 ②，切到 console_1" eval 'closed && focus_is 2 && tabs_are 2 "1:console_1* │ 2:新 tab-"'
pal "%日志"; e2e_keys Enter; sleep 0.4
check "结果区的 日志：焦点到 ③，切到日志" eval 'closed && focus_is 3 && [[ $(tabbar 3 | sed -E "s/ {3,}.*//") == "1:日志* │ 2:console_1 #1-" ]]'
pal "%t_user"; y=$(row_y "$(rowof "^t_user  .*Tab")"); e2e_click $(( $(left) + 10 )) "$y"; sleep 0.4
check "点击 tab 那一行：同 ↵" eval 'closed && focus_is 1 && tabs_are 1 "1:t_order- │ 2:t_user*"'

# ---- 「所有」范围也列 tab；tab 不记进「最近用过」
pal "t_user"
check "「所有」：t_user 既有表，也有 tab" eval 'list | grep -q "^t_user  doraemon.public  表|" && list | grep -q "^t_user  doraemon › 0: data › pane-1  Tab|" || { list; false; }'
clear_input; sleep 0.3
check "输入为空：最前面最近用过的里没有 tab（刚才 ↵ 过的 t_order 等 tab 不算）" eval '[[ $(list | head -2 | cut -d"|" -f1 | awk "{print \$NF}" | tr "\n" " ") == "表 表 " ]] || { list | head -3; false; }'

e2e_done
