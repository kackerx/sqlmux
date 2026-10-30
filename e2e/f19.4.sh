#!/usr/bin/env bash
# F19.4 结果不满一页时行数常驻（specs/m19-polish/task.md F19.4；tech-design §7.8「查询条」）
# 第 1 页的行数少于 LIMIT 时总数就是这一页的行数：窄 pane（默认布局的 ① 只有 63 列）里放不下全文就缩成「N 行」，排在让位顺序的最后；
# 转置视图同样；满一页的（t_order 6000 行、LIMIT 100）照旧，计数那一段第一个让位。只读，用共用库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
edit() { key /; clear_in; }
run() { edit; e2e_type "$1"; sleep 0.3; key Enter; wait_for 8 settled; sleep 0.3; }
qbar() { e2e_text 35 97 3; }                       # 默认布局里 ① 的查询条第二行（63 列宽的 pane）

start -S "$D/st"; sleep 1
check "（准备）默认布局：① 只有 63 列宽（窄 pane）" eval '[[ $(e2e_panes | awk "\$1 == 1 { print \$4 }") == 63 ]]'
open_table t_order; wait_for 8 settled
check "t_order（6000 行，满一页）在窄 pane 里：计数那一段让位了，查询条里没有「行」" eval '! qbar | grep -q "行" && qbar | grep -q "PAGE 1/60" || { qbar; false; }'
run "id < 4"
check "WHERE id < 4（3 行，不满一页）：窄 pane 里查询条有「3 行」" eval 'qbar | grep -q "3 行" || { qbar; false; }'
check "ORDER、LIMIT、PAGE 都还在，COLS 先让位了，「3 行」最后才让（ORDER 之后）" eval 'qbar | grep -q "ORDER" && qbar | grep -q "LIMIT 100" && qbar | grep -q "PAGE 1/1" && ! qbar | grep -q "COLS" || { qbar; false; }'
edit; key Enter; wait_for 8 settled; sleep 0.3
check "清掉 WHERE（又是满一页）：「行」又让位了，照旧" eval '! qbar | grep -q "行" && qbar | grep -q "PAGE 1/60" || { qbar; false; }'
run "id < 21"
check "WHERE id < 21：20 行" eval 'qbar | grep -q "20 行"'
key Escape; key T; sleep 0.8
check "转置视图（T）同一条查询条：还是「20 行」" eval 'qbar | grep -q "20 行" || { qbar; e2e_plain | sed -n 1,4p | cut -c34-100; false; }'
key T; sleep 0.5
open_table t_user; wait_for 8 settled
check "t_user（50 行 < LIMIT）：不用加 WHERE，窄 pane 里就是「50 行」" eval 'qbar | grep -q "50 行" || { qbar; false; }'
e2e_stop

# ---- 宽的 pane：放得下全文，不缩
start -x 220 -S "$D/st2"; sleep 1; solo
open_table t_user; wait_for 8 settled
check "窗口 220 列、① 占满：t_user 的全文照常（auto · 50 行 …），不是缩写" eval 'e2e_text 35 218 3 | grep -qE "auto · 50 行" || { e2e_text 35 218 3; false; }'
e2e_done
