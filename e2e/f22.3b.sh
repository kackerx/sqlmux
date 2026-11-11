#!/usr/bin/env bash
# F22.3 树的右键菜单加「刷新」（MySQL）（specs/m22-menus-keys/task.md F22.3）：同 f22.3.sh 的树和面板两步，在 MySQL 的自建库上。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_mydb || { echo "e2e: could not create the private MySQL database" >&2; exit 1; }
mkdir -p "$D/c"; (umask 077; printf '[[connection]]\nname = "rw"\nengine = "mysql"\ndsn = "%s"\n' "$E2E_MYDB" >"$D/c/connections.toml")
rclick() { _sgr 2 "$1" "$2" M; _sgr 2 "$1" "$2" m; sleep 0.6; }
srow() { local y; for y in $(seq 3 43); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && { echo "$y"; return; }; done; }
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }
mitems() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0; for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/^ *//; s/^[^ ]* //; s/ *$//'; done | tr '\n' '|'; }

SOLO= start -x 200 -C "$D/c"; sleep 1
check "（准备）树里有 rw" eval '[[ -n $(srow rw) ]]'
y=$(srow rw); rclick 12 "$y"
check "右键连接（rw）：菜单里有「刷新」和 R" eval '[[ $(mitems) =~ 刷新\ +R\| ]] || { echo "  [$(mitems)]"; false; }'
key Escape; sleep 0.3
echo "create table f22_new (id int primary key, name varchar(20))" | e2e_mysql "sqlmux_e2e_$$"
sleep 1
check "外面建了 f22_new：刷新之前树里还没有" eval '[[ -z $(srow f22_new) ]]'
y=$(srow rw); rclick 12 "$y"; key j; key Enter; sleep 1.5
check "右键 rw → 刷新：树上出现 f22_new" eval 'wait_for 8 eval "[[ -n \$(srow f22_new) ]]"'
pal "@f22_new"; sleep 0.5
check "面板的「表」范围（@f22_new）搜得到" row_has "f22_new"
key Escape; sleep 0.4
e2e_done
