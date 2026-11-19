#!/usr/bin/env bash
# F22.12 断开连接（issue #12；specs/m22-menus-keys/task.md F22.12；tech-design §7.8「右键菜单」「断开连接」）
# 两个 PG 连接 ca / cb（各自一份库，都有同名的表 f22_12）：@f22_12 两条；右键 ca →「断开连接」后只剩 cb 的一条，树上 ca 收起；ca 开着的 tab 还在、画着旧数据（库里又加了一行它也不变）；
# 在那个 tab 上 R 重新连上、数据刷新；命令面板里的 conn.disconnect 同；有未提交的事务时先弹确认，n 取消什么都不变、y 断开并回滚。自建两个库，state 放临时目录。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; psql "$SQLMUX_TEST_PG" -qAt -c "drop database if exists sqlmux_e2e_${$}b with (force)" >/dev/null 2>&1; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
DB_B=$(python3 -c 'import sys, urllib.parse as u; print(u.urlsplit(sys.argv[1])._replace(path="/" + sys.argv[2]).geturl())' "$SQLMUX_TEST_PG" "sqlmux_e2e_${$}b")
psql "$SQLMUX_TEST_PG" -qAt -c "create database sqlmux_e2e_${$}b" >/dev/null && psql "$DB_B" -q -v ON_ERROR_STOP=1 -f "$E2E_ROOT/testdata/seed/pg.sql" >/dev/null || exit 1
for d in "$E2E_DB" "$DB_B"; do psql "$d" -q -c "create table f22_12(id int); insert into f22_12 select generate_series(1, 3)"; done
mkdir -p "$D/c"; (umask 077; printf '[[connection]]\nname = "ca"\nengine = "postgres"\ndsn = "%s"\n[[connection]]\nname = "cb"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" "$DB_B" >"$D/c/connections.toml")
psql_a() { psql "$E2E_DB" -At -c "$1"; }
rclick() { _sgr 2 "$1" "$2" M; _sgr 2 "$1" "$2" m; sleep 0.6; }
srow() { local y; for y in $(seq 3 43); do [[ $(e2e_text 1 32 "$y") == *" $1"* ]] && { echo "$y"; return; }; done; }
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }
mitems() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0; for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/^ *//; s/^[^ ]* //; s/ *$//'; done | tr '\n' '|'; }
# 右键连接 NAME，选菜单里的「断开连接」（第三项：选择库… / 刷新 / 断开连接）
disconnect_menu() { local y; y=$(srow "$1"); rclick 12 "$y"; key j; key j; key Enter; sleep 1; }
tables() { pal "@f22_12"; sleep 0.8; list | cut -d'|' -f1 | tr '\n' ';'; key Escape; sleep 0.3; }
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; }
run() { key Enter; wait_for 8 eval '[[ $(bar) != *busy* ]]'; sleep 0.4; }
rows_in_grid() { e2e_plain | grep -cE "│ +[0-9]+ │ +[0-9]+ "; }

start -x 200 -C "$D/c" -S "$D/st"; sleep 1
check "（准备）树里有 ca、cb，ca 已连上" eval '[[ -n $(srow ca) && -n $(srow cb) ]]'
y=$(srow cb); e2e_click 10 "$y"; sleep 0.3; key l; wait_for 8 eval '[[ -n $(e2e_plain | grep "f22_12" | sed -n 2p) ]]'; sleep 0.5
# open_of CONN KEY：面板里 @f22_12 的那一条（CONN.public）用 KEY 打开（Enter 当前 tab，C-t 新 tab）
open_of() { local n; pal "@f22_12"; sleep 0.8; n=$(list | cut -d'|' -f1 | grep -n "$1.public" | cut -d: -f1)
  for ((i = 1; i < n; i++)); do e2e_keys Down; sleep 0.15; done; e2e_keys "$2"; sleep 1.2; }
open_of ca Enter; wait_for 5 eval '[[ $(e2e_plain | head -1) == *" f22_12 "* ]]'
open_of cb C-t; sleep 1
check "@f22_12：两条（cb 和 ca 各一）" eval '[[ $(tables) == *cb.public*ca.public* || $(tables) == *ca.public*cb.public* ]]' 
check "（准备）右键 ca：菜单里有「断开连接」（选择库… / 刷新 R / 断开连接 / 编辑… / 删除… dd）" eval 'y=$(srow ca); rclick 12 "$y"; [[ $(mitems) == *刷新*断开连接*编辑…*删除…* ]] || { echo "  [$(mitems)]"; false; }'
key Escape; sleep 0.3
disconnect_menu ca
check "断开 ca 后 @f22_12 只剩 cb 一条" eval '[[ $(tables) == *cb.public* && $(tables) != *ca.public* ]] || { tables; false; }'
check "树上 ca 收起、回到没连上（▸），cb 还是展开的" eval '[[ $(e2e_text 1 32 $(srow ca) | sed "s/^[^▸▾]*//") == ▸* ]] && [[ $(e2e_text 1 32 $(srow cb)) == *▾* ]]'
check "它开着的 tab 没关：pane ① 还是两个 tab" tabs_are 1 "1:f22_12- │ 2:f22_12*"
click_tab 1 "1:"; sleep 0.8
check "切到 ca 的 tab：状态栏是 ca，还画着旧数据（3 行）" eval '[[ $(bar) == *" ca "* ]] && [[ $(rows_in_grid) == 3 ]] || { bar; rows_in_grid; false; }'
psql_a "insert into f22_12 values (4)" >/dev/null; sleep 1.5
check "库里又加了一行：断开着的 tab 不变（还是 3 行）" eval '[[ $(rows_in_grid) == 3 ]]'
key R; wait_for 10 eval '[[ $(rows_in_grid) == 4 ]]'
check "在这个 tab 上 R：重新连上，数据刷新（4 行）" eval '[[ $(rows_in_grid) == 4 ]] || { rows_in_grid; false; }'
check "重连之后树上 ca 又是已连上的（再展开看得到表），@f22_12 又是两条" eval '[[ $(tables) == *cb.public* && $(tables) == *ca.public* ]] || { tables; false; }'
# 命令面板里的 conn.disconnect：作用于当前 tab 的连接（ca）
pal "断开连接"; check "命令面板里搜得到「断开连接」（conn.disconnect）" eval 'list | grep -q "conn.disconnect"'
key Enter; sleep 1.2
check "面板的断开连接：@f22_12 只剩 cb，tab 还在、数据还是 4 行" eval '[[ $(tables) == *cb.public* && $(tables) != *ca.public* ]] && tabs_are 1 "1:f22_12* │ 2:f22_12-" && [[ $(rows_in_grid) == 4 ]]'
key R; wait_for 10 eval '[[ $(tables) == *ca.public* ]]'
check "R 又连上了" eval '[[ $(tables) == *ca.public* ]]'

# 未提交的事务：ca 的 query 里 begin + insert，不提交
key C-l; sleep 0.3; setsql "begin"; run; setsql "insert into f22_12 values (99)"; run
intx() { psql_a "select count(*) from pg_stat_activity where datname = current_database() and state like 'idle in transaction%'"; }
check "（准备）ca 的 query 里事务开着：状态栏「事务中」，服务端有一个 idle in transaction，库里还看不到 99" eval 'bar | grep -q 事务中 && wait_for 5 eval "[[ \$(intx) == 1 ]]" && [[ $(psql_a "select count(*) from f22_12 where id = 99") == 0 ]] || { intx; false; }'
key Escape; sleep 0.3
y=$(srow ca); rclick 12 "$y"; key j; key j; key Enter; sleep 0.8
check "有未提交的事务：弹确认「ca 有未提交的事务，断开会回滚。」，按钮是「断开」" eval 'screen_has "ca 有未提交的事务，断开会回滚" && screen_has "断开"'
e2e_cap | grep -n "未提交" | head -2
key n; sleep 0.6
check "n：取消，什么都没变——事务还开着、@f22_12 还有 ca" eval '! screen_has "未提交的事务，断开" && [[ $(intx) == 1 && $(tables) == *ca.public* ]]'
y=$(srow ca); rclick 12 "$y"; key j; key j; key Enter; sleep 0.8; key y; sleep 1.5
check "y：断开了，服务端的事务回滚收掉了（没有 idle in transaction、99 不在）" eval 'wait_for 5 eval "[[ \$(intx) == 0 ]]" && [[ $(psql_a "select count(*) from f22_12 where id = 99") == 0 ]] && [[ $(tables) != *ca.public* ]] || { intx; tables; false; }'
check "确认之后 ca 的 query tab 还在" tabs_are 2 "1:query_1*"
key C-l; key C-l; setsql "select 42 as answer"; run; wait_for 8 eval 'e2e_plain | grep -qE "^ *1 · [0-9]+ms"'; sleep 1
check "在它上面执行：照懒连接重连，执行成功（结果区「1 · …ms」，没有报错），@f22_12 又有 ca" eval 'e2e_plain | grep -qE "· [0-9]+ms" && ! e2e_plain | grep -q "连接还没连上" && [[ $(tables) == *ca.public* ]] || { tables; false; }'
e2e_done
