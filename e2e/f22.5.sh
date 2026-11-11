#!/usr/bin/env bash
# F22.5 面板的「键 :」范围（specs/m22-menus-keys/task.md F22.5；tech-design §12「范围与前缀」「Redis 键」）
# PG 和 Redis（几千键）连接同开：@tbl 只出表、没有键；:tbl 只出键，↵ 开键 tab；Tab 从「表」到「键」；「所有」里没有键；
# 冷启动进 : 才开始建索引（之前没有 SCAN），空输入列当前 db 的键。每个脚本自起 redis-server。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1
rc flushall >/dev/null
for i in $(seq 1 3000); do echo "set t_ord:$i v"; done | rc >/dev/null
rc set plain v >/dev/null
mkdir -p "$D/c"
(umask 077; printf '[[connection]]\nname = "pg"\nengine = "postgres"\ndsn = "%s"\n[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$SQLMUX_TEST_PG" "$RPORT" >"$D/c/connections.toml")
mon() { : >"$D/mon.txt"; redis-cli -p "$RPORT" monitor >>"$D/mon.txt" 2>&1 & MON=$!; sleep 0.5; }
names() { list | sed 's/|.*//' | tr '\n' ';'; }
opal() { e2e_keys C-p; sleep 0.4; e2e_type "$1"; sleep 1.5; }
cpal() { e2e_keys Escape; sleep 0.4; }
srow() { local y; for y in $(seq 3 43); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && { echo "$y"; return; }; done; }
ntabs() { e2e_plain | grep -oE "[0-9]:[^ ]* $1" | wc -l | tr -d " "; }

e2e_start -C "$D/c" "$E2E_BIN"; wait_for 8 flag_is alternate_on 1; sleep 3
mon
opal "@t_ord"
check "@t_ord：只出表（t_order、t_order_item），没有键" eval 'names | grep -q "t_order" && ! names | grep -q "Key;"' || names
cpal; opal "t_ord"
check "「所有」里 t_ord：有表，没有键" eval 'names | grep -q "t_order" && ! names | grep -q "Key;"' || names
check "线路：只开了「表」和「所有」，服务端没收到 SCAN（索引没开始建）" eval '! grep -aqiE "\"scan\"" "$D/mon.txt"' || grep -aiE '"scan"' "$D/mon.txt" | head -3
cpal; e2e_click 12 "$(srow rd)"; sleep 0.5   # the palette is of the connection the focus is on: rd
opal ":"
check "进「键」范围：开始建索引，服务端收到 SCAN" eval 'wait_for 8 eval "grep -aqiE \"\\\"scan\\\"\" \"$D/mon.txt\""'
wait_for 15 eval 'names | grep -q "Key;"'
check "空输入：列当前连接（rd）当前 db 的键（Key 标签）" eval 'names | grep -q "Key;"' || names
cpal; opal ":t_ord:2999"
check ":t_ord:2999：只出键（t_ord:2999 在，位置 rd/db0，标签 Key），没有表" eval 'names | grep -q "t_ord:2999  rd/db0  Key" && ! names | grep -q "t_order"' || names
key Enter; sleep 1
check "↵：开了 t_ord:2999 的键 tab，面板关了" eval 'closed && [[ $(ntabs t_ord:2999) == 1 ]]' || e2e_plain | sed -n 1,4p | cut -c30-150
opal "@t_ord"; key Tab; sleep 0.8
check "从「表」按 Tab：到「键」范围，输入是 :t_ord" input_is ":t_ord"
check "Tab 之后的列表里有键、没有表" eval 'names | grep -q "Key;" && ! names | grep -q "t_order"' || names
key Tab; sleep 0.5
check "再按 Tab：到「命令」范围（>t_ord）" eval 'input_is ">t_ord"'
cpal
kill "$MON" 2>/dev/null
e2e_done
