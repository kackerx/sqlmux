#!/usr/bin/env bash
# F22.12 断开连接的边界（审查时点名的三条；tech-design §7.8「断开连接」）
# A. 查询 / 取数还在跑时断开：不冒「canceled」之类的报错，tab 留着旧数据，服务端那条查询没了；
# B. 展开另一个库的拨号还没回来（经 1.5 s 往返的代理）时断开：那个库不能自己又连上（对照：不断开时它连得上）；
# C. 断开之后在它的 tab 上按 gd：不崩，重连后照样跳；D. 断开后它的 query 里补全不再有它的表，打字不会把它连回来。自建两个库，state 放临时目录。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); PX=
trap 'e2e_stop; [[ -n $PX ]] && kill $PX 2>/dev/null; psql "$SQLMUX_TEST_PG" -qAt -c "drop database if exists sqlmux_e2e_${$}b with (force)" >/dev/null 2>&1; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
DB_B=sqlmux_e2e_${$}b
psql "$SQLMUX_TEST_PG" -qAt -c "create database $DB_B" >/dev/null || exit 1
psql "$E2E_DB" -q -c "create table f22_12(id int); insert into f22_12 select generate_series(1, 3)"
PGPORT_=$(python3 -c 'import sys, urllib.parse as u; print(u.urlsplit(sys.argv[1]).port)' "$SQLMUX_TEST_PG")
SPORT=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
python3 "$(dirname "$0")/slowproxy.py" "$SPORT" "$PGPORT_" 1500 >"$D/slow.log" 2>&1 & PX=$!
for i in $(seq 50); do grep -q '^up' "$D/slow.log" 2>/dev/null && break; sleep 0.1; done
SLOW_DSN=$(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(p._replace(netloc=p.netloc.rsplit(":", 1)[0] + ":" + sys.argv[3], path="/" + sys.argv[2]).geturl())' "$SQLMUX_TEST_PG" "sqlmux_e2e_$$" "$SPORT")
mkdir -p "$D/c" "$D/p"
(umask 077; printf '[[connection]]\nname = "ca"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/c/connections.toml"; printf '[[connection]]\nname = "px"\nengine = "postgres"\ndsn = "%s"\n' "$SLOW_DSN" >"$D/p/connections.toml")
psql_a() { psql "$E2E_DB" -At -c "$1"; }
rclick() { _sgr 2 "$1" "$2" M; _sgr 2 "$1" "$2" m; sleep 0.6; }
srow() { local y; for y in $(seq 3 43); do [[ $(e2e_text 1 32 "$y") == *" $1"* ]] && { echo "$y"; return; }; done; }
disconnect_menu() { local y; y=$(srow "$1"); rclick 12 "$y"; key j; key j; key Enter; sleep 0.8; }
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; }
bad() { e2e_plain | grep -iE "canceled|cancelled|context|deadline|closed|EOF" | head -3; }   # 「查询已取消」是取消在跑的查询时本来就有的提示，不算
sleepers() { psql "$SQLMUX_TEST_PG" -At -c "select count(*) from pg_stat_activity where query like '%pg_sleep(30)%' and state = 'active' and pid <> pg_backend_pid()"; }

# ---- A. 在途的请求
start -x 200 -C "$D/c" -S "$D/st"; sleep 1
open_table f22_12; key C-l; sleep 0.3
setsql "select pg_sleep(30)"; key Enter; sleep 1.5
check "（准备）pg_sleep(30) 在跑：服务端有一条 active" eval '[[ $(sleepers) == 1 ]]'
disconnect_menu ca; sleep 1.5
check "A1 查询在跑时断开：屏幕上没有 canceled / 取消 / context 之类的报错" eval '[[ -z $(bad) ]] || { bad; false; }'
check "A2 服务端那条 pg_sleep(30) 没了（取消 / 连接关了）" eval 'wait_for 8 eval "[[ \$(sleepers) == 0 ]]"'
check "A3 query tab 还在、程序没崩" eval 'running && tabs_are 2 "1:query_1*"'
key C-l; sleep 0.3
check "A4 data pane 的 tab 还画着旧数据（3 行）" eval '[[ $(e2e_plain | grep -cE "│ +[0-9]+ │ +[0-9]+ ") == 3 ]]'
e2e_lock f22_12
key R
check "（准备）data tab 上 R：重连后取数被表锁住在等" eval 'wait_for 10 eval "[[ -n \$(psql_a \"select 1 from pg_stat_activity where wait_event_type = '"'Lock'"' and datname = current_database() limit 1\") ]]"'
disconnect_menu ca; sleep 1.5
check "A5 取数在等时断开：没有 canceled 之类的报错，tab 还是旧数据" eval '[[ -z $(bad) && $(e2e_plain | grep -cE "│ +[0-9]+ │ +[0-9]+ ") == 3 ]] || { bad; false; }'
e2e_unlock
_e2e_kill

# ---- B. 展开另一个库的拨号没回来时断开（px 经 1.5 s 的往返）
dbconns() { psql "$SQLMUX_TEST_PG" -At -c "select count(*) from pg_stat_activity where datname = '$DB_B' and pid <> pg_backend_pid()"; }
start -x 200 -C "$D/p" -S "$D/st2"; sleep 1
wait_for 15 eval '[[ -n $(srow px) ]]'; key C-h; sleep 0.3
y=$(srow px); e2e_click 10 "$y"; sleep 0.2; key l
wait_for 30 eval '[[ -n $(srow "$DB_B") ]]'; sleep 1
y=$(srow "$DB_B"); e2e_click 10 "$y"; sleep 0.3; key l; sleep 12
check "B0 对照：不断开时，展开 $DB_B 之后服务端有它的连接" eval '[[ $(dbconns) -ge 1 ]]'
_e2e_kill
psql "$SQLMUX_TEST_PG" -qAt -c "select pg_terminate_backend(pid) from pg_stat_activity where datname = '$DB_B'" >/dev/null; sleep 1
start -x 200 -C "$D/p" -S "$D/st3"; sleep 1
wait_for 15 eval '[[ -n $(srow px) ]]'; key C-h; sleep 0.3
y=$(srow px); e2e_click 10 "$y"; sleep 0.2; key l
wait_for 30 eval '[[ -n $(srow "$DB_B") ]]'; sleep 1
y=$(srow "$DB_B"); e2e_click 10 "$y"; sleep 0.3; key l      # the dial of $DB_B starts: 1.5 s a round trip
disconnect_menu px
sleep 14
check "B1 拨号没回来时断开：$DB_B 没有自己又连上（服务端没有它的连接）" eval '[[ $(dbconns) == 0 ]] || { dbconns; false; }'
check "B2 树上 px 收起、没有被重新展开，程序没崩" eval 'running && [[ $(e2e_text 1 32 $(srow px)) == *▸* ]]'
_e2e_kill

# ---- C. 断开之后在它的 tab 上 gd
mkdir -p "$D/c2"; (umask 077; printf '[[connection]]\nname = "ca"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/c2/connections.toml")
SOLO=1 start -x 200 -C "$D/c2" -S "$D/st4"; sleep 1
open_table t_order_item; sleep 1
disconnect_menu ca
key C-l; sleep 0.3
e2e_keys g; sleep 0.1; e2e_keys d; sleep 3
check "C1 断开后在 t_order_item 的 tab 上 gd：程序没崩" eval 'running'
check "C2 断开后的第一个 gd：重新连上了（树上 ca 又是展开的）；这一下没有跳、也没有 toast（观察：要再按一次，见批次结论）" eval 'wait_for 8 eval "[[ \$(e2e_text 1 32 \$(srow ca)) == *▾* ]]"'
e2e_keys g; sleep 0.1; e2e_keys d; sleep 2
check "C3 再按一次 gd：跳到 t_order，WHERE \"id\" = 1" eval '[[ $(e2e_text 34 198 1 | noicon) == *"t_order ─"* && $(e2e_text 36 150 2) == *"\"id\" = 1"* ]] || { e2e_text 34 198 1; false; }'
_e2e_kill

# ---- D. 补全
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }
start -x 200 -C "$D/c2" -S "$D/st5"; sleep 1
key C-l; key C-l; key i; e2e_type "select * from f22_"; sleep 1.2
check "（对照）连着时 query 里 select * from f22_ 补得出 f22_12" eval '[[ -n $(pop) ]] && e2e_plain | grep -q f22_12'
key Escape; key g g; key d G
disconnect_menu ca
key C-l; key C-l; key i; e2e_type "select * from f22_"; sleep 2
check "D1 断开后同样输入：没有补全框、屏幕上没有 f22_12" eval '[[ -z $(pop) ]] && ! e2e_plain | grep -q f22_12'
check "D2 打字补全不会把 ca 连回来（树上还是 ▸）" eval '[[ $(e2e_text 1 32 $(srow ca)) == *▸* ]]'
e2e_done
