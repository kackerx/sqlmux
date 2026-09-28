#!/usr/bin/env bash
# F1.11 LIMIT 自定义每页行数（specs/m1-browse/task.md F1.11；tech-design §7.8「查询条」LIMIT）
# 在自建库里做：发出的 SQL 靠锁住 t_order、读等锁的那条语句。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
APP=e2e-f111-$$
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$E2E_DB" "$APP" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
dd() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }'; }             # 下拉框的 X Y W H
dd_rows() { local g y; g=($(dd)); for ((y = g[1] + 3; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | sed 's/ *$//'; done | tr '\n' ' '; }
limit() { key g l; e2e_type "$1"; sleep 0.4; }                             # 打开 LIMIT 下拉框，在过滤框里输入
sent() { e2e_lock t_order; "$@"; wait_for 5 eval '[[ -n $(e2e_waiting $APP) ]]'; SQL=$(e2e_waiting $APP); e2e_unlock; wait_for 8 settled; sleep 0.3; }

start -C "$D/own"; open_table t_order; wait_for 8 settled
key ']'; wait_for 8 settled; key ']'; wait_for 8 settled
limit 250
check "过滤框输入 250：候选第一项就是 250" eval '[[ $(dd_rows) == "250 "* ]] || { echo "  $(dd_rows)"; false; }'
sent key Enter
check "↵：chip 为 LIMIT 250，SQL 为 limit 251，从第 3 页回到第 1 页" eval 'qb_has "LIMIT 250   PAGE 1/24" && [[ $SQL == *"limit 251 offset 0" ]] || { echo "  $SQL / $(qb)"; false; }'
limit 99999; key Enter; wait_for 8 settled
check "输入 99999：按上限 10000" qb_has "LIMIT 10000   PAGE 1/1"
limit 100
check "输入 100：列表是 100 1000，预设的 100 不重复" eval '[[ $(dd_rows) == "100 1000 " ]] || { echo "  $(dd_rows)"; false; }'
key Escape
limit 0
check "输入 0：只当过滤字符，列出 100 500 1000，没有 0 这个候选" eval '[[ $(dd_rows) == "100 500 1000 " ]] || { echo "  $(dd_rows)"; false; }'
key Escape
check "esc：LIMIT 不变" qb_has "LIMIT 10000"

e2e_done
