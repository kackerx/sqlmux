#!/usr/bin/env bash
# F20.8（续二）键名索引：键 tab 的写、FLUSHALL 清所有 db、带 numkeys / SWAPDB / MULTI-EXEC 的整份重建（specs/m20-redis-ui/task.md F20.8；tech-design §9.8「索引的增量更新与后台匹配」）
# 键 tab 里删光 hash 的最后一个字段、点 DELETE、点 RENAME 之后补全跟上（不用 R）；db1 先建过索引，在 db0 执行 FLUSHALL 后 db1 的索引也清空；
# ZUNIONSTORE（numkeys）、SWAPDB、MULTI…EXEC 触发后台整份重建（线路上有 SCAN），重建后补全是新内容。每个脚本自起 redis-server。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1
mkdir -p "$D/c"
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$RPORT" >"$D/c/connections.toml")
sstart() { e2e_start -x 200 -C "$D/c" "$E2E_BIN rd"; wait_for 8 flag_is alternate_on 1; sleep 2; }
to2() { key C-h; key C-l; key C-l; }                                       # tree → ① → ②, wherever the focus was
typed() { to2; key Escape; key g g; key d G; key i; e2e_type "$1"; sleep 1.5; }
run() { typed "$1"; key Escape; key Escape; key Enter; sleep 1.2; e2e_plain | grep -q "y 执行" && { key y; sleep 1; }; }
top_has() { e2e_plain | sed -n 3,12p | cut -c90- | grep -qF -- "$1"; }       # the completion list under the typed line of ②
mon() { : >"$D/mon.txt"; redis-cli -p "$RPORT" monitor >>"$D/mon.txt" 2>&1 & MON=$!; sleep 0.5; }
unmon() { kill "$MON" 2>/dev/null; wait "$MON" 2>/dev/null; }
scans() { grep -ac '"scan"' "$D/mon.txt"; }
toast() { e2e_text 1 "$(e2e_flag pane_width)" "$(( $(H) - 1 ))" | python3 -c 'import sys; print(sys.stdin.read().strip("└─┘│ \n"))'; }
side() { e2e_plain | python3 -c 'import re, sys; [print(re.sub("[-] ?", "", l[1:33]).rstrip("│ ")) for l in sys.stdin.read().split("\n")]'; }
nth_row() { side | python3 -c "
import re, sys
n = int(sys.argv[2])
for i, l in enumerate(sys.stdin.read().split('\\n')):
    if re.search(sys.argv[1], l.strip()):
        n -= 1
        if n == 0: print(i + 1); break" "$1" "${2:-1}"; }
click_row() { local y; y=$(nth_row "$@"); [[ -n $y ]] || return 1; e2e_click 14 "$y"; sleep 0.5; }
expand() { click_row "^[▸▾] +$1( |\$)" "${2:-1}"; }
to_row() { local y i; y=$(nth_row "$1" "${2:-1}"); key C-h; key g g; for ((i = 4; i < y; i++)); do key j; done; }
open_key() { key C-h; key C-l; to_row "$1" "$2"; key Enter; sleep 1.2; }   # the key tab opens in the pane the focus came from: ①, not the console's ②
chip_click() { local x; x=$(e2e_find "$1" 2 | cut -d' ' -f1); [[ -n $x ]] && { e2e_click $((x + 1)) 2; sleep 0.6; }; }   # a chip of the key tab's info row
confirm_open() { e2e_plain | grep -qE "y (删除|覆盖) +n 取消"; }
# db DBn: the console's db dropdown (the chip "rd · db0 ▾" in ②'s title) picks it
pickdb() { local x; to2; x=$(e2e_find "db0 ▾" 1 | head -1 | cut -d' ' -f1); [[ -n $x ]] || x=$(e2e_find "db1 ▾" 1 | head -1 | cut -d' ' -f1); e2e_click $((x + 1)) 1; sleep 0.8; e2e_type "$1"; sleep 0.4; key Enter; sleep 1; }

rc flushall >/dev/null
rc set a:one v >/dev/null; rc set hold v >/dev/null
rc hset hash:solo f v >/dev/null; rc set del:me v >/dev/null; rc set ren:me v >/dev/null
rc -n 1 set b:two v >/dev/null
sstart
typed "GET a"; sleep 1

# ---- 键 tab 的写：删光 hash 的最后一个字段
typed "GET hash:s"
check "（对照）写之前：补全 hash:s 里有 hash:solo；GET del 里有 del:me；GET ren 里有 ren:me" eval 'top_has hash:solo && typed "GET del" && top_has del:me && typed "GET ren" && top_has ren:me'
expand hash; open_key '^solo$'
check "（准备）打开 hash:solo：一个字段 f" eval 'e2e_plain | grep -q "hash:solo" && e2e_plain | grep -qE "│ +1 +│ *f +│ *v"' || e2e_plain | sed -n 1,8p | cut -c34-120
key g g; key d; key d; sleep 0.3; key C-s; sleep 1.5
check "（准备）dd、C-s 写回：hash:solo 没了" eval '[[ $(rc exists hash:solo) == 0 ]]'
typed "GET hash:s"
check "键 tab 里删光 hash 的最后一个字段后：补全 hash:s 里没有 hash:solo（不用 R）" eval '! top_has hash:solo' || e2e_plain | sed -n 1,10p | cut -c90-190

# ---- 键 tab 的 DELETE、RENAME 芯片
expand del; open_key '^me$'
chip_click DELETE; sleep 0.5; confirm_open && { key y; sleep 1.5; }
check "（准备）点 DELETE 删掉了 del:me" eval '[[ $(rc exists del:me) == 0 ]]' || { toast; e2e_plain | sed -n 1,5p | cut -c34-150; }
typed "GET del"
check "键 tab 里删键之后：补全 del 里没有 del:me（不用 R）" eval '! top_has del:me'
expand ren; open_key '^me$'
chip_click RENAME; sleep 0.5
for i in $(seq 1 8); do key BSpace; done; e2e_type "ren:done"; key Enter; sleep 1.5
check "（准备）点 RENAME 改名成 ren:done" eval '[[ $(rc exists ren:me) == 0 && $(rc exists ren:done) == 1 ]]' || toast
typed "GET ren"
check "键 tab 里改名之后：补全 ren 里有 ren:done、没有 ren:me（不用 R）" eval 'top_has ren:done && ! top_has ren:me' || e2e_plain | sed -n 1,10p | cut -c90-190

# ---- 带 numkeys / SWAPDB / MULTI…EXEC：整份重建（线路上有 SCAN）
rc set za 1 >/dev/null; rc zadd zs1 1 m >/dev/null; rc zadd zs2 2 n >/dev/null
typed "GET a"; sleep 1; mon
run "ZUNIONSTORE dst:z 2 zs1 zs2"
sleep 2
typed "GET dst"
check "ZUNIONSTORE dst:z 2 zs1 zs2（带 numkeys，认不出目的键）：整份重建，线路上有 SCAN；重建后补全 dst 里有 dst:z" eval '(( $(scans) > 0 )) && top_has dst:z' || { echo "  scans $(scans)"; e2e_plain | sed -n 1,10p | cut -c90-190; }
unmon; mon
run "MULTI"; run "SET m:multi 1"; run "EXEC"
sleep 2
typed "GET m:"
check "MULTI / SET m:multi 1 / EXEC：EXEC 之后整份重建（线路上有 SCAN），补全 m: 里有 m:multi" eval '[[ $(rc exists m:multi) == 1 ]] && (( $(scans) > 0 )) && top_has m:multi' || { echo "  exists $(rc exists m:multi) scans $(scans)"; e2e_plain | sed -n 1,10p | cut -c90-190; }
unmon; mon
run "SWAPDB 0 1"
sleep 2
typed "GET b"
check "SWAPDB 0 1：整份重建当前 db（db0）；补全里是换过来的那批键：b:two 在、a:one 不在" eval '(( $(scans) > 0 )) && top_has b:two && ! top_has a:one' || { echo "  scans $(scans)"; e2e_plain | sed -n 1,10p | cut -c90-190; }
unmon
run "SWAPDB 0 1"; sleep 2                                                    # swap back

# ---- FLUSHALL 清这个连接所有 db 的索引：db1 先建过索引
pickdb db1
typed "GET a"; sleep 3; typed "GET a"
check "（准备）console 切到 db1 后：补全 GET a 里是 db1 的键…（db1 里只有 b:two，没有 a:*，所以空）" eval '! top_has a:one' || true
typed "GET b"; sleep 3; typed "GET b"
check "（准备）db1 的索引建好了：补全 GET b 里有 b:two" eval 'top_has b:two' || e2e_plain | sed -n 1,10p | cut -c90-190
pickdb db0
run "FLUSHALL"
check "（准备）FLUSHALL 跑了：db0、db1 都空了" eval '[[ $(rc dbsize) == 0 && $(rc -n 1 dbsize) == 0 ]]'
rc -n 1 set b:fresh v >/dev/null                                             # 从外面写进 db1：不在索引里（索引是空的，不是旧的）
pickdb db1
typed "GET b"; sleep 1.5; typed "GET b"
check "FLUSHALL 之后 db1 的索引也清空：补全 GET b 里没有旧的 b:two（外面写的 b:fresh 要 R 才进，所以也没有）" eval '! top_has b:two && ! top_has b:fresh' || e2e_plain | sed -n 1,10p | cut -c90-190

e2e_done
