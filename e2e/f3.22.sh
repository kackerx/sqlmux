#!/usr/bin/env bash
# F3.22 撤回一格的修改、改动行号标黄（specs/m3-console/task.md F3.22；tech-design §10.1「r 撤回光标所在格的修改」）
# 行号的颜色由 golden 覆盖；这里在真实终端里按 r，看撤回的是哪一格、行号颜色和保存按钮的计数跟着变。
# 保存失败时行号 error 色优先见 f2.2.sh；新增行、删除行上的 r 见 f3.24.sh。不写库，用共用的 sqlmux 库。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

WARN=#e0af68 FOCUS=#9ece6a DIM=#565f89
SAVE=$(printf '\xef\x83\x87')                                                 # U+F0C7
col_x() { e2e_find "$1" "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }'; }
row_y() { echo $(( $(grid_y) + $1 )); }
cell() { local x; x=$(col_x "$1"); e2e_text "$x" $((x + 8)) "$(row_y "$2")" | sed 's/ *│.*//; s/ *$//; s/^ *//'; }   # NAME N
saves() { local q; q=$(qb); q=${q#*"$SAVE"}; q=${q%%[!\ 0-9]*}; echo $q; }    # 保存按钮上的修改数（没有就是空）
rowno_fg() { local c y; y=$(row_y "$1"); c=$(e2e_find ┼ "$(grid_y)"); e2e_style $((${c%% *} - 2)) "$y" | grep -oE 'fg=[^ ]+' | cut -c4-; }   # N：第 N 行行号个位的颜色
set_cell() { key Enter; e2e_type "$1"; sleep 0.2; key Enter; }

start; open_table t_order; wait_for 8 settled
key 3 l; set_cell 5; key 4 l; set_cell hello; key j; key 4 h; set_cell 6        # 第 1 行 amount、note，第 2 行 amount
check "改了三格：第 1、2 行的行号是 warn 色（光标所在的第 2 行也是），第 3 行照旧 dim；保存按钮计数 3" eval '[[ $(rowno_fg 1) == $WARN && $(rowno_fg 2) == $WARN && $(rowno_fg 3) == $DIM && $(saves) == 3 ]] || { echo "  $(rowno_fg 1) $(rowno_fg 2) $(rowno_fg 3) saves $(saves)"; false; }'
key k; key 4 l; key r
check "第 1 行的 note 上按 r：只有这一格恢复成 <null>，amount 还是 5，行号仍是 warn 色，计数 2" eval '[[ $(cell note 1) == "<nul"* && $(cell amount 1) == 5 && $(rowno_fg 1) == $WARN && $(saves) == 2 ]] && mode_is NORMAL'
key r
check "没有修改的格上按 r：什么都不变" eval '[[ $(cell note 1) == "<nul"* && $(saves) == 2 ]] && ! screen_has 撤回'
key 4 h; key r
check "再撤回第 1 行的 amount：恢复成 1.99，第 1 行没有修改了，行号回到当前行的 focus 色，计数 1" eval '[[ $(cell amount 1) == 1.99 && $(rowno_fg 1) == $FOCUS && $(saves) == 1 ]]'
key j; key r
check "撤回第 2 行的 amount：全部撤回，行号恢复，保存按钮上没有计数" eval '[[ $(cell amount 2) == 2.99 && $(rowno_fg 2) == $FOCUS && $(rowno_fg 1) == $DIM && -z $(saves) ]]'

# ---- 结果区的表格不能编辑，r 不做事
SOLO= start; key C-l
key i; e2e_type "select 1 as a"; key Escape; key Enter; wait_for 8 eval '[[ $(bar) != *busy* ]]'; sleep 0.5
key C-j; before=$(e2e_plain | md5)
key r; sleep 0.3
check "结果区上按 r：什么都不变，没有提示" eval '[[ $(focused) == 3 && $(e2e_plain | md5) == "$before" ]] && running'

e2e_done
