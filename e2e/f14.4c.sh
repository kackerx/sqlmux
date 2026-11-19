#!/usr/bin/env bash
# F14.4 结构修改的模板（SQLite）（specs/m14-sqlite/task.md F14.4；tech-design §7.8「右键菜单」）
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
mkdir -p "$D/own"; printf 'confirm_dangerous = true\n' >"$D/own/config.toml"
nidx() { sqlite3 "$DB" "select count(*) from sqlite_schema where type = 'index' and name = 'ix_nm'"; }
rclick() { _sgr 2 "$1" "$2" M; _sgr 2 "$1" "$2" m; sleep 0.6; }
srow() { local y; for y in $(seq 3 43); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && { echo "$y"; return; }; done; }
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }
mitems() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0; for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/^ *//; s/^[^ ]* //; s/ *$//; s/  \+[^ ]\+$//'; done | tr '\n' '|'; }
mclick() { local g y; g=($(pop)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do [[ $(e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y) == *"$1"* ]] && { e2e_click $((g[0] + 3)) $y; sleep 1.2; return; }; done; echo "  no menu item $1"; return 1; }
newest() { local f; f=$(ls -t "$E2E_TMP/data/sqlmux/queries"/*.sql 2>/dev/null | head -1); [[ -n $f ]] && cat "$f"; }   # the query just opened: its text on disk
tmpl() { sleep 1.5; newest | tr -d '\n'; }

e2e_start -C "$D/own" "$E2E_BIN $DB"; wait_for 5 flag_is alternate_on 1; sleep 1.5
rclick 6 "$(srow cust)"
check "表 cust 右键：打开 / 在新 tab 打开 / 删除表… 之后是五个模板项（SQLite 也都支持）" eval '[[ $(mitems) == "打开|在新 tab 打开|删除表…|加列…|加索引…|改名…|删除列…|删除索引…|" ]]' || mitems
for row in '加列…|alter table "main"."cust" add column <name> <type>;' '加索引…|create index <name> on "cust" (<column>);' '改名…|alter table "main"."cust" rename to <new_name>;' '删除列…|alter table "main"."cust" drop column <column>;' '删除索引…|drop index <index>;'; do
  want=${row#*|}
  [[ ${row%%|*} == 加列… ]] || rclick 6 "$(srow cust)"
  mclick "${row%%|*}"
  check "${row%%|*}：$(echo "$want" | tr -d '"')，第一个占位符选中（SELECT）" eval '[[ $(tmpl) == "$want" && $(bar) == *SELECT* ]]' || newest
  key Escape; key Escape; sleep 0.3
done
rclick 6 "$(srow cust)"; mclick "删除索引…"; sleep 1.5
e2e_type ix_nm; key Escape; sleep 0.5
key Enter; sleep 1
check "删除索引模板换上 ix_nm 后 ↵：危险确认，索引还在" eval 'e2e_plain | grep -qE "危险语句" && [[ $(nidx) == 1 ]]'
key Escape; sleep 0.5
key Enter; sleep 0.8; e2e_type y; sleep 1.5
check "再 ↵、y 确认：drop index ix_nm 执行成功，文件里索引没了" eval '[[ $(nidx) == 0 ]]'
e2e_done
