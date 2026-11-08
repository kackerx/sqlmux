#!/usr/bin/env bash
# F21.4 的「启动即用」在真实 Redis 上（specs/m21-cli-outer/task.md F21.4；tech-design §9.8「索引存盘」；审查提的手测项）：
# 建完索引 :qa；在外面 DEL 几个键、SET 几个新键；重启后第一次补全先出旧键、底部「索引更新中」，重建完旧键没了、新键有了，文件也换成新的；
# 控制台里 FLUSHDB 之后文件马上写空，重启后补全里没有旧键。2 万键、150ms 代理（慢到看得见「更新中」）。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1
rc flushall >/dev/null
rc -n 0 EVAL "for i = 1, 20000 do redis.call('SET', 'q:other:' .. i, 1) end
for i = 1, 5 do redis.call('SET', 'old:a:' .. i, 1) end" 0 >/dev/null
redis_slow 150 || exit 1
mkdir -p "$D/c" "$D/data"
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$SPORT" >"$D/c/connections.toml"
 printf 'confirm_dangerous = false\n' >"$D/c/config.toml")
FILE="$D/data/sqlmux/redis-index/rd/db0"
sstart() { e2e_start -C "$D/c" -D "$D/data" "$E2E_BIN rd"; wait_for 8 flag_is alternate_on 1; sleep 12; }   # COMMAND DOCS comes through the slow proxy
to_console() { key C-l; key Escape; key g g; key d G; key i; }
quit_app() { key Escape; e2e_type ':qa'; e2e_keys Enter; wait_for 8 screen_has '[e2e-exit 0]'; }
clearline() { key Escape; key g g; key d G; key i; }
has() { e2e_plain | grep -qF -- "$1"; }

# ---- 第一次：建索引，文件写出来
sstart; to_console; e2e_type "get old:"; sleep 1
check "第一次运行：文件里有 20005 个键（头一行总数 20005）" eval 'wait_for 90 test -s "$FILE" && [[ $(head -1 "$FILE") == "sqlmux-redis-index 1 20005 0" ]] && grep -qx "old:a:1" "$FILE" || { ls -l "$FILE"; head -2 "$FILE"; false; }'
quit_app

# ---- 外面改库：删旧键、加新键
rc -n 0 EVAL "for i = 1, 5 do redis.call('DEL', 'old:a:' .. i); redis.call('SET', 'new:b:' .. i, 1) end" 0 >/dev/null
check "（准备）库里 old:a:* 没了，new:b:* 有 5 个" eval '[[ $(rc exists old:a:1) == 0 && $(rc exists new:b:5) == 1 ]]'

# ---- 第二次：先出旧键 + 「索引更新中」，重建完旧键消失、新键出现
sstart; to_console; e2e_type "get old:a"
check "重启后第一次补全：马上有旧键 old:a:1（文件里的索引），底部有「索引更新中」" eval 'wait_for 3 has "old:a:1" && has "索引更新中"' || e2e_plain | tail -12
check "重建完：「索引更新中」没了（最多等 90 秒）" eval 'wait_for 90 eval "! e2e_plain | grep -qF 索引更新中"'
clearline; e2e_type "get old:a"; sleep 1
check "重建完：old:a:* 不在补全里了" eval '! has "old:a:1"'
clearline; e2e_type "get new:b"; sleep 1
check "重建完：新键 new:b:1 在补全里" eval 'has "new:b:1"'
check "文件换成新的：有 new:b:1、没有 old:a:1，总数 20005" eval 'wait_for 10 grep -qx "new:b:1" "$FILE" && ! grep -qx "old:a:1" "$FILE" && [[ $(head -1 "$FILE") == "sqlmux-redis-index 1 20005 0" ]]'

# ---- 控制台里 FLUSHDB：文件马上写空，重启后补全里没有旧键
clearline; e2e_type "flushdb"; key Escape; key Enter; sleep 3
check "FLUSHDB 之后库空了，文件只剩头一行（总数 0）" eval '[[ $(rc dbsize) == 0 ]] && wait_for 8 eval "[[ \$(wc -l <\"$FILE\") == 1 ]]" && [[ $(head -1 "$FILE") == "sqlmux-redis-index 1 0 0" ]]' || { head -2 "$FILE"; rc dbsize; }
quit_app
sstart; to_console; e2e_type "get new:b"; sleep 2
check "FLUSHDB 后重启：补全里没有旧键 new:b:1" eval '! has "new:b:1"'
e2e_done
