#!/usr/bin/env bash
# F18.14 树上删连接：dd（tree.delete）和右键菜单（specs/m18-windows-mssql/task.md F18.14；tech-design §7.8「右键菜单」「连接管理」、§6.4 tree）
# 连接节点上 dd 弹 F18.12 的确认框（没 tab 是「删除连接 lite？」，有 tab 写几个），n 什么都不动，y 后树里和文件里都没了；
# 右键菜单：PG「选择库… / 编辑… / 删除…」，MySQL「选择 schema… / 编辑… / 删除…」，SQLite 和 Redis（连不上也一样）「编辑… / 删除…」；
# 「编辑…」打开连接管理并选中这个连接，「删除…」等于 dd；exec 连接（不在连接管理里）没有「编辑…」；命令行开的临时连接两项都没有、dd 无事；默认键位没有 D。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'chmod -R u+w "$D" 2>/dev/null; e2e_stop; rm -rf "$D"' EXIT
python3 -c 'import sqlite3, sys; c = sqlite3.connect(sys.argv[1]); c.execute("create table t1 (id integer primary key)"); c.commit()' "$D/lite.db"
mkdir -p "$D/c"
(umask 077; printf '[[connection]]\nname = "play"\nengine = "postgres"\ndsn = "%s"\n\n[[connection]]\nname = "my"\nengine = "mysql"\ndsn = "%s"\n\n[[connection]]\nname = "lite"\nengine = "sqlite"\ndsn = "%s"\n\n[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:1/0"\n\n[[connection]]\nname = "exe"\nengine = "exec"\ndialect = "postgres"\ncommand = ["/nonexistent-x", "{sql}"]\n' "$SQLMUX_TEST_PG" "$SQLMUX_TEST_MYSQL" "$D/lite.db" >"$D/c/connections.toml")
ct() { echo "$E2E_TMP/config/sqlmux/connections.toml"; }
rclick() { _sgr 2 "$1" "$2" M; _sgr 2 "$1" "$2" m; sleep 0.6; }
side() { e2e_plain | python3 -c 'import sys; [print(l[1:33].rstrip("│ ")) for l in sys.stdin.read().split("\n")]'; }
srow() { local y t; for y in $(seq 3 43); do t=$(e2e_text 1 32 "$y" | noicon | tr -s ' ' | sed 's/[ │]*$//'); [[ $t == *" $1" ]] && { echo "$y"; return; }; done; }   # 侧栏里以 NAME 结尾的行（连接节点）
has_node() { side | noicon | tr -s " " | grep -qE "^ [▾▸] $1( |$)"; }
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }
mraw() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0; for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/^ *//; s/^[^ ]* //; s/ *$//'; done | tr '\n' '|'; }
mitems() { mraw | sed 's/  \+[^ |]\+|/|/g'; }   # the keys at the right dropped (F22.3)
menu_is() { [[ $(mitems) == "$1" ]] || { echo "  menu: [$(mitems)], want [$1]"; false; }; }
tsel() { local y; for y in $(seq 3 43); do [[ $(e2e_style 4 "$y") == *bg=#364a82* ]] && { e2e_text 1 32 "$y" | noicon | tr -s ' ' | sed 's/^[│ ]*[▾▸]* *//; s/[ │]*$//'; return; }; done; }
tree_focus() { local i; for ((i = 0; i < 6; i++)); do [[ $(focused) == 0 ]] && return 0; key C-h; done; }
tree_to() { local i; tree_focus; key gg; for ((i = 0; i < 60; i++)); do [[ $(tsel) == "$1" ]] && return 0; key j; done; return 1; }
tree_delete_keys() { local l; l=$(XDG_CONFIG_HOME=$D/xc "$E2E_BIN" keys 2>&1 | grep -F "tree.delete"); [[ $(grep -c . <<<"$l") == 1 && $l == *dd* && $l != *D* ]] || { echo "  [$l]"; false; }; }
dialog() { e2e_plain | grep -o "删除连接 [^ ]*？[^│]*" | head -1 | sed 's/ *$//'; }
mgr_open() { e2e_plain | grep -q "┌─ 连接"; }
box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }
lsel() { local g y; g=($(box)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do style_has $((g[0] + 20)) "$y" bg=#364a82 >/dev/null && { e2e_text $((g[0] + 2)) $((g[0] + 26)) "$y" | noicon | sed 's/^ *//; s/ *$//'; return; }; done; }

SOLO= start -C "$D/c" -S "$D/st"; sleep 2
check "（准备）树里有五个连接节点 play、my、lite、rd、exe" eval 'has_node play && has_node my && has_node lite && has_node rd && has_node exe || { side | noicon | head -30; false; }'

# ---- 右键菜单
rclick 5 "$(srow play)"
check "右键 PG 连接 play：选择库… / 刷新 / 断开连接 / 编辑… / 删除…（编辑和删除排在选择库、刷新后面，F22.3）" menu_is "选择库…|刷新|断开连接|编辑…|删除…|"
key Escape
rclick 5 "$(srow my)"
check "右键 MySQL 连接 my：选择 schema… / 刷新 / 断开连接 / 编辑… / 删除…" menu_is "选择 schema…|刷新|断开连接|编辑…|删除…|"
key Escape
rclick 5 "$(srow lite)"
check "右键 SQLite 连接 lite：刷新 / 断开连接 / 编辑… / 删除…" menu_is "刷新|断开连接|编辑…|删除…|"
key Escape
rclick 5 "$(srow rd)"
check "右键 Redis 连接 rd（连不上的）：刷新 / 断开连接 / 编辑… / 删除…" menu_is "刷新|断开连接|编辑…|删除…|"
key Escape
rclick 5 "$(srow exe)"
check "右键 exec 连接 exe（不在连接管理里）：有「断开连接」「删除…」，没有「编辑…」（F22.12）" eval 'mitems | grep -q "断开连接" && mitems | grep -q "删除…" && ! mitems | grep -q "编辑…" || { echo "  menu [$(mitems)]"; false; }'
key Escape
rclick 5 "$(srow rd)"
check "（对照）菜单里「刷新」后面是 R，「删除…」后面是 dd，「编辑…」没有键（F22.3：菜单项右侧画键）" eval '[[ $(mraw) =~ 刷新\ +R\|断开连接\|编辑…\|删除…\ +dd\| ]]'
key j; key Enter; sleep 1
check "「编辑…」：打开连接管理，选中 rd（右边表单的 name 是 rd）" eval 'mgr_open && [[ $(lsel) == rd ]] && e2e_plain | grep -qE "name +rd" || { echo "  mgr $(mgr_open && echo open) sel [$(mgr_open && lsel)]"; e2e_plain | head -30 | cut -c1-120; false; }'
key Escape; sleep 0.4
check "esc 关掉连接管理" eval '! mgr_open'

# ---- dd：没 tab 的 lite
cp "$(ct)" "$D/orig.toml"
tree_to lite; sleep 0.2
check "（准备）光标在 lite 节点上" eval '[[ $(tsel) == lite ]] || { echo "  tsel [$(tsel)]"; false; }'
key d; key d; sleep 0.5
check "dd：确认框「删除连接 lite？」，没有 tab 的话不带「它开着的…」" eval '[[ $(dialog) == "删除连接 lite？" ]] || { echo "  dialog [$(dialog)]"; false; }'
key n; sleep 0.4
check "n：什么都不动——文件没变、树里还有 lite" eval 'cmp -s "$(ct)" "$D/orig.toml" && has_node lite && [[ -z $(dialog) ]]'
key d; key d; sleep 0.5; key y; sleep 1.2
check "y：树里没有 lite 了，connections.toml 里也没有，别的连接原样" eval '! has_node lite && ! grep -q "lite" "$(ct)" && grep -q "name = \"play\"" "$(ct)" && grep -q "name = \"rd\"" "$(ct)" && has_node play || { side | noicon | head -6; false; }'

# ---- 右键菜单的「删除…」等于 dd；有 tab 的连接：确认框写几个 tab
rclick 5 "$(srow rd)"; key j; key j; key Enter; sleep 0.6
check "右键「删除…」：同一个确认框「删除连接 rd？」" eval '[[ $(dialog) == "删除连接 rd？" ]] || { echo "  dialog [$(dialog)]"; false; }'
key y; sleep 1.2
check "y：rd 删掉了（树里和文件里）" eval '! has_node rd && ! grep -q "\"rd\"" "$(ct)"'
open_table t_user; wait_for 8 settled
tree_to play; key d; key d; sleep 0.5
check "dd 在开着 t_user 和 query_1 的 play 上：确认框「删除连接 play？它开着的 2 个 tab 会一起关掉」" eval '[[ $(dialog) == "删除连接 play？它开着的 2 个 tab 会一起关掉" ]] || { echo "  dialog [$(dialog)]"; false; }'
key y; sleep 1.5
check "y：play 删掉了，它的 tab 都关了（t_user 不在了），my 还在" eval '! has_node play && ! e2e_plain | grep -q "t_user" && has_node my && ! grep -q "name = \"play\"" "$(ct)"'

# ---- 默认键位里没有 D：连接节点上 D 什么都不弹
tree_to my; key D; sleep 0.5
check "连接节点 my 上按 D：什么都不弹（默认键位里没有 D，只有 dd）" eval '[[ -z $(dialog) ]] && has_node my'
check "sqlmux keys 里 tree.delete 的键只有 dd（没有 D）" tree_delete_keys

# ---- 命令行开的临时连接（不在 connections.toml 里）：右键没有菜单、dd 无事
e2e_stop
e2e_start -C "$D/c" -S "$D/st9" "$E2E_BIN $SQLMUX_TEST_PG"; wait_for 8 flag_is alternate_on 1; sleep 3
cp "$(ct)" "$D/orig2.toml"
check "（准备）临时连接 sqlmux@localhost 在树里" eval 'has_node "sqlmux@localhost"'
rclick 5 "$(srow sqlmux@localhost)"
check "右键临时连接：有「断开连接」（F22.12），没有「编辑…」「删除…」" eval 'mitems | grep -q "断开连接" && ! mitems | grep -qE "编辑|删除" || { echo "  menu [$(mitems)]"; false; }'
key Escape; tree_to sqlmux@localhost; key d; key d; sleep 0.5
check "dd 在临时连接上：什么都不弹，connections.toml 没变" eval '[[ -z $(dialog) ]] && cmp -s "$(ct)" "$D/orig2.toml" && has_node "sqlmux@localhost"'
check "程序没崩" running
e2e_done
