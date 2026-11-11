#!/usr/bin/env bash
# F22.8 只画底部说明的框只给 Redis 索引那一路（specs/m22-menus-keys/task.md F22.8；tech-design §9.4、§9.8）
# WHERE 历史：被输入过滤光了什么框都不画，删掉输入又有匹配时框重新出来、选中项是那一条（select 底）；
# Redis 控制台：索引封顶（[redis] index_keys = 100，库里 2.5 万键：多于一批 SCAN，封顶时游标还没回 0）时 GET k 的候选框底部是「已索引 100 / 共 25000 键」，输入匹配不到任何键时什么都不画。
# PG 共用库（只读）、state 放临时目录；自起 redis-server。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
typ() { e2e_type "$1"; sleep 0.4; }
edit() { key /; clear_in; }
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }'; }
items() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | noicon | tr -s ' ' | sed 's/^ //; s/ $//'; done; }

# ---- WHERE 历史
SOLO=1 start -S "$D/st"; open_table t_order; wait_for 8 settled
edit; typ "status = 'done'"; key Enter; wait_for 8 settled; sleep 0.3
edit; key C-r; sleep 0.6
check "（准备）C-r：下拉里是 status = 'done' 那一条" eval 'items | grep -q "^status = .done." || { items; false; }'
typ "id = 1"
check "输入 id = 1 把历史过滤光：什么框都不画（连提示行也没有）" eval '[[ -z $(pop) ]] && ! e2e_plain | grep -q "C-p/C-n 移动"' || { pop; items; }
for i in 1 2 3 4 5 6; do key BSpace; done; sleep 0.6
check "删掉输入：框重新出来，列着 status = 'done'" eval '[[ -n $(pop) ]] && items | grep -q "^status = .done." || { pop; items; false; }'
g=($(pop)); y=$((g[1] + 1))
check "选中项是那一条（第一行是 select 底 #364a82）" eval '[[ $(e2e_style $((g[0] + 4)) $y) == *bg=#364a82* ]]' || e2e_style $((g[0] + 4)) $y
key Enter; wait_for 8 settled; sleep 0.3
check "↵ 应用它：WHERE 是 status = 'done'" eval 'e2e_plain | sed -n 2p | grep -q "WHERE status = .done."' || e2e_plain | sed -n 2p
_e2e_kill

# ---- Redis：封顶的索引
redis_start R || exit 1; rc flushall >/dev/null
python3 -c 'print("\n".join("SET k22:%d v" % i for i in range(25000)))' | rc >/dev/null
mkdir -p "$D/rd"
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$RPORT" >"$D/rd/connections.toml")
printf '[redis]\nindex_keys = 100\n' >"$D/rd/config.toml"
e2e_start -C "$D/rd" "$E2E_BIN rd"; wait_for 8 flag_is alternate_on 1; sleep 3
key C-l; key Escape; key g g; key d G; key i; sleep 0.3
typ "GET k22:"; wait_for 15 eval 'e2e_plain | grep -q "已索引"'; sleep 0.5
check "（对照）GET k22:：有候选框，底部是「已索引 100 / 共 25000 键」" eval '[[ -n $(pop) ]] && e2e_plain | grep -q "已索引 100 / 共 25000 键"' || { pop; e2e_plain | tail -20 | cut -c30-140; }
for i in $(seq 8); do key BSpace; done; typ "GET zzqq"; sleep 1
check "GET zzqq（匹配不到任何键）：什么都不画，「已索引…」那一行也不画" eval '[[ -z $(pop) ]] && ! e2e_plain | grep -q "已索引"' || { pop; e2e_plain | tail -20 | cut -c30-140; }
e2e_done
