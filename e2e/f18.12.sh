#!/usr/bin/env bash
# F18.12 删除正在用的连接：确认后一起关它的 tab（specs/m18-windows-mssql/task.md F18.12；tech-design §7.8「连接管理」）
# 连接管理里 x 删 play：它开着 t_play1（有没保存的修改）、t_play2、query_1 三个 tab，确认框写「它开着的 3 个 tab 会一起关掉，其中 1 个有没保存的修改，会丢」；
# n 什么都不动；y 后 play 的 tab 全关、空 pane 收掉、树里没有 play、connections.toml 里没有它、别的连接（doraemon）的 tab 不动、
# 启动连接换成剩下的 doraemon、没保存的修改没进库。play 是自建库，t_event 在里面删掉了，所以 @t_event 打开的是 doraemon 的那张。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'chmod -R u+w "$D" 2>/dev/null; e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -qAt -c "create table t_play1 (id int primary key, v text); create table t_play2 (id int primary key); insert into t_play1 values (1, 'a'), (2, 'b'); drop table t_event" >/dev/null
mkdir -p "$D/c"
(umask 077; printf '[[connection]]\nname = "play"\nengine = "postgres"\ndsn = "%s"\n\n[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" "$SQLMUX_TEST_PG" >"$D/c/connections.toml")
ct() { echo "$E2E_TMP/config/sqlmux/connections.toml"; }   # 程序读写的是 start -C 拷出来的那份
box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }
mgr_open() { e2e_plain | grep -q "┌─ 连接"; }
lrows() { local g y; g=($(box)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 2)) $((g[0] + 26)) "$y" | noicon | sed 's/^ *//; s/ *$//'; done; }
lsel() { local g y; g=($(box)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do style_has $((g[0] + 20)) "$y" bg=#364a82 >/dev/null && { e2e_text $((g[0] + 2)) $((g[0] + 26)) "$y" | noicon | sed 's/^ *//; s/ *$//'; return; }; done; }
cmdp() { pal ">$1"; key Enter; sleep 0.7; }
tsel() { local y; for y in $(seq 3 43); do [[ $(e2e_style 4 "$y") == *bg=#364a82* ]] && { e2e_text 1 32 "$y"; return; }; done; }
tree_focus() { local i; for ((i = 0; i < 6; i++)); do [[ $(focused) == 0 ]] && return 0; key C-h; done; }
tree_to() { local i; tree_focus; key G; for ((i = 0; i < 60; i++)); do [[ $(tsel) == *" $1 "* ]] && return 0; key k; done; return 1; }
side() { e2e_plain | python3 -c 'import sys; [print(l[1:33].rstrip("│ ")) for l in sys.stdin.read().split("\n")]'; }
dialog() { e2e_plain | grep -o "删除连接 [^ ]*？[^│]*" | head -1 | sed 's/ *$//'; }
panes_are() { [[ $(e2e_panes | awk '$1 != "-" { printf "%s ", $1 }') == "$1" ]] || { echo "  panes: $(e2e_panes | awk '$1 != "-" { printf "%s ", $1 }'), want '$1'"; false; }; }
set_cell() { key Enter; e2e_type "$1"; sleep 0.2; key Enter; }

SOLO= start -C "$D/c" -S "$D/st"; sleep 1
open_table t_play1; wait_for 8 settled
open_table t_play2; wait_for 8 settled
tree_to doraemon; key l; sleep 2
open_table t_event; wait_for 8 settled
check "（准备）play 开着 t_play1、t_play2、query_1，doraemon 开着 t_event（别的连接的 tab）" tabs_are 1 "1:t_play1 │ 2:t_play2- │ 3:t_event*"
key gT; key gT; sleep 0.4; key l; sleep 0.2; set_cell X
check "（准备）t_play1 的 v 格改成 X，没保存（表格上有待保存的改动）" eval 'e2e_plain | grep -q "X"'

cp "$(ct)" "$D/orig.toml"
cmdp conn.manage
check "conn.manage：选中 play，x：确认框写「删除连接 play？它开着的 3 个 tab 会一起关掉，其中 1 个有没保存的修改，会丢」" eval 'mgr_open && [[ $(lsel) == play ]]; key x; sleep 0.5; [[ $(dialog) == "删除连接 play？它开着的 3 个 tab 会一起关掉，其中 1 个有没保存的修改，会丢" ]] || { echo "  dialog [$(dialog)]"; false; }'

key n; sleep 0.4
check "n：什么都不动——文件没变、浮层还在选中 play、tab 条上三个 play 的 tab 还在" eval 'cmp -s "$(ct)" "$D/orig.toml" && mgr_open && [[ $(lsel) == play ]] && [[ -z $(dialog) ]]'
key Escape; sleep 0.4
check "（对照）n 之后关掉浮层：t_play1、t_play2 的 tab 和 query_1 都在，没有保存过的 X 还在" eval 'e2e_plain | grep -q "t_play1" && e2e_plain | grep -q "t_play2" && e2e_plain | grep -q "query_1" && e2e_plain | grep -q "X"'

cmdp conn.manage; key x; sleep 0.5; key y; sleep 1.5
check "y：play 的整块从 connections.toml 里没了，doraemon 块原样" eval '! grep -q "name = \"play\"" "$(ct)" && grep -q "name = \"doraemon\"" "$(ct)" || { cat "$(ct)"; false; }'
check "列表里只剩 doraemon 和新建" eval '[[ $(lrows | sed "/^$/d" | head -2 | tr "\n" ,) == "doraemon,新建," ]]'
key Escape; sleep 0.6
check "树里没有 play 连接节点（顶上的「⓪ play」是 session 名），doraemon 还在" eval '! side | noicon | tr -s " " | grep -qE "^ [▾▸] play( |$)" && side | noicon | tr -s " " | grep -qE "^ [▾▸] doraemon( |$)" || { side | noicon | grep -nE "play|doraemon"; false; }'
check "play 的三个 tab 全关了：t_play1、t_play2、query_1 在工作区和 tab 条里都没有" eval '! e2e_plain | grep -q "t_play" && ! side | grep -q "query_1"'
check "doraemon 的 t_event tab 不动，还开着、有数据（3 行）" eval 'e2e_plain | head -1 | grep -q "t_event" && e2e_plain | grep -q "signup"'
check "空了的 query pane 收掉了：只剩侧栏和 ① 两个 pane" panes_are "0 1 "
check "启动连接（play）换成剩下的 doraemon：状态栏右边的连接是 doraemon · sqlmux@…（左边 session 名 play 不变）" eval 'bar | grep -q "doraemon · sqlmux@" && ! bar | grep -q "play · " || { bar; e2e_plain | tail -2; false; }'
check "没保存的修改丢了：库里 t_play1 的 v 还是 a" eval '[[ $(psql "$E2E_DB" -At -c "select v from t_play1 where id = 1") == a ]]'
check "程序没崩" running
e2e_done
