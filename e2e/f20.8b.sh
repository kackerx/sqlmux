#!/usr/bin/env bash
# F20.8（续）键名索引的增量更新：不写进 f20.8.sh 的几种写（specs/m20-redis-ui/task.md F20.8；tech-design §9.8「索引的增量更新与后台匹配」）
# SET … NX 没写成、EXPIRE 0、快速 SQL 的写、树菜单删键 / 改名、FLUSHALL 清空；认不出键的写（EVAL）在后台整份重建，重建期间补全用旧索引，期间写的键在新索引换上后还在。
# 每个脚本自起 redis-server（redis.sh），用 redis-cli monitor 看线路。
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
run() { typed "$1"; key Escape; key Escape; key Enter; sleep 1; }
screen_has() { e2e_plain | grep -qF -- "$1"; }
top_has() { e2e_plain | sed -n 3,12p | cut -c60- | grep -qF -- "$1"; }   # the completion list under the typed line (rows 3–12 of the query pane), not the line itself or the toast
mon() { : >"$D/mon.txt"; redis-cli -p "$RPORT" monitor >>"$D/mon.txt" 2>&1 & MON=$!; sleep 0.5; }
unmon() { kill "$MON" 2>/dev/null; wait "$MON" 2>/dev/null; }
scans() { grep -ac '"scan"' "$D/mon.txt"; }
quiet() { local a b=-1 n=0; while ((n < 4)); do a=$(wc -l <"$D/mon.txt"); [[ $a == "$b" ]] && n=$((n + 1)) || n=0; b=$a; sleep 1; done; }
# qs TEXT: quick SQL from the sidebar (`;` opens the palette's SQL scope), ↵ runs it
qs() { key Escape; key Escape; key C-h; sleep 0.3; e2e_type ";$1"; sleep 1.2; [[ -n $(pop) ]] && { key Escape; sleep 0.3; }; key Enter; sleep 1.8; }   # a completion list open is closed first (↵ would accept it)
rclick() { _sgr 2 "$1" "$2" M; _sgr 2 "$1" "$2" m; sleep 0.6; }
trow() { e2e_plain | awk -v n="$1" 'NR > 3 && index(substr($0, 1, 34), n) { print NR; exit }'; }

rc flushall >/dev/null
rc set solo v >/dev/null; rc set user:1 v >/dev/null; rc set hold:k v >/dev/null; rc set bye:k v >/dev/null
sstart "$D/c"
typed "GET m"; sleep 1; mon

# ---- SET … NX 没写成：键还在；EXPIRE 0：键没了
run "SET hold:k other NX"
typed "GET hold"
check "SET hold:k other NX（已存在，没写成）：补全 hold 里 hold:k 还在" eval 'screen_has hold:k'
run "EXPIRE bye:k 0"
typed "GET bye"
check "EXPIRE bye:k 0（键过期删掉）：补全 bye 里 bye:k 不在了" eval '! screen_has bye:k'
check "线路：两条命令各一条 EXISTS，没有 SCAN" eval '[[ $(grep -aci "\"exists\"" "$D/mon.txt") == 2 && $(scans) == 0 ]]' || { grep -aiE "exists|scan" "$D/mon.txt" | head -5; }

# ---- 快速 SQL（; 面板）里的写同样
qs "SET qs:new 1"; key Escape; sleep 0.4                                  # ↵ 跑完面板还开着（结果区），esc 关掉
typed "GET qs:"
check "（准备）快速 SQL 真的跑了 SET qs:new 1" eval '[[ $(rc get qs:new) == 1 ]]'
check "快速 SQL 里 SET qs:new 1 之后：控制台补全 qs: 里有 qs:new（不用 R）" eval 'screen_has qs:new' || e2e_plain | sed -n 1,10p | cut -c60-165
check "线路：没有 SCAN" eval '[[ $(scans) == 0 ]]'

# ---- 树菜单：删键、改名（键 solo 在 db0 下第一层）
key C-h; sleep 0.3
y=$(trow solo); rclick 14 "$y"
check "（准备）solo 上右键：菜单是 打开 / 删除… / 改名… / 设 TTL…" eval 'e2e_plain | grep -q "删除…" && e2e_plain | grep -q "改名…"'
key j; key Enter; sleep 0.8; key y; sleep 1.2
check "（准备）树上删掉了键 solo（库里没有了）" eval '[[ $(rc exists solo) == 0 ]]'
key C-l; typed "GET solo"
check "树菜单删键之后：补全 solo 里没有 solo（不用 R）" eval '! top_has solo' || e2e_plain | sed -n 1,10p | cut -c60-165
key C-h; sleep 0.3
y=$(trow user)
for i in 1 2; do e2e_plain | sed -n "${y}p" | cut -c1-34 | grep -q "▸" && { e2e_click $(e2e_find "▸" "$y" | head -1 | cut -d" " -f1) "$y"; sleep 0.8; }; done   # expand user: click its arrow (a click on an unfocused tree may only focus it)
rclick 14 $((y + 1))                                                       # its only key, 1
key j; key j; key Enter; sleep 0.8; for i in $(seq 1 12); do key BSpace; done; e2e_type "user:renamed"; key Enter; sleep 1.5          # 状态栏的输入框里是整个键名
check "（准备）树上把 user:1 改名成 user:renamed" eval '[[ $(rc exists user:renamed) == 1 && $(rc exists user:1) == 0 ]]' || e2e_plain | sed -n 1,12p | cut -c1-80
key C-l; typed "GET user:"
check "树菜单改名之后：补全 user: 里有 user:renamed、没有 user:1（不用 R）" eval 'top_has user:renamed && ! top_has "user:1"' || e2e_plain | sed -n 1,10p | cut -c60-165

# ---- FLUSHALL 清空（这个连接所有 db）；之后外面写的键要 R
run "FLUSHALL"; e2e_plain | grep -q "y 执行" && { key y; sleep 1; }
check "（准备）FLUSHALL 跑了：库是空的" eval '[[ $(rc dbsize) == 0 ]]'
typed "GET q"
check "FLUSHALL 之后：补全里没有 qs:new（索引清空）" eval '! top_has qs:new'
unmon

# ---- EVAL（认不出键）整份重建，旧索引照用，重建期间写的键不丢（慢代理，12006 个键，树被截断）
rc flushall >/dev/null; redis_big 0; rc set old:one v >/dev/null
redis_slow 300 || exit 1
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$SPORT" >"$D/cs/connections.toml")
sstart "$D/cs"; mon; quiet; : >"$D/mon.txt"
typed "GET old:o"; quiet                                                   # 第一次补全：建索引，等它建完
typed "GET old:o"; wait_for 10 eval 'screen_has old:one'
check "（准备）索引建好了：GET old:o 补出 old:one" eval 'screen_has old:one'
: >"$D/mon.txt"
run "EVAL \"redis.call('SET', 'ev:made', '1')\" 0"
sleep 1.5
n1=$(scans)
typed "GET old:o"
check "EVAL 认不出键：整份重建在后台跑（服务端的 SCAN 在走），这期间补全用旧索引，仍有 old:one" eval 'screen_has old:one && (( n1 > 0 ))' || { echo "  scans $n1"; e2e_plain | sed -n 1,8p | cut -c60-165; }
run "SET during:rebuild 1"
quiet
typed "GET ev:"
check "重建完：EVAL 写的 ev:made 在新索引里（SCAN 扫到）" eval 'screen_has ev:made'
typed "GET during:"
check "重建期间 SET 的 during:rebuild 在新索引换上之后还在（增量记下来、换上时再应用一遍）" eval 'screen_has during:rebuild'
unmon

# ---- 匹配在算（超过 2000 个名字走 tea.Cmd）时按 ↵：记下这次 ↵，结果回来、上下文没变就接受新列表选中的那项，不接受旧列表的候选，也不吞 ↵
rc flushall >/dev/null; redis_big 0
sstart "$D/c"; sleep 2
typed "GET b"; sleep 6; key Escape                                          # 索引建好（12000 个 bulk: 键，树被截断）
typed "GET bk7777"; key Enter; sleep 2                                      # 最后一个字之后马上 ↵，不等补全
check "打完 GET bk7777 马上 ↵：接受的是新列表选中的 bulk:7777，不是旧列表（GET b、GET bk777 …）里的某一项，也没有吞掉 ↵" eval 'e2e_text 60 160 2 | grep -q "GET bulk:7777 *│"' || e2e_plain | sed -n 1,6p | cut -c60-165

e2e_done
