#!/usr/bin/env bash
# F14.4d 结构修改的模板在 vim = false（plain 编辑器）下：第一个占位符一样被选中、打字就替换它，C-e 执行走危险确认（specs/m14-sqlite/task.md F14.4；复审）。
# F14.4 结构修改的模板（SQLite，vim = false）（specs/m14-sqlite/task.md F14.4；tech-design §7.8「右键菜单」）
# 表节点的右键菜单和命令面板里有「加列… / 加索引… / 改名… / 删除列… / 删除索引…」：各开一条按引擎拼好的模板 SQL 到当前连接的新 query，占位符
# <name> / <type> / <column> / <index> / <new_name> 是字面的，第一个在 SELECT 模式下选中、打字直接替换（M15 F15.8，原来 VISUAL 选中、要按 c）；删除类直接写、不写成注释，执行走危险确认。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
DB=$D/shop.db; sqlite3 "$DB" <<'SQL'
create table cust (id integer primary key, nm text);
create index ix_nm on cust (nm);
SQL
mkdir -p "$D/own"; printf 'confirm_dangerous = true\nvim = false\n' >"$D/own/config.toml"
nidx() { sqlite3 "$DB" "select count(*) from sqlite_schema where type = 'index' and name = 'ix_nm'"; }
rclick() { _sgr 2 "$1" "$2" M; _sgr 2 "$1" "$2" m; sleep 0.6; }
srow() { local y; for y in $(seq 3 43); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && { echo "$y"; return; }; done; }
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }
mitems() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0; for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/^ *//; s/^[^ ]* //; s/ *$//; s/  \+[^ ]\+$//'; done | tr '\n' '|'; }
mclick() { local g y; g=($(pop)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do [[ $(e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y) == *"$1"* ]] && { e2e_click $((g[0] + 3)) $y; sleep 1.2; return; }; done; echo "  no menu item $1"; return 1; }
newest() { local f; f=$(ls -t "$E2E_TMP/data/sqlmux/queries"/*.sql 2>/dev/null | head -1); [[ -n $f ]] && cat "$f"; }   # the query just opened: its text on disk
tmpl() { sleep 1.5; newest | tr -d '\n'; }

e2e_start -C "$D/own" "$E2E_BIN $DB"; wait_for 5 flag_is alternate_on 1; sleep 1.5
rclick 6 "$(srow cust)"; mclick "加列…"
check "plain 编辑器：加列模板开出来（alter table main.cust add column <name> <type>;）" eval '[[ $(tmpl) == "alter table \"main\".\"cust\" add column <name> <type>;" ]]' || newest
e2e_type age; sleep 0.4; key Escape; sleep 1.5
check "打字就替换选中的第一个占位符：<name> 换成 age（c 也不用按），<type> 还在" eval '[[ $(newest | tr -d "\n") == "alter table \"main\".\"cust\" add column age <type>;" ]]' || newest
rclick 6 "$(srow cust)"; mclick "删除索引…"; sleep 1.5
e2e_type ix_nm; sleep 0.4
check "删除索引模板：打 ix_nm 替换 <index>（文字是 drop index ix_nm;）" eval '[[ $(tmpl) == "drop index ix_nm;" ]]' || newest
e2e_keys C-e; sleep 1
check "C-e 执行：走危险确认（「这次执行有 1 条危险语句」），索引还在" eval 'e2e_plain | grep -qF "drop index ix_nm" && e2e_plain | grep -qE "危险语句" && [[ $(nidx) == 1 ]]' || e2e_plain | tail -8 | cut -c1-160
key Escape; sleep 0.4
e2e_keys C-e; sleep 0.8; e2e_type y; sleep 1.2
check "再 C-e、y 确认：drop index ix_nm 执行了，索引没了" eval '[[ $(nidx) == 0 ]]'
open_table cust; wait_for 8 settled
pal ">改名"; sleep 0.4
check "命令面板里表的改名叫「改名表…」" eval 'row_has "改名表…"' || list
e2e_done
