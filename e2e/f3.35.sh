#!/usr/bin/env bash
# F3.35 悬停 tab 显示关闭按钮（specs/m3-console/task.md F3.35；tech-design §7.8「tab 栏」）
# × 的画法（标记的位置、不移位）由 golden 覆盖；这里测真实终端里的悬停命中、点 × 关的是哪一个 tab、有修改时的确认、结果区的 tab。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

SELECT=#364a82
CLOSE=$(printf '\xef\x80\x8d')                                                 # close：nf-fa-times U+F00D
x_at() { e2e_find "$CLOSE" "$(tab_y "$1")" | cut -d' ' -f1; }                  # N：pane N 的 tab 栏上 × 的列（没有就是空）
hover() { local x; x=$(tab_x "$1" "$2"); e2e_move $((x + 4)) "$(tab_y "$1")"; sleep 0.3; }   # N TAB：指针停在这个 tab 的名字上
box() { e2e_plain | grep -oE '[^│]*处修改未保存[^│]*' | head -1 | sed 's/^ *//; s/ *$//'; }

start; two_tabs; key C-p; e2e_type "@t_sku"; sleep 0.3; key Enter; wait_for 8 settled   # 1:t_user │ 2:t_order- │ 3:t_sku*
x3=$(tab_x 1 3:t_sku); m2=$(( $(e2e_find "t_order-" "$(tab_y 1)" | cut -d' ' -f1) + 7 )); s1=$(( $(e2e_find "t_user " "$(tab_y 1)" | cut -d' ' -f1) + 6 ))
check "指针不在 tab 上：没有 ×" eval '[[ -z $(x_at 1) ]]'
hover 1 2:t_order
check "悬停 tab 2（上一个 tab，标记 -）：- 的位置换成 ×，后面的 tab 不移位" eval '[[ $(x_at 1) == $m2 && $(tab_x 1 3:t_sku) == $x3 ]] || { echo "  × at [$(x_at 1)], want $m2"; false; }'
hover 1 1:t_user
check "悬停没有标记的 tab 1：× 画在名字后面的空格上，tab 2 的 - 回来" eval '[[ $(x_at 1) == $s1 && $(tab_x 1 3:t_sku) == $x3 && -n $(e2e_find "t_order-" "$(tab_y 1)") ]] || { echo "  × at [$(x_at 1)], want $s1"; false; }'
e2e_move "$(x_at 1)" "$(tab_y 1)"; sleep 0.3
check "指针停在 × 上：select 底" style_has "$(x_at 1)" "$(tab_y 1)" bg=$SELECT
hover 1 2:t_order; e2e_click "$(x_at 1)" "$(tab_y 1)"; sleep 0.4; e2e_move 100 30; sleep 0.3   # 指针移开，免得画出下一个 tab 的 ×
check "点 tab 2 的 ×：关掉 t_order，当前 tab 还是 t_sku" eval 'tabs_are 1 "1:t_user │ 2:t_sku*" && [[ $(e2e_plain | head -1) == *" t_sku ─"* ]]'

# ---- 点的既不是当前也不是上一个 tab：当前 tab 和上一个的 - 都不变
key C-p; e2e_type "@t_log"; sleep 0.3; key Enter; wait_for 8 settled             # 1:t_user │ 2:t_sku- │ 3:t_log*
hover 1 1:t_user; e2e_click "$(x_at 1)" "$(tab_y 1)"; sleep 0.4; e2e_move 100 30; sleep 0.3
check "点 tab 1 t_user 的 ×：关掉它，t_log 仍是当前、t_sku 仍标 -" tabs_are 1 "1:t_sku- │ 2:t_log*"
key C-p; e2e_type "@t_user"; sleep 0.3; key Enter; wait_for 8 settled; click_tab 1 1:t_sku   # 1:t_sku* │ 2:t_log │ 3:t_user-

# ---- 有修改的 tab：点 × 照样确认（同 x）；确认框弹出时、取消之后，当前 tab 都不变
click_tab 1 3:t_user; key l; key Enter; e2e_type x; key Enter; click_tab 1 1:t_sku   # t_user 的 name 改一格，回到 t_sku
hover 1 3:t_user; e2e_click "$(x_at 1)" "$(tab_y 1)"; sleep 0.4; e2e_move 100 30; sleep 0.3
check "点有修改的 t_user 的 ×：弹确认框「t_user 有 1 处修改未保存，关闭会丢弃。」，当前 tab 不切（还是 t_sku）" eval '[[ $(box) == "t_user 有 1 处修改未保存，关闭会丢弃。" && $(e2e_plain | head -1) == *" t_sku ─"* ]] || { echo "  $(box)"; false; }'
key n
check "n：取消，t_user 还在，当前 tab 不变（还是 t_sku）" eval '[[ -z $(box) ]] && tabs_are 1 "1:t_sku* │ 2:t_log │ 3:t_user-" && [[ $(e2e_plain | head -1) == *" t_sku ─"* ]]'
hover 1 3:t_user; e2e_click "$(x_at 1)" "$(tab_y 1)"; sleep 0.4; e2e_move 100 30; key y
check "再点、y：关掉 t_user" tabs_are 1 "1:t_sku* │ 2:t_log"

# ---- tab 多到放不下：pane 右边框那一格不是 tab，也不是 ×（默认布局，① 只有 70 列）
SOLO= start; for tb in t_order t_user t_sku t_log t_event t_order_item agent goal; do key C-p; e2e_type "@$tb"; sleep 0.3; key Enter; sleep 0.4; done; wait_for 8 settled
g=($(geom 1)); rb=$((g[0] + g[2] - 1)); before=$(tabbar 1)
e2e_move "$rb" "$(tab_y 1)"; sleep 0.3
check "指针在 ① 右边框那一格：不出 ×" eval '[[ -z $(x_at 1) ]]'
e2e_click "$rb" "$(tab_y 1)"; sleep 0.4; e2e_move 60 20; sleep 0.3
check "点右边框那一格：不会关掉看不见的 tab，tab 一个不少" eval '[[ $(tabbar 1) == "$before" ]] && ! e2e_plain | grep -q 处修改未保存 || { echo "  before: $before"; echo "  after:  $(tabbar 1)"; false; }'

# ---- 结果区的 tab 同样有 ×；「日志」tab 不能关、不画 ×
SOLO= start; key C-l; key i; e2e_type "select 1 as x"; key Escape; key Enter; wait_for 8 eval '[[ $(bar) != *busy* && -n $(geom 3) ]]'; sleep 0.3
hover 3 1:日志
check "悬停结果区的「日志」tab：不画 ×" eval '[[ -z $(x_at 3) ]]'
hover 3 2:console_1
check "悬停结果 tab：画 ×" eval '[[ -n $(x_at 3) ]]'
e2e_click "$(x_at 3)" "$(tab_y 3)"; sleep 0.4
check "点 ×：关掉这个结果 tab，只剩日志" eval '[[ $(tabbar 3 | sed -E "s/ {3,}.*//") == "1:日志*" ]] || { tabbar 3; false; }'

e2e_done
