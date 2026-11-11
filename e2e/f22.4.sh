#!/usr/bin/env bash
# F22.4 格子的右键菜单加 添加行 / 删除行 / 克隆行，克隆行 yp（specs/m22-menus-keys/task.md F22.4；tech-design §7.6「右键菜单」、§10「克隆行」）
# 右键格子：菜单里有这三项和各自的键（o、dd、yp）；点「克隆行」、按 yp 各多一行待提交（值抄当前行，自增的 id 留空）；点「删除行」标记，再右键写「取消删除」；
# 按 y 停住时 which-key 里有 p 克隆行；只读连接上三项是 dim、点了菜单不关也没有新行。自建库 t_clone（3 行）。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/paging.sh"
. "$(dirname "$0")/ordering.sh"
. "$(dirname "$0")/cellops.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -c "create table t_clone (id serial primary key, name text, qty int)" -c "insert into t_clone (name, qty) values ('ann', 30), ('bob', 10), ('cat', 20)" >/dev/null
mkdir -p "$D/rw" "$D/ro"
(umask 077; printf '[[connection]]\nname = "rw"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/rw/connections.toml"
 printf '[[connection]]\nname = "ro"\nengine = "postgres"\ndsn = "%s"\nread_only = true\n' "$E2E_DB" >"$D/ro/connections.toml")
psql_n() { psql "$E2E_DB" -At -c "$1"; }
count() { e2e_plain | grep -c "$1"; }
fg_of() { local xy; xy=$(find_xy "$1"); e2e_style "${xy% *}" "${xy#* }" | sed 's/ bg=.*//'; }

SOLO=1 start -C "$D/rw"; open_table t_clone; wait_for 8 settled
check "（准备）t_clone 3 行：ann bob cat 各一行" eval '[[ $(count " ann ") == 1 && $(count " bob ") == 1 ]]'

# ---- 右键格子：三项和键
read -r cx cy <<<"$(cellxy name 1)"
rclick "$cx" "$cy"; sleep 0.6
check "右键 ann 的 name 格：菜单里有 添加行 o、删除行 dd、克隆行 yp" eval 'e2e_plain | grep -qE "添加行 +o " && e2e_plain | grep -qE "删除行 +dd " && e2e_plain | grep -qE "克隆行 +yp " || { e2e_plain | grep -E "添加行|删除行|克隆行"; false; }'
menu_click "克隆行"
check "点「克隆行」：多一行，抄 ann（两行 ann），库里还是 3 行（待提交）" eval '[[ $(count " ann ") == 2 && $(psql_n "select count(*) from t_clone") == 3 ]]' || e2e_plain | sed -n 3,12p | cut -c34-120
check "新行里自增的 id 留空：第二行的 id 格没有 1（新行在 ann 下面）" eval '[[ $(e2e_plain | grep " ann " | sed -n 2p) != *" 1 "* ]]' || e2e_plain | grep " ann "
key g g; key y; key p; sleep 0.8
check "yp：再多一行（三行 ann）" eval '[[ $(count " ann ") == 3 ]]'

# ---- 删除行 / 取消删除
read -r cx cy <<<"$(cellxy name 4)"
rclick "$cx" "$cy"; sleep 0.6; menu_click "删除行"
read -r cx cy <<<"$(cellxy name 4)"
rclick "$cx" "$cy"; sleep 0.6
check "点「删除行」标记之后再右键同一行：这一项写「取消删除」" eval 'menu_has "取消删除" && ! menu_has "删除行" >/dev/null 2>&1'
key Escape; sleep 0.3

# ---- which-key
key Escape; key y; sleep 1.5
check "按 y 停住：which-key 里有 p 克隆行" eval 'e2e_plain | grep -qF "p → 克隆行"' || e2e_plain | tail -12
key Escape; sleep 0.3
_e2e_kill

# ---- 只读连接：三项是 dim，点了没有用
SOLO=1 start -C "$D/ro"; open_table t_clone; wait_for 8 settled
read -r cx cy <<<"$(cellxy name 1)"
rclick "$cx" "$cy"; sleep 0.6
check "只读连接上右键：三项都在" eval 'menu_all "添加行" "删除行" "克隆行"'
check "三项是 dim：字色和「复制这一格」不同，也不同于「导出…」" eval '[[ $(fg_of 克隆行) != "$(fg_of 导出…)" && $(fg_of 添加行) == "$(fg_of 克隆行)" && $(fg_of 删除行) == "$(fg_of 克隆行)" ]]' || { fg_of 克隆行; fg_of 导出…; }
menu_click "克隆行"
check "点「克隆行」：菜单没关，没有新行（只有一行 ann）" eval 'menu_has "克隆行" && [[ $(count " ann ") == 1 ]]'
key Escape; sleep 0.3
key y; key p; sleep 0.8
check "yp：只读连接的 toast，没有新行" eval 'e2e_plain | tail -3 | grep -q "只读连接" && [[ $(count " ann ") == 1 ]]' || e2e_plain | tail -3
e2e_done
