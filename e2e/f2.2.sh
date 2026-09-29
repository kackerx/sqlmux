#!/usr/bin/env bash
# F2.2 保存、刷新与确认框（specs/m2-edit/task.md F2.2；tech-design §10.3–§10.5、§12）
# UPDATE 的生成（单列 / 复合主键、唯一索引、NULL、DEFAULT、json 旧值）由集成测试覆盖；这里在自建库里
# 走真实的保存：结果文字、库里的值、并发修改时的整体回滚、保存中 C-c 取消、R 和关闭 / 退出前的确认框。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
. "$(dirname "$0")/palette.sh"
ERROR=#f7768e
SAVE=$(printf '\xef\x83\x87')                                                 # U+F0C7
psql_n() { psql "$E2E_DB" -At -c "$1"; }
amount() { psql_n "select amount from t_order where id = $1"; }
col_x() { e2e_find "$1" "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }'; }
row_y() { echo $(( $(grid_y) + $1 )); }
saves() { local q; q=$(qb); q=${q#*"$SAVE"}; q=${q%%[!\ 0-9]*}; echo $q; }    # 保存按钮上的修改数（没有就是空）
result() { qb | sed 's/.*   //; s/^ *//; s/ *$//'; }                                # 查询条右侧的文字
# shows ID VALUE：第 ID 行的 amount 格里显示的是 VALUE（修改还在）。保存结果显示期间按钮会让位（§7.8，026fe0e），saves 取不到计数
shows() { e2e_text $(col_x amount) $(( $(col_x amount) + 8 )) $(row_y $1) | grep -q "$2" || { echo "  id $1 amount: $(e2e_text $(col_x amount) $(( $(col_x amount) + 8 )) $(row_y $1))"; false; }; }
# set_col ID N VALUE：光标移到 id = ID 那一行（第 ID 行）的第 N+1 列，改成 VALUE；set_amount ID VALUE 改 amount
set_col() { key g g; key 0; [[ $1 -gt 1 ]] && key $(($1 - 1)) j; [[ $2 -gt 0 ]] && key $2 l; key Enter; e2e_type "$3"; sleep 0.2; key Escape; }
set_amount() { set_col "$1" 3 "$2"; }
# F3.20 起保存失败的原因在 ① 底部的错误栏（lib.sh errbar），查询条右侧只留成功的提示。
# 数据库报错用外键：user_id = 999 过得了前置校验（F3.21），PG 报 23503，带 DETAIL
FK_MSG='insert or update on table "t_order" violates foreign key constraint "t_order_user_id_fkey"'
box() { e2e_plain | grep -oE '[^│]*处修改未保存[^│]*' | head -1 | sed 's/^ *//; s/ *$//'; }   # 确认框里的那句话
box_open() { [[ -n $(box) ]] && mode_is COMMAND; }

start -C "$D/own"; open_table t_order; wait_for 8 settled

# ---- C-s 保存：查询条右侧「已保存 N 行 · 耗时」，库里的值变了，重新加载，修改标记消失
set_amount 1 9.99
key C-s; wait_for 8 eval '[[ $(result) == 已保存* ]]'
check "C-s：查询条右侧「已保存 1 行 · <耗时>」" eval '[[ $(result) =~ ^已保存\ 1\ 行\ ·\ [0-9.]+(µs|ms|s)$ ]] || { echo "  $(result)"; false; }'
check "库里 id 1 的 amount 是 9.99，保存按钮上没有计数，表格重新加载成 9.99" eval '[[ $(amount 1) == 9.99 && -z $(saves) ]] && e2e_text $(col_x amount) $(( $(col_x amount) + 8 )) $(row_y 1) | grep -q 9.99'
set_amount 2 8.88; set_amount 3 7.77
check "改两行：保存按钮计数 2" eval '[[ $(saves) == 2 ]]'
key :; e2e_type w; sleep 0.3; key Enter; wait_for 8 eval '[[ $(result) == "已保存 2 行"* ]]'
check ":w：同样保存，已保存 2 行（按行计），库里两行都变了" eval '[[ $(result) == "已保存 2 行"* && $(amount 2) == 8.88 && $(amount 3) == 7.77 ]] || { echo "  $(result)"; false; }'
set_amount 4 6.66
e2e_click "$(e2e_find "$SAVE" 3 | cut -d' ' -f1)" 3; wait_for 8 eval '[[ $(result) == "已保存 1 行"* ]]'
check "点击保存按钮：同样保存" eval '[[ $(amount 4) == 6.66 && -z $(saves) ]]'

# ---- 失败：从加载到保存之间别的连接改了这一行，整体回滚，修改保留，指出是哪一行
set_amount 10 5.55; set_amount 12 4.44
psql_n "update t_order set amount = 100 where id = 10" >/dev/null
key C-s; wait_for 8 eval '[[ $(errbar 1) == *已回滚* ]]'
check "别的连接改了 id 10：错误栏「id = 10 的行数据已变化或行不存在，已回滚」（不是数据库的错，没有 SQLSTATE），error 色；查询条右侧没有它" eval 'errbar_is 1 "id = 10 的行数据已变化或行不存在，已回滚 ×" && style_has 36 $(errbar_y 1) fg=$ERROR && [[ $(result) != *已回滚* ]] || { echo "  $(result)"; false; }'
check "整体回滚：id 12 也没写进去；两处修改都还在；第 10 行的行号是 error 色" eval '[[ $(amount 12) == 12.99 && $(amount 10) == 100.00 ]] && shows 10 5.55 && shows 12 4.44 && style_has 37 $(row_y 10) fg=$ERROR'
key g g; key 9 j; key 3 l; key Enter; e2e_type 10.99; sleep 0.2; key Escape    # 改回库里原来的 10.99 以外的值会再冲突：先放弃 id 10 这处
key R; key y; wait_for 8 settled

# ---- 数据库报错：错误栏第一行 [SQLSTATE] 加行和 PG 的 Message，下面一行是 DETAIL；修好再保存，错误栏自动消失（F3.20）
set_col 12 1 999
key C-s; wait_for 8 eval '[[ $(errbar 1) == *23503* ]]'
check "user_id 写 999：「[23503] id = 12：<PG 错误的 Message>，已回滚」，不带 ERROR:（§10.3，docs b8ca2c7），第二行是 DETAIL" eval '[[ $(errbar 1 | head -1) == "[23503] id = 12：insert or update on table"*"，已回滚 ×" && $(errbar 1 | sed -n 2p) == "DETAIL: Key (user_id)=(999) is not present in table \"t_user\"." && $(errbar 1 | wc -l) -eq 2 ]] || { errbar 1; false; }'
check "库里没变，修改还在" eval '[[ $(psql_n "select user_id from t_order where id = 12") == 13 ]] && [[ $(saves) == 1 ]]'
set_col 12 1 3
check "编辑单元格不清错误栏" eval '[[ $(errbar 1 | head -1) == "[23503] id = 12："* ]]'
key C-s; wait_for 8 eval '[[ $(result) == 已保存* ]]'
check "改成存在的 user 3 再保存：成功，错误栏自动消失" eval '[[ $(result) == "已保存 1 行"* && -z $(errbar 1) && $(psql_n "select user_id from t_order where id = 12") == 3 ]] || { echo "  $(result)"; errbar 1; false; }'

# ---- 保存中 C-c：取消，回滚，修改保留，「已取消，已回滚」
set_amount 11 3.33
psql "$E2E_DB" -q -c "begin" -c "select 1 from t_order where id = 11 for update" -c "select pg_sleep(60)" -c "commit" >/dev/null 2>&1 & LOCKER=$!
wait_for 5 eval '[[ $(psql_n "select count(*) from pg_locks l join pg_class c on c.oid = l.relation where c.relname = '"'t_order'"' and l.mode = '"'RowShareLock'"' and l.granted") -ge 1 ]]'
key C-s; sleep 0.8
check "id 11 被别的事务锁住：保存在等锁（busy）" eval '[[ $(bar) == *busy* ]]'
key C-c; wait_for 5 eval '[[ $(result) == *已回滚* ]]'
check "C-c：「已取消，已回滚」，修改保留，库里没变" eval '[[ $(result) == "已取消，已回滚" && $(bar) != *busy* ]] && shows 11 3.33 || { echo "  $(result)"; false; }'
psql_n "select pg_terminate_backend(pid) from pg_stat_activity where datname = current_database() and query like '%pg_sleep(60)%' and pid <> pg_backend_pid()" >/dev/null; wait $LOCKER 2>/dev/null
check "锁放开之后：id 11 仍是原值 11.99" eval '[[ $(amount 11) == 11.99 ]]'

# ---- R：有修改时先确认；n 保留，y 丢弃并重新取数
key R
check "有修改时 R：确认框「有 1 处修改未保存，刷新会丢弃。」" eval 'box_open && [[ $(box) == "有 1 处修改未保存，刷新会丢弃。" ]] && e2e_plain | grep -q "y 刷新   n 取消" || { echo "  $(box)"; false; }'
key n
check "n：取消，修改还在" eval '! box_open && shows 11 3.33 && mode_is NORMAL'
key R; key y; wait_for 8 settled
check "y：丢弃修改，重新取数（id 11 回到 11.99）" eval '[[ -z $(saves) ]] && e2e_text $(col_x amount) $(( $(col_x amount) + 8 )) $(row_y 11) | grep -q 11.99'

# ---- 关闭 / 退出前的确认：x、:q、SPC x、:qa、连按两次 C-c；点框外等于取消
set_amount 5 2.22
key x
check "x 关闭有修改的 tab：「t_order 有 1 处修改未保存，关闭会丢弃。」y 关闭" eval '[[ $(box) == "t_order 有 1 处修改未保存，关闭会丢弃。" ]] && e2e_plain | grep -q "y 关闭   n 取消" || { echo "  $(box)"; false; }'
key Escape
check "esc：取消，tab 还在" eval '! box_open && [[ $(e2e_text 34 160 43 | noicon) == *"1:t_order*"* && $(saves) == 1 ]]'
key :; e2e_type q; sleep 0.3; key Enter
check ":q：同样确认" eval '[[ $(box) == "t_order 有 1 处修改未保存，关闭会丢弃。" ]]'
e2e_click 60 10; sleep 0.3
check "点框外：等于取消" eval '! box_open && [[ $(saves) == 1 ]]'
key Space x
check "SPC x 关闭的 pane 里有带修改的 tab：「这个 pane 里有 1 处修改未保存，关闭会丢弃。」" eval '[[ $(box) == "这个 pane 里有 1 处修改未保存，关闭会丢弃。" ]] || { echo "  $(box)"; false; }'
key n
key :; e2e_type qa; sleep 0.3; key Enter
check ":qa：「有 1 处修改未保存，退出会丢弃。」y 退出" eval '[[ $(box) == "有 1 处修改未保存，退出会丢弃。" ]] && e2e_plain | grep -q "y 退出   n 取消" || { echo "  $(box)"; false; }'
key n
key C-c; key C-c
check "连按两次 C-c：同样确认，不直接退出" eval 'running && [[ $(box) == "有 1 处修改未保存，退出会丢弃。" ]]'
key C-c
check "确认框里 C-c 等同 esc：取消" eval 'running && ! box_open'

# ---- 当前 tab 有修改时，从树或面板 ↵ 打开表：新开 tab，原 tab 的修改还在
key C-h; key g g; for ((i = 0; i < 12; i++)); do [[ $(e2e_text 2 31 $(( 4 + i ))) == *" t_user "* ]] && break; done; key $((i)) j; key Enter; wait_for 8 settled
check "树上 ↵ t_user：新开 tab（F3.18 起一律新开；有修改的 t_order 还在）" eval '[[ $(e2e_text 34 160 43 | noicon) == *"1:t_order- │ 2:t_user*"* ]] || { e2e_text 34 160 43; false; }'
key g T
check "回到 t_order：修改还在" eval '[[ $(saves) == 1 ]]'
key :; e2e_type qa; sleep 0.3; key Enter; key y
check "退出确认框里 y：退出，库里没有写入 id 5" eval 'wait_for 3 exited && [[ $(amount 5) == 5.99 ]]'

# ---- 放得下时是 PG 错误的 Message 全文：没有 ERROR: 前缀，也没有 (SQLSTATE …)
start -x 220 -C "$D/own"; open_table t_order; wait_for 8 settled
set_col 12 1 999; key C-s; wait_for 8 eval '[[ $(errbar 1) == *已回滚* ]]'
check "220 列宽：「[23503] id = 12：${FK_MSG}，已回滚」" eval 'errbar_is 1 "[23503] id = 12：${FK_MSG}，已回滚 ×"'

# ---- 放不下时（M3 默认布局，① 只有 70 列）：截短中间的原文加 …，「[23503] id = 12：」和「，已回滚」完整
SOLO= start -C "$D/own"; open_table t_order; wait_for 8 settled
set_col 12 1 999; key C-s; wait_for 8 eval '[[ $(errbar 1) == *已回滚* ]]'
check "错误放不下时：第一行截短中间的原文加 …，两头完整（DETAIL 等行的右截短由 golden 覆盖）" eval 'r=$(errbar 1 | head -1); [[ $r == "[23503] id = 12：insert or update"*"…，已回滚 ×" ]] || { errbar 1; false; }'

e2e_done
