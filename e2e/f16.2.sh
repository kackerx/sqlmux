#!/usr/bin/env bash
# F16.2 Redis 的树（specs/m16-redis/task.md F16.2；tech-design §7.8「Redis 的树」、§8.7「树」、§7.7、§14）
# 连接 → db → 命名空间 → 键：db 只列服务端上有键的和 DSN 指定的；键名按分隔符（默认 :，[redis] delimiter、连接的 delimiter）切成命名空间，
# 尾注是下面扫到的键数；键前面是类型图标和颜色；只用 SCAN（COUNT 1000，每批的 TYPE 一条 pipeline）不用 KEYS，扫到 scan_keys 个就停、
# db 下面写「已扫描 N / 总数」；侧栏的 / 过滤改成服务端的 SCAN MATCH（没有 * 两头补 *）；R 重扫；展开状态记在 state。
# 键和命名空间上的操作（删除、改名、TTL）在 f16.2b.sh。seed 是 testdata/seed/redis.txt 去掉 EVAL 的那份，db0 一共 29 个键（没装 RedisJSON，没有 json:doc）。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1; redis_seed; redis_big 5
rc -n 9 set nine 9 >/dev/null; rc -n 10 set ten 10 >/dev/null; printf 'SET "k\\xffbin" 1\n' | rc -n 6 >/dev/null
rc -n 5 set tail:key x >/dev/null
mkdir -p "$D/c" "$D/st"
(umask 077; cat >"$D/c/connections.toml" <<TOML
[[connection]]
name = "rd"
engine = "redis"
dsn = "redis://localhost:$RPORT/0"

[[connection]]
name = "big"
engine = "redis"
dsn = "redis://localhost:$RPORT/5"

[[connection]]
name = "slash"
engine = "redis"
dsn = "redis://localhost:$RPORT/0"
delimiter = "/"

[[connection]]
name = "dbl"
engine = "redis"
dsn = "redis://localhost:$RPORT/0"
delimiter = "::"
TOML
)
STR=#9ece6a HASH=#bb9af7 LIST=#ff9e64 SET=#7aa2f7 ZSET=#e0af68 STREAM=#7dcfff
side() { e2e_plain | python3 -c 'import re, sys; [print(re.sub("[\ue000-\uf8ff] ?", "", l[1:int(sys.argv[1]) + 1]).rstrip("│ ")) for l in sys.stdin.read().split("\n")]' "${SW:-32}"; }   # the sidebar's rows, the icons off, the indentation kept
side_all() { e2e_plain | python3 -c 'import re, sys; [print(re.sub("[\ue000-\uf8ff] ?", "", l)) for l in sys.stdin.read().split("\n")]'; }   # every row of the screen, the icons off
tree() { side | python3 -c "
import re, sys
on = False
for l in sys.stdin:
    if not l.strip(): continue
    ind = len(l) - len(l.lstrip())
    if on and ind <= 2: break
    if not on and re.match(r'^▾ +db\\d+', l.strip()): on = True
    if on: print(re.sub(r' +', ' ', l.strip()))"; }   # the rows from the first open db node (the DSN's; F16.10 sorts the dbs by number, so it is not always the first) of the first connection and what is under them, spaces squeezed
nth_row() { side | python3 -c "
import re, sys
n = int(sys.argv[2])
for i, l in enumerate(sys.stdin.read().split('\\n')):
    if re.search(sys.argv[1], l.strip()):
        n -= 1
        if n == 0: print(i + 1); break" "$1" "${2:-1}"; }   # the screen row of the Nth sidebar row matching the regex (its text with the icons off)
click_row() { local y; y=$(nth_row "$@"); [[ -n $y ]] || return 1; e2e_click 14 "$y"; sleep 0.5; }   # click on the name part of a row
icon_fg() { local x; x=$(e2e_plain | sed -n "${1}p" | python3 -c 'import sys; l = sys.stdin.read(); print(next((i + 1 for i, c in enumerate(l) if "\ue000" <= c <= "\uf8ff"), ""))'); [[ -n $x ]] && e2e_style "$x" "$1" | grep -oE "fg=#[0-9a-f]+" | head -1; }   # the colour of the first icon on screen row Y
quit_app() { e2e_type ":"; sleep 0.4; e2e_type qa; key Enter; wait_for 8 exited; sleep 0.5; }
sstart() { e2e_start $SX -C "${DIR:-$D/c}" ${ST:+-S "$ST"} -P "$PATH" "$E2E_BIN $1"; wait_for 8 flag_is alternate_on 1; sleep 2; }
shown() { side | python3 -c "
import re, sys
on, n = False, 0
for l in sys.stdin:
    if not l.strip(): continue
    ind, t = len(l) - len(l.lstrip()), l.strip()
    if on and ind <= 2: break
    if on and ind == 5 and t[0] in '▸▾': n += int(re.search(r'(\\d+)\$', t).group(1))
    if on and ind == 7 and t[0] not in '▸▾' and not t.startswith('已扫描'): n += 1
    if not on and re.match(r'^▾ +db\\d+', t): on = True
print(n)"; }   # how many keys the first db's top level stands for: the number after each namespace, one for each key on its own
expand() { click_row "^[▸▾] +$1( |\$)" "${2:-1}"; }   # click the Nth node whose name is $1

# ---- 树的形状：命名空间按名字排、尾注是键数，根上的键在命名空间之后
sstart rd
want='▾ db0 29
▸ (空) 1
▸ a 1
▸ app 6
▸ cache 2
▸ hash 2
▸ list 2
▸ seed 1
▸ set 2
▸ str 7
▸ stream 1
▸ trail 1
▸ zset 1
app
nodelimiter
▸ db1
▸ db5
▸ db6
▸ db9
▸ db10'
check "db0（尾注 29）下面：(空) a app cache hash list seed set str stream trail zset 十二个命名空间按名字排，尾注是各自下面的键数；根上的键 app、nodelimiter 在后面；有键的 db1 db5 db6 db9 db10 收着、按号排（9 在 10 前）" eval '[[ $(tree) == "$want" ]]' || { tree; }
check "根上的键 app 前面是字符串的图标颜色" eval 'y=$(nth_row "^app\$" 1); [[ $(icon_fg "$y") == "fg=$STR" ]]' || { icon_fg "$(nth_row "^ *app *\$" 1)"; }
expand app; expand str; expand hash; expand list; expand set; expand zset; expand stream
check "展开 app：users(3) orders(2) config，名字按字节序排（config、orders、users：命名空间在前、键在后）" eval 'tree | grep -qzE "▾ app 6\n▸ orders 2\n▸ users 3\nconfig\n"' || tree
check "展开 str：七个键按名字排（binary json long number plain ttl unicode）" eval 'tree | grep -qzE "▾ str 7\nbinary\njson\nlong\nnumber\nplain\nttl\nunicode\n"' || tree
for t in "str:plain|$STR" "hash:user|$HASH" "list:queue|$LIST" "set:tags|$SET" "zset:rank|$ZSET" "stream:events|$STREAM"; do
  ns=${t%%:*}; rest=${t#*:}; k=${rest%%|*}; c=${rest#*|}
  check "$ns 下面的键 ${k}：图标的颜色是 ${c}（string 绿、hash 紫、list 橙、set 蓝、zset 黄、stream 青）" eval 'y=$(nth_row "^$k\$" 1); [[ $(icon_fg "$y") == "fg=$c" ]]' || { echo "  row $y: $(icon_fg "$(nth_row "^$k\$" 1)")"; }
done
expand trail; expand a
check "键名两头的分隔符：trail:（以 : 结尾）是命名空间 trail 下面一个没有名字的键「(空)」；a::b 是 a → (空) → b" eval 'tree | grep -qzE "▾ a 1\n▸ \(空\) 1\n" && tree | grep -qzE "▾ trail 1\n\(空\)\n"' || tree
y=$(nth_row '^[▸▾] +\(空\)( |$)' 1); e2e_click 14 "$y"; sleep 0.5
check "展开最前面的 (空)（:lead 的名字以分隔符开头）：里面是键 lead" eval 'tree | grep -qzE "▾ \(空\) 1\nlead\n"' || tree
quit_app; _e2e_kill

# ---- 右键菜单：键、命名空间、db 节点各有各的
rclick() { _sgr 2 "$1" "$2" M; _sgr 2 "$1" "$2" m; sleep 0.6; }
menu_items() { side_all | python3 -c "
import re, sys
for l in sys.stdin:
    m = re.search(r'│ +(打开|编辑…|删除…?|改名…|设 TTL…|刷新) +│', l)
    if m: print(m.group(1))"; }   # the items of the open menu, one per line
sstart rd
expand str
y=$(nth_row '^plain$' 1); rclick 14 "$y"
check "键上右键：菜单是 打开 / 删除… / 改名… / 设 TTL…" eval '[[ $(menu_items | tr "\n" "/") == "打开/删除…/改名…/设 TTL…/" ]]' || e2e_plain | sed -n $((y - 1)),$((y + 5))p | cut -c1-70
key Escape; sleep 0.3
y=$(nth_row '^[▸▾] +app( |$)' 1); rclick 14 "$y"
check "命名空间上右键：只有 删除…" eval '[[ $(menu_items | tr "\n" "/") == "删除…/" ]]' || e2e_plain | sed -n $((y - 1)),$((y + 4))p | cut -c1-70
key Escape; sleep 0.3
y=$(nth_row '^[▾▸] +db0( |$)' 1); rclick 14 "$y"
check "db 节点上右键：只有 刷新" eval '[[ $(menu_items | tr "\n" "/") == "刷新/" ]]' || e2e_plain | sed -n $((y - 1)),$((y + 4))p | cut -c1-70
key Escape; sleep 0.3
y=$(nth_row '^[▸▾] +rd( |$)' 1); rclick 14 "$y"
check "连接节点上右键：Redis 连接也有 编辑… / 删除…（F18.14，之前没有菜单）" eval '[[ $(menu_items | tr "\n" "/") == "编辑…/删除…/" ]]' || menu_items
key Escape; sleep 0.3
quit_app; _e2e_kill

# ---- 命令面板的「表」范围在 Redis 连接上列键（F20.3 起：树读全了的 db 的键，位置 rd/db0，标签 Key）
sstart rd
pal "@plain"; sleep 1
check "面板 @plain：Redis 连接上这个范围列键：str:plain（位置 rd/db0，标签 Key），set:binary 的成员 plain 不算（只列键名）" eval 'e2e_plain | grep -qE "str:plain +rd/db0 +Key" && [[ $(e2e_plain | grep -c "rd/db0") == 1 ]]' || e2e_plain | grep -n plain
key Escape; sleep 0.3
quit_app; _e2e_kill

# ---- 扫描预算：[redis] scan_keys 扫够了就停，db 下面写已扫描多少、一共多少；不超预算就没有这一行
mkdir -p "$D/b" "$D/w"; cp "$D/c/connections.toml" "$D/b/"; cp "$D/c/connections.toml" "$D/w/"
printf '[redis]\nscan_keys = 10\n\n[tree]\nwidth = 50\n' >"$D/b/config.toml"; printf '[tree]\nwidth = 50\n' >"$D/w/config.toml"
SW=50; DIR=$D/b; sstart rd
check "scan_keys = 10：db0（一共 29 个键）只读了 10 个：根上和命名空间的尾注加起来是 10，最后一行写「已扫描 10 / 29，用 / 过滤」，db0 的尾注还是 29" eval '[[ $(shown) == 10 ]] && side | grep -qF "已扫描 10 / 29，用 / 过滤" && [[ $(tree | head -1) == "▾ db0 29" ]]' || { shown; tree | head -3; side | grep 已扫描; }
quit_app; _e2e_kill
SW=50; DIR=$D/w; sstart big
check "默认预算 10000：db5（12001 个键）读了 10000 个，最后一行写「已扫描 10000 / 12k，用 / 过滤」，尾注 12k" eval '[[ $(shown) == 10000 ]] && side | grep -qF "已扫描 10000 / 12k，用 / 过滤" && [[ $(tree | head -1) == "▾ db5 12k" ]]' || { shown; tree | head -3; side | grep 已扫描; }
quit_app; _e2e_kill
sstart rd
check "键不超预算（db0 的 29 个）：没有「已扫描」那一行，29 个都在" eval '! side | grep -q 已扫描 && [[ $(shown) == 29 ]]' || { shown; side | grep 已扫描; }
quit_app; _e2e_kill; SW=; DIR=

# ---- 只用 SCAN：服务端上看到的命令（MONITOR）里没有 KEYS；扫描是 SCAN 0 MATCH * COUNT 1000，每个键一个 TYPE，db 的大小是 DBSIZE
rc monitor >"$D/mon.txt" 2>&1 & MON=$!
sleep 0.5
sstart rd
sleep 1
key C-h; key g g; key R; sleep 1.5
key /; e2e_type user; sleep 1.5; key Escape; sleep 0.5
quit_app; _e2e_kill; kill $MON 2>/dev/null; wait $MON 2>/dev/null
check "展开 db、R、过滤之后，服务端上没有见过 KEYS（只有 SCAN）" eval '! grep -qi "\"keys\"" "$D/mon.txt" && grep -qi "\"scan\"" "$D/mon.txt"' || grep -ic '"keys"' "$D/mon.txt"
check "扫描的参数是 SCAN 0 MATCH * COUNT 1000，db 的大小来自 DBSIZE，库的列表来自 INFO keyspace" eval 'grep -q "\"scan\" \"0\" \"match\" \"\\*\" \"count\" \"1000\"" "$D/mon.txt" && grep -q "\"dbsize\"" "$D/mon.txt" && grep -qi "\"info\" \"keyspace\"" "$D/mon.txt"' || grep -i "scan\|dbsize" "$D/mon.txt" | head -5
check "每个键取一次类型（TYPE，至少 29 个）" eval '(( $(grep -ci "\"type\" " "$D/mon.txt") >= 29 ))' || grep -ci '"type" ' "$D/mon.txt"
check "过滤 user 时问服务端的是 SCAN … MATCH *user*（两头补了 *）" eval 'grep -q "\"match\" \"\\*user\\*\"" "$D/mon.txt"' || grep -i "match" "$D/mon.txt" | head -5

# ---- 侧栏的 / 过滤：服务端的 SCAN MATCH，不是本地过滤
sstart rd
key C-h; key /; e2e_type user; sleep 1.5
check "/user：只剩命中的键，各命名空间都展开着（app → users → 1 2 3，hash → user），计数栏是 4/29" eval '[[ $(tree) == "▾ db0 29
▾ app 3
▾ users 3
1
2
3
▾ hash 1
user" ]] && side | sed -n 2p | grep -qE "^ *user +4/29"' || { tree; side | sed -n 2p; }
for i in 1 2 3 4; do key BSpace; done; e2e_type "app:*"; sleep 1.5
check "/app:*：写了 * 就照写，不再两头补：app: 开头的六个键（键 app 本身不在），命名空间都展开着，计数栏 6/29" eval '[[ $(tree) == "▾ db0 29
▾ app 6
▾ orders 2
100
101
▾ users 3
1
2
3
config" ]] && side | sed -n 2p | grep -qE "^ *app:\\* +6/29"' || { tree; side | sed -n 2p; }
for i in 1 2 3 4 5; do key BSpace; done; e2e_type "zzz"; sleep 1.5
check "/zzz：什么都没命中：树里没有 db 和键，计数栏 0/29" eval '[[ -z $(tree) ]] && side | sed -n 2p | grep -qE "^ *zzz +0/29"' || { tree; side | sed -n 2p; }
key Escape; sleep 0.6
check "esc 清掉过滤：整棵树回来（db0 29，十二个命名空间，db1…）" eval '[[ $(tree | head -2 | tr "\n" "/") == "▾ db0 29/▸ (空) 1/" && $(tree | grep -c "^▸") -ge 14 ]]' || tree
# 另一个 db 也连上时，过滤问每个连着的 db
click_row '^[▸▾] +db1( |$)' 1; sleep 1.5
key C-h; key /; e2e_type other; sleep 1.5
check "db1 也展开（连着）之后 /other：只有 db1 下面的 other（one two）命中，db0 不显示" eval '[[ $(tree) == "▾ db1 2
▾ other 2
one
two" ]]' || { tree; }
key Escape; sleep 0.5
quit_app; _e2e_kill

# ---- R：重扫光标所在的 db（新键、新命名空间、新的 db 都出来），尾注跟着变
sstart rd
rc set zz:new 1 >/dev/null; rc -n 12 set twelve 1 >/dev/null
key C-h; key g g; key j; key R; sleep 2
check "外面加了键 zz:new 和 db12：在 db0 上 R：命名空间 zz 出来（尾注 1），db0 的尾注 30，db12 排在 db10 后面" eval '[[ $(tree | head -1) == "▾ db0 30" ]] && tree | grep -qx "▸ zz 1" && [[ $(tree | grep -E "^▸ db" | tr "\n" " ") == "▸ db1 ▸ db5 ▸ db6 ▸ db9 ▸ db10 ▸ db12 " ]]' || tree
rc del zz:new >/dev/null; rc -n 12 del twelve >/dev/null
quit_app; _e2e_kill

# ---- 键名不是合法的 UTF-8：显示成 �，不影响别的
sstart rd
click_row '^[▸▾] +db6( |$)' 1; sleep 1.5
check "db6 里有一个键 k\\xffbin：树里显示 k�bin（一个替换字符），没有乱码、没有报错" eval 'side | grep -qF "k�bin" && running' || side | sed -n 20,30p
quit_app; _e2e_kill

# ---- 分隔符：连接的 delimiter 盖过 [redis] 的；写了一个不在键名里的分隔符，所有键都在根上；两个字符的分隔符也行
sstart slash
check "连接 slash（delimiter = \"/\"）：键名里没有 /，没有命名空间，29 个键都在 db0 下面一层（连 a::b、:lead、trail: 也是整个名字）" eval '[[ $(tree | head -1) == "▾ db0 29" ]] && [[ $(tree | grep -c "^▸ ") == 5 ]] && tree | grep -qx "a::b" && tree | grep -qx ":lead" && tree | grep -qx "trail:" && tree | grep -qx "app:users:1"' || tree
quit_app; _e2e_kill
sstart dbl
check "连接 dbl（delimiter = \"::\"）：只有 a::b 被切开（命名空间 a，尾注 1），其余整个名字在根上（app:users:1 …）" eval 'tree | grep -qx "▸ a 1" && tree | grep -qx "app:users:1" && tree | grep -qx ":lead" && ! tree | grep -q "▸ app"' || tree
quit_app; _e2e_kill
mkdir -p "$D/g"; cp "$D/c/connections.toml" "$D/g/"; printf '[redis]\ndelimiter = "/"\n' >"$D/g/config.toml"
DIR=$D/g; sstart rd
check "[redis] delimiter = \"/\"（全局）：rd 没写 delimiter，跟着它：没有命名空间" eval '[[ $(tree | grep -c "^▸ ") == 5 ]] && tree | grep -qx "app:users:1"' || tree
quit_app; _e2e_kill
sstart dbl
check "全局 /，连接 dbl 写了 \"::\"：连接的盖过全局，命名空间 a 出来" eval 'tree | grep -qx "▸ a 1" && tree | grep -qx "app:users:1"' || tree
quit_app; _e2e_kill; DIR=

# ---- 展开状态（在 session 上，只在内存里，§7.8）：节点按 db 和命名空间的路径区分；R 之后不丢
sstart rd
expand str; expand app; expand users
key C-h; key g g; key j; key R; sleep 2
check "展开 str、app → users 之后在 db0 上 R：重扫完它们还是展开的（展开状态保留）" eval 'tree | grep -qx "▾ str 7" && tree | grep -qx "▾ users 3" && tree | grep -qx "▾ app 6"' || tree
expand hash
click_row '^[▸▾] +db5( |$)' 1; wait_for 15 eval 'tree | grep -qx "▸ hash 1"'   # db5 has 12k keys: the scan is slow when four scripts run at once
check "db0 的 hash 展开着；点开 db5（它也有一个命名空间 hash）：db5 的 hash 是收着的，两个 db 的同名命名空间各管各的" eval 'tree | grep -qx "▾ hash 2" && tree | grep -qx "▸ hash 1"' || tree
y=$(nth_row '^[▸▾] +hash( |$)' 2); e2e_click 14 "$y"; sleep 0.5
check "展开 db5 的 hash（hash:big）：db0 的 hash 没有跟着变（两个都是 ▾）" eval '[[ $(tree | grep -c "^▾ hash") == 2 ]]' || tree
quit_app; _e2e_kill

e2e_done
