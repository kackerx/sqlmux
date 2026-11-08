#!/usr/bin/env bash
# F16.1 Redis 连接（specs/m16-redis/task.md F16.1；tech-design §8.7「连接」、§14、§15）
# engine = "redis"：redis:// 的 DSN（用户名、密码走 ACL，db 在路径里）、HELLO 3、每个 db 是树里一个子连接（连它的 db，不用 SELECT）、
# [redis] 段和连接的 delimiter 的校验、表单里的 redis 引擎（db 一栏、PING 测试）、状态栏的地址块、连接断了自动重拨。
# 每份脚本自己起 redis-server（e2e/redis.sh），seed 是 testdata/seed/redis.txt 去掉两条 EVAL 的那份：db0 30 个键、db1 2 个键。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1; redis_seed
redis_start A --requirepass pw1 --user alice on '>secret' '~*' '+@all' || exit 1
DEAD=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')   # a port nothing listens on
rc -n 3 set marker3 three >/dev/null
rc -n 7 flushdb >/dev/null
mkdir -p "$D/c" "$D/f"
(umask 077; cat >"$D/c/connections.toml" <<TOML
[[connection]]
name = "rd"
engine = "redis"
dsn = "redis://localhost:$RPORT/0"

[[connection]]
name = "d7"
engine = "redis"
dsn = "redis://localhost:$RPORT/7"

[[connection]]
name = "q3"
engine = "redis"
dsn = "redis://localhost:$RPORT?db=3"

[[connection]]
name = "alice"
engine = "redis"
dsn = "redis://alice:secret@localhost:$APORT/0"

[[connection]]
name = "nopw"
engine = "redis"
dsn = "redis://localhost:$APORT/0"

[[connection]]
name = "badpw"
engine = "redis"
dsn = "redis://alice:wrong@localhost:$APORT/0"

[[connection]]
name = "down"
engine = "redis"
dsn = "redis://localhost:$DEAD/0"
TOML
)
nclients() { rc client list | grep -c "name=sqlmux"; }            # the connections sqlmux has to the R server
dbclients() { rc client list | grep "name=sqlmux" | grep -c " db=$1 "; }
side() { e2e_plain | python3 -c 'import re, sys; [print(re.sub("[\ue000-\uf8ff] ?", "", l[1:33]).strip("│ ")) for l in sys.stdin.read().split("\n")]'; }   # the sidebar's rows, the icons off
dbrows() { side | python3 -c "
import re, sys
print(' '.join(m.group(1) for l in sys.stdin for m in [re.match(r'^[▸▾] +(db\d+)', l)] if m), end=' ')"; }   # the db nodes in screen order, with a space after each
dbrow() { side | python3 -c "
import re, sys
for l in sys.stdin:
    m = re.match(r'^([▸▾]) +($1)(?: +(\S+))?\s*\$', l.strip())
    if m: print(' '.join(g for g in m.groups() if g)); break"; }   # one db node as "▾ db0 30"
addr() { bar | grep -oE '[^ ]+ · [^ ]*localhost:[0-9]+[^ ]*' | tail -1; }
nth_row() { local y n=0; for y in $(seq 4 42); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && ((++n == $2)) && { echo "$y"; return; }; done; }
node_x() { e2e_find "$1" "$2" | tr ' ' '\n' | awk '$1 < 32 { print; exit }'; }
click_node() { local y; y=$(nth_row "$1" "${2:-1}"); e2e_click "$(node_x "$1" "$y")" "$y"; sleep 0.4; }
quit_app() { e2e_type ":"; sleep 0.4; e2e_type qa; key Enter; wait_for 8 exited; sleep 0.5; }
toast() { e2e_text 1 "$(e2e_flag pane_width)" "$(( $(H) - 1 ))" | python3 -c 'import sys; print(sys.stdin.read().strip("└─┘│ \n"))'; }
sstart() { e2e_start $SX -C "$D/c" "$E2E_BIN $1"; wait_for 8 flag_is alternate_on 1; sleep 2; }

# ---- 连上：树、状态栏、连接数、HELLO 3
sstart rd
check "sqlmux rd：树是 rd → db0（尾注是键数 $(rc -n 0 dbsize)）；db1 在、没连（▸）" eval '[[ $(dbrows) == "db0 db1 db3 " && $(dbrow db0) == "▾ db0 $(rc dbsize)" && $(dbrow db1) == "▸ db1" ]]' || { dbrows; dbrow db0; dbrow db1; }
check "状态栏的地址块：rd · localhost:<端口>（DSN 里没有用户，没有 user@）" eval '[[ $(addr) == "rd · localhost:$RPORT" ]]' || addr
check "服务端看到的客户端：名字 sqlmux，都在 db0，用的是 RESP3（HELLO 3）" eval 'n=$(nclients); (( n >= 2 )) && [[ $(dbclients 0) == $n ]] && [[ $(rc client list | grep "name=sqlmux" | grep -c " resp=3 ") == $n ]]' || rc client list | grep name=sqlmux | cut -c1-120
N0=$(nclients); echo "  (db0 上 $N0 条连接)"
check "此刻服务端没有 db1 的客户端（展开才连）" eval '[[ $(dbclients 1) == 0 ]]'
y=$(nth_row db1 1); e2e_click "$(node_x db1 "$y")" "$y"; sleep 1.5
check "点开 db1：它自己连上了 db1（客户端里有 db=1），db0 的还在；展开出 other 命名空间，尾注 2" eval '(( $(dbclients 1) >= 2 )) && (( $(dbclients 0) >= 2 )) && side | grep -qE "^[▸▾] +other +2"' || { rc client list | grep name=sqlmux | cut -c1-100; e2e_plain | sed -n 3,30p | cut -c1-40; }
for i in 1 2 3; do key C-h; key g g; key R; sleep 1; done
check "R 重扫几次：连接数不涨（没有泄漏）" eval '(( $(nclients) <= $(dbclients 0) + $(dbclients 1) ))'
quit_app
check "退出 sqlmux：服务端上一条 sqlmux 的连接都不剩" eval 'wait_for 5 eval "[[ \$(nclients) == 0 ]]"' || nclients
_e2e_kill

# ---- DSN 里的 db：空的 db7 也列出来（DSN 指定的），?db=3 也认；每个 db 的树里是它自己的键
sstart d7
check "DSN 指向空的 db7：树里有键的 db0、db1、db3 和 DSN 的 db7 都在，按号排（F16.10：db7 不再排第一），连着的是 db7" eval '[[ $(dbrows) == "db0 db1 db3 db7 " ]]' || { dbrows; e2e_plain | sed -n 3,12p | cut -c1-40; }
check "db7 是空的：没有键，尾注 0；服务端上的客户端在 db=7" eval '(( $(dbclients 7) >= 2 )) && [[ $(dbrow db7) == "▾ db7 0" ]]' || dbrow db7
_e2e_kill
sstart q3
check "?db=3：连着 db3，树里按号 db0 db1 db3、连着的是 db3，里面是 marker3；地址块还是 q3 · localhost:<端口>" eval '[[ $(dbrows) == "db0 db1 db3 " ]] && e2e_plain | grep -q marker3 && (( $(dbclients 3) >= 2 ))' || { dbrows; e2e_plain | sed -n 3,12p | cut -c1-40; }
_e2e_kill

# ---- 用户名密码（ACL）、没有密码、错的密码、连不上
sstart alice
check "alice:secret（ACL 用户）：连上了，地址块 alice · alice@localhost:<端口>，服务端上是 user=alice" eval '[[ $(addr) == "alice · alice@localhost:$APORT" ]] && rca client list | grep name=sqlmux | grep -q "user=alice"' || { addr; rca client list | grep name=sqlmux | cut -c1-200; }
quit_app; _e2e_kill
SX="-x 200" sstart rd
click_node nopw
check "连一个要密码的服务端、DSN 里没有：toast「nopw：NOAUTH Authentication required.」，名字 error 色" wait_for 6 eval 'y=$(nth_row nopw 1); style_has "$(node_x nopw "$y")" "$y" fg=#f7768e >/dev/null && [[ $(toast) == *"nopw：NOAUTH Authentication required."* ]]' || { toast; }
click_node badpw
check "错的密码：toast「badpw：WRONGPASS …」，名字 error 色" wait_for 6 eval 'y=$(nth_row badpw 1); style_has "$(node_x badpw "$y")" "$y" fg=#f7768e >/dev/null && [[ $(toast) == *"badpw：WRONGPASS"* ]]' || { toast; }
click_node down
check "连不上（端口上没人）：toast「down：…」带 dial 的地址，名字 error 色" wait_for 14 eval 'y=$(nth_row down 1); style_has "$(node_x down "$y")" "$y" fg=#f7768e >/dev/null && [[ $(toast) == *"down："*"dial tcp 127.0.0.1:$DEAD"* ]]' || { toast; }
check "这三个失败之后 rd 照常（树里还有 db0）、程序没退" eval 'running && [[ $(dbrows) == *db0* ]]' || dbrows
quit_app; _e2e_kill

e2e_done
