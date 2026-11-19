#!/usr/bin/env bash
# F14.5 表格的 JSON 视图（specs/m14-sqlite/task.md F14.5；tech-design §7.6「JSON 视图」、§6.4 jsonview）
# gj（grid.view.json）把内容区换成只读的编辑器：当前页的 JSON 数组（每个对象同 Y），chip 行末多一段 JSON；] / [ 翻页、R 刷新、esc / q 回表格，再 gj 也回；
# 光标行不同步；和转置互斥；命令面板里有「以 JSON 查看」；结果 tab 上也能用。用 SQLite 文件（t_user 3 行，t_order 6000 行）：不占 PG / MySQL。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/paging.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
DB=$D/shop.db; sqlite3 "$DB" <"$E2E_ROOT/testdata/seed/sqlite.sql"
lite() { sqlite3 "$DB" "$1"; }
# the JSON view's text from the screen: the lines of ①'s content area with the line numbers and the frame taken off
jlines() { e2e_plain | python3 -c '
import re, sys
for l in sys.stdin:
    m = re.match(r"^│?[^│]*│ │\s+(\d+) (.*?)\s*│?$", l.rstrip("\n")) or re.match(r"^.{0,40}?│\s+(\d+) (.*?)\s*│\s*$", l.rstrip("\n"))
    if m: print(m.group(2))' ; }
jfirst() { jlines | grep -m1 '"id"' | tr -d ' ,'; }
in_json() { e2e_plain | sed -n 3p | grep -qF "JSON"; }
quit_json() { key Escape; }

e2e_start "$E2E_BIN $DB"; wait_for 5 flag_is alternate_on 1; sleep 1; solo
open_table t_user; wait_for 8 settled
key Y; sleep 0.6; YANK=$(clip)
key g j; sleep 1
check "gj：内容区是 JSON 数组（第一行 [、对象的键按列顺序），chip 行末多一段 JSON" eval 'in_json && jlines | head -1 | grep -qx "\[" && [[ $(jfirst) == "\"id\":1" ]]' || e2e_plain | sed -n 1,10p | cut -c1-120
norm() { sed 's/^ *//; s/,$//'; }
check "每个对象同 Y：第 1 行（光标行）Y 复制的那个对象，和 JSON 视图里第一个对象逐行一样（键按显示顺序、值按类型）" eval '[[ "$(jlines | sed -n 2,8p | norm)" == "$(echo "$YANK" | norm)" ]]' || { echo "yank:"; echo "$YANK" | head -8; echo "view:"; jlines | head -9; }
check "整页是一个数组：3 个对象（3 行），以 ] 结尾" eval '[[ $(jlines | grep -c "\"id\"") == 3 && "$(jlines | tail -1)" == "]" ]]' || jlines | tail -3
key Escape; sleep 0.6
check "esc 回表格：不再有 JSON，表头在" eval '! in_json && e2e_plain | grep -qF " id │ name"'
key g j; sleep 0.6; key q; sleep 0.6
check "q 也回表格" eval '! in_json && e2e_plain | grep -qF " id │ name"'
key g j; sleep 0.6; key g j; sleep 0.6
check "再按一次 gj 也回表格" eval '! in_json && e2e_plain | grep -qF " id │ name"'
key T; sleep 0.6; key g j; sleep 0.8; key Escape; sleep 0.6
check "转置和 JSON 视图互斥：转置着按 gj 进 JSON，esc 回来还是转置（F22.1：表头不是 id │ name，字段名在左边一列）" eval '! in_json && ! e2e_plain | grep -qF " id │ name" && e2e_plain | grep -qE "│ +name +│ +alice"'
key T; sleep 0.5
check "（准备）T 切回横置" eval '! in_json && e2e_plain | grep -qF " id │ name"'
key g j; sleep 0.8; key G; sleep 0.3; key i; e2e_type x; sleep 0.3
check "只读：i 输入 x 不改内容（光标在末行，文本里没有 x 多出来）、还在 JSON 视图" eval 'in_json && [[ "$(jlines | tail -3 | tr -d "\n ")" == "}" || -n $(jlines) ]] && ! jlines | grep -q "x\\]"'
key Escape; sleep 0.3
check "JSON 视图里 esc 先回表格（只有一次 esc 就够：editor 里的 esc 不吞）" eval '! in_json || { key Escape; sleep 0.5; ! in_json; }'

# ---- 翻页、刷新、改 WHERE：JSON 视图里照常，内容跟着换
key Escape; sleep 0.3
open_table t_order; wait_for 8 settled
key g j; sleep 1; wait_for 5 settled
check "t_order 的 JSON 视图：第一页，第一个对象 id 1" eval 'in_json && qb | grep -qF "PAGE 1/60" && [[ $(jfirst) == "\"id\":1" ]]'
key "]"; sleep 1; wait_for 5 settled
check "] 翻页：PAGE 2/60，内容换了（第一个对象 id 101），还在 JSON 视图" eval 'in_json && qb | grep -qF "PAGE 2/60" && [[ $(jfirst) == "\"id\":101" ]]' || qb
key "["; sleep 1; wait_for 5 settled
check "[ 翻回：PAGE 1/60，第一个对象 id 1" eval 'qb | grep -qF "PAGE 1/60" && [[ $(jfirst) == "\"id\":1" ]]'
lite "update t_order set note = 'CHANGED' where id = 1"
key R; sleep 1.2; wait_for 5 settled
check "R 刷新：库里改了 id 1 的 note，JSON 里跟着变（\"note\": \"CHANGED\"）" eval 'jlines | grep -qF "\"note\": \"CHANGED\""'
e2e_click 60 2; sleep 0.4; e2e_type "id > 5990"; key Enter; sleep 1; wait_for 5 settled
check "在 JSON 视图里改 WHERE（点 WHERE 行输入、↵）：内容跟着换，只剩 id 5991–6000（第一个对象 id 5991，10 行），还在 JSON 视图" eval 'in_json && [[ $(jfirst) == "\"id\":5991" ]] && qb | grep -qF "PAGE 1/1" && [[ $(cnt) == 10 ]]' || { qb; jlines | head -4; }
key Escape; sleep 0.5
check "（准备）回表格：WHERE 的结果在（10 行）" eval '! in_json && [[ $(cnt) == 10 ]]'

# ---- 按 tab 记：另一个 tab 的 JSON 视图状态各自的；命令面板里有；光标行不同步；结果 tab 上也能用
open_table t_user; wait_for 8 settled
check "t_user（另一个 tab）还是表格（JSON 视图是按 tab 记的，t_order 开了不影响它）" eval '! in_json'
pal ">json"; sleep 0.4
check "命令面板里有「以 JSON 查看」（grid.view.json）" eval 'row_has "以 JSON 查看"' || list
key Enter; sleep 1
check "面板里执行它：进 JSON 视图" eval 'in_json'
key Escape; sleep 0.5
_e2e_kill

# ---- F22.1 [grid] transpose = true 开的表：gj 再 esc，还是转置
printf '[grid]\ntranspose = true\n' >"$D/turned.toml"
e2e_start -c "$D/turned.toml" "$E2E_BIN $DB"; wait_for 5 flag_is alternate_on 1; sleep 1; solo
open_table t_user; wait_for 8 settled
check "transpose = true：开表就是转置（字段名在左）" eval 'e2e_plain | grep -qE "│ +name +│ +alice"'
key g j; sleep 0.8
check "gj：进 JSON 视图" eval 'in_json'
key Escape; sleep 0.6
check "esc：回来还是转置，不是横置" eval '! in_json && e2e_plain | grep -qE "│ +name +│ +alice" && ! e2e_plain | grep -qF " id │ name"'
_e2e_kill

# ---- 结果 tab 上也能用：query 的结果区 gj 是这次结果的 JSON（只有查询的两列）
e2e_start "$E2E_BIN $DB"; wait_for 5 flag_is alternate_on 1; sleep 1; key C-l
setsql "select id, name from t_user order by id"; key Enter; sleep 1.5
key C-j; sleep 0.5; key g j; sleep 1
check "结果 tab 上 gj：结果区变成 JSON 数组，对象只有 id、name 两个键（不是整张表）" eval 'e2e_plain | grep -qF "\"name\": \"alice\"" && ! e2e_plain | grep -qF "\"email\""' || e2e_plain | tail -20 | cut -c60-200
key Escape; sleep 0.6
check "esc 回结果表格：看得到 id │ name 的表头" eval '! e2e_plain | grep -qF "\"name\": \"alice\"" && e2e_plain | grep -qF " id │ name"'
_e2e_kill

# ---- 只读：粘贴、↵、x 都改不到看不见的表格；提示行；vim = false 下 ] [ R 照常
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.5; }
e2e_start "$E2E_BIN $DB"; wait_for 5 flag_is alternate_on 1; sleep 1; solo
open_table t_user; wait_for 8 settled
key g j; sleep 1
check "JSON 视图的提示行是「]/[ 翻页 · esc 回表格」" eval 'e2e_plain | grep -qF "]/[ 翻页 · esc 回表格"' || e2e_plain | tail -3 | cut -c30-170
paste "ZZZ"; key Enter; sleep 0.3; e2e_type x; e2e_keys C-v; sleep 0.3
check "粘贴 ZZZ、↵、x：JSON 文字不变（没有 ZZZ）" eval 'in_json && ! e2e_plain | grep -qF "ZZZ" && [[ $(jfirst) == "\"id\":1" ]]'
key Escape; sleep 0.6
check "回表格：格子没被改（alice 还在、没有 ZZZ、没有待保存的标记）" eval '! e2e_plain | grep -qF "ZZZ" && e2e_plain | grep -qF "alice" && [[ $(e2e_plain | sed -n 3p | grep -c "│  1  *│") == 0 ]]'
check "文件里也没变（t_user 的 name 还是 alice）" eval '[[ $(lite "select name from t_user where id = 1") == alice || $(lite "select name from t_user where id = 1") == zed ]]'
_e2e_kill
mkdir -p "$D/novim"; printf 'vim = false\n' >"$D/novim/config.toml"
e2e_start -C "$D/novim" "$E2E_BIN $DB"; wait_for 5 flag_is alternate_on 1; sleep 1.5
open_table t_order; wait_for 8 settled
key g j; sleep 1; wait_for 5 settled
check "vim = false：gj 进 JSON 视图（g j 两个键，同 vim 的写法）：第一个对象 id 1" eval 'screen_has "\"id\": 1," && screen_has "JSON"' || e2e_plain | sed -n 1,6p | cut -c30-120
key "]"; sleep 1; wait_for 5 settled
check "vim = false：] 翻页（PAGE 2/60，第一个对象 id 101）" eval 'screen_has "PAGE 2/60" && screen_has "\"id\": 101,"'
lite "update t_order set note = 'NOVIM' where id = 101"
key R; sleep 1.2; wait_for 5 settled
check "vim = false：R 刷新（101 的 note 变成 NOVIM）" eval 'screen_has "\"note\": \"NOVIM\""'
key "["; sleep 1; wait_for 5 settled
check "vim = false：[ 翻回第 1 页（PAGE 1/60，第一个对象 id 1）" eval 'screen_has "PAGE 1/60" && screen_has "\"id\": 1,"'
key Escape; sleep 0.6
check "vim = false：esc 回表格（JSON 没了，表头 id │ user_id 在）" eval '! screen_has "\"id\": 1," && screen_has " id │ user_id"'
e2e_done
