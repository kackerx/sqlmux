#!/usr/bin/env bash
# F22.3 树的右键菜单加「刷新」：数据库节点（specs/m22-menus-keys/task.md F22.3 a；tech-design §7.8「右键菜单」）
# f22.3.sh 测了连接和 schema 节点；这里是 PG 的数据库节点：菜单里「选择 schema…」之后有「刷新 R」，点了外面新建的表出现在树上。自建库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/cellops.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
mkdir -p "$D/c"; (umask 077; printf '[[connection]]\nname = "rw"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/c/connections.toml")
side() { e2e_plain | noicon | sed 's/^│//; s/│.*//; s/^ *//; s/ *$//'; }
row_of() { side | grep -nE "$1" | head -1 | cut -d: -f1; }
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }
mitems() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0; for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/^ *//; s/^[^ ]* //; s/ *$//'; done | tr '\n' '|'; }

SOLO= start -x 200 -C "$D/c"; sleep 1
check "（准备）树里：连接 rw → 数据库 sqlmux_e2e_…" eval '[[ -n $(row_of "^▾ sqlmux_e2e") ]]' || side | head -8
y=$(row_of "^▾ sqlmux_e2e"); rclick 12 "$y"; sleep 0.6
check "右键数据库节点：菜单是「选择 schema…」「刷新 R」" eval '[[ $(mitems) =~ ^选择\ schema…\|刷新\ +R\|$ ]] || { echo "  [$(mitems)]"; false; }'
key Escape; sleep 0.3
psql "$E2E_DB" -qAt -c "create table f22_dbnode (id int primary key)" >/dev/null
sleep 1
check "外面建了 f22_dbnode：刷新之前树里没有" eval '[[ -z $(row_of "f22_dbnode") ]]'
y=$(row_of "^▾ sqlmux_e2e"); rclick 12 "$y"; sleep 0.6; menu_click "刷新"
check "右键数据库 → 点「刷新」：树上出现 f22_dbnode" eval 'wait_for 8 eval "[[ -n \$(row_of f22_dbnode) ]]"' || side | head -20
e2e_done
