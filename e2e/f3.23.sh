#!/usr/bin/env bash
# F3.23 查询条的工具按钮与自动刷新（specs/m3-console/task.md F3.23；tech-design §7.8「工具按钮」「自动刷新」、§7.7）
# 按钮的顺序、底框、颜色、让位由 golden 覆盖（含自动刷新开着、有请求在跑）；这里在真实 PG 上测：自动刷新跟着别的连接改的数据变、
# 有修改 / 在编辑 / 不可见时跳过、不清掉保存结果；停止按钮取消取数和保存；让位时从面板打开的下拉框锚在哪；
# [icon] 的 fg 只换「亮着」时的颜色。自建库：另一条连接改数据、锁表锁行。有没有发出取数看 pg_stat_activity。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
APP=e2e-f323-$$
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$E2E_DB" "$APP" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
. "$(dirname "$0")/palette.sh"
INFO=#7dcfff WARN=#e0af68 ERROR=#f7768e DIM=#565f89 SELECT=#364a82
AUTO=$(printf '\xef\x80\x97') STOP=$(printf '\xef\x81\x8d')                    # U+F017 auto_refresh、U+F04D stop
psql_n() { psql "$E2E_DB" -At -c "$1"; }
col_x() { e2e_find "$1" "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }'; }
row_y() { echo $(( $(grid_y) + $1 )); }
cell() { local x; x=$(col_x "$1"); e2e_text "$x" $((x + 8)) "$(row_y "$2")" | sed 's/ *│.*//; s/ *$//; s/^ *//'; }   # NAME N
bx() { e2e_find "$1" 3 | cut -d' ' -f1; }                                       # ICON：查询条第二行上这个图标的列
dd() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }'; }
dd_rows() { local g y; g=($(dd)); for ((y = g[1] + 3; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | sed 's/ *$//'; done; }
result() { qb | sed 's/.*   //; s/^ *//; s/ *$//'; }
set_amount() { key g g; key 0; [[ $1 -gt 1 ]] && key $(($1 - 1)) j; key 3 l; key Enter; e2e_type "$2"; sleep 0.2; key Enter; }
auto() { e2e_click "$(bx "$AUTO")" 3; sleep 0.3; e2e_type "$1"; sleep 0.3; key Enter; }   # 点自动刷新按钮，在下拉框里选 2s / 关 …
# mark / fetched：记下服务端的时间；之后本次运行的连接有没有取过 t_order（每条连接只留最后一条语句）
mark() { T0=$(psql_n "select clock_timestamp()"); }
fetched() { [[ -n $(psql_n "select 1 from pg_stat_activity where application_name = '$APP' and query_start > '$T0' and query like '%t_order%'") ]]; }

start -C "$D/own"; open_table t_order; wait_for 8 settled

# ---- 空闲：停止 dim、不可点；自动刷新 info
e2e_move "$(bx "$STOP")" 3; sleep 0.3
check "空闲时停止按钮是 dim 色，悬停不亮（没有请求，不可点）；自动刷新是 info 色、不带间隔" eval 'style_has $(bx "$STOP") 3 fg=$DIM && ! style_has $(bx "$STOP") 3 bg=$SELECT >/dev/null && style_has $(bx "$AUTO") 3 fg=$INFO && [[ $(e2e_text $(( $(bx "$AUTO") + 1 )) $(( $(bx "$AUTO") + 3 )) 3) == "   " ]]'
e2e_move 100 30

# ---- 下拉框：点按钮打开，关 / 2s / 5s / 10s / 30s / 60s；选 2s 后按钮 warn 色带间隔
e2e_click "$(bx "$AUTO")" 3; sleep 0.3
check "点自动刷新按钮：下拉框开在按钮下面，列出 关 2s 5s 10s 30s 60s" eval 'g=($(dd)); [[ ${g[0]} == $(( $(bx "$AUTO") - 1 )) && ${g[1]} == 4 && $(dd_rows | tr "\n" " ") == "关 2s 5s 10s 30s 60s " ]] && mode_is COMMAND || { echo "  [$(dd)] $(dd_rows | tr "\n" ,)"; false; }'
e2e_type 2s; sleep 0.3; key Enter
check "选 2s：按钮 warn 色，画成 <图标> 2s" eval 'style_has $(bx "$AUTO") 3 fg=$WARN && [[ $(e2e_text $(( $(bx "$AUTO") + 1 )) $(( $(bx "$AUTO") + 3 )) 3) == " 2s" ]] && mode_is NORMAL'
psql_n "update t_order set amount = 42 where id = 1" >/dev/null
check "另一条连接改了 id 1：2 秒一轮，表格跟着变（42.00）" wait_for 5 eval '[[ $(cell amount 1) == 42.00 ]]'

# ---- 有未保存的修改、正在编辑单元格时跳过这一轮；按钮照样 warn 色带间隔
set_amount 2 7
psql_n "update t_order set amount = 43 where id = 1" >/dev/null; sleep 4.5
check "有未保存的修改：不刷新（id 1 还是 42.00，修改还在），按钮仍是 warn 色带 2s" eval '[[ $(cell amount 1) == 42.00 && $(cell amount 2) == 7 ]] && style_has $(bx "$AUTO") 3 fg=$WARN && [[ $(e2e_text $(( $(bx "$AUTO") + 1 )) $(( $(bx "$AUTO") + 3 )) 3) == " 2s" ]]'
key r
check "撤回修改之后：接着刷新（43.00）" wait_for 5 eval '[[ $(cell amount 1) == 43.00 ]]'
key Enter; sleep 0.3
psql_n "update t_order set amount = 44 where id = 1" >/dev/null; sleep 4.5
check "正在编辑单元格（没改文字）：不刷新，还在编辑" eval '[[ $(cell amount 1) == 43.00 ]] && mode_is INSERT'
key Escape
check "退出编辑：接着刷新（44.00）" wait_for 5 eval '[[ $(cell amount 1) == 44.00 ]]'

# ---- 刷新不清掉保存结果那行提示
set_amount 2 8; key C-s; wait_for 8 eval '[[ $(result) == 已保存* ]]'
psql_n "update t_order set amount = 45 where id = 1" >/dev/null
check "保存之后自动刷新：数据跟着变（45.00），「已保存 1 行」还在" eval 'wait_for 5 eval "[[ \$(cell amount 1) == 45.00 ]]" && [[ $(result) == "已保存 1 行"* ]] || { echo "  $(result)"; false; }'

# ---- 不是可见 pane 的当前 tab 时跳过：切到别的 tab、被 zoom 挡住（reviewer 在 F3.23 实测）
key C-p; e2e_type "@t_user"; sleep 0.3; key C-t; wait_for 8 settled; sleep 2.5
mark; sleep 4.5
check "切到新开的 t_user：t_order 不是当前 tab，不再取数" eval '! fetched'
check "间隔按 tab 记：t_user 的自动刷新是关着的" eval 'style_has $(bx "$AUTO") 3 fg=$INFO'
key g T; mark
check "切回 t_order：接着刷新" wait_for 5 fetched
two_panes; key C-l; key Space; e2e_type z; sleep 2.5                           # ① | ② 空，zoom ②，① 被挡住
mark; sleep 4.5
check "zoom 另一个 pane、① 被挡住：t_order 不再取数" eval '! fetched && (( $(e2e_panes | awk "\$1 != \"-\"" | wc -l) == 1 ))'
key Space; e2e_type z; mark
check "取消 zoom：接着刷新" wait_for 5 fetched
key Space; e2e_type x; sleep 0.3                                               # 关掉 ②
auto 关
check "选「关」：按钮回到 info 色、不带间隔，不再刷新" eval 'style_has $(bx "$AUTO") 3 fg=$INFO && [[ $(e2e_text $(( $(bx "$AUTO") + 1 )) $(( $(bx "$AUTO") + 3 )) 3) == "   " ]] && { sleep 2.5; mark; sleep 4.5; ! fetched; }'

# ---- 停止：有请求在跑时 error 色、可点，等同 C-c（task.md F3.23 验收：慢查询时点停止能取消）
e2e_lock t_order
key R; sleep 0.5
check "取数在等锁：停止按钮是 error 色" eval '[[ $(bar) == *busy* ]] && style_has $(bx "$STOP") 3 fg=$ERROR'
e2e_click "$(bx "$STOP")" 3; sleep 0.5
check "点停止：取消，toast「查询已取消」，busy 消失，停止回到 dim" eval 'toast_is "查询已取消" && [[ $(bar) != *busy* ]] && style_has $(bx "$STOP") 3 fg=$DIM && [[ $(psql_n "select count(*) from pg_stat_activity where application_name = '"'$APP'"' and wait_event_type = '"'Lock'"'") == 0 ]]'
e2e_unlock
set_amount 3 9
psql "$E2E_DB" -q -c "begin" -c "select 1 from t_order where id = 3 for update" -c "select pg_sleep(60)" -c "commit" >/dev/null 2>&1 & LOCKER=$!
wait_for 5 eval '[[ $(psql_n "select count(*) from pg_locks l join pg_class c on c.oid = l.relation where c.relname = '"'t_order'"' and l.mode = '"'RowShareLock'"' and l.granted") -ge 1 ]]'
key C-s; sleep 0.8
check "保存在等行锁：停止按钮也是 error 色" eval '[[ $(bar) == *busy* ]] && style_has $(bx "$STOP") 3 fg=$ERROR'
e2e_click "$(bx "$STOP")" 3; wait_for 5 eval '[[ $(result) == *已回滚* ]]'
check "点停止：保存取消，「已取消，已回滚」，修改还在" eval '[[ $(result) == "已取消，已回滚" && $(cell amount 3) == 9 ]]'
psql_n "select pg_terminate_backend(pid) from pg_stat_activity where datname = current_database() and query like '%pg_sleep(60)%' and pid <> pg_backend_pid()" >/dev/null; wait $LOCKER 2>/dev/null

# ---- 按钮让位时（默认布局，① 只有 70 列）从面板打开：下拉框锚在查询条第二行的右端，右边对齐 pane 的右边框（a2ec756）
SOLO= start -C "$D/own"; open_table t_order; wait_for 8 settled
check "70 列的 ①：三组按钮都让位了" eval '[[ -z $(bx "$AUTO") && -z $(bx "$STOP") ]]'
key C-p; e2e_type ">自动刷新"; sleep 0.3; key Enter; sleep 0.3
check "面板里执行「自动刷新」：下拉框在查询条下面，右边框和 ① 的右边框同一列" eval 'g=($(dd)); p=($(geom 1)); [[ ${g[1]} == 4 && $(( g[0] + g[2] )) == $(( p[0] + p[2] )) && $(dd_rows | head -1) == 关 ]] || { echo "  dropdown [$(dd)] pane [$(geom 1)]"; false; }'
e2e_type 2s; sleep 0.3; key Enter
psql_n "update t_order set amount = 46 where id = 1" >/dev/null
check "从面板开的 2s 照样生效（按钮看不见）" wait_for 5 eval '[[ $(e2e_text 34 103 $(row_y 1)) == *" 46.00 "* ]]'

# ---- [icon] 的 fg 只换「亮着」时的颜色：停止空闲时照旧 dim，自动刷新关着时照旧 info
mkdir -p "$D/themed/themes"; cp "$D/own/connections.toml" "$D/themed/"
printf 'theme = "x"\n' >"$D/themed/config.toml"
printf '[icon]\nstop = { fg = "#010203" }\nauto_refresh = { fg = "#040506" }\nrow_add = { fg = "#070809" }\nrow_delete = { fg = "#0a0b0c" }\n' >"$D/themed/themes/x.toml"
start -C "$D/themed"; open_table t_order; wait_for 8 settled
ADD=$(printf '\xef\x81\xa7') DEL=$(printf '\xef\x81\xa8')                      # U+F067 row_add、U+F068 row_delete
check "[icon] 覆盖：新增行、删除行换成主题的颜色；停止空闲仍是 dim，自动刷新关着仍是 info" eval 'style_has $(bx "$ADD") 3 fg=#070809 && style_has $(bx "$DEL") 3 fg=#0a0b0c && style_has $(bx "$STOP") 3 fg=$DIM && style_has $(bx "$AUTO") 3 fg=$INFO'
auto 5s
check "自动刷新开着：用主题的 #040506" style_has "$(bx "$AUTO")" 3 fg=#040506
auto 关
e2e_lock t_order; key R; sleep 0.5
check "有请求在跑：停止用主题的 #010203" eval '[[ $(bar) == *busy* ]] && style_has $(bx "$STOP") 3 fg=#010203'
key C-c; e2e_unlock

e2e_done
