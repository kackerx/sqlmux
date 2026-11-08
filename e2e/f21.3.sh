#!/usr/bin/env bash
# F21.3 命令行 --sql / --schema（specs/m21-cli-outer/task.md F21.3；tech-design §14「启动」）
# sqlmux <连接> --sql "<语句>"：一个 window，只有侧栏、一个聚焦的 query pane（新编号，不是 query_1）和它下面的结果区；语句启动后立刻执行；
# 退出不写 last.toml。--schema：PG 的 schema、Redis 的 db 号；没有的 schema toast 并留在缺省的；--sql 和 --table 同时给是 usage 错误。
# PG 用共用库（只读）和自建库（建 schema、测危险语句），Redis 用私有的 redis-server。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1; redis_seed
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -c "create schema other" -c "create table other.t (id int primary key)" -c "insert into other.t values (1), (2), (3)" \
  -c "create table public.t (id int primary key)" -c "insert into public.t values (1)"
mkdir -p "$D/c" "$D/st"
(umask 077; printf '[[connection]]\nname = "seed"\nengine = "postgres"\ndsn = "%s"\n\n[[connection]]\nname = "own"\nengine = "postgres"\ndsn = "%s"\n\n[[connection]]\nname = "ro"\nengine = "postgres"\ndsn = "%s"\nread_only = true\n\n[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' \
  "$SQLMUX_TEST_PG" "$E2E_DB" "$E2E_DB" "$RPORT" >"$D/c/connections.toml")
printf 'language = "en"\nconfirm_dangerous = true\n' >"$D/c/config.toml"
lastf() { echo "$D/st/sqlmux/last.toml"; }
toast_text() { e2e_text 1 "$(e2e_flag pane_width)" $(( $(H) - 1 )); }
sstart() { e2e_start -C "$D/c" -S "$D/st" "$E2E_BIN $1"; wait_for 6 flag_is alternate_on 1; wait_for 15 settled; sleep 1.5; }
quit_app() { e2e_type ':qa'; e2e_keys Enter; wait_for 5 screen_has '[e2e-exit 0]'; }
npanes() { e2e_panes | awk '$1 != "-" && $1 != 0' | wc -l | tr -d ' '; }
has_row() { e2e_plain | grep -qE "│ +1 │ +$1 +"; }   # the result's first row has this value in its first column
psql_n() { psql "$E2E_DB" -At -c "$1"; }

# ---- 共用库：一条语句、一个结果
sstart "seed --sql 'select count(*) as n from t_order'"
check "seed --sql：结果区的第一行是 t_order 的行数（和 psql 数出来的一样）" eval 'has_row "$(psql "$SQLMUX_TEST_PG" -At -c "select count(*) from t_order")" || { e2e_plain | tail -22; false; }'
check "布局：侧栏、query pane、结果区三块，聚焦在 query pane（①）" eval '[[ $(npanes) == 2 && $(focused) == 1 ]] || { e2e_panes; false; }'
check "query pane 里是这条语句，tab 条只有一个 query（不是 query_1），没有 data pane 的引导页" eval 'e2e_plain | grep -q "select count(\*) as n from t_order" && tabs_are 1 "1:query_2*" && ! e2e_plain | grep -q "query_1" && ! e2e_plain | grep -q "Open a table" || { tabbar 1; false; }'
key g; key t
check "gt：没有别的 tab，还是那一个 query" eval 'tabs_are 1 "1:query_2*"'
quit_app
check ":qa 退出：没有写 last.toml" eval '[[ ! -e "$(lastf)" ]]'

# ---- 连接用位置参数、不给用 default_connection（这里没有写：第一个，seed）
sstart "--sql 'select 41 + 1 as answer'"
check "不给连接：用第一个连接 seed，结果是 42" eval 'bar | grep -q "seed · " && has_row 42 || { bar; false; }'
quit_app

# ---- 多条语句：逐条执行，各一个结果 tab
sstart "seed --sql 'select 1 as a; select 2 as b'"
check "两条语句：结果区有两个结果（日志之外）" eval 'e2e_plain | grep -q "query_2 #1 " && e2e_plain | grep -q "query_2 #1·2" || { e2e_plain | tail -5; false; }'
quit_app

# ---- --schema
sstart "own --schema other --sql 'select count(*) as n from t'"
check "--schema other：跑的是 other.t（3 行），标题里是 other" eval 'has_row 3 && e2e_plain | grep -q "\.other ▾" || { e2e_plain | head -3 | cut -c1-200; false; }'
quit_app
sstart "own --schema nope --sql 'select count(*) as n from t'"
check "--schema nope：toast「own: no schema nope」，用的是缺省的 public.t（1 行）" eval 'toast_text | grep -q "own: no schema nope" && has_row 1 || { toast_text; false; }'
quit_app
sstart "own --sql 'select count(*) as n from t'"
check "（对照）不给 --schema：public.t" eval 'has_row 1'
quit_app

# ---- 确认：危险语句和只读连接照 §13
sstart "own --sql 'delete from public.t'"
check "own --sql 'delete from public.t'（没有 WHERE）：弹确认，还没有执行" eval 'e2e_plain | grep -qi "dangerous" && [[ $(psql_n "select count(*) from public.t") == 1 ]] || { e2e_plain | grep -i danger; false; }'
key Escape; sleep 0.3
check "取消：没有执行，query 还在（里面还是那条语句）" eval '[[ $(psql_n "select count(*) from public.t") == 1 ]] && e2e_plain | grep -q "delete from public.t" && ! e2e_plain | grep -qi "dangerous"'
quit_app
sstart "ro --sql 'delete from public.t where id = 1'"
check "ro（只读连接）--sql 'delete …'：整次都没有执行，日志里写了" eval '[[ $(psql_n "select count(*) from public.t") == 1 ]] && e2e_plain | grep -qi "read-only connection" || { e2e_plain | tail -8; false; }'
quit_app

# ---- Redis
sstart "rd --sql dbsize"
check "rd --sql dbsize：结果是 db0 的键数" eval 'has_row "$(rc dbsize)" || { e2e_plain | tail -10; false; }'
quit_app
sstart "rd --schema 1 --sql dbsize"
check "rd --schema 1 --sql dbsize：结果是 db1 的键数（和 db0 的不同）" eval '[[ $(rc -n 1 dbsize) != $(rc dbsize) ]] && has_row "$(rc -n 1 dbsize)" || { e2e_plain | tail -10; false; }'
quit_app

# ---- usage
ux() { XDG_CONFIG_HOME="$D/c-x" XDG_STATE_HOME="$D/st2" "$E2E_BIN" "$@" 2>&1 </dev/null; echo "rc=$?"; }
mkdir -p "$D/c-x/sqlmux"; cp "$D/c/connections.toml" "$D/c-x/sqlmux/"
check "--sql 和 --table 同时给：stderr 是英文报错，退出码非零，不进界面" eval 'o=$(ux --sql "select 1" --table t_order seed); [[ $o == *"--sql or --table"* && $o != *"rc=0"* && $(echo "$o" | wc -l) == 2 ]] || { echo "$o"; false; }'
check "--sql 和 --layout 同时给：报错、非零" eval 'o=$(ux --sql "select 1" --layout x); [[ $o == *"--sql or --layout"* && $o != *"rc=0"* ]] || { echo "$o"; false; }'
check "--schema 单独给：报错「--schema needs --table or --sql」" eval 'o=$(ux --schema x seed); [[ $o == *"--schema needs --table or --sql"* && $o != *"rc=0"* ]] || { echo "$o"; false; }'
check "usage 行列出 --sql 和 --schema（两个位置参数时打印的那一行）" eval 'ux seed rd | grep -q -- "usage:.*--schema.*--sql"'
check "这些退出都没有写 last.toml" eval '[[ ! -e "$(lastf)" ]]'

# ---- --table 也认 --schema
sstart "own --schema other --table t"
check "--schema other --table t：开出 other.t（3 行）" eval 'tabs_are 1 "1:t*" && has_row 1 && e2e_plain | grep -qE "│ +3 │ +3 " || { tabbar 1; false; }'
quit_app
e2e_done
