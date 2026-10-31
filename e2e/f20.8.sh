#!/usr/bin/env bash
# F20.8 键名索引的增量更新与后台匹配（specs/m20-redis-ui/task.md F20.8；tech-design §9.8「索引的增量更新与后台匹配」）
# 控制台写命令成功后：取命令里的键、一条 EXISTS 增量改索引，不整库 SCAN（SET 新键补得出、DEL 补不出、RENAME 两头、FLUSHDB 清空）；
# 树上 R 在后台重建，旧索引照用，补全不出现空档；面板 @ 里键的图标：树读到类型的用类型图标，其余通用 key 图标。每个脚本自起 redis-server。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1
mkdir -p "$D/c" "$D/cs"
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$RPORT" >"$D/c/connections.toml")
sstart() { e2e_start -C "$1" "$E2E_BIN rd"; wait_for 8 flag_is alternate_on 1; sleep 2; }
typed() { key C-l; key Escape; key g g; key d G; key i; e2e_type "$1"; sleep 1.2; }
run() { typed "$1"; key Escape; key Escape; key Enter; sleep 1; }               # type a command into the console and run it
screen_has() { e2e_plain | grep -qF -- "$1"; }
mon() { : >"$D/mon.txt"; redis-cli -p "$RPORT" monitor >>"$D/mon.txt" 2>&1 & MON=$!; sleep 0.5; }
unmon() { kill "$MON" 2>/dev/null; wait "$MON" 2>/dev/null; }
scans() { grep -ac '"scan"' "$D/mon.txt"; }
exists_n() { grep -aci '"exists"' "$D/mon.txt"; }
opal() { e2e_keys Escape; e2e_keys Escape; sleep 0.2; e2e_keys C-p; sleep 0.4; e2e_type "$1"; sleep 1.2; }
cpal() { e2e_keys Escape; sleep 0.3; }
tree_db0() { key C-h; local y x; y=$(e2e_plain | awk 'NR > 3 && index(substr($0, 1, 34), "db0") { print NR; exit }'); x=$(e2e_find db0 "$y" | cut -d' ' -f1); e2e_click "$x" "$y"; sleep 0.5; }
# icon_of NAME: the icon of the palette's row for the key NAME (the two cell columns before the name)
icon_of() { local s1 s2 l i; read s1 s2 <<<"$(seps)"; l=$(left); i=$(list | grep -n "^$1 " | head -1 | cut -d: -f1); [[ -n $i ]] || { echo none; return; }
  e2e_text $((l + 2)) $((l + 3)) $((s1 + i)) | tr -d ' ' | python3 -c 'import sys; print("".join("U+%04X" % ord(c) for c in sys.stdin.read().strip()))'; }
haskey() { list | grep -q "^$1 .*Key|"; }
KEYIC=U+F084 STRIC=U+F031 HASHIC=U+F292

rc flushall >/dev/null
for k in str:one set:s1 user:1 ; do :; done
rc set str:one v >/dev/null; rc hset hash:h1 f v >/dev/null; rc sadd set:s1 m >/dev/null; rc set user:1 v >/dev/null

sstart "$D/c"
# ---- @ 里键的图标：树读全了的 db 每个键的类型都读到了（树加载时发 TYPE），用类型图标
opal "@str:one"; wait_for 6 eval 'haskey str:one'
check "@str:one：树读到类型了：string 图标（U+F031）" eval '[[ $(icon_of str:one) == $STRIC ]] || { echo "  icon: $(icon_of str:one)"; list; false; }'
cpal; opal "@hash:h1"; wait_for 6 eval 'haskey hash:h1'
check "@hash:h1：hash 图标（U+F292）" eval '[[ $(icon_of hash:h1) == $HASHIC ]] || { echo "  icon: $(icon_of hash:h1)"; list; false; }'
cpal; opal "@set:s1"; wait_for 6 eval 'haskey set:s1'
check "@set:s1：set 图标（U+F009）" eval '[[ $(icon_of set:s1) == U+F009 ]] || { echo "  icon: $(icon_of set:s1)"; list; false; }'
cpal

# ---- 写命令后增量更新：一条 EXISTS，没有整库 SCAN
typed "GET m"; sleep 1                                                    # the index exists (the tree read all of db0)
mon
run "SET m20:new 1"
typed "GET m20:n"
check "SET m20:new 1 之后马上补全 m20:n：m20:new 在候选里" eval 'screen_has m20:new' || e2e_plain | sed -n 1,10p | cut -c60-165
check "线路：写命令之后服务端收到一条 EXISTS m20:new，没有 SCAN" eval '[[ $(exists_n) == 1 ]] && grep -aqi "\"exists\" \"m20:new\"" "$D/mon.txt" && [[ $(scans) == 0 ]]' || { grep -aiE "exists|scan" "$D/mon.txt" | head -4; }
opal "@m20:new"; wait_for 6 eval 'haskey m20:new'
check "@m20:new：增量加进索引的键没读过类型：面板里有它，通用 key 图标（U+F084）" eval '[[ $(icon_of m20:new) == $KEYIC ]] || { echo "  icon: $(icon_of m20:new)"; list; e2e_plain | sed -n 8,16p | cut -c10-130; false; }'
cpal
run "DEL m20:new"
typed "GET m20:n"
check "DEL m20:new 之后补全 m20:n：m20:new 不在了" eval '! screen_has m20:new'
check "线路：DEL 之后又一条 EXISTS（共 2 条），仍然没有 SCAN" eval '[[ $(exists_n) == 2 && $(scans) == 0 ]]' || { grep -aiE "exists|scan" "$D/mon.txt" | head -4; }
run "SET m20:a 1"; run "RENAME m20:a m20:b"
typed "GET m20:"
check "RENAME m20:a m20:b 之后补全 m20:：m20:b 在、m20:a 不在" eval 'screen_has m20:b && ! screen_has m20:a' || e2e_plain | sed -n 1,10p | cut -c60-165
check "线路：RENAME 两头各一条 EXISTS（m20:a、m20:b，一次往返发出），没有 SCAN" eval 'grep -aqi "\"exists\" \"m20:b\"" "$D/mon.txt" && [[ $(grep -aci "\"exists\" \"m20:a\"" "$D/mon.txt") == 2 ]] && [[ $(scans) == 0 ]]' || { grep -aiE "exists|scan" "$D/mon.txt" | tail -4; }
unmon
run "FLUSHDB"
typed "GET st"
check "FLUSHDB 之后索引清空：补全里没有 str:one（准确的空，不是旧的）" eval '! screen_has str:one'

# ---- 树上 R 在后台重建，旧索引照用：慢代理（一次来回 300 ms），12006 个键，树被截断
rc flushall >/dev/null; redis_big 0; for k in i18n_agent:expert:zh i18n:hello user:1; do rc set "$k" v >/dev/null; done
redis_slow 300 || exit 1
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$SPORT" >"$D/cs/connections.toml")
sstart "$D/cs"; mon
quiet() { local a b=-1 n=0; while ((n < 4)); do a=$(wc -l <"$D/mon.txt"); [[ $a == "$b" ]] && n=$((n + 1)) || n=0; b=$a; sleep 1; done; }
quiet; : >"$D/mon.txt"                                                       # 树先加载完（10000 键的 SCAN 和 TYPE 在慢代理下要十几秒）：线路安静 4 秒再往下
typed "SMEMBERS i18n"                                                       # 索引在后台建（树被截断，一批 COUNT 1000，慢代理下每批半秒以上）
quiet                                                                       # 线路安静 4 秒：前缀 SCAN 和索引的批都跑完了，索引建好
sleep 1
typed "SMEMBERS expert"
wait_for 10 eval 'screen_has i18n_agent:expert:zh'
check "（准备）索引建好了：SMEMBERS expert 补出 i18n_agent:expert:zh" eval 'screen_has i18n_agent:expert:zh'
unmon; mon
tree_db0; key R; sleep 0.5; key C-l                                      # 树 → ①；typed 再 C-l 到 ② 的控制台
typed "SMEMBERS expert"
n1=$(scans); sleep 1.5; n2=$(scans)
check "树上 R 之后重建在后台跑（服务端的 SCAN 还在增加），同时补全照常有 i18n_agent:expert:zh（旧索引照用，没有空档）" eval 'screen_has i18n_agent:expert:zh && (( n2 > n1 ))' 
unmon

e2e_done
