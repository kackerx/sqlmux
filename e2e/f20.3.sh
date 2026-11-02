#!/usr/bin/env bash
# F20.3 命令面板的 @ 列出 Redis 键（specs/m20-redis-ui/task.md F20.3；tech-design §12「Redis 键」、§9.8）
# @ 和「所有」里和表、query 混排键：名称是键名，位置 <连接>/db<n>，标签 Key；↵ 同树上 ↵（没开着就在当前 data pane 开键 tab，开着的聚焦过去）；
# 数据只取已建好的键名索引和树已扫到的键，面板自己不触发扫描（敲字时线路上没有 SCAN）；没读过的 db（树没展开、没建索引）不列。每个脚本自起 redis-server。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1
rc flushall >/dev/null
for k in p:employees:1 p:employees:2 p:other:1 team:kaixuan:x; do rc set "$k" v >/dev/null; done
rc -n 1 set q:db1key:1 v >/dev/null
mkdir -p "$D/c"
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$RPORT" >"$D/c/connections.toml")
mon() { : >"$D/mon.txt"; redis-cli -p "$RPORT" monitor >>"$D/mon.txt" 2>&1 & MON=$!; sleep 0.5; }
unmon() { kill "$MON" 2>/dev/null; wait "$MON" 2>/dev/null; }
names() { list | sed 's/|.*//' | tr '\n' ';'; }                      # "name  location  label;" per row
opal() { e2e_keys C-p; sleep 0.4; e2e_type "$1"; sleep 1.2; }          # open the palette with TEXT typed
wopal() { opal "$1"; wait_for 6 eval 'names | grep -q "Key;"'; sleep 0.3; }   # the same, and wait for keys in the list (the matching runs in a Cmd; the list keeps the last one's till it is done)
cpal() { e2e_keys Escape; sleep 0.3; }
ntabs() { e2e_plain | grep -oE "[0-9]:[^ ]* $1" | wc -l | tr -d " "; }          # how many tabs named KEY the screen shows

e2e_start -C "$D/c" "$E2E_BIN"; wait_for 8 flag_is alternate_on 1; sleep 2
mon
wopal "@p:employees"
check "@p:employees：两个键，名称是键名，位置 rd/db0，标签 Key；没有 p:other:1（同分的先后随 SCAN 的顺序，不比）" eval '[[ $(names | tr ";" "\n" | sort | tr "\n" ";") == "p:employees:1  rd/db0  Key;p:employees:2  rd/db0  Key;" ]]' || names
cpal; wopal "@pemp1"
check "@pemp1：模糊匹配，p:employees:1 在里面" eval 'names | grep -q "p:employees:1  rd/db0  Key"' || names
cpal; wopal "kaixuan"
check "「所有」范围（没有 @）里输入 kaixuan：team:kaixuan:x 也在" eval 'names | grep -q "team:kaixuan:x  rd/db0  Key"' || names
cpal; opal "@q:db1"
check "db1 的键：树没展开过、没建过索引：@q:db1 没有结果" eval '[[ -z $(names) ]]' || names
cpal
unmon
check "线路：打开面板、敲了几个查询，服务端没收到 SCAN / KEYS（面板自己不扫库）" eval '! grep -aqiE "\"(scan|keys)\"" "$D/mon.txt"' || grep -aiE '"(scan|keys)"' "$D/mon.txt" | head -3

# ---- 没打字不列键；反向词 ! 每敲一个字重算（!pl 再 !pla：含 pl 但不含 pla 的键要出现）
opal "@"
check "@ 后什么都没打：不列键（只有 query 等）" eval '! names | grep -q "Key;"' || names
cpal; opal "@!pl"; wait_for 4 eval 'names | grep -q "p:other:1  rd/db0  Key"'
check "@!pl：含 pl 的键（p:employees:*）不在，不含 pl 的 p:other:1、team:kaixuan:x 在" eval 'names | grep -q "p:other:1  rd/db0  Key" && names | grep -q "team:kaixuan:x" && ! names | grep -q "p:employees"' || names
e2e_type a; wait_for 4 eval 'names | grep -q "p:employees:1  rd/db0  Key"'
check "再打一个 a（!pla）：含 pl 但不含 pla 的 p:employees:1、:2 出现了（不是在上一次 !pl 的结果里继续缩小）" eval 'names | grep -q "p:employees:1  rd/db0  Key" && names | grep -q "p:employees:2  rd/db0  Key"' || names
cpal

# ---- ↵：开键 tab
wopal "@p:employees:2"; key Enter; sleep 1
check "↵：当前 data pane（①）里开了 p:employees:2 的键 tab，面板关了" eval 'e2e_plain | head -3 | grep -q "p:employees:2" && closed' || { e2e_plain | sed -n 1,4p | cut -c30-150; }
check "（准备）tab 栏里有一个 p:employees:2" eval '[[ $(ntabs p:employees:2) == 1 ]]'
wopal "@p:employees:2"; key Enter; sleep 1
check "再来一次同一个键：聚焦已开的 tab，不再开第二个（tab 栏里还是一个 p:employees:2）" eval '[[ $(ntabs p:employees:2) == 1 ]] && closed' || { e2e_plain | sed -n 1,4p | cut -c30-150; }
wopal "@p:employees:1"; key Enter; sleep 1
check "另一个键：同一个 pane 里再开一个 tab（两个：p:employees:1、p:employees:2）" eval '[[ $(ntabs p:employees:1) == 1 && $(ntabs p:employees:2) == 1 ]]'

ty=$(e2e_plain | awk '/[0-9]:[^ ]* p:employees:1/ { print NR; exit }')
check "引导页：①最初是空的引导页，选键后键 tab 开在它上面（替换）：tab 栏是 1: p:employees:2 和 2: p:employees:1 两个，没有留下第三个空的" eval '[[ -n $ty ]] && [[ $(e2e_text 36 97 "$ty" | grep -oE "[0-9]:" | wc -l | tr -d " ") == 2 ]]' || e2e_text 36 97 "$ty"
opal ""
check "选过的键不进「最近」：空输入的面板列表里没有键（Key 标签）" eval '! list | grep -q "  Key|"' || list | head
cpal

# ---- 树读过的 db 才有：展开树上的 db1，它的键就在 @ 里了
key C-h; sleep 0.3
y=$(e2e_plain | awk '/db1/ { print NR; exit }'); x=$(e2e_find db1 "$y" | cut -d' ' -f1)
e2e_click "$x" "$y"; sleep 1.2
check "（准备）树上展开了 db1，读到了键 q" eval 'e2e_plain | grep -q "▾.* db1" && e2e_plain | grep -q "q:db1key\|▸.* q \|▾.* q "' || e2e_plain | sed -n 1,10p | cut -c1-34
wopal "@q:db1"
check "树读过 db1 之后：@q:db1 列出它的键，位置是 rd/db1" eval 'names | grep -q "q:db1key:1  rd/db1  Key"' || names
cpal

e2e_done
