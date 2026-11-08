#!/usr/bin/env bash
# F21.7 索引第一次建立期间补全框要有反馈（specs/m21-cli-outer/task.md F21.7；tech-design §8.7、§9.8「索引存盘」末尾）
# 6 万键 + 慢代理：500ms 往返、连上就敲 GET p:，300ms 内有框和「索引建立中」、10 秒内有候选；1000ms 往返索引 30 秒内建完、:qa 后文件在；
# 索引建完那行消失、候选是模糊结果；重启后有文件就是「索引更新中」，不是「建立中」。每个脚本自起 redis-server。
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
mkdir -p "$D/c" "$D/data"
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$SPORT" >"$D/c/connections.toml")
FILE="$D/data/sqlmux/redis-index/rd/db0"
# the app starts, and as soon as the tree shows rd (connected, no wait for the tree to be loaded) the console takes the typing
start_app() { e2e_start -C "$D/c" -D "$D/data" "$E2E_BIN rd"; wait_for 8 flag_is alternate_on 1; wait_for 30 eval 'e2e_plain | grep -q "▾  rd\|▸  rd"'; }
to_console() { key C-l; key Escape; key g g; key d G; key i; }
quit_app() { key Escape; e2e_type ':qa'; e2e_keys Enter; wait_for 8 screen_has '[e2e-exit 0]'; }
# typed TEXT: type it char by char, then poll every 30 ms for up to 60 s; prints "box_ms building_ms cand_ms" measured from the last char
# (box_ms: a completion box is open (the ┌ under the cursor row), building_ms: its 「索引建立中」 / 「索引更新中」 line, cand_ms: a p:team / p:employees row in it); -1 = never
typed() { python3 - "$E2E_SOCK" "$1" "$2" <<'PY'
import subprocess, sys, time
sock, text, limit = sys.argv[1], sys.argv[2], float(sys.argv[3])
def cap(): return subprocess.run(["tmux", "-L", sock, "capture-pane", "-p", "-t", "t"], capture_output=True, text=True).stdout
def send(c): subprocess.run(["tmux", "-L", sock, "send-keys", "-t", "t", "-l", c])
for i, c in enumerate(text):
    t0 = time.time(); send(c)
    while text[: i + 1] not in cap():
        if time.time() - t0 > 5: break
t0 = time.time(); box = bld = cand = None
while time.time() - t0 < limit and None in (box, bld, cand):
    s = cap(); ms = int((time.time() - t0) * 1000); rows = "\n".join(s.split("\n")[2:20])
    if box is None and "│  p" in rows or box is None and "索引" in rows: box = ms
    if bld is None and ("索引建立中" in rows or "索引更新中" in rows): bld = ms
    if cand is None and ("p:team" in rows or "p:employees" in rows): cand = ms
    time.sleep(0.03)
print(box if box is not None else -1, bld if bld is not None else -1, cand if cand is not None else -1)
PY
}

# ---- 500ms 往返，没有文件：连上就敲 GET p:
redis_slow 500 || exit 1
check "（准备）还没有索引文件" eval '[[ ! -e "$FILE" ]]'
start_app; to_console
read -r box bld cand < <(typed "GET p:" 15)
echo "# 500ms：框 ${box}ms、「建立中」那行 ${bld}ms、第一批候选 ${cand}ms（最后一个字符起）"
check "500ms 往返、连上就敲 GET p:：300ms 内有框" eval '(( box >= 0 && box <= 300 ))'
check "…框里有「索引建立中」那一行（300ms 内）" eval '(( bld >= 0 && bld <= 300 )) && e2e_plain | grep -q "索引建立中" || e2e_plain | sed -n 3,14p | cut -c90-150'
check "…10 秒内有候选（前缀兜底的结果）" eval '(( cand >= 0 && cand <= 10000 ))'
check "索引建完：「建立中」那行消失，候选是模糊结果（p:team / p:employees 的键）" eval 'wait_for 120 eval "! e2e_plain | grep -qF 索引建立中" && e2e_plain | sed -n 3,14p | grep -q "p:"'
check "索引文件写出（60446 个键）" eval 'wait_for 60 test -s "$FILE" && [[ $(head -1 "$FILE") == "sqlmux-redis-index 1 60446 0" ]]'
quit_app

# ---- 重启：有文件，就是「更新中」，不是「建立中」
start_app; to_console
read -r box bld cand < <(typed "GET p:" 10)
check "重启后：框 300ms 内出来，候选马上有（文件），底部是「索引更新中」、没有「索引建立中」" eval '(( box >= 0 && box <= 300 )) && ! e2e_plain | grep -q "索引建立中" && wait_for 3 eval "e2e_plain | grep -q 索引更新中" || e2e_plain | sed -n 3,14p | cut -c90-150'
quit_app

# ---- 1000ms 往返，没有文件：索引 30 秒内建完，:qa 后文件在
rm -f "$FILE"; redis_slow 1000 || exit 1
start_app; to_console; typed "GET p:" 1 >/dev/null; t0=$SECONDS
check "1000ms 往返、6 万键：索引 30 秒内建完、文件写出" eval 'wait_for 30 test -s "$FILE" && [[ $(head -1 "$FILE") == "sqlmux-redis-index 1 60446 0" ]] || { echo "  ${SECONDS}s: $(ls -l "$FILE" 2>&1 | cut -c1-60)"; false; }'
echo "# 1000ms：索引文件在 $((SECONDS - t0)) 秒后写出（最后一个字符起）"
quit_app
check ":qa 后文件还在" eval '[[ -s "$FILE" ]]'
e2e_done
