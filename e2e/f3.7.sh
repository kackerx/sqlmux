#!/usr/bin/env bash
# F3.7 pane 混放 tab 与引导页（specs/m3-console/task.md F3.7；tech-design §5「表格和 console 可以放在同一个 pane」「引导页」、§6.4、§11「文件」）
# 引导页和混放后的 tab 栏的画法由 golden 覆盖；这里测点击、按键作用域、打开表的目标 pane、:wq 在真实 PG 上的保存。
# 默认布局 ⓪ | ① [34,103] | ② console_1 [105,160]。自建库：:wq 要写 t_order。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
NF_TABLE=$(printf '\xef\x83\x8e') NF_CONSOLE=$(printf '\xef\x92\x89') SELECT=#364a82
title() { local g; g=($(geom "$1")); e2e_text "${g[0]}" $((g[0] + g[2] - 1)) "${g[1]}"; }
body() { local g y s=; g=($(geom "$1")); for ((y = g[1] + 1; y < g[1] + g[3] - 2; y++)); do s+=$(e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | tr -d ' '); done; echo "$s"; }
landing() { [[ $(body "$1") == *打开表t*新建consolec* ]] || { echo "  ⟨$1⟩ body: $(body "$1" | cut -c1-80)"; false; }; }
btn() { local g y x; g=($(geom "$1")); for ((y = g[1] + 1; y < g[1] + g[3] - 2; y++)); do x=$(e2e_find "$2" $y | tr ' ' '\n' | awk -v l=${g[0]} -v r=$((g[0] + g[2])) '$1 > l && $1 < r { print; exit }'); [[ -n $x ]] && { echo "$x $y"; return; }; done; }   # N LABEL → 引导页按钮的 X Y
mode() { bar | awk '{ print $NF }'; }
psql_n() { psql "$E2E_DB" -At -c "$1"; }
cell_edit() { key i; local i; for ((i = 0; i < 12; i++)); do e2e_keys BSpace; done; e2e_type "$1"; sleep 0.2; key Enter; }   # 改当前格

start -C "$D/own"
check "没有 tab 的 ① 显示引导页：两个按钮带键位 t / c，标题只有 ①" eval 'landing 1 && [[ $(title 1) =~ ^┌─\ ①\ ─+┐$ ]]'
read x y <<<"$(btn 1 打开表)"; e2e_move $x $y; sleep 0.3
check "悬停「打开表」：select 底" style_has $x $y bg=$SELECT
e2e_click $x $y; sleep 0.4
check "点「打开表」：打开面板的表范围（输入 @）" eval 'palette_open && e2e_plain | grep -q "$SEARCH_ICON @ "'
e2e_type t_order; sleep 0.3; key Enter; wait_for 8 eval '[[ -n $(grid_y) ]]'
check "选 t_order：开在 ① 里，标题是 table 图标" eval 'tabs_are 1 "1:t_order*" && [[ $(title 1) == "┌─ ① $NF_TABLE t_order ─"* ]]'

# ---- + 新开引导 tab；「打开表」的表开在这个 tab 里；c 就地换成 console
click_tab 1 +
check "点 +：新开引导 tab「新 tab」并切过去，显示引导页" eval 'tabs_are 1 "1:t_order- │ 2:新 tab*" && landing 1 && focus_is 1'
key t; e2e_type t_order; sleep 0.3; key Enter; wait_for 8 eval '[[ -n $(grid_y) ]]'
check "引导 tab 上按 t 选 t_order：开在这个 tab 里，替换它，不切到已开的 t_order" eval 'tabs_are 1 "1:t_order- │ 2:t_order*"'
click_tab 1 +; key C-p; e2e_type @t_sku; sleep 0.3; key C-t; wait_for 8 eval '[[ -n $(grid_y) ]]'
check "引导 tab 上从面板 C-t 打开：也替换它，不另开（cd4729f）" eval 'tabs_are 1 "1:t_order │ 2:t_order- │ 3:t_sku*"'
key x
click_tab 1 +; key c
check "引导 tab 上按 c：就地换成 console_2，标题 console 图标加 ▶ run ↵" eval 'tabs_are 1 "1:t_order │ 2:t_order- │ 3:console_2*" && [[ $(title 1) == "┌─ ① $NF_CONSOLE console_2 ─"*"▶ run  ↵ ─┐" ]]'
click_tab 1 +; key x
check "引导 tab 上按 x：直接关掉，不确认，回到 console_2" eval 'tabs_are 1 "1:t_order │ 2:t_order │ 3:console_2*" && ! screen_has 关闭会丢弃'
click_tab 1 +; key C-p; e2e_type '>console.new'; sleep 0.3; key Enter
check "引导 tab 上从面板执行 console.new：也就地换成 console_3" eval '[[ $(tabbar 1) == "1:t_order │ 2:t_order │ 3:console_2"*" │ 4:console_3*" ]] || { echo "  $(tabbar 1)"; false; }'
key :; e2e_type q; sleep 0.3; key Enter

# ---- 同一个 pane 里切换 table / console：标题图标、▶ run、按键作用域跟着变（§6.4）
key gT
check "gT 到 t_order：标题 table 图标，没有 ▶ run" eval '[[ $(title 1) == "┌─ ① $NF_TABLE t_order ─"* && $(title 1) != *"▶ run"* ]]'
key /
check "表 tab 上按 /：编辑 WHERE（grid 的键）" eval '[[ $(mode) == INSERT && $(bar) == *"-- editing WHERE --"* ]]'
key Escape; key gt
check "gt 到 console_2：标题 console 图标，有 ▶ run" eval '[[ $(title 1) == "┌─ ① $NF_CONSOLE console_2 ─"*"▶ run  ↵ ─┐" ]]'
key /
check "console tab 上按 /：编辑器的搜索命令行（COMMAND）" eval '[[ $(mode) == COMMAND && $(bar) != *"editing WHERE"* ]]'
key Escape

# ---- 打开表的目标 pane（§5）：当前 tab 是 console 时不替换它
tree_open() { key /; key Escape; key /; e2e_type "$1"; sleep 0.3; key Enter; key Enter; wait_for 8 eval '[[ -n $(grid_y) ]]'; }   # 树里过滤出 NAME 再 ↵
key C-h; tree_open t_log
check "①、② 当前都是 console 时从树 ↵ 打开表：在最近聚焦的 ① 新开 tab，console_2 还在" eval 'tabs_are 1 "1:t_order │ 2:t_order │ 3:console_2- │ 4:t_log*" && tabs_are 2 "1:console_1*" && focus_is 1'
key C-l; key Space; e2e_type q; sleep 0.3; e2e_type 0; sleep 0.3      # ② 聚焦过之后按编号跳到树
tree_open t_sku
check "最近聚焦的 ② 当前是 console、① 当前是表：表开到 ① 的新 tab（F3.18），原来的 t_log 还在，② 不变" eval '[[ $(tabbar 1) == *"4:t_log- │ 5:t_sku*" ]] && tabs_are 2 "1:console_1*" && focus_is 1 || { echo "  $(tabbar 1)"; false; }'

# ---- 分割出的新 pane 显示引导页
key Space; e2e_type %; sleep 0.3
check "SPC % 分出的新 pane：引导页，标题只有 ⟨n⟩" eval 'landing 2 && [[ $(title 2) =~ ^┌─\ ②\ ─+┐$ ]] && focus_is 2'
key Space; e2e_type x; sleep 0.3

# ---- :wq（§11「文件」）：表 tab 上先保存，成功后才关闭；失败时 tab 留着，不弹丢弃确认
# 失败用外键：user_id = 999 过得了前置校验（F3.21），数据库报 23503；原因在 ① 底部的错误栏（F3.20）
start -C "$D/own"; open_table t_order
key l; cell_edit 999
key :; e2e_type wq; sleep 0.3; key Enter; sleep 1.5
check ":wq 保存失败（user_id = 999）：tab 留着、没有丢弃确认，错误栏写原因，库里没变" eval 'tabs_are 1 "1:t_order*" && ! screen_has 关闭会丢弃 && [[ $(errbar 1 | head -1) == "[23503] id = 1："*"，已回滚 ×" && $(psql_n "select user_id from t_order where id = 1") == 2 ]] || { errbar 1; false; }'
cell_edit 3
check "再编辑一次：错误栏还在（编辑单元格不清，F3.20）" eval '[[ $(errbar 1 | head -1) == "[23503] id = 1："* ]]'
key :; e2e_type wq; sleep 0.3; key Enter; sleep 1.5
check ":wq 保存成功：写进库，tab 关掉" eval '[[ $(psql_n "select user_id from t_order where id = 1") == 3 && -z $(grid_y) ]] && ! screen_has t_order\ ─'

e2e_done
