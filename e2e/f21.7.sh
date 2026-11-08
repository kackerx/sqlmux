#!/usr/bin/env bash
# F21.7 索引第一次建立期间补全框要有反馈（specs/m21-cli-outer/task.md F21.7；tech-design §9.8「索引存盘」末尾）
# 6 万键、500ms 往返的代理：没有文件时连上就敲 GET p:，COMMAND DOCS 在树的第一批之前发、几秒内到，之后 300ms 内就有框，里面只有一行
# 「索引建立中」，10 秒内有候选，索引建完那行消失、候选是模糊结果、文件写出来；:qa 重启后有文件，是「索引更新中」不是「索引建立中」。
# 1000ms 往返的代理：索引在 30 秒内建完、文件写出来（原来一批 3 秒的超时从排队时算起，建不完）。每个脚本自起 redis-server。
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
sstart() { e2e_start -C "$D/c" -D "$D/data" "$E2E_BIN rd"; wait_for 8 flag_is alternate_on 1; sleep "${1:-12}"; }   # COMMAND DOCS is in by then (500 ms: 3 s after the dial)
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

# ---- 连上就敲（启动后约 2 秒，to_console 又要 2 秒）：敲完 10 秒内有候选
sstart 2; to_console
ms=$(timed "GET p:" "p:employees")
echo "# 连上就敲，敲完到候选：${ms}ms"
check "500ms 往返、连上就敲 GET p:：敲完 10 秒内有候选" eval '(( ms >= 0 && ms <= 10000 ))'
check "索引建完：「索引建立中」消失（30 秒内）、文件写出来、候选是模糊结果" eval 'wait_for 30 eval "! e2e_plain | grep -qF 索引建立中" && wait_for 10 test -s "$FILE" && e2e_plain | grep -qE "p:(employees|team)"'
quit_app

# ---- DOCS 到了以后再敲：300ms 内有框，里面只有一行
rm -f "$FILE"
sstart 12; to_console
ms=$(timed "GET p:" "索引建立中")
echo "# DOCS 到了之后最后一个字符到「索引建立中」：${ms}ms（含 tmux 轮询的 20–40ms）"
check "没有文件、索引还在建：DOCS 到了之后敲 GET p: 后 300ms 内有框，里面是「索引建立中」" eval '(( ms >= 0 && ms <= 300 ))'
check "框里只有那一行（没有候选：前缀的结果要半秒往返才到）" eval '! has "p:employees" && ! has "p:team"'
check "10 秒内前缀的结果先填进框里，「索引建立中」那一行还在" eval 'wait_for 10 eval "e2e_plain | grep -qE \"p:(employees|team)\"" && has "索引建立中" || { e2e_plain | tail -12; false; }'
check "索引建完：「索引建立中」消失（30 秒内）、文件写出来" eval 'wait_for 30 eval "! e2e_plain | grep -qF 索引建立中" && wait_for 10 test -s "$FILE"'
quit_app

# ---- 重启：有文件了，是「索引更新中」，不是「索引建立中」
sstart 12; to_console
ms=$(timed "GET p:" "索引更新中")
check "有文件：敲 GET p: 后是「索引更新中」（不是建立中），候选马上有" eval '(( ms >= 0 && ms <= 300 )) && ! has "索引建立中" && has "p:"'
quit_app

# ---- 1000ms 往返：DOCS 到了以后 300ms 内有框；索引 30 秒内建完、文件写出来（一批的 3 秒超时原来从排队时算起，建不完）
rm -f "$FILE"; redis_slow 1000 || exit 1
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$SPORT" >"$D/c/connections.toml")
sstart 25; to_console
t1=$(date +%s)
ms=$(timed "GET p:" "索引建立中")
echo "# 1000ms：DOCS 到了之后最后一个字符到「索引建立中」：${ms}ms"
check "1000ms 往返：DOCS 到了之后敲 GET p: 后 300ms 内有框和「索引建立中」" eval '(( ms >= 0 && ms <= 300 ))'
check "1000ms 往返：30 秒内索引建完、文件写出来（60447 行）" eval 'wait_for 30 test -s "$FILE" && [[ $(wc -l <"$FILE") == 60447 ]] || { ls -l "$FILE"; false; }'
echo "# 1000ms：敲完到文件写出来 $(( $(date +%s) - t1 ))s"
check "1000ms 往返：没有「索引建立失败」" eval '! has "索引建立失败"'
e2e_done
