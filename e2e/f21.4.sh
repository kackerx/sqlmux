#!/usr/bin/env bash
# F21.4 Redis 键名索引存盘，启动即用（specs/m21-cli-outer/task.md F21.4；tech-design §9.8「索引存盘」）
# 6 万键、150ms 往返的慢代理（F20.11 的库）：第一次运行敲 get p:emp 建索引，建完文件写出（0600、头一行、一行一个键）；:qa 退出再起，
# 第二次运行最后一个字符后 300ms 内候选出现（用文件里的索引，不等 SCAN）；index_cache = false 不读不写。每个脚本自起 redis-server。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1
rc flushall >/dev/null
rc -n 0 EVAL "for i = 1, 30000 do redis.call('SET', 'q:other:' .. i, 1) end
for i = 1, 20000 do redis.call('SET', 'a:b:c:' .. (i % 50) .. ':' .. i, 1) end
for i = 1, 9500 do redis.call('SET', 'p:team:' .. i, 1) end
for i = 1, 346 do redis.call('SET', 'p:employees:' .. i .. '@x.com', 1) end
for i = 1, 600 do redis.call('SET', 'i18n_agent:expert:' .. i, 1) end" 0 >/dev/null
check "（准备）库里 60446 个键" eval '[[ $(rc dbsize) == 60446 ]]' || rc dbsize
redis_slow 150 || exit 1
mkdir -p "$D/c" "$D/data"
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$SPORT" >"$D/c/connections.toml")
FILE="$D/data/sqlmux/redis-index/rd/db0"
sstart() { e2e_start -C "$D/c" -D "$D/data" "$E2E_BIN rd"; wait_for 8 flag_is alternate_on 1; sleep 12; }   # COMMAND DOCS comes through the slow proxy: the keys are not completed before it is in
to_console() { key C-l; key Escape; key g g; key d G; key i; }
quit_app() { key Escape; e2e_type ':qa'; e2e_keys Enter; wait_for 8 screen_has '[e2e-exit 0]'; }
# timed TEXT NEEDLE: type TEXT char by char, then time until NEEDLE is among the first rows of the screen; prints "cand_ms"
timed() { python3 - "$E2E_SOCK" "$1" "$2" <<'PY'
import subprocess, sys, time
sock, text, needle = sys.argv[1:4]
def cap(): return subprocess.run(["tmux", "-L", sock, "capture-pane", "-p", "-t", "t"], capture_output=True, text=True).stdout
def send(c): subprocess.run(["tmux", "-L", sock, "send-keys", "-t", "t", "-l", c])
for i, c in enumerate(text):
    t0 = time.time(); send(c)
    while text[: i + 1] not in cap():
        if time.time() - t0 > 5: break
t0 = time.time()
while needle not in "\n".join(cap().split("\n")[2:12]):
    if time.time() - t0 > 60: break
print("%d" % ((time.time() - t0) * 1000))
PY
}

# ---- 第一次：没有文件，建索引（慢代理上要几秒），建完写文件
check "（准备）还没有索引文件" eval '[[ ! -e "$FILE" ]]'
sstart; to_console
cold=$(timed "get p:emp" "p:employees")
check "第一次运行：索引建完后文件写出来（60446 个键）" eval 'wait_for 90 test -s "$FILE" && [[ $(wc -l <"$FILE") == 60447 ]] || { ls -l "$FILE"; false; }'
check "文件：权限 0600，头一行是格式版本和总数，之后一行一个键名" eval '[[ $(fmode "$FILE") == 600 && $(head -1 "$FILE") == "sqlmux-redis-index 1 60446 0" ]] && grep -qx "p:employees:1@x.com" "$FILE"'
echo "# 第一次（冷，建索引）：最后一个字符到候选 ${cold}ms"
quit_app

# ---- 第二次：文件里的索引马上能用
sstart; to_console
warm=$(timed "get p:emp" "p:employees")
echo "# 第二次（用文件）：最后一个字符到候选 ${warm}ms（第一次 ${cold}ms）"
check "第二次运行：最后一个字符后 300ms 内候选出现" eval '(( warm <= 300 ))'
check "候选里是 p:employees 的键" eval 'e2e_plain | sed -n 3,12p | grep -q "p:employees"'
quit_app

# ---- index_cache = false：不读不写，已有文件不删
rm -f "$FILE"; printf '[redis]\nindex_cache = false\n' >"$D/c/config.toml"
sstart; to_console
timed "get p:emp" "p:employees" >/dev/null; sleep 20
check "index_cache = false：没有写文件" eval '[[ ! -e "$FILE" ]]'
quit_app
mkdir -p "$(dirname "$FILE")"; printf 'sqlmux-redis-index 1 1 0\np:employees:only\n' >"$FILE"
sstart; to_console
timed "get p:emp" "p:employees" >/dev/null
check "index_cache = false：已有的文件没有读（候选里没有文件里独有的键）、没有删、没有改" eval '! e2e_plain | grep -q "p:employees:only" && [[ $(wc -l <"$FILE") == 2 ]]'
e2e_done
