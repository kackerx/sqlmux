#!/usr/bin/env bash
# F22.4 克隆行：值、命令面板、转置视图（specs/m22-menus-keys/task.md F22.4 a、c；tech-design §10「克隆行」）
# f22.4.sh 测了菜单、yp、which-key、只读；这里补：克隆出的行抄了 name 和 qty、保存后库里多一行且 id 是新值；命令面板 >克隆行 可用；转置视图里 yp 也多一列。自建库 t_clone。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/cellops.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -c "create table t_clone (id serial primary key, name text, qty int)" -c "insert into t_clone (name, qty) values ('ann', 30), ('bob', 10), ('cat', 20)" >/dev/null
mkdir -p "$D/rw"; (umask 077; printf '[[connection]]\nname = "rw"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/rw/connections.toml")
psql_n() { psql "$E2E_DB" -At -c "$1"; }
count() { e2e_plain | grep -c "$1"; }

SOLO=1 start -C "$D/rw"; open_table t_clone; wait_for 8 settled
check "（准备）t_clone 3 行" eval '[[ $(count " ann ") == 1 ]]'
key j; pal ">克隆行"; sleep 0.4
check "命令面板 >克隆行：列表里有「克隆行」" row_has "克隆行" || list
key Enter; sleep 0.8
check "↵：bob 那一行克隆出一行（两行 bob，各带 qty 10）" eval '[[ $(e2e_plain | grep -cE " bob +│ +10 ") == 2 ]]' || e2e_plain | sed -n 3,12p | cut -c34-120
key C-s; wait_for 8 eval '[[ $(psql_n "select count(*) from t_clone") == 4 ]]'; wait_for 8 settled
check "保存：库里 4 行，新行 name bob、qty 10，id 是序列的下一个值 4" eval '[[ $(psql_n "select count(*) from t_clone") == 4 && $(psql_n "select id from t_clone where name = '\''bob'\'' order by id desc limit 1") == 4 && $(psql_n "select qty from t_clone where id = 4") == 10 ]]' || psql_n "table t_clone"
key T; sleep 0.6
check "（准备）T 转置：表头是 1 2 3 4" eval '[[ $(e2e_text 35 159 "$(hy)") == *"│ 1 "*"│ 4 "* ]]' || e2e_text 35 159 "$(hy)"
key y; key p; sleep 0.8
# 新记录那一列表头是 +，它的 name / qty 和左边一列（光标所在、被抄的记录）相同，id 是 <default>
newcol() { e2e_plain | noicon | awk -F'│' -v h="$(hy)" -v f="$1" 'NR == h { for (i = 1; i <= NF; i++) { g = $i; gsub(/ /, "", g); if (g == "+") c = i } } NR > h && c { k = 0; for (i = 1; i < c; i++) { g = $i; gsub(/ /, "", g); if (g == f) k = 1 } } k && c { a = $(c - 1); b = $c; gsub(/ /, "", a); gsub(/ /, "", b); print a "=" b }'; }
check "转置视图里 yp：多一列（表头 +），name、qty 抄左边那条记录，id 是 <default>" eval 'n=$(newcol name); q=$(newcol qty); [[ -n $n && ${n%=*} == "${n#*=}" && ${q%=*} == "${q#*=}" && $(newcol id) == *"=<default>" ]] || { echo "  name $n qty $q id $(newcol id)"; false; }'
e2e_done
