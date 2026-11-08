#!/usr/bin/env bash
# F21.7 索引第一次建立期间补全框要有反馈（specs/m21-cli-outer/task.md F21.7；tech-design §9.8「索引存盘」末尾）
# 6 万键、500ms 往返的代理（COMMAND DOCS 要二十几秒、索引要建好几秒；1 秒往返时索引批次排在 DOCS 后面超过 3 秒的 countTimeout 会失败）：没有文件时敲 GET p: 之后 300ms 内就有框，里面只有一行「索引建立中」；索引建完那行消失、候选是模糊结果；
# :qa 重启后有文件，是「索引更新中」不是「索引建立中」。每个脚本自起 redis-server。
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
redis_slow 500 || exit 1
mkdir -p "$D/c" "$D/data"
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$SPORT" >"$D/c/connections.toml")
FILE="$D/data/sqlmux/redis-index/rd/db0"
sstart() { e2e_start -C "$D/c" -D "$D/data" "$E2E_BIN rd"; wait_for 8 flag_is alternate_on 1; sleep 45; }   # COMMAND DOCS is a big reply, a chunk of it each half second
to_console() { key C-l; key Escape; key g g; key d G; key i; }
quit_app() { key Escape; e2e_type ':qa'; e2e_keys Enter; wait_for 8 screen_has '[e2e-exit 0]'; }
has() { e2e_plain | grep -qF -- "$1"; }
# first TEXT: type TEXT, then time until NEEDLE is on the screen; prints ms (-1: not within 20 s)
timed() { python3 - "$E2E_SOCK" "$1" "$2" <<'PY'
import subprocess, sys, time
sock, text, needle = sys.argv[1:4]
def cap(): return subprocess.run(["tmux", "-L", sock, "capture-pane", "-p", "-t", "t"], capture_output=True, text=True).stdout
for c in text: subprocess.run(["tmux", "-L", sock, "send-keys", "-t", "t", "-l", c])
t0 = time.time()
while needle not in cap():
    if time.time() - t0 > 20: print(-1); sys.exit()
print("%d" % ((time.time() - t0) * 1000))
PY
}

check "（准备）还没有索引文件" eval '[[ ! -e "$FILE" ]]'
sstart; to_console
ms=$(timed "GET p:" "索引建立中")
echo "# 最后一个字符到「索引建立中」：${ms}ms（含 tmux 轮询的 20–40ms）"
check "没有文件、索引还在建：敲 GET p: 后 300ms 内有框，里面是「索引建立中」" eval '(( ms >= 0 && ms <= 300 ))'
check "框里只有那一行（没有候选：前缀的结果要半秒往返才到）" eval '! has "p:employees" && ! has "p:team"'
check "索引建完：「索引建立中」消失（最多等 90 秒）、文件写出来" eval 'wait_for 90 eval "! e2e_plain | grep -qF 索引建立中" && wait_for 20 test -s "$FILE"'
check "候选是模糊结果（p: 开头的键）" eval 'wait_for 20 eval "e2e_plain | grep -qE \"p:(employees|team)\""' || e2e_plain | tail -12
quit_app

# ---- 重启：有文件了，是「索引更新中」，不是「索引建立中」
sstart; to_console
ms=$(timed "GET p:" "索引更新中")
check "有文件：敲 GET p: 后是「索引更新中」（不是建立中），候选马上有" eval '(( ms >= 0 && ms <= 300 )) && ! has "索引建立中" && has "p:"'
e2e_done
