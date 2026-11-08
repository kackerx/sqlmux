#!/usr/bin/env bash
# F17.13 库 / schema 的勾选（specs/m17-polish-exec/task.md F17.13；tech-design §7.8「库 / schema 的勾选」、§8.3、§14）
# 连接节点（PG）右键「选择库…」、数据库节点右键「选择 schema…」（MySQL 连接节点上也是「选择 schema…」）、命令面板里同名命令：打开 COLS 式的列表浮层
# （[x] 名字，a 全选 A 全不选，space 勾选，esc 关闭）；关闭时有改动才写回 connections.toml 的 databases / schemas 并重载；没勾的库不列、没勾的 schema 不加载
# （树、面板 @、query 的下拉都只认勾了的）；节点尾注 勾了的/总数；DSN 里的库固定勾选。PG 用共享的 sqlmux 库（只读，配置写在每次启动自己的临时目录里），MySQL 同理。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/cellops.sh"

# a third database next to postgres and sqlmux (the author's machine had a leftover sqlmux_play; the name matches db-clean's, so a crash leaves nothing)
PLAY=sqlmux_e2e_$$_p   # short: a 7-digit pid and the sidebar cuts the name
psql "$SQLMUX_TEST_PG" -qAt -c "create database $PLAY" >/dev/null || { echo "e2e: cannot create $PLAY" >&2; exit 1; }
D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"; psql "$SQLMUX_TEST_PG" -qAt -c "drop database if exists $PLAY with (force)" >/dev/null' EXIT
CT() { echo "$E2E_TMP/config/sqlmux/connections.toml"; }
side() { e2e_plain | noicon | sed 's/^│//; s/│.*//; s/^ *//; s/ *$//'; }       # the sidebar's rows, icons and the border cut
row_of() { side | grep -nE "$1" | head -1 | cut -d: -f1; }                         # the screen row (1-based) of the first sidebar row that matches
box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }
list_rows() { local g y; g=($(box)); [[ -n ${g[0]} ]] || return 0                    # the open list's rows: "[x] name  note"
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) "$y" | sed 's/^ *//; s/ *$//'; done; }
list_title() { local g; g=($(box)); [[ -n ${g[0]} ]] || return 0; e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) "${g[1]}" | sed 's/[┌─┐ ]//g'; }
list_open() { [[ -n $(box) ]] && list_title | grep -qE "^(库|schema)$|^库|^schema"; }
menu_pick() { rclick "$1" "$2"; sleep 0.6; menu_click "$3"; sleep 0.9; }          # X Y ITEM: right-click the node, click the menu item
quit_app() { e2e_type ":"; sleep 0.4; e2e_type qa; key Enter; wait_for 8 exited; sleep 0.5; }
sess() { e2e_start "$E2E_BIN"; wait_for 8 flag_is alternate_on 1; sleep 3; }       # the default connection doraemon (the shared sqlmux database), the layout with a query pane
toml_has() { grep -qF -- "$1" "$(CT)"; }

# ---- PG：连接节点上的「选择库…」
sess
dbn=$(psql "$SQLMUX_TEST_PG" -Atc "select count(*) from pg_database where not datistemplate")
check "（准备）树里：连接 doraemon（没有尾注）→ 默认库 sqlmux → schema agentable、public" eval 'side | grep -qE "^▾ doraemon *$" && side | grep -qE "^▾ sqlmux *$" && side | grep -qE "^▸ agentable" && side | grep -qE "^▾ public"' || { side | head -8; false; }
y=$(row_of "^▾ doraemon")
menu_pick 8 $y "选择库…"
check "连接节点右键 → 选择库…：打开列表浮层，标题「库」，每行 [x] 库名，顶上是勾了的/总数（${dbn}/${dbn}）" eval 'list_title | grep -q "库" && list_rows | head -1 | grep -qE "${dbn}/${dbn}" && list_rows | grep -qE "^\[x\] postgres" && list_rows | grep -qE "^\[x\] sqlmux$" && list_rows | grep -qE "^\[x\] $PLAY"' || { list_title; list_rows; false; }
key A; sleep 0.3
check "A 全不选：DSN 里的库 sqlmux 固定勾选、不会掉（1/${dbn}），别的都是 [ ]" eval 'list_rows | head -1 | grep -qE "1/${dbn}" && list_rows | grep -qE "^\[x\] sqlmux$" && list_rows | grep -qE "^\[ \] postgres" && list_rows | grep -qE "^\[ \] $PLAY"' || { list_rows; false; }
key Escape; sleep 1.5
check "esc 关闭：有改动才写回 connections.toml：databases = [\"sqlmux\"]，别的字段原样" eval 'toml_has "databases = [\"sqlmux\"]" && toml_has "name = \"doraemon\"" && toml_has "engine = \"postgres\"" && [[ $(grep -c "^\[\[connection\]\]" "$(CT)") == 1 ]] || { cat "$(CT)"; false; }'
check "树上：连接节点后面有尾注 1/${dbn}（dim），别的库（postgres、$PLAY）不列" eval 'side | grep -qE "^▾ doraemon +1/${dbn}$" && ! side | grep -qE "^[▸▾] (postgres|$PLAY)"' || { side | head -8; false; }

# ---- PG：数据库节点上的「选择 schema…」
y=$(row_of "^▾ sqlmux")
menu_pick 10 $y "选择 schema…"
check "库节点右键 → 选择 schema…：标题「schema」，agentable 和 public 两行，尾注是已加载的表数（agentable 3、public 7+）" eval 'list_title | grep -q "schema" && list_rows | head -1 | grep -qE "2/2" && list_rows | grep -qE "^\[x\] agentable +3$" && list_rows | grep -qE "^\[x\] public +[0-9]+$"' || { list_title; list_rows; false; }
key A; sleep 0.3
check "A 全不选：默认 schema（search_path 落到的 public）固定勾选：1/2，[ ] agentable、[x] public（不会关在 0 个上）" eval 'list_rows | head -1 | grep -qE "1/2" && list_rows | grep -qE "^\[ \] agentable" && list_rows | grep -qE "^\[x\] public"' || { list_rows; false; }
key a; sleep 0.3
key Space; sleep 0.3
check "space 勾掉光标所在的 agentable：1/2，[ ] agentable" eval 'list_rows | head -1 | grep -qE "1/2" && list_rows | grep -qE "^\[ \] agentable" && list_rows | grep -qE "^\[x\] public"' || { list_rows; false; }
key Escape; sleep 2
check "写回：schemas = [\"sqlmux.public\"]（库.schema），databases 那行还在" eval 'toml_has "schemas = [\"sqlmux.public\"]" && toml_has "databases = [\"sqlmux\"]" || { cat "$(CT)"; false; }'
check "树上：agentable 不列了、public 还在，库节点尾注 1/2" eval '! side | grep -qE "^[▸▾] agentable" && side | grep -qE "^▾ public" && side | grep -qE "^▾ sqlmux +1/2$"' || { side | head -10; false; }
pal "@agent"; sleep 0.8
check "面板 @ 搜 agent（agentable.agent / agent_version）：搜不到 agentable 里的表（没勾的 schema 不加载）" eval '! list | cut -d"|" -f1 | grep -q "agentable"' || { list | head -4; false; }
key Escape; sleep 0.3
pal "@t_user"; sleep 0.8
check "（对照）面板 @ 搜 t_user（public 里的）：还搜得到" eval 'list | cut -d"|" -f1 | grep -q "^t_user"' || { list | head -4; false; }
key Escape; sleep 0.3
key C-l; key g s; sleep 0.9
check "query 的 gs 下拉：只有勾了的（public，没有 agentable，也没有没勾的库 postgres）" eval 'e2e_plain | grep -q "public" && ! e2e_plain | grep -qE "│ (agentable|postgres) " ' || { e2e_plain | sed -n 1,16p | cut -c30-120; false; }
key Escape; sleep 0.3
mkdir -p "$D/c"; cp "$(CT)" "$D/c/connections.toml"
quit_app
_e2e_kill

# ---- 重启后还是这样（配置在文件里）
e2e_start -C "$D/c" "$E2E_BIN"; wait_for 8 flag_is alternate_on 1; sleep 3
check "重启：树还是窄的：连接节点尾注 1/${dbn}、库节点尾注 1/2、agentable 不列、别的库不列" eval 'side | grep -qE "^▾ doraemon +1/${dbn}$" && side | grep -qE "^▾ sqlmux +1/2$" && ! side | grep -qE "^[▸▾] (agentable|postgres|$PLAY)"' || { side | head -8; false; }

# ---- 取消勾选回到全部：尾注消失、文件里那两行没了
y=$(row_of "^▾ sqlmux")
menu_pick 10 $y "选择 schema…"
key a; sleep 0.3; key Escape; sleep 2
check "schema 浮层里 a 全选再 esc：库节点没有尾注，agentable 回来了，connections.toml 里没有 schemas 了（databases 还在）" eval '! toml_has "schemas" && toml_has "databases" && ! side | grep -qE "^▾ sqlmux +[0-9]+/[0-9]+$" && side | grep -qE "^▸ agentable"' || { cat "$(CT)"; side | head -8; false; }
y=$(row_of "^▾ doraemon")
menu_pick 8 $y "选择库…"
key a; sleep 0.3; key Escape; sleep 2
check "库浮层里 a 全选再 esc：连接节点没有尾注，别的库回来了，connections.toml 里没有 databases 了" eval '! toml_has "databases" && ! side | grep -qE "^▾ doraemon +[0-9]+/[0-9]+$" && side | grep -qE "^▸ postgres" && side | grep -qE "^▸ $PLAY"' || { cat "$(CT)"; side | head -12; false; }
check "文件里连接的其他字段一直没动（name、engine、dsn 原样、只有一个 [[connection]]）" eval 'toml_has "name = \"doraemon\"" && toml_has "engine = \"postgres\"" && toml_has "$SQLMUX_TEST_PG" && [[ $(grep -c "^\[\[connection\]\]" "$(CT)") == 1 ]] || { cat "$(CT)"; false; }'

# ---- 命令面板里的同名命令：作用于光标所在节点
key C-h; key g g
pal ">选择"; sleep 0.6; key Enter; sleep 0.9
check "光标在连接节点上执行面板里的「选择库 / schema…」：打开的是库的浮层（标题「库」）" eval 'list_title | grep -q "库" && list_rows | grep -qE "postgres"' || { list_title; list_rows; false; }
key Escape; sleep 0.5
key j
pal ">选择"; sleep 0.6
check "面板 >选择：有「选择库 / schema…」这条命令" eval 'list | cut -d"|" -f1 | grep -q "选择"' || { list | head -6; false; }
key Enter; sleep 0.9
check "再 j 到库节点上执行：打开的是 schema 的浮层（标题「schema」，agentable、public）" eval 'list_title | grep -q "schema" && list_rows | grep -qE "agentable" && list_rows | grep -qE "public"' || { list_title; list_rows; false; }
key Escape; sleep 0.5
quit_app
e2e_done
