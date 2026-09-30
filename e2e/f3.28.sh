#!/usr/bin/env bash
# F3.28 SPC r 显示 / 隐藏结果区（specs/m3-console/task.md F3.28；tech-design §11「结果区」）
# 默认布局；第一次执行后 ③ 结果区在底部全宽 [34,27] 127x18，① ② 在上面 26 行高。隐藏时结果区从布局里拿掉，tab 都留着。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

rtabs_are() { local t; t=$(tabbar 3 | sed -E 's/ {3,}.*//'); [[ $t == "$1" ]] || { echo "  ③ tabs: '$t', want '$1'"; false; }; }   # 结果区的 tab 栏（去掉右边的键位提示）
run() { key Enter; wait_for 8 eval '[[ $(bar) != *busy* ]]'; sleep 0.3; }

start; key C-l
key Space; e2e_type r; sleep 0.3
check "还没有结果区时 SPC r：不做事" eval 'geom_is 2 "105 1 56 44" && [[ -z $(geom 3) ]] && focus_is 2'
key i; e2e_type "select 1 as x"; key Escape; run
check "执行一条 SQL：底部出现结果区 ③" eval 'geom_is 3 "34 27 127 18" && geom_is 2 "105 1 56 26"'
key Space; e2e_type r; sleep 0.3
check "SPC r：结果区消失，console 和 ① 变高（占满 44 行），焦点不动" eval '[[ -z $(geom 3) ]] && geom_is 2 "105 1 56 44" && geom_is 1 "34 1 70 44" && focus_is 2'
key Space; e2e_type r; sleep 0.3
check "再按 SPC r：原样回来，结果 tab 还在" eval 'geom_is 3 "34 27 127 18" && rtabs_are "1:日志- │ 2:console_1 #1*"'
key C-j; key Space; e2e_type r; sleep 0.3
check "焦点在结果区时隐藏：焦点回到上一个聚焦的 ②" eval '[[ -z $(geom 3) ]] && focus_is 2'
run
check "隐藏着再执行：结果区自动出现，结果放进新的一次" eval 'geom_is 3 "34 27 127 18" && rtabs_are "1:日志- │ 2:console_1 #2*"'

# ---- 面板里是开关类命令（5514975）：显示 ON / OFF，执行后面板不关
state() { e2e_plain | grep -a "result.toggle" | grep -oE " (ON|OFF) " | tr -d ' '; }
key C-p; e2e_type ">结果区"; sleep 0.4
check "面板里「显示 / 隐藏结果区」：结果区显示着，是 ON" eval '[[ $(state) == ON ]]'
key Enter; sleep 0.4
check "↵：结果区隐藏，面板不关，变成 OFF" eval 'palette_open && [[ $(state) == OFF && -z $(geom 3) ]]'
key Enter; sleep 0.4
check "再 ↵：结果区回来，ON" eval 'palette_open && [[ $(state) == ON ]] && geom_is 3 "34 27 127 18"'
key Escape

e2e_done
