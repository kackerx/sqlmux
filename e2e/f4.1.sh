#!/usr/bin/env bash
# F4.1 快速 SQL 接上 M3（specs/m4-palette/task.md F4.1；tech-design §12「写语句」「C-t 送到结果区」「C-e 在 console 中打开」、§9.3、§13）
# 提示和按钮的位置、按钮的让位交给 golden / 单测；这里测真实 PG 上写语句一句不发、C-t 的 quick #n 与 R、C-e 进 console（文件、撤销、目标 pane）。
# 默认布局：① 空、② console_1，焦点在 ①；有结果后 ③ 结果区在底部全宽。自建库：要改函数、插数据；console 的文件放在 -D 的目录里。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); CD=$D/data/sqlmux/consoles/doraemon
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
APP=e2e-f41-$$
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$E2E_DB" "$APP" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
# console_2 已有内容（C-e 追加在它后面）；console_4.sql 是个目录（读不出来）
(umask 077; mkdir -p "$CD/console_4.sql"; printf 'select 1;\n-- old' >"$CD/console_2.sql")
. "$(dirname "$0")/palette.sh"
WARN=#e0af68 RP=3
psql_n() { psql "$E2E_DB" -At -c "$1"; }
psql_n "create function e2e_w() returns int language sql as \$\$ insert into t_log (logged_at, msg) values (now(), 'w') returning 1 \$\$" >/dev/null
psql_n "create function e2e_f() returns int language sql as 'select 1'" >/dev/null
# mark / sent_since：记下服务端的时间，之后本次运行的连接发出过的语句（每条连接只留最后一条）
mark() { T0=$(psql_n "select clock_timestamp()"); }
sent_since() { psql_n "select query from pg_stat_activity where application_name = '$APP' and query_start > '$T0'" | tr '\n' ' '; }
blocked() { [[ $(title) == "只读 C-e console" && $(line1) == "写语句不在这里执行 · C-e 在 console 中打开" ]] || { echo "  title '$(title)' / '$(line1)'"; false; }; }
# cline P N：pane P 里 console 第 N 行的文字（行号栏之后）
cline() { local g; g=($(geom "$1")); e2e_text $((g[0] + 6)) $((g[0] + g[2] - 2)) $((g[1] + $2)) | sed 's/ *$//'; }
ccur() { local g; g=($(geom "$1")); echo "$(( $(e2e_flag cursor_y) - g[1] + 1 )),$(( $(e2e_flag cursor_x) - g[0] - 4 ))"; }   # P：终端光标在 console 的第几行、第几个字符
# ③ 结果区（同 f3.8）
rtabs() { tabbar $RP | sed -E 's/ {3,}.*//; s/- │/ │/g; s/-$//'; }
rtabs_are() { [[ $(rtabs) == "$1" ]] || { echo "  ③ tabs: '$(rtabs)', want '$1'"; false; }; }
rrow() { local g; g=($(geom $RP)); e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $((g[1] + $1)) | sed 's/^ //; s/ *$//'; }
rval() { rrow 3 | awk -F'│' '{ gsub(/ /, "", $2); print $2 }'; }   # 结果第一行第一列
logs() { local g y; g=($(geom $RP)); for ((y = g[1] + 1; y < g[1] + g[3] - 2; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/^ //; s/ *$//'; done | sed '/^$/d'; }
done_run() { wait_for 8 eval '[[ $(bar) != *busy* ]]'; sleep 0.3; }

start -C "$D/own" -D "$D/data"
pal
# 先把要用到的列取进缓存（输入里出现表名、CTE 时补全会取列，f4.1 以外的事）：之后本次运行的连接应当一句不发
sql "select * from t_log"; sql "select * from mv_order_by_status"; clear_all; e2e_type ";with x as (delete from t_log returning *) select * from x"; sleep 1
n0=$(psql_n "select count(*) from t_log")

# ---- 写语句不执行（F-05、§9.3）：结果区第一行 warn 色的提示，数据库什么都没收到
mark; sql "delete from t_log"
check "delete from t_log ↵：标题只剩「只读」和 C-e console，标题下第一行是 warn 色的「写语句不在这里执行 · C-e 在 console 中打开」" eval 'blocked && g=($(pbox)) && style_has $((g[0] + 2)) $(( $(prows | grep -n -m1 写语句 | cut -d: -f1) + g[1] - 1 )) fg=$WARN'
bad=
while IFS= read -r q; do sql "$q"; blocked >/dev/null || bad+=" [$q → $(title) / $(line1)]"; done <<'Q'
with x as (delete from t_log returning *) select * from x
DELETE FROM t_log
/* 注释 */ delete from t_log
(select * from t_log for update)
select * from t_log for update
explain analyze delete from t_log
copy t_log to '/tmp/sqlmux-e2e-f41'
reindex table t_log
cluster t_log
vacuum t_log
refresh materialized view mv_order_by_status
checkpoint
load 'plpgsql'
Q
check "with … delete、大写、前面带注释、括号里的 for update、explain analyze、copy to 文件、reindex、cluster、vacuum、refresh、checkpoint、load：都是写语句提示" eval '[[ -z $bad ]] || { echo "  $bad"; false; }'
check "这些都没发给数据库：本次运行的连接在这之后一句没发，t_log 的行数不变" eval '[[ -z $(sent_since) && $(psql_n "select count(*) from t_log") == "$n0" ]] || { echo "  sent: $(sent_since)"; false; }'
sql "selec 1"
check "拼错的首词 selec 照常发给数据库：标题下是它的语法错误" eval '[[ $(line1) == "ERROR: syntax error at or near \"selec\" (SQLSTATE 42601)" && -n $(sent_since) ]] || { echo "  $(line1)"; false; }'
bad=
for q in "TABLE t_log" "explain select 1" "values (1)" "show search_path"; do sql "$q"; [[ $(title) == *" 行 · "*" · 只读 C-y CSV · C-t 结果区 · C-e console" ]] || bad+=" [$q → $(title) / $(line1)]"; done
check "读语句照常执行：TABLE（大写）、explain、values、show 都出结果，标题带三个按钮" eval '[[ -z $bad ]] || { echo "  $bad"; false; }'
sql "select e2e_w()"
check "判为读、调用的函数会写：只读事务报错，t_log 不变（§13）" eval '[[ $(line1) == *"cannot execute INSERT in a read-only transaction"* && $(psql_n "select count(*) from t_log") == "$n0" ]] || { echo "  $(line1)"; false; }'

# ---- 写语句也进历史，找回来再 C-e（F4.1 细节）
clear_all; e2e_type '\;'; sleep 0.3   # 单独的 ; 要转义，否则 tmux 当成命令分隔符
for ((i = 0; i < 30; i++)); do [[ $(selected) == "delete from t_log" ]] && break; e2e_keys Down; sleep 0.1; done   # 列表只有 12 行，往下翻
check "; 后面为空：历史里有 delete from t_log" eval '[[ $(selected) == "delete from t_log" ]] || { list; false; }'
e2e_keys Enter; sleep 0.5
check "选中它 ↵：填进输入，还是写语句提示" eval '[[ $(input) == ";delete from t_log" ]] && blocked'

# ---- C-e：在目标 pane 按 console.new 开 console，追加在已有内容后面，一个撤销步（§12「C-e 在 console 中打开」）
e2e_keys C-e; sleep 0.5
check "C-e：面板关掉，① 的引导 tab 换成 console_2（console_1 开在 ②），焦点在 ①" eval 'closed && focus_is 1 && tabs_are 1 "1:console_2*"'
check "console_2.sql 原来的两行后面空一行，接着这条 SQL；光标在 SQL 那一行行首、NORMAL" eval '[[ $(cline 1 1) == "select 1;" && $(cline 1 2) == "-- old" && -z $(cline 1 3) && $(cline 1 4) == "delete from t_log" ]] && [[ $(ccur 1) == 4,1 ]] && mode_is NORMAL || { for i in 1 2 3 4; do echo "  $i: $(cline 1 $i)"; done; false; }'
key u
check "u 一次撤掉整个追加" eval '[[ $(cline 1 1) == "select 1;" && $(cline 1 2) == "-- old" && -z $(e2e_text 35 102 4 | tr -d " ") ]]'
key C-r
pal '\;'; e2e_keys C-e; sleep 0.3
check "; 后面为空时 C-e 不做事：面板还开着" eval 'is_open && [[ $(input) == ";" ]]'
e2e_keys Escape; sleep 0.3

# ---- C-t：面板里的结果成为结果区里固定的 quick #n，面板不关（§12「C-t 送到结果区」）
key C-l; key i; e2e_type "select 1 as a"; key Escape; key Enter; done_run
check "console_1 执行一次：③ 结果区 1:日志 │ 2:console_1 #1" rtabs_are "1:日志 │ 2:console_1 #1*"
key Space; e2e_type r; sleep 0.4
check "SPC r：结果区隐藏" eval '[[ -z $(geom $RP) ]]'
# 面板开着时它盖住了 ③ 的边框，e2e_panes 认不出 ③：看树的工作区（sidebar 里 pane-3 下面列出它的 tab）
wtree() { e2e_plain | sed 's/^│//; s/│.*//' | noicon | sed 's/^ *//; s/ *$//'; }   # the sidebar's rows (cut -c counts bytes here)
pal; sql "select count(*) from t_log"; e2e_keys C-t; sleep 0.5
check "C-t：隐藏着的结果区出来，工作区里多了 quick #2（序号和 console 的共用）；面板还开着，输入还在" eval 'is_open && [[ $(input) == ";select count(*) from t_log" ]] && wtree | grep -q "quick #2$" && wtree | grep -q "pane-3$" || { wtree | tail -8; false; }'
e2e_keys C-t; sleep 0.5; e2e_keys Escape; sleep 0.3
check "同一个结果再按一次：又一个 quick #3" rtabs_are "1:日志 │ 2:console_1 #1 │ 3:quick #2 │ 4:quick #3*"
pal; sql "delete from t_log"; e2e_keys C-t; sleep 0.4; sql "selec 1"; e2e_keys C-t; sleep 0.4
check "写语句提示、报错时 C-t 不做事" eval '! wtree | grep -q "quick #4$"'
sql "select g from generate_series(1, 150) as s(g)"; e2e_keys C-t; sleep 0.5   # 以 ) 结尾：末尾的词不会被 ↵ 补全
# 补全列表开着时 C-y / C-t 照常生效，esc 先关列表（2db2d60）
sql "select id from t_order where id <= 2 order by id"; e2e_type " and t_o"; sleep 0.6
check "输入 t_o：补全列表开着" eval '[[ -n $(pop) ]]'
e2e_keys C-y; sleep 0.3; e2e_keys C-t; sleep 0.5
check "列表开着按 C-y：（假的）系统剪贴板里是结果的 CSV" eval 'wait_for 3 eval "[[ \"\$(clip)\" == \"\$(printf \"id\\n1\\n2\")\" ]]" || { clip | od -c | head -3; false; }'
check "列表开着按 C-t：送进结果区（quick #4 之后多了 quick #5）" eval 'wtree | grep -q "quick #5$"'
e2e_keys Escape; sleep 0.3
check "esc：先关补全列表，面板还在" eval 'is_open && [[ -z $(pop) ]]'
e2e_keys Escape; sleep 0.3
click_tab $RP "1:"
check "日志：每次 C-t 记一行 quick  <SQL>  N 行，截断的写 100+" eval 'l=$(logs); [[ $(grep -c "  quick  select count(\*) from t_log  1 行" <<<"$l") == 2 && $l == *"  quick  select g from generate_series(1, 150) as s(g)  100+ 行"* && $l == *"  quick  select id from t_order where id <= 2 order by id  2 行"* ]] || { echo "$l"; false; }'
e2e_click 130 2; sleep 0.3; key Enter; done_run                                  # 点 ② 的第 1 行聚焦它，↵ 执行
check "console 再执行：只替换没固定的 console_1 #1，quick 的 tab 都留着" rtabs_are "1:日志 │ 2:console_1 #6* │ 3:quick #2 │ 4:quick #3 │ 5:quick #4 │ 6:quick #5"

# ---- R：在 Meta 的只读事务里重跑，原地替换，#n 和导出名不变（§12）
psql_n "insert into t_log (logged_at, msg) values (now(), 'e2e')" >/dev/null
click_tab $RP "3:"; key R; done_run
check "quick #2 上按 R：原地换成新的行数（$((n0 + 1))），tab 还是 quick #2，没有多出 tab" eval 'rtabs_are "1:日志 │ 2:console_1 #6 │ 3:quick #2* │ 4:quick #3 │ 5:quick #4 │ 6:quick #5" && [[ $(rval) == $((n0 + 1)) ]] || { rrow 3; false; }'
g=($(geom $RP)); e2e_click $((g[0] + g[2] - 9)) "${g[1]}"; sleep 0.5   # 标题上的导出按钮（倒数第二个，同 f3.8）
check "导出：当前目录的 quick-2.csv，内容是重跑之后的" eval '[[ $(cat "$E2E_TMP/quick-2.csv" 2>/dev/null) == "$(printf "count\n%s" $((n0 + 1)))" ]] || { ls "$E2E_TMP"; false; }'
pal; sql "select current_schema()"; e2e_keys C-t; sleep 0.5; e2e_keys Escape; sleep 0.3
check "树在 public：quick #7 里是 public" eval '[[ $(rval) == public ]] || { rrow 3; false; }'
TREE_AGENTABLE="12 5"; e2e_click $TREE_AGENTABLE; sleep 0.3                  # 点树上的 agentable：焦点和光标都到那里
click_tab $RP "7:"; key R; done_run
check "树的光标移到 agentable 后在 quick #7 上 R：search_path 取按 R 时树的 schema" eval '[[ $(rval) == agentable ]] || { rrow 3; false; }'
pal; sql "select e2e_f()"; e2e_keys C-t; sleep 0.5; e2e_keys Escape; sleep 0.3
psql_n "create or replace function e2e_f() returns int language plpgsql as \$\$ begin raise exception 'bad' using detail = 'the detail', hint = 'the hint'; end \$\$" >/dev/null
key R; done_run
check "R 失败：切到日志，ERROR、DETAIL、HINT 各占一行" eval 'l=$(logs); [[ $(grep -A2 "  quick  select e2e_f()  ERROR: bad" <<<"$l" | sed "s/^ *//" | tail -2 | tr "\n" /) == "DETAIL: the detail/HINT: the hint/" ]] || { echo "$l" | tail -4; false; }'
click_tab $RP "8:"
check "quick #8 里还是上一次的 1" eval 'rtabs_are "1:日志 │ 2:console_1 #6 │ 3:quick #2 │ 4:quick #3 │ 5:quick #4 │ 6:quick #5 │ 7:quick #7 │ 8:quick #8*" && [[ $(rval) == 1 ]]'

# ---- C-e 的目标 pane、取的文字、schema；补全列表开着也照常；按钮能点
e2e_click 130 2; sleep 0.3; e2e_click $TREE_AGENTABLE; sleep 0.3               # 点 ② 聚焦，再点树上的 agentable：② 是最近聚焦过的普通 pane
pal; sql "select 2 as y"; clear_all; e2e_type ";delete from agent_v"; sleep 0.6
check "输入 agent_v：补全列表开着（树在 agentable，补全用它的表）" eval '[[ -n $(pop) ]]'
e2e_keys C-e; sleep 0.5
check "焦点在树上时 C-e：开到最近聚焦过的 ②，新开 tab console_3（当前不是引导 tab，不替换），焦点在 ②" eval 'closed && focus_is 2 && tabs_are 2 "1:console_1- │ 2:console_3*"'
check "取的是输入框里的文字（delete from agent_v，不是上次执行的 select 2 as y）；新文件只有这一行，光标在 1,1" eval '[[ $(cline 2 1) == "delete from agent_v" && -z $(cline 2 2) ]] && [[ $(ccur 2) == 1,1 ]] || { cline 2 1; ccur 2; false; }'
check "console_3 的 schema 取树当前的 agentable" eval 'text_has 105 160 1 ".agentable ▾"'
pal; sql "select 3 as z"; e2e_keys C-e; sleep 0.5
check "console_4.sql 读不出来：toast「读取失败」，面板不关，SQL 还在输入框里" eval 'screen_has "读取失败" && screen_has "console_4.sql" && is_open && [[ $(input) == ";select 3 as z" ]]'
rmdir "$CD/console_4.sql"
y=$(( $(prows | grep -n -m1 " 只读" | cut -d: -f1) + $(pbox | cut -d" " -f2) - 1 ))
c=$(e2e_find "C-t 结果区" "$y"); e2e_click "${c%% *}" "$y"; sleep 0.5
check "点标题上的 C-t 结果区：同 C-t，面板还开着" eval 'is_open && wtree | grep -q "quick #9$" || { wtree | tail -5; false; }'
c=$(e2e_find "C-e console" "$y"); e2e_click "${c%% *}" "$y"; sleep 0.5
check "点标题上的 C-e console：同 C-e，② 新开 console_4，里面是 select 3 as z" eval 'closed && focus_is 2 && tabs_are 2 "1:console_1 │ 2:console_3- │ 3:console_4*" && [[ $(cline 2 1) == "select 3 as z" ]]'

e2e_done
