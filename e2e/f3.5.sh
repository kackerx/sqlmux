#!/usr/bin/env bash
# F3.5 WHERE 里的 ;（specs/m3-console/task.md F3.5；tech-design §9.6「只允许一条语句」）
# 词法、分句、读写判定、自动 LIMIT 由 sqlkit 的单测覆盖；这里只测接进 WHERE 之后：不发查询、错误栏报错（F3.20）、不进历史。
# 自建库。有没有发出查询：看 pg_stat_activity 里本次运行的连接最近一条语句（PG 解析失败的也会记在这里）。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); ST=$D/state; SJ=$ST/sqlmux/state.json
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
APP=e2e-f35-$$
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$E2E_DB" "$APP" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
ERROR=#f7768e
where() { key /; clear_in; e2e_type "$1"; sleep 0.2; key Enter; }
last() { psql "$E2E_DB" -At -c "select query from pg_stat_activity where application_name = '$APP'" | tr '\n' ' '; }   # 各连接最近一条语句
hist() { python3 -c 'import json, sys; print("|".join(q["where"] for q in json.load(open(sys.argv[1]))["tables"]["doraemon/public.t_order"]["history"]))' "$SJ" 2>/dev/null; }

start -C "$D/own" -S "$ST"; open_table t_order
where "status = 'done'"; wait_for 8 settled

where "1=1; drop table t_log"; sleep 1
check "WHERE 里有 ;：没有发出查询（各连接最近一条语句里没有它）" eval '[[ -n $(last) && $(last) != *"drop table"* ]] || { echo "  last: $(last)"; false; }'
check "错误栏是 error 色的「WHERE 里不能有 ;」（不是数据库的错，没有 SQLSTATE，F3.20），表格照旧画着 status = 'done' 的数据" eval 'errbar_is 1 "WHERE 里不能有 ; ×" && style_has 36 $(errbar_y 1) fg=$ERROR && [[ -n $(grid_y) ]] && e2e_text 35 159 $(( $(grid_y) + 1 )) | grep -q " done "'
check "回到 NORMAL，输入框保留原文" eval 'mode_is NORMAL && [[ $(where_in) == "1=1; drop table t_log" ]]'
where "note = ';'"
wait_for 8 settled
check "字符串里的 ; 不算：照常发出查询" eval '[[ $(last) == *"note = '"';'"'"* ]] || { echo "  last: $(last)"; false; }'
check "取数成功：错误栏自动消失" eval '[[ -n $(grid_y) && -z $(errbar 1) ]]'
check "t_log 还在" eval '[[ $(psql "$E2E_DB" -At -c "select to_regclass('"'public.t_log'"') is not null") == t ]]'
check "被拒的 WHERE 不进历史" eval '[[ $(hist) == "note = '"';'"'|status = '"'done'"'" ]] || { echo "  history: $(hist)"; false; }'

# 时序：上一页还在路上（锁着 t_order）时执行带 ; 的 WHERE，放锁后旧页回来
e2e_lock t_order
where "id < 10"; wait_for 5 eval '[[ -n $(e2e_waiting $APP) ]]'
where "1=1; drop table t_log"; sleep 0.3
e2e_unlock; wait_for 5 eval '[[ $(bar) != *busy* ]]'; sleep 0.5
check "旧页（id < 10）回来后不画、不清错误栏：表格仍是 note = ';' 的空结果，也没有接着发出带 ; 的 count" eval 'errbar_is 1 "WHERE 里不能有 ; ×" && [[ -n $(grid_y) && -z $(e2e_text 35 159 $(( $(grid_y) + 1 )) | tr -d " ") && $(last) != *"drop table"* ]] || { echo "  last: $(last)"; false; }'

e2e_done
