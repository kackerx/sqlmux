#!/usr/bin/env bash
# F20.2 Redis 树过滤的高亮（specs/m20-redis-ui/task.md F20.2；tech-design §7.8「Redis 的树」）
# 侧栏 / 过滤一个 Redis 连接（服务端 SCAN MATCH *<输入>*）：每个节点（命名空间和键）在自己那一段名字里找输入文字，命中的字用 match 色（#ff9e64）画；
# 区分大小写（同 MATCH）；输入里有 glob 字符（* ? [）不画；一次命中跨过分隔符（p:emp）两段都不画；清掉过滤后不再有颜色。每个脚本自起 redis-server。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1
rc flushall >/dev/null
for k in p:employees:1 p:employees:2 p:other:1 p:Employees:1 kaixuan:a:1 team:kaixuan:x team:z:kaixuan abc:def:1; do rc set "$k" v >/dev/null; done
mkdir -p "$D/c"
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$RPORT" >"$D/c/connections.toml")
MATCH=#ff9e64
side() { e2e_plain | python3 -c 'import re, sys; [print(re.sub("[-] ?", "", l[1:33]).rstrip("│ ")) for l in sys.stdin.read().split("\n")]'; }
tree() { side | python3 -c "
import re, sys
for l in sys.stdin:
    l = ''.join(c for c in l if not 0xE000 <= ord(c) <= 0xF8FF)
    if l.strip(): print(re.sub(r' +', ' ', l.rstrip()))" | sed -n 1,16p; }   # icons (private use) dropped
# the sidebar rows (screen y) whose text has WORD as a node name: "▾ WORD 12" or the key's own name; and what colour its letters are
yrows() { side | awk -v w="$1" 'NR > 3 { for (i = 1; i <= NF; i++) if ($i == w) { print NR; break } }'; }
col_of() { local y=$1 w=$2 c; for c in $(e2e_find "$w" "$y"); do (( c < 33 )) && { echo "$c"; return; }; done; }   # first column of WORD on row Y, inside the sidebar
# lit Y WORD: every letter of WORD on row Y is match-coloured; unlit: none is
lit() { local y=$1 w=$2 x i; x=$(col_of "$y" "$w"); [[ -n $x ]] || { echo "  no $w on row $y"; return 1; }
  for ((i = 0; i < ${#w}; i++)); do [[ $(e2e_style $((x + i)) "$y") == *"fg=$MATCH"* ]] || { echo "  $w at $((x + i)),$y: $(e2e_style $((x + i)) "$y")"; return 1; }; done; }
unlit() { local y=$1 w=$2 x i; x=$(col_of "$y" "$w"); [[ -n $x ]] || { echo "  no $w on row $y"; return 1; }
  for ((i = 0; i < ${#w}; i++)); do [[ $(e2e_style $((x + i)) "$y") == *"fg=$MATCH"* ]] && { echo "  $w at $((x + i)),$y is lit"; return 1; }; done; return 0; }
all_unlit() { local y; for y in $(seq 3 $(( $(H) - 3 ))); do for x in $(seq 2 31); do [[ $(e2e_style $x $y) == *"fg=$MATCH"* ]] && { echo "  lit cell at $x,$y"; return 1; }; done; done; return 0; }
filter() { key C-h; key /; key C-u; e2e_type "$1"; sleep 1.2; }
done_filtering() { wait_for 6 eval '! tree | grep -q "搜索中"'; }

SOLO=1
e2e_start -C "$D/c" "$E2E_BIN"; wait_for 8 flag_is alternate_on 1; solo; wait_for 8 eval 'tree | grep -q "db0"'
filter employees; done_filtering
check "（准备）过滤 employees：命名空间 p 下只有 employees，键 1、2" eval 'tree | grep -q "▾ p " && tree | grep -q "▾ employees 2" && ! tree | grep -q "other"' || tree
y=$(yrows employees | head -1)
check "命名空间 employees 的字是 match 色" eval '[[ -n $y ]] && lit $y employees'
y=$(yrows p | head -1)
check "命名空间 p 不含输入：不是 match 色" eval '[[ -n $y ]] && unlit $y p'

filter kaixuan; done_filtering
check "（准备）过滤 kaixuan：三个命名空间 / 键里有它" eval 'tree | grep -q "▾ kaixuan " && tree | grep -q "▾ team "' || tree
ys=$(yrows kaixuan | tr '\n' ' '); n=$(echo $ys | wc -w | tr -d ' ')
check "kaixuan 出现的每一处（命名空间 kaixuan、team 下的 kaixuan 命名空间、键 z 下的键名）都是 match 色，共 $n 处" eval '[[ $n -ge 2 ]] && for y in $ys; do lit $y kaixuan || exit 1; done'
y=$(yrows team | head -1)
check "命名空间 team 不含 kaixuan：不是 match 色" eval '[[ -n $y ]] && unlit $y team'

filter Employees; done_filtering
check "区分大小写：过滤 Employees 只留 p:Employees:1（不留小写的 employees）" eval 'tree | grep -q "▾ Employees 1" && ! tree | grep -q "▾ employees"' || tree
y=$(yrows Employees | head -1)
check "Employees 的字是 match 色" eval '[[ -n $y ]] && lit $y Employees'

filter p:emp; done_filtering
check "（准备）过滤 p:emp：服务端 *p:emp* 子串，p 和 employees 都在" eval 'tree | grep -q "▾ p " && tree | grep -q "▾ employees"' || tree
check "p:emp 一次命中跨过分隔符（落在 p 和 employees 两段上）：两段都不画 match 色（§7.8 ponytail 的上限）" all_unlit

filter 'p:emp*'; done_filtering
check "输入里有 glob 字符（p:emp*，含 * 不补两头，按原样 MATCH）：树照样过滤，但任何节点都不画 match 色" eval 'tree | grep -q "employees" && all_unlit'

filter employees; done_filtering
key Escape; sleep 1; done_filtering
check "清掉过滤：树回到完整的，没有 match 色" eval 'all_unlit' || tree

e2e_done
