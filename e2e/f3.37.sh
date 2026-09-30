#!/usr/bin/env bash
# F3.37 打开表的目标 pane 与「同一 pane 里唯一」（specs/m3-console/task.md F3.37；tech-design §7.8「打开已有的表」、§12「表」）
# 默认布局 ⓪ | ① [34,103] | ② console_1 [105,160]；执行过 SQL 后底部多出结果区 ③。
# 目标 pane 是最近聚焦过的普通 pane（console 所在的也算，结果区永远不算）；同一个 pane 里一张表只开一个 tab，别的 pane 不管。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

. "$(dirname "$0")/palette.sh"
pal_open() { e2e_keys C-p; sleep 0.3; e2e_type "@$1"; sleep 0.3; key "${2:-Enter}"; wait_for 8 settled; sleep 0.2; }   # NAME [KEY]
tree_open() { key /; key Escape; key /; e2e_type "$1"; sleep 0.3; key Enter; key "${2:-Enter}"; wait_for 8 settled; sleep 0.2; }   # 在树里过滤出 NAME 再 ↵（或 t）
to_tree() { key Space; e2e_type q; sleep 0.3; e2e_type 0; sleep 0.3; }   # 按编号跳到树，「最近聚焦过的 pane」不变

start
pal_open t_order
check "① 的引导 tab 上打开 t_order：替换引导 tab" eval 'tabs_are 1 "1:t_order*" && focus_is 1'

# ---- 焦点在 ② console 时从树打开：开在 ②（task.md 验收）；① 也开着 t_order，② 照样新开一个
key C-l; to_tree; tree_open t_order
check "最近聚焦的是 ② console：从树 ↵ t_order 开在 ② 的新 tab，console_1 还在；① 的 t_order 不管" eval 'tabs_are 2 "1:console_1- │ 2:t_order*" && tabs_are 1 "1:t_order*" && focus_is 2'
pal_open t_order
check "在 ② 里再打开 t_order：只切过去，② 里不会有两个" eval 'tabs_are 2 "1:console_1- │ 2:t_order*" && focus_is 2 && closed'
key g T; pal_open t_order C-t
check "C-t 也一样：② 里已有，切过去，不新开" eval 'tabs_are 2 "1:console_1- │ 2:t_order*"'
key g T; to_tree; tree_open t_order t
check "树里的 t 也一样" eval 'tabs_are 2 "1:console_1- │ 2:t_order*" && focus_is 2'

# ---- ① 里再打开 t_order：只切过去，① 里只有一个
click_tab 1 1:t_order; pal_open t_user
check "① 里新开 t_user" tabs_are 1 "1:t_order- │ 2:t_user*"
pal_open t_order
check "① 里再打开 t_order：切过去，① 里不会有两个" eval 'tabs_are 1 "1:t_order* │ 2:t_user-" && focus_is 1'

# ---- + 开的引导 tab 上选了这个 pane 里已经开着的表：切过去，关掉引导 tab，不留空 tab（tester 提问后定）
click_tab 1 +; pal_open t_user
check "+ 的引导 tab 上从面板选 t_user（① 里已开着）：切过去，引导 tab 关掉" eval 'tabs_are 1 "1:t_order- │ 2:t_user*" && ! e2e_plain | grep -q "新 tab"'

# ---- 目标 pane 永远不是结果区：焦点在结果区时打开表，进最近聚焦过的普通 pane
key C-l; key g t; key i; e2e_type "select 1 as x"; key Escape; key Enter; wait_for 8 eval '[[ $(bar) != *busy* && -n $(geom 3) ]]'; sleep 0.3
key C-j
check "C-j：焦点到结果区 ③" focus_is 3
pal_open t_sku
check "焦点在结果区时打开 t_sku：开在最近聚焦的 ②，不在结果区" eval 'tabs_are 2 "1:console_1- │ 2:t_order │ 3:t_sku*" && [[ $(tabbar 3) != *t_sku* ]] && focus_is 2 || { echo "  ② $(tabbar 2) | ③ $(tabbar 3)"; false; }'

# ---- t、C-t 的标题都是「打开」，树的提示行没有 t tab（5514975）
to_tree; key '?'; sleep 0.4
check "树上 ? 的键位帮助：↵ 和 t 都是「打开」" eval 'e2e_plain | grep -qE "↵ +→ 打开" && e2e_plain | grep -qE "(^|[│ ])t +→ 打开" && ! e2e_plain | grep -q "新 tab 打开"'
key Escape
check "树的提示行：j/k move · ↵ open，没有 t tab" eval '[[ $(e2e_text 2 31 $(( $(H) - 2 )) | sed "s/ *$//") == " j/k move · ↵ open" ]] || { e2e_text 2 31 $(( $(H) - 2 )); false; }'

e2e_done
