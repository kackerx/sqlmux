#!/usr/bin/env bash
# F19.2 执行中的转圈（specs/m19-polish/task.md F19.2；tech-design §7.8「转圈」、§11「执行中」）
# 帧依次 ●•• •●• ••● •●•，每帧 150ms；画在四处：状态栏的 busy 块、结果 tab 的执行中占位、查询条里计数完成前、树上连接中的节点名后面；
# 一个 tick 驱动，只在屏幕上有转圈时才 tick：空闲时两次截屏一样。帧用正则匹配四帧，不按固定帧截屏。
# 状态栏和占位用 select pg_sleep(8)（共用库只读）；树上的连接中和查询条的计数用一个慢代理（slowproxy.py 一次来回 800 ms）拖慢。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; kill $PX 2>/dev/null; rm -rf "$D"' EXIT
FRAMES='●••|•●•|••●'
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; }
# sample N times (each takes ~0.3s): print the dots found by REGEX (group 1) on the screen, one per line ("-" when none)
sample() { local n=$1 re=$2 i; for ((i = 0; i < n; i++)); do e2e_plain | python3 -c '
import re, sys
m = re.search(sys.argv[1], sys.stdin.read())
print(m.group(1) if m else "-")' "$re"; done; }
# the bar's and the placeholder's dots from the same screens: lines "bar frame placeholder frame"
sample2() { local n=$1 i; for ((i = 0; i < n; i++)); do e2e_plain | python3 -c '
import re, sys
t = sys.stdin.read()
b = re.search(r" busy ([●•]{3}) ", t); p = re.search(r"执行中 ([●•]{3}) ", t)
print(b.group(1) if b else "-", p.group(1) if p else "-")'; done; }
distinct() { LC_ALL=C sort -u | grep -v '^-$' | wc -l | tr -d ' '; }

# ---- 状态栏的 busy 块和结果 tab 的执行中占位
SOLO= start; sleep 1
key C-l; key C-l; sleep 0.3
setsql "select pg_sleep(8)"; key Enter
wait_for 3 eval 'bar | grep -q busy'
sample2 14 >"$D/two.txt"; awk '{ print $1 }' "$D/two.txt" >"$D/bar.txt"; awk '{ print $2 }' "$D/two.txt" >"$D/ph.txt"
check "pg_sleep(8) 执行中：状态栏 busy 后面是三点转圈（●•• / •●• / ••● 之一），采样里至少 8 次都有" eval '[[ $(grep -cE "^(●••|•●•|••●)$" "$D/bar.txt") -ge 8 ]] || { tr "\n" " " <"$D/bar.txt"; false; }'
check "采样里状态栏的帧在变：至少出现三种不同的帧" eval '[[ $(distinct <"$D/bar.txt") -ge 3 ]] || { tr "\n" " " <"$D/bar.txt"; false; }'
check "结果 tab 的执行中占位：「执行中 ●•• · 0s · C-c 取消」，转圈跟在「执行中」后面，帧在变（至少三种）" eval '[[ $(distinct <"$D/ph.txt") -ge 3 ]] || { tr "\n" " " <"$D/ph.txt"; false; }'
check "两处的帧各自是四帧里的一个，不是别的字符（没有出现三点以外的东西）" eval '! grep -vE "^(●••|•●•|••●|-)$" "$D/bar.txt" "$D/ph.txt"'
wait_for 20 settled; sleep 0.5
check "执行完：状态栏没有 busy，没有三点；结果出来了（pg_sleep 一列）" eval '! bar | grep -qE "busy|[●•]{3}" && e2e_plain | grep -q "pg_sleep"'
check "空闲时不重画：隔 2 秒两次截屏一模一样（没有 tick 在发）" eval 'a=$(e2e_plain); sleep 2; b=$(e2e_plain); [[ $a == "$b" ]]'
e2e_stop

# ---- 慢代理：树上连接中的转圈、查询条里计数完成前的转圈
read -r PU PW PH PP <<<"$(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(p.username, p.password, p.hostname, p.port)' "$SQLMUX_TEST_PG")"
SP=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
python3 "$(dirname "$0")/slowproxy.py" "$SP" "$PP" 800 >"$D/px.log" 2>&1 & PX=$!
for i in $(seq 30); do grep -q '^up' "$D/px.log" 2>/dev/null && break; sleep 0.1; done
mkdir -p "$D/c"
(umask 077; printf '[[connection]]\nname = "play"\nengine = "postgres"\ndsn = "postgres://%s:%s@127.0.0.1:%s/sqlmux?sslmode=disable"\n' "$PU" "$PW" "$SP" >"$D/c/connections.toml")
e2e_start -C "$D/c" "$E2E_BIN"; wait_for 6 flag_is alternate_on 1
sample 12 'play +([●•]{3})' >"$D/conn.txt"
check "连接中：树上 play 节点名后面是三点转圈，不再是静态的 …，帧在变" eval '[[ $(distinct <"$D/conn.txt") -ge 2 ]] || { tr "\n" " " <"$D/conn.txt"; e2e_plain | sed -n 3,6p | cut -c1-40; false; }'
wait_for 60 eval 'e2e_plain | grep -q "t_order"'; sleep 1
check "连上之后树上的转圈没了（play 节点后面没有三点）" eval '! e2e_plain | sed -n 3,6p | grep -qE "[●•]{3}"'
solo
e2e_keys C-p; sleep 0.3; e2e_type "@t_order"; sleep 0.5; e2e_keys Enter
sample 40 '(?:auto|[a-z]+) · ([●•]{3})' >"$D/cnt.txt"
check "打开 t_order：行数统计里计数完成前是三点转圈（不是静态的 …），帧在变" eval '[[ $(distinct <"$D/cnt.txt") -ge 2 ]] || { tr "\n" " " <"$D/cnt.txt"; e2e_plain | sed -n 1,4p | cut -c34-160; false; }'
wait_for 20 eval 'e2e_plain | grep -qE "PAGE 1/60"'; sleep 1
check "计数完成后：PAGE 1/60，查询条里没有三点，也没有 …" eval '! e2e_plain | sed -n 1,4p | grep -qE "[●•]{3}|…"'
check "空闲时不重画：隔 2 秒两次截屏一样" eval 'a=$(e2e_plain); sleep 2; b=$(e2e_plain); [[ $a == "$b" ]]'
e2e_done
