#!/usr/bin/env bash
# F14.4 结构修改的模板（PG）（specs/m14-sqlite/task.md F14.4；tech-design §7.8「右键菜单」）
# 表节点的右键菜单和命令面板里有「加列… / 加索引… / 改名… / 删除列… / 删除索引…」：各开一条按引擎拼好的模板 SQL 到当前连接的新 query，占位符
# <name> / <type> / <column> / <index> / <new_name> 是字面的，第一个在 SELECT 模式下选中、打字直接替换（M15 F15.8，原来 VISUAL 选中、要按 c）；删除类直接写、不写成注释，执行走危险确认。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -c "create table cust (id int primary key, nm text)" -c "create index ix_nm on cust (nm)" -c "create view v_cust as select id from cust" >/dev/null
mkdir -p "$D/own"; printf 'confirm_dangerous = true\n' >"$D/own/config.toml"
printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
psql_n() { psql "$E2E_DB" -At -c "$1"; }
rclick() { _sgr 2 "$1" "$2" M; _sgr 2 "$1" "$2" m; sleep 0.6; }
srow() { local y; for y in $(seq 3 43); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && { echo "$y"; return; }; done; }
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }
mitems() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0; for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/^ *//; s/^[^ ]* //; s/ *$//; s/  \+[^ ]\+$//'; done | tr '\n' '|'; }
mclick() { local g y; g=($(pop)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do [[ $(e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y) == *"$1"* ]] && { e2e_click $((g[0] + 3)) $y; sleep 1.2; return; }; done; echo "  no menu item $1"; return 1; }
newest() { local f; f=$(ls -t "$E2E_TMP/data/sqlmux/queries"/*.sql 2>/dev/null | head -1); [[ -n $f ]] && cat "$f"; }   # the query just opened: its text on disk
tmpl() { sleep 1.5; newest | tr -d '\n'; }

start -C "$D/own"; sleep 1
rclick 6 "$(srow cust)"
check "表 cust 右键：打开 / 在新 tab 打开 / 删除表… 之后是五个模板项" eval '[[ $(mitems) == "打开|在新 tab 打开|删除表…|加列…|加索引…|改名…|删除列…|删除索引…|" ]]' || mitems
mclick "加列…"
check "加列…：新 query 里是 alter table \"public\".\"cust\" add column <name> <type>;（schema 是库里的 public）" eval '[[ $(tmpl) == "alter table \"public\".\"cust\" add column <name> <type>;" ]]' || newest
check "光标停在第一个占位符上并选中（SELECT）" eval 'mode_is SELECT'
e2e_type age; key Escape; sleep 1.5
check "直接打字替换：<name> 换成 age，<type> 还在" eval '[[ $(newest | tr -d "\n") == "alter table \"public\".\"cust\" add column age <type>;" ]]' || newest

# 其余四项：各一条模板，第一个占位符选中（SELECT）
for row in '加索引…|create index <name> on "public"."cust" (<column>);' '改名…|alter table "public"."cust" rename to <new_name>;' '删除列…|alter table "public"."cust" drop column <column>;' '删除索引…|drop index "public".<index>;'; do
  rclick 6 "$(srow cust)"; mclick "${row%%|*}"
  check "${row%%|*}：$(echo "${row#*|}" | tr -d '"')" eval '[[ $(tmpl) == "${row#*|}" ]] && mode_is SELECT' || newest
  key Escape; key Escape; sleep 0.3
done
# 删除类直接写、不写成注释；占位符换成真名之后执行走危险确认，取消什么都不动、确认才删
rclick 6 "$(srow cust)"; mclick "删除索引…"; sleep 1.5
e2e_type ix_nm; key Escape; sleep 0.5
key Enter; sleep 1
check "删除索引模板换上 ix_nm 后 ↵ 执行：弹危险确认（「这次执行有 1 条危险语句」，列出 drop index），索引还在" eval 'e2e_plain | grep -qF "drop index \"public\".ix_nm" && e2e_plain | grep -qE "危险语句" && [[ $(psql_n "select count(*) from pg_indexes where indexname = '"'"'ix_nm'"'"'") == 1 ]]'
key Escape; sleep 0.5
check "esc 取消：索引还在" eval '[[ $(psql_n "select count(*) from pg_indexes where indexname = '"'"'ix_nm'"'"'") == 1 ]] && ! e2e_plain | grep -qE "危险语句"'
key Enter; sleep 0.8; e2e_type y; sleep 1.2
check "再 ↵、y 确认：索引删掉了" eval '[[ $(psql_n "select count(*) from pg_indexes where indexname = '"'"'ix_nm'"'"'") == 0 ]]'
# 视图：右键菜单里有「改名…」（没有加列 / 加索引 / 删除列 / 删除索引：视图没有）
tclick() { local y x; y=$(srow "$1"); x=$(e2e_plain | sed -n "${y}p" | python3 -c "import sys; l=sys.stdin.read(); print(max(l.find('▸'), l.find('▾')) + 1)"); e2e_click "$x" "$y"; sleep 0.5; }
tclick "Views"
rclick 6 "$(srow v_cust)"
check "视图 v_cust 右键：打开 / 在新 tab 打开 / 删除视图… / 改名…（视图只有改名这一项模板）" eval '[[ $(mitems) == *"删除视图…|改名…|" && $(mitems) != *"加列"* ]]' || mitems
mclick "改名…"
check "视图的改名模板：alter view \"public\".\"v_cust\" rename to <new_name>;" eval '[[ $(tmpl) == "alter view \"public\".\"v_cust\" rename to <new_name>;" ]]' || newest
# 命令面板：当前表 tab 上有这五项，同一条模板
key Escape; key Escape; open_table cust; wait_for 8 settled
pal ">加索引"; sleep 0.4; key Enter
check "命令面板「加索引…」：对当前表 cust 开模板，光标所在的列（id）直接写进去：create index <name> on \"public\".\"cust\" (\"id\");" eval '[[ $(tmpl) == "create index <name> on \"public\".\"cust\" (\"id\");" && $(bar) == *SELECT* ]]' || newest
key Escape; key Escape; open_table cust; wait_for 8 settled; key l
pal ">删除列"; sleep 0.4; key Enter
check "光标在 nm 列上再从面板「删除列…」：alter table \"public\".\"cust\" drop column \"nm\";（列名直接写进模板，没有占位符）" eval '[[ $(tmpl) == "alter table \"public\".\"cust\" drop column \"nm\";" ]]' || newest
e2e_done
