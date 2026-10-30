#!/usr/bin/env bash
# F5.16 新命令：pane.focus.next、query.format、tab.quit、tab.reopen、session.new（specs/m5-workspace/task.md F5.16；tech-design §6.8「M5 验收后加的命令」）
# 五个命令都没有默认键：keymaps.toml 的 [keys.global] 里各绑一个 F 键（F2–F6），在真实终端里按。顺时针的顺序、格式化和撤销、tab.quit 的几种情况、
# tab.reopen 的记录与恢复有单测；这里测界面上看得到的结果。只读，用共用库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
cat >"$D/keymaps.toml" <<'T'
[keys.global]
"<F2>" = "pane.focus.next"
"<F3>" = "tab.quit"
"<F4>" = "tab.reopen"
"<F5>" = "session.new"
"<F6>" = "query.format"
T
begin() { e2e_stop; start -K "$D/keymaps.toml" "$@"; }

paste() { t set-buffer -b e2e -- "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
settle() { wait_for 8 settled; }
cline() { e2e_text 104 159 $(($1 + 1)) | sed 's/ *$//'; }                     # query 的第 N 行（默认布局，② 在右边）
clines() { local i; for ((i = 1; i <= $1; i++)); do cline $i; done; }
cy() { echo $(( $(e2e_flag cursor_y) )); }
sname() { bar | sed 's/^ //; s/ ▾.*//'; }
wins() { bar | sed -E 's/^ [^▾]*▾  //; s/ {3,}.*//'; }
panes() { e2e_panes | awk '$1 != "-" { print $1 }' | tr '\n' ' '; }
orbit() { local n=$1 i out=; for ((i = 0; i < n; i++)); do key F2; out+="$(focused) "; done; echo "$out"; }

# ---- pane.focus.next：顺时针（从 12 点起）。默认布局：树在左、① 在中、② query 在右，树和 ① 的中心同一水平线，近的在前
begin
check "（准备）默认布局：焦点在 ①" focus_is 1
check "F2：① → ② → 树 → ①（② 在 3 点钟方向最先；树和 ① 的中心在同一水平线，角度相同时按编号：树在前）" eval '[[ $(orbit 3) == "2 0 1 " ]] || { echo "  $(orbit 0)"; false; }'
key C-l; key i; paste "select 1 as a"; key Escape; key Enter; wait_for 8 eval '[[ $(panes) == *3* ]]'; settle
check "（准备）② 执行：结果区 ③ 出现在 ② 下面" eval '[[ $(panes) == "0 1 2 3 " ]] && g2=($(geom 2)) && g3=($(geom 3)) && (( g3[1] > g2[1] ))'
check "有结果区时从 ② 起：② → ③ → 树 → ① → ②（各 pane 的中心相对界面中心的角度：② 81°、③ 128°、树 270°、① 306°）" eval '[[ $(focused) == 2 && $(orbit 4) == "3 0 1 2 " ]] || { echo "  $(orbit 0)"; false; }'
e2e_keys Space; e2e_type r; sleep 0.4
check "SPC r 收起结果区：③ 不算在内；① 回到整高，和树在同一水平线，角度相同按编号：② → 树 → ① → ②" eval '[[ $(panes) == "0 1 2 " && $(orbit 3) == "0 1 2 " ]] || { echo "  $(panes) | $(orbit 0)"; false; }'
e2e_keys Space; e2e_type b; sleep 0.4
e2e_keys Space; e2e_type b; sleep 0.4   # F19.5：第一个 SPC b 聚焦树，第二个才收起、焦点回 ②
check "再收起树（SPC b 两次）：只剩 ① 和 ②，F2 在两个之间来回" eval 'o=$(orbit 4); [[ $o == "1 2 1 2 " ]] || { echo "  $o"; false; }'

# 一排满高 [树|1|2|3]：树 → 1 → 2 → 3 → 树（同一水平线上，右边的 ②③ 先，左边的树 ① 后，各自按编号）
begin; key C-l; e2e_keys Space; e2e_type %; sleep 0.6
check "（准备）② 右边分出 ③，一排四个满高的 pane，焦点在 ③" eval '[[ $(panes) == "0 1 2 3 " ]] && focus_is 3'
check "F2：③ → 树 → ① → ② → ③（编号顺序 树 → 1 → 2 → 3 → 树）" eval '[[ $(orbit 4) == "0 1 2 3 " ]] || { echo "  $(orbit 0)"; false; }'
# ① 上下分开（1 在上、3 在下）：树 → 1 → 2 → 3 → 树
begin; e2e_keys Space; e2e_type '"'; sleep 0.6
check "（准备）① 分成上 ① 和下 ②（新 pane，焦点在它），query 变成 ③（编号按位置）" eval '[[ $(panes | tr " " "\n" | sort | tr "\n" " ") == "0 1 2 3 " ]] && g1=($(geom 1)) && g2=($(geom 2)) && g3=($(geom 3)) && (( g2[1] > g1[1] && g3[0] > g1[0] )) && focus_is 2'
check "F2：下 ② → 树 → 上 ① → query ③ → 下 ②（树 → 左上 → 右 → 左下，按中心的角度顺时针：树 270°、① 315°、③ 90°、② 225°）" eval '[[ $(orbit 4) == "0 1 3 2 " ]] || { echo "  $(orbit 0)"; false; }'

# ---- query.format：第一条语句开头到最后一条结尾之间格式化，外面的注释、空行不动；一次 u 撤回；光标留在原处（F8.3）；INSERT 下不做
begin; key C-l; key i
paste $'-- head\nselect 1,2 from t_user where id=1 order by id;\n\nselect 2\n-- tail'
key Escape; key g g; key 0; key j; key 0
check "（准备）NORMAL，光标在第一条语句里（第 2 行）" eval '[[ $(cline 1) == "-- head" && $(cy) == 2 ]]'
key F6; sleep 1.5
check "F6：第一行的注释还在原处，语句拆成多行（select / from / where / order by 各占行）" eval '[[ $(cline 1) == "-- head" && $(cline 2) == "select" ]] && clines 14 | grep -qx "from" && clines 14 | grep -qx "where" && clines 14 | grep -qx "order by"'
check "语句之间的空行、最后一条语句照格式化；末尾的注释 -- tail 还是最后一行" eval 'n=$(clines 20 | grep -c .); [[ $(clines 20 | grep -v "^ *$" | tail -1) == "-- tail" ]] && clines 20 | grep -qx "select" '
check "光标留在原处（F8.3）：第 2 行行首" eval '[[ $(cy) == 2 && $(( $(e2e_flag cursor_x) - 103 )) == 0 ]] || { echo "  cursor $(cy):$(( $(e2e_flag cursor_x) - 103 ))"; false; }'
key u
check "u 一次撤回：回到格式化之前的五行（-- head、长语句、空行、select 2、-- tail）" eval '[[ $(cline 1) == "-- head" && $(cline 2) == "select 1,2 from t_user where id=1 order by id;" && $(cline 4) == "select 2" && $(cline 5) == "-- tail" ]] || { clines 6; false; }'
key j; key j; key 0; c0="$(cy):$(( $(e2e_flag cursor_x) - 103 ))"; key F6; sleep 1.5
check "光标在第二条语句里时 F6：两条语句都格式化，光标留在原来的行号和列号（F8.3，不再移到语句开头）" eval '[[ $(clines 30 | grep -c "^select$") == 2 && "$(cy):$(( $(e2e_flag cursor_x) - 103 ))" == "$c0" ]] || { echo "  cursor $(cy):$(( $(e2e_flag cursor_x) - 103 )) want $c0"; clines 20; false; }'
key u; key i; key F6; sleep 1
check "INSERT 下 F6：不做事，文字不变" eval '[[ $(cline 2) == "select 1,2 from t_user where id=1 order by id;" ]]'
key Escape

# ---- session.new：和 session 列表里的 n 一样，用当前连接
begin
check "（准备）一个 session doraemon" eval '[[ $(sname) == doraemon ]]'
key F5; sleep 0.4; key Enter; wait_for 8 eval '[[ $(sname) == 1 ]]'; sleep 0.5
check "F5 session.new：新 session 1，一个 window 0: data*，一个空 pane（F8.18），连着当前连接（侧栏有 Tables (6)）" eval '[[ $(sname) == 1 && $(wins) == "0: data*" && $(panes) == "0 1 " ]] && e2e_plain | head -12 | grep -q "Tables (6)"'

# ---- tab.quit：关 tab；pane 空了关 pane；window 里只剩这个空 pane 就关 window；最后一个 window 不关
begin; key C-l; key C-l 2>/dev/null; key C-h
two_tabs
check "（准备）① 里两个 tab：t_user、t_order，焦点在 ①" eval 'focus_is 1 && tabs_are 1 "1:t_user- │ 2:t_order*"'
key F3
check "pane 里还有 tab：F3 只关当前 tab（t_order），pane 还在" eval 'tabs_are 1 "1:t_user*" && [[ $(panes) == "0 1 2 " ]]'
key F3
check "关到 pane 空了、window 里还有别的 pane：F3 关掉这个 pane（树和 query 还在）" eval '[[ $(panes) == "0 1 " && $(wins) == "0: data*" ]] && tabs_are 1 "1:query_1*"'
begin
open_table t_order
key F3
check "pane 空了（引导页），window 里有 query pane：关掉 ①，剩树和 query" eval '[[ $(panes) == "0 1 " ]] && ! e2e_plain | grep -q "打开表" && tabs_are 1 "1:query_1*"'
key F3
check "只剩 query 这一个 pane、window 是 session 里最后一个：F3 关掉 query tab，pane 和 window 不关，留下引导页" eval '[[ $(panes) == "0 1 " ]] && e2e_plain | grep -q "打开表" && [[ $(wins) == "0: data*" ]]'
key F3
check "最后一个 window 里最后一个空 pane：再按 F3 什么都不做" eval '[[ $(panes) == "0 1 " && $(wins) == "0: data*" ]] && running'

# 两个 window：F8.18 起新 window 只有一个空 pane，F3 连 window 一起关
begin; e2e_keys Space; e2e_type c; sleep 0.6
check "（准备）SPC c：window 1，只有一个空 pane ①（引导页），焦点在 ①" eval '[[ $(wins) == "0: data  1: data-2*" && $(panes) == "0 1 " ]] && focus_is 1'
key F3
check "它是 window 里唯一的 pane、pane 本来就空：F3 连 window 一起关，回到 window 0" eval '[[ $(wins) == "0: data*" ]] && [[ $(panes) == "0 1 2 " ]]'
# window 里有别的 pane：空 pane 关掉，再关最后的 query，window 跟着关
e2e_keys Space; e2e_type c; sleep 0.6; e2e_keys Space; e2e_type %; sleep 0.4; key c; key C-h
check "（准备）window 1 分出 ②、c 新建 query_2，焦点回到空的 ①" eval '[[ $(wins) == "0: data  1: data-2*" && $(panes) == "0 1 2 " ]] && focus_is 1 && tabs_are 2 "1:query_2*"'
key F3
check "按下时 pane 本来就空、有别的 pane：F3 关掉这个 pane" eval '[[ $(panes) == "0 1 " && $(wins) == "0: data  1: data-2*" ]]'
key F3
check "pane 里的最后一个 tab 关了、它是 window 里唯一的 pane：连 window 一起关，回到 window 0" eval '[[ $(wins) == "0: data*" ]] && [[ $(panes) == "0 1 2 " ]]'

# 有未保存修改的 tab：走 tab.close 的确认；n 不关，y 关掉并接着关空 pane
begin; open_table t_user; key l; key Enter; e2e_type x; key Enter
key F3
check "表 tab 有 1 处未保存修改：F3 弹确认框，tab 还在" eval 'screen_has "未保存" && screen_has "y " && [[ $(panes) == "0 1 2 " ]]'
key n
check "n：不关，tab 和修改都还在" eval '! screen_has "┌─ 确认" && tabs_are 1 "1:t_user*" && [[ $(panes) == "0 1 2 " ]]' || e2e_plain | tail -25
key F3; key y
check "再 F3、y：tab 关了，pane 空了跟着关" eval '[[ $(panes) == "0 1 " ]]'

# ---- tab.reopen：只记逐个关掉的 tab，恢复 WHERE / ORDER / LIMIT；连按往前翻；原 pane 没了开在当前 pane；query 回到原来的文件
# （tab.close 关到空 pane 时 pane 也跟着关，所以用「① 里还有别的 tab」的布局测「原 pane 还在」）
qbar() { e2e_text 36 103 3; }
wh() { e2e_text 36 95 2 | sed -E 's/ {2,}(([^ ]+) ){0,2}▾ *$//; s/^ *WHERE *//; s/ *$//'; }
t1() { tabbar 1 | sed 's/-//g; s/\*//g'; }
begin
key F4; sleep 0.4
check "没有关过的 tab：F4 什么都不做，不弹 toast" eval '[[ $(panes) == "0 1 2 " ]] && ! screen_has "没有" && running'
two_tabs; settle
key /; clear_in; e2e_type "id < 50"; key Enter; settle
key g l; key C-n; key Enter; settle
order_by amount; settle
check "（准备）① 里 t_user、t_order，t_order：WHERE id < 50、ORDER amount、LIMIT 500" eval 'tabs_are 1 "1:t_user- │ 2:t_order*" && [[ $(wh) == "id < 50" && $(qbar) == *"ORDER amount"* && $(qbar) == *"LIMIT 500"* ]]'
key x
check "x 关掉 t_order：① 里只剩 t_user" eval 'tabs_are 1 "1:t_user*" && [[ $(panes) == "0 1 2 " ]]'
key F4; settle
check "F4：t_order 回到 ①，WHERE、ORDER、LIMIT 原样，从第 1 页开始" eval '[[ $(t1) == "1:t_user │ 2:t_order" ]] && [[ $(wh) == "id < 50" && $(qbar) == *"ORDER amount"* && $(qbar) == *"LIMIT 500"* && $(qbar) == *"PAGE 1/"* ]] || { echo "  $(tabbar 1) | $(wh) | $(qbar)"; false; }'
key F4; sleep 0.4
check "列表空了：再按 F4 什么都不做（不多开 tab）、不弹 toast" eval 'tabs_are 1 "1:t_user- │ 2:t_order*" && ! screen_has "没有"'

# 连按往前翻
begin; two_tabs; e2e_keys C-p; sleep 0.3; e2e_type "@t_sku"; sleep 0.3; e2e_keys C-t; sleep 0.8
check "（准备）① 里 t_user、t_order、t_sku" eval '[[ $(t1) == "1:t_user │ 2:t_order │ 3:t_sku" ]]'
key x; key x
check "（准备）先关 t_sku、再关 t_order：只剩 t_user" eval 'tabs_are 1 "1:t_user*"'
key F4; settle
check "F4：先开最近关的 t_order" eval '[[ $(t1) == "1:t_user │ 2:t_order" ]]'
key F4; settle
check "再 F4：往前翻，t_sku 也回来" eval '[[ $(t1) == "1:t_user │ 2:t_order │ 3:t_sku" ]] && tabs_are 1 "1:t_user │ 2:t_order- │ 3:t_sku*" || { tabbar 1; false; }'

# 这张表在这个 pane 里已经开着：只切过去，不多开
key x; open_table t_sku; settle
key F4; settle
check "t_sku 关掉后又手工打开；F4 的最近一条是 t_sku：只切过去，tab 数不变" eval '[[ $(t1) == "1:t_user │ 2:t_order │ 3:t_sku" ]] && [[ $(tabbar 1) == *"3:t_sku"* ]]' || tabbar 1

# 整个 pane 关掉：里面的 tab 不记
begin; two_tabs; settle
e2e_keys Space; e2e_type x; sleep 0.5
check "SPC x 关掉 ① 整个 pane（t_user、t_order 一起）" eval '[[ $(panes) == "0 1 " ]] && tabs_are 1 "1:query_1*"'
key F4; sleep 0.4
check "整个 pane 关掉时里面的 tab 不记：F4 什么都不做" eval '[[ $(panes) == "0 1 " ]] && tabs_are 1 "1:query_1*"'

# 原来的 pane 没了：开在当前 pane
begin; open_table t_order; settle
key x
check "（准备）t_order 是 ① 唯一的 tab：x 关掉后 pane 也没了，剩树和 query" eval '[[ $(panes) == "0 1 " ]] && tabs_are 1 "1:query_1*"'
key F4; settle
check "原 pane 没了：F4 开在当前 pane（query 所在的）里，t_order 是当前 tab" eval 'tabs_are 1 "1:query_1- │ 2:t_order*"'

# query：回到原来的文件、文字还在
begin; key C-l; key i; paste "select 42 as marker"; key Escape
e2e_type ":"; sleep 0.2; e2e_type q; key Enter; sleep 0.6
check "（准备）:q 关掉 query_1，它所在的 pane 没了" eval '[[ $(panes) == "0 1 " ]] && ! e2e_plain | grep -q "select 42 as marker"'
key F4; sleep 1
check "F4：query_1 回来，文字还在" eval 'tabbar 1 | grep -q "query_1" && e2e_plain | grep -q "select 42 as marker"'

e2e_done
