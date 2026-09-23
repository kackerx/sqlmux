#!/usr/bin/env bash
# F0.8 命中表与鼠标（specs/m0-skeleton/task.md F0.8；tech-design §7.4、§5「按编号跳转」）
# 全部用注入的 SGR 鼠标序列（e2e_click / e2e_move / e2e_down …，1 起算的列、行）。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

start() { e2e_start "$@" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3; }
L() { e2e_keys Space; e2e_type "$1"; sleep 0.3; }
focused() { e2e_panes | awk '$6 == 1 { print $1 }'; }
geom() { e2e_panes | awk -v n="$1" '$1 == n { print $2, $3, $4, $5 }'; }
nums() { e2e_panes | awk '{ printf "%s ", $1 }'; }
focus_is() { local f; f=$(focused); [[ $f == "$1" ]] || { echo "  focused ⟨${f}⟩, want ⟨$1⟩"; false; }; }
geom_is()  { local g; g=$(geom "$1"); [[ $g == "$2" ]] || { echo "  ⟨$1⟩ at [$g], want [$2]"; false; }; }
w_of() { geom "$1" | awk '{ print $3 }'; }
h_of() { geom "$1" | awk '{ print $4 }'; }
at() { local c; c=$(e2e_find "$1" "$2"); echo "${c%% *} $2"; }   # TEXT Y → "X Y"（第一处）
SELECT=#364a82 WARN=#e0af68
wk_open() { e2e_plain | grep -q '^┌─ SPC '; }
pending_idle() { local c; c=$(e2e_find "C-p" 45); [[ $(e2e_text $((c + 7)) $((c + 7)) 45) == "·" ]]; }
cmdline_open() { [[ $(e2e_text 1 160 45) == *" COMMAND " ]]; }
numbers_shown() { [[ $(e2e_text 16 16 23) == 0 ]]; }

# ---- 点击获得焦点；双击标题缩放 / 还原
start
e2e_click 130 20; sleep 0.3; check "点击 console 内部：console 获得焦点" focus_is 2
e2e_click 10 20;  sleep 0.3; check "点击侧栏：侧栏获得焦点" focus_is 0
e2e_click 60 10;  sleep 0.3; check "点击 data：data 获得焦点" focus_is 1
e2e_dclick 50 1; sleep 0.3
check "双击 data 标题：缩放" eval '[[ $(nums) == "1 " ]] && geom_is 1 "1 1 160 44"'
e2e_dclick 10 1; sleep 0.3
check "在左上角的标题上再双击：还原" eval '[[ $(nums) == "0 1 2 " ]] && geom_is 1 "34 1 70 44"'
e2e_click 50 1; sleep 0.5; e2e_click 50 1; sleep 0.3
check "两次点击间隔超过 400ms：不算双击" eval '[[ $(nums) == "0 1 2 " ]]'
sleep 0.5   # 与上一次点击拉开 400ms 以上
e2e_click 50 1; e2e_click 50 1; e2e_click 50 1; sleep 0.3
check "连点三次：前两次缩放，第三次重新计数（仍是缩放状态）" eval '[[ $(nums) == "1 " ]]'
e2e_dclick 10 1; sleep 0.3

# ---- 拖动（§7.4）：左右分割拖中间 1 列间隔，上下分割拖上面 pane 的下边框
start
e2e_down 104 20; e2e_drag_to 90 20; sleep 0.3
check "按住间隔往左拖：比例实时变化" eval '(( $(w_of 1) == 56 ))'
e2e_drag_to 80 20; e2e_up 80 20; sleep 0.3
check "松开：data 46 列" eval '(( $(w_of 1) == 46 ))'
e2e_move 60 20; sleep 0.3
check "松开后再移动指针：比例不再变化" eval '(( $(w_of 1) == 46 ))'
e2e_down 80 20; e2e_drag_to 1 20; e2e_up 1 20; sleep 0.3
check "拖到最左：停在 10%" eval '(( $(w_of 1) * 100 / 127 == 10 ))'
g=$((34 + $(w_of 1))); e2e_down $g 20; e2e_drag_to 160 20; e2e_up 160 20; sleep 0.3
check "拖到最右：停在 90%" eval 'w=$(w_of 1); (( (w + 1) * 100 / 127 >= 89 && w * 100 / 127 <= 90 ))'
start
L '"'
e2e_down 60 22; e2e_drag_to 60 12; sleep 0.3
check "上下分割：按住上面 pane 的下边框往上拖，比例实时变化" eval '(( $(h_of 1) == 12 ))'
e2e_up 60 12; sleep 0.3
y=$(geom 2 | awk '{ print $2 }'); e2e_down 60 $y; e2e_drag_to 60 30; e2e_up 60 30; sleep 0.3
check "下面 pane 的上边框（标题行）不是拖动柄" eval '(( $(h_of 1) == 12 ))'
start
L z; e2e_down 104 20; e2e_drag_to 80 20; e2e_up 80 20; sleep 0.3; L z
check "缩放时不登记拖动柄：拖动无效" eval '(( $(w_of 1) == 70 ))'

# ---- 细栏、提示、which-key 项：点击执行对应的 Action
start
L b; e2e_click 2 10; sleep 0.3
check "点击 3 列宽的细栏：侧栏展开，但不获得焦点" eval 'geom_is 0 "1 1 32 44" && focus_is 1'
e2e_click $(at "SPC b" 1); sleep 0.3
check "点击侧栏标题上的 SPC b：折叠侧栏" eval '[[ $(e2e_text 1 3 1) == "┌─┐" ]]'
e2e_click 2 10; sleep 0.3
e2e_click $(at "▶ run" 1); sleep 0.3
check "点击 console 标题的 ▶ run：先让 console 获得焦点" focus_is 2
e2e_click 60 10; sleep 0.2
e2e_keys Space; sleep 0.6; e2e_click $(for y in $(seq 30 44); do c=$(e2e_find "b →" $y); [[ -n $c ]] && { echo "$c $y"; break; }; done); sleep 0.3
check "点击 which-key 里的 b：效果同按 b（折叠侧栏），浮层关闭、待输入清空" eval '[[ $(e2e_text 1 3 1) == "┌─┐" ]] && ! wk_open && pending_idle'

# ---- 悬停（§7.4）：提示、+、状态栏按钮、which-key 项为 select 底；▶ run 为 warn 底
start
hover() { e2e_move $1 $2; sleep 0.3; }
read x y <<<"$(at "SPC b" 1)"; hover $x $y
check "悬停侧栏提示 SPC b：select 底" style_has $x $y bg=$SELECT
hover 60 20; check "移开后恢复" style_has $x $y bg=#24283b
read x y <<<"$(at "▶ run" 1)"; hover $x $y
check "悬停 ▶ run：warn 底" style_has $x $y bg=$WARN
hover 60 20; check "移开后恢复 focus 底" style_has $x $y bg=#9ece6a
read x y <<<"$(at "│ +" 43)"; x=$((x + 2)); hover $x $y
check "悬停 tab 栏的 +：select 底" style_has $x $y bg=$SELECT
read x y <<<"$(at "C-p" 45)"; hover $x $y
check "悬停状态栏 C-p 入口：select 底（含图标）" eval 'style_has $x $y bg=$SELECT && style_has $((x - 2)) $y bg=$SELECT'
hover 60 20
e2e_keys Space; sleep 0.6
read x y <<<"$(for y in $(seq 30 44); do c=$(e2e_find "s →" $y); [[ -n $c ]] && { echo "$c $y"; break; }; done)"; hover $x $y
check "悬停 which-key 的一项：select 底" style_has $x $y bg=$SELECT
e2e_keys Escape; sleep 0.2

# ---- 滚轮（§7.4）：作用于指针下方的 pane，每格 3 行，到顶停住
start
row4() { e2e_text "$1" "$2" "$3"; }
d0=$(row4 35 60 5); c0=$(row4 106 150 2)   # data 第 5 行是表格的第一行数据（F0.9）
e2e_wheel 130 20 down; sleep 0.3
check "指针在 console 上滚动：console 滚动、data 不动、焦点不变" eval '[[ $(row4 106 150 2) != "$c0" && $(row4 35 60 5) == "$d0" ]] && focus_is 1'
e2e_wheel 60 20 down; sleep 0.3
check "data 上滚一格：前进 3 行（689 → 692），表头不动" eval '[[ $(row4 35 60 5) == *692* && $(row4 35 60 3) == *biz_type* ]]'
e2e_wheel 60 20 up; e2e_wheel 60 20 up; e2e_wheel 60 20 up; sleep 0.3
check "往上滚到顶就停住" eval '[[ $(row4 35 60 5) == *689* ]]'
s0=$(row4 2 31 4); e2e_wheel 10 10 down; sleep 0.3
check "侧栏滚动的是表列表（agent → mt_task，3 行）" eval '[[ $s0 == *" agent "* && $(row4 2 31 4) == *mt_task* ]]'

# ---- 点击浮层外部（§7.4）
start
e2e_keys Space; sleep 0.6; e2e_click 60 5; sleep 0.3
check "which-key 打开时点击任意处：关闭浮层、清空待输入" eval '! wk_open && pending_idle'
L q; e2e_click 130 20; sleep 0.3
check "SPC q 时点击 console：跳到 console，编号关闭" eval 'focus_is 2 && ! numbers_shown'
L q; e2e_click 10 30; sleep 0.3
check "SPC q 时点击侧栏：跳到 ⟨0⟩" eval 'focus_is 0 && ! numbers_shown'
L q; e2e_click 80 45; sleep 0.3
check "SPC q 时点击状态栏：只关闭编号" eval 'focus_is 0 && ! numbers_shown'
e2e_type ':'; sleep 0.3
check "之后的按键不被吞：: 打开命令行" cmdline_open
e2e_keys Escape; sleep 0.2
L q; e2e_click 33 20; sleep 0.3; e2e_type ':'; sleep 0.3
check "SPC q 时点击间隔：只关闭编号，随后 : 照常生效" eval 'focus_is 0 && cmdline_open'
e2e_keys Escape; sleep 0.2

e2e_done
