#!/usr/bin/env bash
# F17.8 外部执行器引擎，Redis 方言（specs/m17-polish-exec/task.md F17.8；tech-design §8.8、§14）
# engine = "exec"、dialect = "redis"、command = [argv 模板，{cmd} 是命令名、{args...} 单独一项、展开成剩下的词]：一条命令一次子进程，
# stdout 是回复本身的 JSON；树（SCAN，不对每个键发 TYPE，键画通用图标）、键 tab（打开时才 TYPE）、控制台都走它；一个 db（0）、只读；
# SELECT / AUTH / CLIENT 这类连接自己的话不交给命令；命令失败时 stderr 原文是错误。假命令（下面的 fake.py）用 RESP 连自己起的 redis-server，把回复转成 JSON。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/cellops.sh"
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1; redis_seed
cat >"$D/fake.py" <<'PY'
#!/usr/bin/env python3
# a fake exec command (Redis dialect): fake.py LOG PORT -- CMD ARGS... -> the reply as JSON on stdout; an error reply is stderr and exit 2
import json, socket, sys
log, port = sys.argv[1], int(sys.argv[2])
words = sys.argv[4:] if len(sys.argv) > 3 and sys.argv[3] == "--" else sys.argv[3:]
with open(log, "a") as f: f.write(" ".join(words) + "\n")
import os
if os.path.exists(log + ".deny") and words[0].upper() == "COMMAND":
    sys.stderr.write("denied: COMMAND is not for you\n"); sys.exit(2)
if any(w.upper() == "FAILME" for w in words):
    sys.stderr.write("denied: FAILME is not for you\n"); sys.exit(2)
s = socket.create_connection(("127.0.0.1", port)); rf = s.makefile("rb")
s.sendall(b"*%d\r\n" % len(words) + b"".join(b"$%d\r\n%s\r\n" % (len(w.encode()), w.encode()) for w in words))
def read():
    line = rf.readline().rstrip(b"\r\n"); t, rest = line[:1], line[1:]
    if t == b"+": return rest.decode()
    if t == b"-": sys.stderr.write(rest.decode() + "\n"); sys.exit(2)
    if t == b":": return int(rest)
    if t == b"$":
        n = int(rest)
        if n < 0: return None
        data = rf.read(n + 2)[:n]; return data.decode("utf-8", "replace")
    if t == b"*":
        n = int(rest)
        return None if n < 0 else [read() for _ in range(n)]
    sys.stderr.write("bad reply\n"); sys.exit(2)
print(json.dumps(read()))
PY
LOG=$D/calls.log; : >"$LOG"
mkdir -p "$D/c"
(umask 077; cat >"$D/c/connections.toml" <<TOML
[[connection]]
name = "rdx"
engine = "exec"
dialect = "redis"
command = ["python3", "$D/fake.py", "$LOG", "$RPORT", "--", "{cmd}", "{args...}"]
TOML
)
lg() { cat "$LOG"; }
ntype() { lg | grep -ic "^type "; }
side() { e2e_plain | python3 -c 'import re, sys; [print(re.sub("[-] ?", "", l[1:33]).rstrip("│ ")) for l in sys.stdin.read().split("\n")]'; }
nth_row() { side | python3 -c "
import re, sys
n = int(sys.argv[2])
for i, l in enumerate(sys.stdin.read().split('\\n')):
    if re.search(sys.argv[1], l.strip()):
        n -= 1
        if n == 0: print(i + 1); break" "$1" "${2:-1}"; }
click_row() { local y; y=$(nth_row "$@"); [[ -n $y ]] || return 1; e2e_click 14 "$y"; sleep 1.2; }
quit_app() { e2e_type ":"; sleep 0.4; e2e_type qa; key Enter; wait_for 8 exited; sleep 0.5; }
runq() { e2e_click 110 6; sleep 0.3; key Escape; key g g; key d G; key i; e2e_type "$1"; key Escape; key Enter; sleep "${2:-2}"; }
has() { e2e_plain | grep -qF -- "$1"; }
keyicon() { e2e_plain | python3 -c '
import re, sys
for l in sys.stdin:
    m = re.match(r"^│ +(.) user +│", l)
    if m: print("U+%04x" % ord(m.group(1))); break'; }
ptitle() { e2e_plain | sed -n 1p | python3 -c "import re, sys; t = sys.stdin.read(); print(re.sub('^[①-⑳⓪] *', '', re.sub('[-] ?', '', t[34:97]).strip('┌─┐ ')).strip(' ─'))"; }

nd=$(lg | grep -ic "^dbsize"); e2e_start -C "$D/c" "$E2E_BIN rdx"; wait_for 8 flag_is alternate_on 1
wait_for 20 eval '[[ $(lg | grep -ic "^dbsize") -gt $nd ]]'; sleep 1   # connecting takes about 4 s here (the same on 79e6ca3): the tree is up when the SCAN has started
check "连接时先跑一次 PING（假命令日志第一条）；SELECT / CLIENT / AUTH 这类连接自己的话没有交给命令" eval '[[ $(lg | head -1 | tr a-z A-Z) == "PING" ]] && ! lg | grep -qiE "^(select|client|auth|hello) " || { lg | head -4; false; }'
check "树：rdx → db0（只有这一个 db，真服务端的 db1 也有键，但 exec 连接只有 db 0）展开，里面有命名空间（str、hash、list…）" eval 'side | grep -qE "^ *▾ +db0" && ! side | grep -qE "^ *[▸▾] +db1( |$)" && side | grep -qE "^ *▸ +str( |$)" && side | grep -qE "^ *▸ +hash( |$)" || { side | head -16; false; }'
check "到现在为止命令没有收到过 TYPE（树不对每个键发 TYPE，一批键 = 一批子进程）" eval '[[ $(ntype) == 0 ]] || { lg | grep -i "^type" | head -3; false; }'
click_row '^[▸▾] +hash( |$)'
check "展开 hash 命名空间：hash:user 的键节点出来，日志里还是没有 TYPE，SCAN 是有的" eval 'side | grep -qE "^ *user$" && [[ $(ntype) == 0 ]] && lg | grep -qi "^SCAN "' || { side | head -20; lg | tail -3; false; }
check "键节点是通用的 key 图标（U+f084）：没有 TYPE 就不知道是不是 hash" eval '[[ $(keyicon) == U+f084 ]] || { keyicon; false; }'
y=$(nth_row '^user$'); key C-h; key g g; for ((i = 4; i < y; i++)); do key j; done; key Enter; sleep 2
check "↵ 打开 hash:user：键 tab，标题 hash:user；打开时才发 TYPE hash:user" eval '[[ $(ptitle) == "hash:user" ]] && lg | grep -qi "^TYPE hash:user"' || { ptitle; lg | tail -4; false; }
check "表格是 hash：field / value 四行（age email meta name），值和 redis-cli 一致" eval 'e2e_plain | grep -qE "│ +1 │ age +│ 30" && e2e_plain | grep -qE "│ +4 │ name +│ Ann" || { e2e_plain | sed -n 1,10p | cut -c30-150; false; }'
key g g; key l; key Enter; sleep 0.5
check "格子进不了编辑（exec 连接只读）：toast「rdx 是只读连接」" eval 'mode_is NORMAL && has "rdx 是只读连接" || { e2e_plain | tail -3 | cut -c1-160; false; }'

# ---- 控制台 / 快速 SQL：读照跑，写被挡，命令失败是 stderr 原文
runq "GET str:plain"
check "query：GET str:plain → hello（回复转成表）" eval 'e2e_plain | grep -qE "│ +1 │ hello" || { e2e_plain | tail -10 | cut -c60-170; false; }'
runq "HGETALL hash:user"
check "HGETALL hash:user → 回复是 JSON 数组，摆成 field / value 表（age 30、email …）" eval 'e2e_plain | grep -q "ann@example.com" || { e2e_plain | tail -10 | cut -c60-170; false; }'
runq "SET newk 1"
check "SET newk 1：被挡（rdx 是只读连接），真库里没有 newk，命令没收到 SET" eval 'has "rdx 是只读连接" && [[ $(rc exists newk) == 0 ]] && ! lg | grep -qi "^SET newk"' || { lg | tail -2; e2e_plain | tail -6 | cut -c60-170; false; }
runq "GET FAILME"
check "GET FAILME：命令失败（退出码 2）：stderr 原文显示（denied: FAILME is not for you）" eval 'has "denied: FAILME is not for you" || { e2e_plain | tail -8 | cut -c60-200; false; }'
runq "DBSIZE"
check "之后接着跑：DBSIZE → 29（每条命令一个子进程，连接还能用）" eval 'e2e_plain | grep -qE "│ +1 │ 29" || { e2e_plain | tail -8 | cut -c60-170; false; }'
check "整场假命令收到的命令里：没有任何写命令（SET / DEL / HSET / EXPIRE / RENAME …）" eval '! lg | grep -qiE "^(set|del|hset|hdel|expire|rename|flush|lpush|sadd|zadd) " || { lg | grep -iE "^(set|del|hset|hdel|expire|rename|flush)" | head; false; }'
quit_app
_e2e_kill

# ---- SELECT 别的 db：报错，也不交给命令
nd=$(lg | grep -ic "^dbsize"); e2e_start -C "$D/c" "$E2E_BIN rdx"; wait_for 8 flag_is alternate_on 1
wait_for 20 eval '[[ $(lg | grep -ic "^dbsize") -gt $nd ]]'; sleep 1   # connecting takes about 4 s here (the same on 79e6ca3): the tree is up when the SCAN has started
runq "SELECT 3"
check "SELECT 3：报「exec 连接只有 db 0」，命令没有收到 SELECT" eval 'e2e_plain | tr -d " \n" | grep -qF "只有db0" || e2e_plain | tr -d " \n" | grep -qiF "db0only"' || { e2e_plain | tail -8 | cut -c60-200; lg | tail -2; false; }
check "（日志里没有 select）" eval '! lg | grep -qi "^select "'
quit_app
_e2e_kill

# ---- COMMAND 被拒：toast「读不到命令列表」，所有命令都按写处理，只读连接上控制台一律不发
touch "$LOG.deny"; n0=$(lg | wc -l)
e2e_start -C "$D/c" "$E2E_BIN rdx"; wait_for 8 flag_is alternate_on 1; wait_for 12 eval 'e2e_plain | tr -d " \n" | grep -qF "读不到命令列表"'
check "COMMAND 被命令拒了：启动时 toast「读不到命令列表」" eval 'e2e_plain | tr -d " \n" | grep -qF "读不到命令列表"' || { e2e_plain | tail -3 | cut -c1-200; false; }
sleep 3
runq "GET str:plain"
check "之后控制台里连 GET 也不发（所有命令按写处理，rdx 只读）：报「rdx 是只读连接」，命令没收到 GET" eval 'has "rdx 是只读连接" && ! tail -n +$((n0 + 1)) "$LOG" | grep -qi "^get str:plain"' || { e2e_plain | tail -6 | cut -c60-200; lg | tail -3; false; }
quit_app
e2e_done
