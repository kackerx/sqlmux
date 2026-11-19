#!/usr/bin/env bash
# F18.12 删除正在用的连接：事务和在跑的查询（specs/m18-windows-mssql/task.md F18.12；tech-design §7.8「连接管理」）
# play 的 query 里 begin 后写了一行没提交：确认框另起一行「play 有未提交的事务，关闭会回滚。」，y 后那行写入被回滚（另开连接查不到）；
# play 的 query 里 pg_sleep 正在跑：删 play 取消它，界面不卡，服务端上那条查询没了。play 是自建库，只在自建库上写。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'chmod -R u+w "$D" 2>/dev/null; e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -qAt -c "create table t_play2 (id int primary key)" >/dev/null
mkdir -p "$D/c"
(umask 077; printf '[[connection]]\nname = "play"\nengine = "postgres"\ndsn = "%s"\n\n[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" "$SQLMUX_TEST_PG" >"$D/c/connections.toml")
ct() { echo "$E2E_TMP/config/sqlmux/connections.toml"; }
box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }
mgr_open() { e2e_plain | grep -q "┌─ 连接"; }
cmdp() { pal ">$1"; key Enter; sleep 0.7; }
side() { e2e_plain | python3 -c 'import sys; [print(l[1:33].rstrip("│ ")) for l in sys.stdin.read().split("\n")]'; }
no_play() { ! side | noicon | tr -s " " | grep -qE "^ [▾▸] play( |$)" && ! grep -q 'name = "play"' "$(ct)"; }
dlg() { e2e_plain | python3 -c 'import sys, re; t = sys.stdin.read(); m = re.search(r"删除连接[^\n]*", t); print(m.group(0) if m else "")'; }
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; }
sleepers() { psql "$E2E_DB" -At -c "select count(*) from pg_stat_activity where query like '%pg_sleep(120)%' and state = 'active' and pid <> pg_backend_pid()" 2>/dev/null; }
run() { key Enter; wait_for 8 eval '[[ $(bar) != *busy* ]]'; sleep 0.4; }

# ---- 事务里写了一行没提交
SOLO= start -C "$D/c" -S "$D/st"; sleep 1
key C-l; sleep 0.3
setsql "begin"; run
setsql "insert into t_play2 values (99)"; run
check "（准备）play 的 query 里 begin 之后插了一行，状态栏有「事务中」" eval 'bar | grep -q "事务中" || { bar; false; }'
cmdp conn.manage; key x; sleep 0.6
check "删 play：确认框里有「删除连接 play？它开着的 1 个 tab…」，另起一行「play 有未提交的事务，关闭会回滚。」" eval 'e2e_plain | grep -q "删除连接 play？它开着的 1 个 tab" && e2e_plain | grep -qF "play 有未提交的事务，关闭会回滚。" || { e2e_plain | grep -E "删除连接|事务" | cut -c1-140; false; }'
key n; sleep 0.4
check "n：什么都不动——文件里还有 play，事务还在（库里还查不到那一行）" eval 'grep -q "name = \"play\"" "$(ct)" && [[ $(psql "$E2E_DB" -At -c "select count(*) from t_play2") == 0 ]]'
key x; sleep 0.5; key y; sleep 2
check "y：play 删掉了（树里和文件里）" no_play
check "那一行写入被回滚：另开连接查 t_play2 没有 id 99（也没有锁在那里：能 drop 掉这张表）" eval '[[ $(psql "$E2E_DB" -At -c "select count(*) from t_play2") == 0 ]] && timeout 8 psql "$E2E_DB" -qAt -c "set lock_timeout = 3000; drop table t_play2" >/dev/null 2>&1'
check "程序没崩" running
e2e_stop

# ---- 在跑的查询：直接取消，界面不卡
e2e_own_db || exit 1   # e2e_stop dropped the first one
(umask 077; printf '[[connection]]\nname = "play"\nengine = "postgres"\ndsn = "%s"\n\n[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" "$SQLMUX_TEST_PG" >|"$D/c/connections.toml")
SOLO= start -C "$D/c" -S "$D/st2"; sleep 1
key C-l; sleep 0.3
setsql "select pg_sleep(120)"; key Enter; sleep 2
check "（准备）pg_sleep(120) 在跑：状态栏 busy，服务端上看得到这条查询" eval 'bar | grep -q "busy" && [[ $(sleepers) -ge 1 ]] || { bar; false; }'
cmdp conn.manage; key x; sleep 0.6
check "删 play：确认框写它开着的 tab 数（query_1 和跑在它上面的结果 tab 共 2 个），没有事务那一行（查询在跑不拦）" eval 'e2e_plain | grep -q "删除连接 play？它开着的 2 个 tab" && ! e2e_plain | grep -q "未提交"'
key y; sleep 2
check "y：界面没卡住，play 删掉了（树里和文件里）" eval 'running && no_play'
key Escape; sleep 0.3
cmdp conn.manage
check "删完之后界面还响应：能再打开连接管理（只剩 doraemon 和新建）" eval 'mgr_open'
key Escape; sleep 0.3
wait_for 6 eval '[[ $(sleepers) == 0 ]]'
check "服务端上那条 pg_sleep 查询被取消了（6 秒内消失）" eval '[[ $(sleepers) == 0 ]]'
e2e_done
