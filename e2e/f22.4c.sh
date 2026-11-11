#!/usr/bin/env bash
# F22.4 克隆行的边角（specs/m22-menus-keys/task.md F22.4；tech-design §7.6「右键菜单」、§10「克隆行」）
# 非键的 serial / identity 列和 generated 列不抄、保存后由库出值；有二进制列的行 yp 弹 toast、不加行；只读连接的菜单 j 一路往下跳过 dim 项；
# Redis 的 stream（NoInsert）上添加 / 克隆是 dim、删除不是；80x24 的窗口在右下角右键，菜单整个在屏幕里、键那一列没被截。自建库，自己的 redis-server。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/cellops.sh"
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
redis_start R || exit 1; redis_seed
psql "$E2E_DB" -q -c "create table t_ser (id serial primary key, name text, seq serial, ident int generated always as identity, qty int, twice int generated always as (qty * 2) stored)" \
  -c "insert into t_ser (name, qty) values ('ann', 30), ('bob', 10)" \
  >/dev/null
# 二进制列：PG 的 bytea 显示成 \x… 十六进制、能原样写回，不算；MySQL 的 varbinary 里不是文本的字节显示成 <binary N bytes>
e2e_own_mydb || { echo "e2e: could not create the private MySQL database" >&2; exit 1; }
echo "create table t_bin (id int auto_increment primary key, b varbinary(8), name varchar(8)); insert into t_bin (b, name) values (0x00ff01, 'x1'), (0xfffe, 'x2')" | e2e_mysql "sqlmux_e2e_$$"
mkdir -p "$D/rw" "$D/ro" "$D/rd" "$D/my"
(umask 077; printf '[[connection]]\nname = "rw"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/rw/connections.toml"
 printf '[[connection]]\nname = "ro"\nengine = "postgres"\ndsn = "%s"\nread_only = true\n' "$E2E_DB" >"$D/ro/connections.toml"
 printf '[[connection]]\nname = "my"\nengine = "mysql"\ndsn = "%s"\n' "$E2E_MYDB" >"$D/my/connections.toml"
 printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$RPORT" >"$D/rd/connections.toml")
psql_n() { psql "$E2E_DB" -At -c "$1"; }
toast_has() { e2e_cap | grep -qF -- "$1"; }
box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }
# 菜单的每一行：「<背景色> <文字>」，选中项的背景和别的不同
mrows() { local g y; g=($(box)); [[ -n ${g[0]} ]] || return 0; for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do echo "$(e2e_style $((g[0] + 3)) $y | sed 's/.*bg=//') $(e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/^ *//; s/ *$//')"; done; }
fg_of() { local xy; xy=$(find_xy "$1"); e2e_style "${xy% *}" "${xy#* }" | sed 's/ bg=.*//'; }

# ---- 计数列、generated 列留给库
SOLO=1 start -C "$D/rw"; open_table t_ser; wait_for 8 settled
key y; key p; sleep 0.8
newrow() { e2e_plain | grep -E "│ +ann +│" | sed -n 2p; }
check "yp：多一行 ann、qty 30；seq、ident、twice 和 id 一样没抄（新行里没有 1 和 60）" eval 'r=$(newrow); [[ -n $r && $r == *" 30 "* && $r != *" 60 "* && $r != *"│ 1 "* ]] || { e2e_plain | sed -n 3,9p | cut -c34-150; false; }'
key C-s; wait_for 8 eval '[[ $(psql_n "select count(*) from t_ser") == 3 ]]'
check "保存：库里新行 id 3、seq 3、ident 3（序列的下一个值）、qty 30、twice 60（库算的）" eval '[[ $(psql_n "select id, seq, ident, name, qty, twice from t_ser order by id desc limit 1") == "3|3|3|ann|30|60" ]] || { psql_n "table t_ser"; false; }'
_e2e_kill

# ---- 二进制列（MySQL）
SOLO=1 start -C "$D/my"; open_table t_bin; wait_for 8 settled
key j; key y; key p; sleep 0.8
check "有 varbinary 列（<binary 2 bytes>）的行 yp：toast「有二进制列，复制不了」，没有新行（还是 2 行），光标还在第 2 行" eval 'toast_has "有二进制列，复制不了" && [[ $(e2e_plain | grep -cE "│ +x[12] ") == 2 ]] && ! e2e_plain | grep -q "<default>" && e2e_plain | grep -qF "<binary 2 bytes>"' || e2e_plain | sed -n 3,8p | cut -c34-150
read -r cx cy <<<"$(cellxy name 1)"; rclick "$cx" "$cy"; sleep 0.6; menu_click "克隆行"; sleep 0.5
check "菜单的「克隆行」同样：toast，不加行" eval 'toast_has "有二进制列，复制不了" && [[ $(e2e_plain | grep -cE "│ +x[12] ") == 2 ]]'
_e2e_kill

# ---- 只读：键盘移动跳过 dim
SOLO=1 start -C "$D/ro"; open_table t_ser; wait_for 8 settled
read -r cx cy <<<"$(cellxy name 1)"; rclick "$cx" "$cy"; sleep 0.6
for i in $(seq 20); do key j; done; sleep 0.3
sel() { local b; b=$(mrows | awk '{ print $1 }' | sort | uniq -c | sort -n | head -1 | awk '{ print $2 }'); mrows | awk -v b="$b" '$1 == b { $1 = ""; print }' | sed 's/^ *//'; }
check "只读连接的菜单 j 按 20 次：停在最后一个能选的「导出…」，不落到 dim 的三项上" eval '[[ $(sel) == *导出…* && $(sel) != *克隆行* ]] || { echo "  sel=[$(sel)]"; mrows; false; }'
key Enter; sleep 0.6
check "↵：执行的是「导出…」（菜单关了，开了导出的格式列表）" eval '[[ -z $(mrows | grep 克隆行) ]] && e2e_plain | grep -qF "CSV · 当前页"' || e2e_plain | sed -n 3,20p | cut -c34-150
_e2e_kill

# ---- Redis stream：NoInsert
SOLO=1 start -C "$D/rd"; e2e_keys C-p; sleep 0.3; e2e_type "@stream:events"; sleep 0.8; key Enter; sleep 1.5; wait_for 8 settled
check "（准备）stream:events 的键 tab，两条" eval 'e2e_plain | grep -qF "1700000000000-2"' || e2e_plain | sed -n 1,10p | cut -c34-150
read -r cx cy <<<"$(echo "$(( $(colx id) + 1 )) $(( $(grid_y) + 1 ))")"; rclick "$cx" "$cy"; sleep 0.6
check "stream 的格右键：添加行、克隆行是 dim，删除行不是（和「复制这一格」同色）" eval '[[ $(fg_of 添加行) == "$(fg_of 克隆行)" && $(fg_of 删除行) != "$(fg_of 克隆行)" && $(fg_of 删除行) == "$(fg_of 复制这一格)" ]]' || { mrows; fg_of 添加行; fg_of 删除行; fg_of 复制这一格; }
key Escape; sleep 0.3
_e2e_kill

# ---- 80x24，右下角
SOLO=1 start -x 80 -y 24 -C "$D/rw"; open_table t_order; wait_for 8 settled
lasty=$(( $(e2e_plain | grep -nE "^│.*│ +[0-9]+ +│" | tail -1 | cut -d: -f1) ))
read -r cx _ <<<"$(echo "$(e2e_find "│" "$lasty" | tr ' ' '\n' | tail -2 | head -1) 0")"
rclick $((cx - 2)) "$lasty"; sleep 0.6
g=($(box))
check "80x24 在最后一行最右一格右键：菜单框整个在屏幕里" eval '[[ -n ${g[0]} ]] && ((g[0] >= 1 && g[0] + g[2] - 1 <= 80 && g[1] >= 1 && g[1] + g[3] - 1 <= 24))' || { echo "  box ${g[*]}"; e2e_plain; }
check "菜单十几项都在，最后一项「克隆行 yp」整行可见（键没被截）" eval 'mrows | grep -qE "克隆行 +yp$" && mrows | grep -qE "删除行 +dd$" && [[ $(mrows | wc -l) -ge 12 ]]' || mrows
e2e_done
