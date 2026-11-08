#!/usr/bin/env bash
# F17.13 勾选的写回细节（specs/m17-polish-exec/task.md F17.13；tech-design §7.8、§14）
# 只改这条连接的块里的 databases / schemas 那一行，注释和别的行逐字不动；PG 的 schemas 写成「库.schema」，别的库的条目原样保留；没有改动 esc 不写文件；
# 点浮层外也关闭并生效；没勾的 schema 里的表手写带前缀的 SQL 照常能跑；命令行的临时连接只改内存、不写文件。共享的 sqlmux 库（只读）。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/cellops.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
mkdir -p "$D/c"
(umask 077; cat >"$D/c/connections.toml" <<TOML
# my comment on top
[[connection]]
name = "doraemon"   # the one under test
engine = "postgres"
dsn = "$SQLMUX_TEST_PG"
schemas = ["postgres.foo"]

# the other one
[[connection]]
name = "other"
engine = "sqlite"
dsn = "$D/x.db"
TOML
)
cp "$D/c/connections.toml" "$D/orig.toml"
CT() { echo "$E2E_TMP/config/sqlmux/connections.toml"; }
side() { e2e_plain | noicon | sed 's/^│//; s/│.*//; s/^ *//; s/ *$//'; }
row_of() { side | grep -nE "$1" | head -1 | cut -d: -f1; }
box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }
list_rows() { local g y; g=($(box)); [[ -n ${g[0]} ]] || return 0
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) "$y" | sed 's/^ *//; s/ *$//'; done; }
menu_pick() { rclick "$1" "$2"; sleep 0.6; menu_click "$3"; sleep 0.9; }
quit_app() { e2e_type ":"; sleep 0.4; e2e_type qa; key Enter; wait_for 8 exited; sleep 0.5; }
sum() { md5 "$1"; }

e2e_start -C "$D/c" "$E2E_BIN doraemon"; wait_for 8 flag_is alternate_on 1; sleep 3
y=$(row_of "^▾ sqlmux")
menu_pick 10 "$y" "选择 schema…"
s0=$(sum "$(CT)")
key Escape; sleep 1.5
check "没有改动就 esc：不写文件（connections.toml 字节不变）" eval '[[ $(sum "$(CT)") == "$s0" ]] && cmp -s "$(CT)" "$D/orig.toml" || { diff "$D/orig.toml" "$(CT)"; false; }'
y=$(row_of "^▾ sqlmux")
menu_pick 10 "$y" "选择 schema…"
key Space; sleep 0.3
e2e_click 130 20; sleep 2.5
check "点浮层外也关闭、改动生效：agentable 不列了" eval '! side | grep -qE "^[▸▾] agentable" && [[ -z $(box) ]]' || { side | head -8; box; false; }
check "写回只动了 schemas 那一行：别的库的条目原样保留（postgres.foo 还在、加了 sqlmux.public），注释、别的块逐字不动" eval 'python3 - "$D/orig.toml" "$(CT)" <<PY
import sys
a = open(sys.argv[1]).read().split("\\n"); b = open(sys.argv[2]).read().split("\\n")
diff = [(x, y) for x, y in zip(a, b) if x != y]
ok = len(a) == len(b) and len(diff) == 1 and diff[0][0].startswith("schemas =") and "postgres.foo" in diff[0][1] and "sqlmux.public" in diff[0][1] and "agentable" not in diff[0][1]
sys.exit(0 if ok else 1)
PY' || diff "$D/orig.toml" "$(CT)"

# ---- 没勾的 schema 里的表：手写带前缀的 SQL 照常能跑
key C-l; key Escape; key g g; key d G; key i; e2e_type "select count(*) as n from agentable.agent"; key Escape; key Enter; sleep 2
check "手写 select count(*) from agentable.agent：照常跑出结果（没勾的 schema 只是不加载，不是不让查）" eval 'e2e_plain | grep -qE "│ +[0-9]+ +│" && ! e2e_plain | grep -qi "does not exist\\|不存在"' || { e2e_plain | tail -12 | cut -c60-170; false; }
quit_app
_e2e_kill

# ---- 命令行的临时连接：只改内存，不写 connections.toml
e2e_start -C "$D/c" "$E2E_BIN $SQLMUX_TEST_PG"; wait_for 8 flag_is alternate_on 1; sleep 3
cp "$(CT)" "$D/before.toml"
y=$(row_of "^▾ +[a-z_0-9]+@")
y=${y:-$(row_of "^▾")}
y=$(row_of "^▾ sqlmux *$")
menu_pick 10 "$y" "选择 schema…"
key Space; sleep 0.3; key Escape; sleep 2.5
check "临时连接（命令行给的 URL）上勾 schema：树上 agentable 不列了（改的是内存）" eval '! side | grep -qE "^[▸▾] agentable"' || { side | head -8; false; }
check "…但 connections.toml 一个字节没动（临时连接不写文件）" eval 'cmp -s "$(CT)" "$D/before.toml" || { diff "$D/before.toml" "$(CT)"; false; }'
quit_app
e2e_done
