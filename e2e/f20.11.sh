#!/usr/bin/env bash
# F20.11 Redis 键名补全在 6 万键的库上不卡（specs/m20-redis-ui/task.md F20.11；tech-design §9.8）
# 6 万个和用户同形状的键（p:employees:…@x.com、i18n_agent:expert:…、多级 : 命名空间）。暖（索引已建好）：敲 get p:emp，每个字符马上回显，最后一个字符后 300ms 内候选出现；
# 冷（150ms 往返的慢代理）：不设门槛，打印「最后一个字符到候选」和「索引建完」的时间。每个脚本自起 redis-server。
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
mkdir -p "$D/c" "$D/cs"
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$RPORT" >"$D/c/connections.toml")
sstart() { e2e_start -C "$1" "$E2E_BIN rd"; wait_for 8 flag_is alternate_on 1; sleep 3; }
to_console() { key C-l; key Escape; key g g; key d G; key i; }
mon() { : >"$D/mon.txt"; redis-cli -p "$RPORT" monitor >>"$D/mon.txt" 2>&1 & MON=$!; sleep 0.5; }
unmon() { kill "$MON" 2>/dev/null; wait "$MON" 2>/dev/null; }
quiet() { local a b=-1 n=0; while ((n < 4)); do a=$(wc -l <"$D/mon.txt"); [[ $a == "$b" ]] && n=$((n + 1)) || n=0; b=$a; sleep 1; done; }
# timed TEXT NEEDLE: type TEXT char by char (each char's echo timed), then time until NEEDLE is on the screen; prints "max_echo_ms cand_ms"
timed() { python3 - "$E2E_SOCK" "$1" "$2" <<'PY'
import subprocess, sys, time
sock, text, needle = sys.argv[1:4]
def cap(): return subprocess.run(["tmux", "-L", sock, "capture-pane", "-p", "-t", "t"], capture_output=True, text=True).stdout
def send(c): subprocess.run(["tmux", "-L", sock, "send-keys", "-t", "t", "-l", c])
worst = 0.0
for i, c in enumerate(text):
    t0 = time.time(); send(c)
    want = text[: i + 1]
    while want not in cap():
        if time.time() - t0 > 5: break
    worst = max(worst, time.time() - t0)
t0 = time.time()
while needle not in cap().replace("┌", "").split("\n", 2)[-1] if False else needle not in "\n".join(cap().split("\n")[2:12]):
    if time.time() - t0 > 60: break
cand = time.time() - t0
print("%d %d" % (worst * 1000, cand * 1000))
PY
}

# ---- 暖：索引建好后敲 get p:emp
sstart "$D/c"; mon
to_console; e2e_type "get b"; sleep 1; quiet; unmon
to_console
read -r echo_ms cand_ms < <(timed "get p:emp" "p:employees")
echo "# 暖：最慢的一个字符回显 ${echo_ms}ms，最后一个字符到候选出现 ${cand_ms}ms（包含 tmux 轮询的开销，约 20–40ms）"
check "暖（6 万键，索引已建好）：最后一个字符后候选在 300ms 内出现" eval '(( cand_ms <= 300 ))'
check "暖：敲字时每个字符都马上回显（最慢的一个 ≤ 300ms，含 tmux 轮询）" eval '(( echo_ms <= 300 ))'
check "候选里是 p:employees 的键（p:emp 的模糊匹配，不是别的）" eval 'e2e_plain | sed -n 3,12p | grep -q "p:employees"'
e2e_stop

# ---- 冷：150ms 往返的慢代理，第一次补全建索引
redis_slow 150 || exit 1
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$SPORT" >"$D/cs/connections.toml")
sstart "$D/cs"; mon; quiet; : >"$D/mon.txt"                                  # 树先加载完
to_console
t0=$(python3 -c 'import time; print(int(time.time() * 1000))')
read -r echo_ms cand_ms < <(timed "get p:emp" "p:employees")
t1=$(python3 -c 'import time; print(int(time.time() * 1000))')
quiet
t2=$(python3 -c 'import time; print(int(time.time() * 1000))')
echo "# 冷（150ms 往返）：最慢的一个字符回显 ${echo_ms}ms；最后一个字符到候选 ${cand_ms}ms（候选可以先是前缀结果）；从第一个字符到候选 $((t1 - t0))ms；建索引的线路安静（建完）在 $((t2 - t0 - 4000))ms"
check "冷：打字期间每个字符回显不卡（最慢的一个 ≤ 500ms，含 tmux 轮询；索引在建，不设候选门槛）" eval '(( echo_ms <= 500 ))'
unmon

# ---- 开始建索引不等键名：控制台敲的字在 COMMAND DOCS 到达之前时，docs 到了以后补全自己出来（不用再敲）。慢代理 400ms，起来马上敲
unmon; e2e_stop; redis_stop; redis_start R || exit 1
rc -n 0 EVAL "for i = 1, 3000 do redis.call('SET', 'p:employees:' .. i, 1) end" 0 >/dev/null
redis_slow 400 || exit 1
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$SPORT" >"$D/cs/connections.toml")
e2e_start -C "$D/cs" "$E2E_BIN rd"; wait_for 8 flag_is alternate_on 1
to_console; e2e_type "get p:emp"
wait_for 40 eval 'e2e_plain | sed -n 3,12p | grep -q "p:employees"'
check "起来马上敲 get p:emp（COMMAND DOCS 还没到）：之后什么都不用再敲，docs 到了和索引建好后候选自己出来" eval 'e2e_plain | sed -n 3,12p | grep -q "p:employees"' || e2e_plain | sed -n 1,12p | cut -c60-165

e2e_done
