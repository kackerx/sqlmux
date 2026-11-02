#!/usr/bin/env bash
# F20.9 树的过滤：浏览结果时 esc 回到输入框（specs/m20-redis-ui/task.md F20.9；tech-design §7.8 侧栏第一行）
# 树上有过滤条件、焦点在树（不在输入框）时，esc = /：进输入框（INSERT），过滤文字保留；输入框里再 esc 清空过滤并退出，树恢复；没有过滤条件时 esc 照旧。
# SQL 树（共用库只读）和 Redis 树（自起 redis-server）各测一遍。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
filt() { e2e_text 2 31 2 | sed 's/ *$//; s/^[^[:alnum:]]*//'; }          # the sidebar's first row (filter input / placeholder)
tree_has() { e2e_plain | cut -c1-34 | grep -q "$1"; }
bar_mode() { bar | grep -oE "(NORMAL|INSERT|VISUAL)" | tail -1; }

# ---- SQL 树
start
key C-h; sleep 0.3
key Escape; sleep 0.4
check "没有过滤条件时树上 esc 照旧：不进输入框（NORMAL），第一行不是输入的文字" eval '[[ $(bar_mode) == NORMAL ]] && tree_has t_sku'
key /; e2e_type ord; key Enter; sleep 0.6
check "（准备）/ 输 ord ↵：过滤着，树里有 t_order、没有 t_sku，焦点在树（NORMAL）" eval 'tree_has t_order && ! tree_has t_sku && [[ $(bar_mode) == NORMAL ]]'
key j; key j; sleep 0.3
key Escape; sleep 0.5
check "过滤着、焦点在树，esc：回到输入框（INSERT），第一行是 ord，树还是过滤后的（有 t_order、没有 t_sku）" eval '[[ $(bar_mode) == INSERT && $(filt) == ord* ]] && tree_has t_order && ! tree_has t_sku' || { bar_mode; filt; }
key Escape; sleep 0.6
check "输入框里再 esc：过滤清空，树恢复（t_sku 回来了），回到 NORMAL" eval 'tree_has t_sku && tree_has t_order && [[ $(bar_mode) == NORMAL && $(filt) != ord* ]]' || { bar_mode; filt; }
key Escape; sleep 0.4
check "过滤清掉之后树上 esc 又是照旧：还在 NORMAL，树不变" eval '[[ $(bar_mode) == NORMAL ]] && tree_has t_sku'
key /; e2e_type sku; key Enter; sleep 0.6; key C-c; sleep 0.5
check "C-c 在树上还是 cancel（决策者 2026-11-02）：过滤着、焦点在树按 C-c，不进输入框（NORMAL），过滤文字不变（sku 1/…）" eval '[[ $(bar_mode) == NORMAL && $(filt) == sku* ]] && tree_has t_sku && ! tree_has t_order' || { bar_mode; filt; }
e2e_stop

# ---- Redis 树
redis_start R || exit 1
rc flushall >/dev/null; for k in p:employees:1 p:employees:2 p:other:1 q:x:1; do rc set "$k" v >/dev/null; done
mkdir -p "$D/c"; (umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$RPORT" >"$D/c/connections.toml")
e2e_start -C "$D/c" "$E2E_BIN"; wait_for 8 flag_is alternate_on 1; sleep 2
key C-h; sleep 0.3
key /; e2e_type employees; key Enter; sleep 1.5
check "（准备）Redis 树 / 输 employees ↵：过滤着（有 employees、没有 q），焦点在树" eval 'tree_has employees && ! tree_has "▸.* q " && [[ $(bar_mode) == NORMAL ]]'
key j; key Escape; sleep 0.6
check "Redis 树过滤着、焦点在树，esc：回到输入框（INSERT），文字 employees，树还是过滤后的" eval '[[ $(bar_mode) == INSERT && $(filt) == employees* ]] && tree_has employees' || { bar_mode; filt; }
key Escape; sleep 1.5
check "输入框里再 esc：过滤清空，树恢复（q 命名空间回来了），回到 NORMAL" eval 'tree_has " q " && [[ $(bar_mode) == NORMAL ]]' || { bar_mode; filt; }

e2e_done
