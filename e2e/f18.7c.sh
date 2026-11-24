#!/usr/bin/env bash
# F18.7 SQL Server（specs/m18-windows-mssql/task.md F18.7 a；tech-design §8.1、§9.3、§13）：只读连接。SQL Server 没有只读事务，靠应用层拦截（Dangerous 认 T-SQL）：
# 写语句整次不发，日志里写「<连接> 是只读连接，整次都没有执行」；省掉 EXEC 的过程调用（sp_rename 'a', 'b'）当 EXEC 拦下；读的照常；表格上 ↵ 编辑 toast「<连接> 是只读连接」。
# 在自建库 sqlmux_e2e_<pid> 上（lib.sh e2e_own_msdb），共用的 sqlmux 库不动。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
[[ -n $SQLMUX_TEST_MSSQL ]] || { echo "e2e: SQLMUX_TEST_MSSQL is not set" >&2; exit 1; }
e2e_own_msdb || { echo "e2e: could not create the private SQL Server database" >&2; exit 1; }
OWN=sqlmux_e2e_$$
mkdir -p "$D/ro"
(umask 077; printf '[[connection]]\nname = "ro"\nengine = "mssql"\ndsn = "%s"\nread_only = true\n' "$E2E_MSDB" >"$D/ro/connections.toml")

ms() { e2e_mssql "$OWN" <<<"$1"; }
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; }
done_run() { wait_for 8 eval '[[ $(bar) != *busy* ]]'; sleep 0.4; }
run() { key Enter; done_run; }
rval() { local g; g=($(geom 3)); e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $((g[1] + 3)) | awk -F'│' '{ gsub(/ /, "", $2); print $2 }'; }
pane_has() { local g y; g=($(geom "$1")); for ((y = g[1]; y < g[1] + g[3]; y++)); do e2e_text "${g[0]}" $((g[0] + g[2] - 1)) "$y"; done | grep -qF -- "$2"; }

start -C "$D/ro"; sleep 2; key C-l
setsql "select count(*) from t_log"; run
check "ro 的 Main：读照常，select count(*) from t_log 是 3" eval '[[ $(rval) == 3 ]] || { echo "  got: $(rval)"; false; }'
bad=
for q in "delete from t_log" "update t_event set kind = N'x'" "insert into t_log (logged_at, msg) values (sysdatetime(), N'x')" "truncate table t_log" "drop table t_log" "alter table t_log add c int" "exec sp_rename 't_log', 't_log2'" "sp_rename 't_log', 't_log2'" "select * into t_copy from t_log"; do
  setsql "$q"; run; pane_has 3 "ro 是只读连接，整次都没有执行" && pane_has 3 "$q" || bad+=" [$q]"
done
check "delete / update / insert / truncate / drop / alter / exec sp_rename / 省掉 EXEC 的 sp_rename / select … into：都整次不发，日志里是「ro 是只读连接，整次都没有执行」" eval '[[ -z $bad ]] || { echo "  not blocked:$bad"; false; }'
check "库里什么都没变：t_log 还在、3 行，没有 t_log2、t_copy、列 c" eval '[[ $(ms "select count(*) from t_log") == 3 && $(ms "select count(*) from sys.tables where name in ('"'"'t_log2'"'"', '"'"'t_copy'"'"')") == 0 && $(ms "select count(*) from sys.columns where name = '"'"'c'"'"' and object_id = object_id('"'"'t_log'"'"')") == 0 && $(ms "select count(*) from t_event where kind = N'"'"'x'"'"'") == 0 ]]'
setsql "select count(*) from t_event"; run
check "拦下之后读的照常：select count(*) from t_event 是 3" eval '[[ $(rval) == 3 ]] || { echo "  got: $(rval)"; false; }'
key C-h; open_table t_user; key l; key Enter
check "ro 的表格上 ↵ 编辑：toast「ro 是只读连接」" eval 'toast_is "ro 是只读连接" && mode_is NORMAL'

# ---- 表单：engine 用空格切成 mssql，端口默认 1433
trow() { local y; for y in $(seq 3 43); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && { echo "$y"; return; }; done; }
CT=$E2E_TMP/config/sqlmux/connections.toml
pal ">conn.new"; key Enter; sleep 0.4
key Tab; sleep 0.2; key Tab; sleep 0.2; for i in 1 2 3 4; do key Space; done; sleep 0.3
check "表单 engine 选 mssql：engine 行是 postgres │ mysql │ sqlite │ redis │ mssql │ duckdb，端口默认 1433" eval 'e2e_plain | grep -qa "engine  postgres │ mysql │ sqlite │ redis │ mssql │ duckdb" && e2e_plain | grep -qa "port 1433"'
key Escape; sleep 0.3; key Escape; sleep 0.3
pal ">conn.new"; key Enter; form_dsn m2 "$SQLMUX_TEST_MSSQL" "" mssql
check "新建连接 engine 切成 mssql：追加的一段是 engine = \"mssql\"，树里多出 m2" eval 'grep -q "^engine = \"mssql\"" <(tail -6 "$CT") && [[ -n $(trow m2) ]] || { tail -8 "$CT"; false; }'
y=$(trow m2); e2e_click "$(e2e_find m2 "$y" | cut -d' ' -f1)" "$y"; wait_for 8 eval '[[ $(e2e_text 1 32 "$(trow m2)") == *"▾ "* ]]'
check "单击 m2：连上并展开（树里有它的 database、dbo、Tables）" eval 's=$(for y in $(seq 3 43); do e2e_text 1 32 "$y"; done); [[ $(e2e_text 1 32 "$(trow m2)") == *"▾ "* && $s == *" dbo "* && $s == *"Tables ("* ]] || { echo "$s" | head -20; false; }'
e2e_done
