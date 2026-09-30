#!/usr/bin/env bash
# F1.7 命令面板：快速 SQL（specs/m1-browse/task.md F1.7；tech-design §12「快速 SQL」、§14 state.json）
# 布局交给 golden（TestGoldenQuickSQL160x45）；这里测真实 PG 上的执行、取消、补全、重启后的历史和剪贴板：
# 第一次启动时 PATH 里没有剪贴板工具（-P /bin），C-y 走 OSC 52 后备、查 tmux 的 buffer；最后照默认 PATH 走假的 pbcopy（F3.38）。
# 在自建库里做：要看写语句被拒绝、数据不变；application_name 用来认出本次运行的连接。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); ST=$D/state
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
APP=e2e-f17-$$
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$E2E_DB" "$APP" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
. "$(dirname "$0")/palette.sh"
ERROR=#f7768e
psql_n() { psql "$E2E_DB" -At -c "$1"; }
pbox() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }   # 面板的 X Y W H
# prows：面板内每一行的文字（第一行是上边框）；结果区的标题行以「只读」收尾
prows() { local g; g=($(pbox)); e2e_rows $((g[0] + 1)) $((g[0] + g[2] - 2)) ${g[1]} $((g[1] + g[3] - 1)) $((g[0] + 1)) | cut -d'|' -f1; }
title() { prows | grep -m1 ' 只读' | tr -s ' ' | sed 's/^ //; s/ $//'; }
title_like() { [[ $(title) =~ $1 ]] || { echo "  title: '$(title)', want /$1/"; false; }; }
line1() { prows | awk '/ 只读/ { getline; print; exit }' | sed 's/^ *//; s/ *$//'; }   # 标题下面第一行（报错时就是错误）
# grid：结果区表格的数据行，每行「列1|列2…」
grid() { prows | awk '/ 只读/ { f = 1; next } f && /┼/ { g = 1; next } g && /│/' | sed 's/^ *[0-9]* │//; s/ *│ */|/g; s/^ *//; s/ *$//'; }
pfoot() { prows | tail -2 | head -1 | sed 's/ *$//'; }                     # 底栏
modified() { [[ $(pfoot) == *"已修改，↵ 重新执行" ]]; }
input() { local g; g=($(pbox)); e2e_text $((g[0] + 3)) $((g[0] + g[2] - 2)) $((g[1] + 2)) | sed 's/^ *//; s/ *$//'; }
clear_all() { local i; for ((i = 0; i < 120; i++)); do e2e_keys BSpace; done; sleep 0.3; }
# sql TEXT：在已打开的面板里清空输入、输入 ;TEXT、↵，等结果回来
sql() { clear_all; e2e_type ";$1"; sleep 0.4; e2e_keys Enter; wait_for 8 eval '[[ -n $(title) && $(title) != "… 行"* ]]'; sleep 0.2; }
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | sed -n 2p; }   # 补全列表：面板之后的第二个浮层
items() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | tr -s ' ' | sed 's/^ //; s/ $//'; done; }
active() { psql_n "select count(*) from pg_stat_activity where application_name = '$APP' and state = 'active' and query ~* 'pg_sleep|fetch'"; }

start -P /bin -C "$D/own" -S "$ST"
pal
check "面板多了「SQL ;」范围" eval '[[ $(e2e_text $(($(left) + 2)) $(($(right) - 2)) $(tabs_y)) == *"SQL ;"* ]]'

# ---- 执行：Meta 上的只读事务，结果在面板下半部分（§12）
sql "select status, count(*) from t_order group by 1 order by 1"
want=$(psql_n "select status || '|' || count(*) from t_order group by status order by status" | tr '\n' /)
check "group by：标题「4 行 · 耗时 · 只读」，右侧 C-y CSV；四行和 psql 一致" eval 'title_like "^4 行 · [0-9.]+(µs|ms|s) · 只读 C-y CSV$" && [[ $(grid | tr "\n" /) == "$want" ]] || { grid; echo "  want $want"; false; }'
check "刚执行完：底栏没有「已修改」" eval '! modified'
e2e_keys BSpace; sleep 0.3
check "改动输入：底栏「已修改，↵ 重新执行」" modified
e2e_type 1; sleep 0.3
check "改回原样：「已修改」消失" eval '! modified'
sql "select * from t_order limit 100"
check "正好 100 行：100 行，没有 +" title_like "^100 行 · "
sql "select * from t_order limit 101"
check "101 行：100+ 行" title_like "^100\+ 行 · "
g=($(pbox)); e2e_wheel $((g[0] + 10)) $((g[1] + g[3] - 6)) down; sleep 0.3
check "滚轮在结果区上：结果向下滚（第一行不再是 id 1）" eval '[[ $(grid | head -1 | cut -d"|" -f1) != 1 ]] || { grid | head -2; false; }'
sql "select i, 1/(i - 5000) from generate_series(1, 10000) as g(i)"   # 以 ) 结尾：末尾的词不会被 ↵ 补全（F1.14）
check "服务端只算到第 101 行：第 5000 行的除零没有发生，显示 100+" eval 'title_like "^100\+ 行 · " && [[ $(line1) != *ERROR* ]]'
sql "selec 1"
check "语法错误：标题下第一行是数据库原文，红色" eval '[[ $(line1) == "ERROR: syntax error at or near \"selec\" (SQLSTATE 42601)" ]] && g=($(pbox)) && style_has $((g[0] + 2)) $(( $(prows | grep -n -m1 "ERROR:" | cut -d: -f1) + g[1] - 1 )) fg=$ERROR || { echo "  $(line1)"; false; }'
sql "delete from t_order where id = 1"
check "写语句：只读事务拒绝，显示数据库的错误，数据不变" eval '[[ $(line1) == *"cannot execute DELETE in a read-only transaction"* && $(psql_n "select count(*) from t_order where id = 1") == 1 ]] || { echo "  $(line1)"; false; }'

# ---- 执行中：标题「… 行」，再按 ↵ 忽略；C-c 取消查询、面板不关、保留上次结果（§12、§8.3）
sql "select 42 as x"
clear_all; e2e_type ";select pg_sleep(30)"; sleep 0.3; e2e_keys Enter; sleep 0.5
check "执行中：标题「… 行 · 只读」，服务端在跑" eval 'title_like "^… 行 · 只读" && [[ $(active) == 1 ]]'
e2e_keys Enter; sleep 0.5; e2e_keys C-c; sleep 0.3
check "C-c：toast「查询已取消」画在遮罩之上（不压暗，§7.5），面板还开着，结果区还是上一次的 42" eval 'toast_is "查询已取消" && is_open && title_like "^1 行 · " && [[ $(grid) == 42 ]]'
check "服务端也取消了；执行中多按的 ↵ 没有排队再跑一次" eval 'wait_for 3 eval "[[ \$(active) == 0 ]]" && sleep 1 && [[ $(active) == 0 ]]'
check "取消之后：输入和上次执行的不同，底栏「已修改」" modified
check "执行完、取消后都 ROLLBACK 了：本次运行的连接没有停在事务里" eval '[[ $(psql_n "select count(*) from pg_stat_activity where application_name = '"'$APP'"' and state like '"'idle in transaction%'"'") == 0 ]]'
e2e_keys C-c; sleep 0.3
check "空闲时 C-c 等同 esc：关闭面板，不出退出提示" eval 'closed && ! screen_has "再按一次"'

# ---- 光标退进 ; 前缀（6f09695 修过的崩溃）
pal ";sel"; for i in 1 2 3 4 5 6; do e2e_keys Left; done; sleep 0.3; e2e_type x; sleep 0.3
check "光标退到 ; 左边再输入：程序照常，输入是 x;sel" eval 'running && is_open && [[ $(input) == "x;sel" ]]'
e2e_keys Escape; sleep 0.3

# ---- 补全：树当前 schema 的表名、输入里出现过的表的列、关键字（§12「补全」、§9.7）
pal ";select * from t_o"
check "表名的前几个字母：出现候选 t_order、t_order_item" eval 'n=$(items | head -2 | cut -d" " -f1 | tr "\n" " ") && [[ $n == "t_order t_order_item " ]] || { items; false; }'
clear_all; e2e_type ";select st"; sleep 0.5
check "还没写表名：没有列候选（没有 status）" eval '! items | grep -q "^status " || { items; false; }'
e2e_type " from t_order where st"; sleep 0.8
check "写了 t_order 之后：列 status 排第一，接着是表 / 视图（F3.12 起词首的 st 也匹配 mv_order_by_status），关键字在最后" eval '[[ $(items | head -1) == "status order_status · t_order" && $(items | sed -n 2p) == "mv_order_by_status 视图" && $(items | sed -n 3p) == *" 关键字" ]] || { items; false; }'
e2e_keys Escape; sleep 0.3
check "esc 先关补全列表，面板还在" eval 'is_open && [[ -z $(pop) ]]'
e2e_type a; sleep 0.5; e2e_keys Enter; sleep 0.3
check "sta：status 已选中，↵ 接受，不执行（F1.14）" eval '[[ $(input) == ";select st from t_order where status" && -z $(title) ]]'
e2e_type " = 'done' and m"; sleep 0.5
check "输入 m：列 meta → 物化视图 mv_order_by_status（F3.10 起标「视图」）→ 关键字，首字符都是 m（F1.14）" eval '[[ $(items | head -1) == "meta "*"· t_order" && $(items | sed -n 2p) == "mv_order_by_status 视图" && $(items | sed -n 3p) == *" 关键字" ]] || { items; false; }'
e2e_keys BSpace; e2e_type paid; sleep 0.5
check "输入完整的 paid：列表开着，选中的就是 paid" eval '[[ $(items | head -1) == "paid "* ]] || { items; false; }'
e2e_keys Enter; wait_for 8 eval '[[ -n $(title) ]]'
check "接受之后文字不变：↵ 直接执行（输入不变，结果区是数据库的报错）" eval '[[ $(input) == ";select st from t_order where status = '"'done'"' and paid" && $(line1) == ERROR:* ]] || { echo "  $(input) / $(line1)"; false; }'

# ---- 树停在 agentable（F1.12：光标所在节点的 schema）：同一事务里 SET LOCAL search_path，agent 能直接找到，补全也换成 agentable 的表
e2e_keys Escape; sleep 0.3; key C-h; key g g; key j; sleep 0.3                   # 光标移到 agentable 节点
pal; sql "select count(*) from agent"
check "树在 agentable：;select count(*) from agent 能找到表，结果和 psql 一致" eval '[[ $(grid) == $(psql_n "select count(*) from agentable.agent") ]] || { echo "  $(line1)"; false; }'
clear_all; e2e_type ";select * from agent_v"; sleep 0.5
check "补全用 agentable 的表" eval '[[ $(items | head -1) == "agent_version 表" ]] || { items; false; }'
e2e_keys Escape Escape; sleep 0.3; key C-h; key j; key C-l                      # 光标回到 public 节点

# ---- C-y：结果转成 CSV（表头 + 显示的行，NULL 为空）进剪贴板（F-04）；PATH 里没有 pbcopy 这类工具，走 OSC 52（F3.38）
t set -g set-clipboard on
q="select logged_at, msg, null::int as n, 'a,\"b\"' as q from t_log order by logged_at"
pal; sql "$q"
want=$(psql_n "copy ($q) to stdout with (format csv, header)"; echo .)
e2e_keys C-y; sleep 0.5
check "C-y：剪贴板里是 CSV，和 PG 的 COPY csv 一致（换行、引号、NULL）" eval '[[ "$(t show-buffer; echo .)" == "$want" ]] || { t show-buffer | od -c | head; false; }'
t delete-buffer
c=$(e2e_find "C-y CSV" $(( $(prows | grep -n -m1 " 只读" | cut -d: -f1) + $(pbox | cut -d" " -f2) - 1 ))); y=$(( $(prows | grep -n -m1 " 只读" | cut -d: -f1) + $(pbox | cut -d" " -f2) - 1 ))
e2e_click "${c%% *}" "$y"; sleep 0.5
check "点击标题行的 C-y CSV：同样复制" eval '[[ "$(t show-buffer; echo .)" == "$want" ]]'

# ---- 历史：; 后面为空时列出，新的在前、去重，↵ 填进输入并执行；存进 state.json，重启后还在
for s in "select 1 as x" "select 2 as y" "select 1 as x"; do sql "$s"; done   # 别名 a、b 会被 ↵ 补成 as、between（F1.14）；x、y 没有候选
e2e_keys Escape; sleep 0.3
start -C "$D/own" -S "$ST"
pal '\;'
check "重启后 ; 列出历史：新的在前，重复的只留一条（select 1 as x 在最前）" eval '[[ $(list | cut -d"|" -f1 | head -2 | tr "\n" /) == "select 1 as x/select 2 as y/" && $(list | grep -c "^select 1 as x|") == 1 ]] || { list; false; }'
e2e_keys Down; sleep 0.2; e2e_keys Enter; wait_for 8 eval '[[ -n $(title) ]]'
check "选中 select 2 as y 按 ↵：填进输入并执行" eval '[[ $(input) == ";select 2 as y" && $(grid) == 2 ]]'
e2e_keys Escape; sleep 0.3; pal
check "「所有」范围不列 SQL 历史" eval '! row_has "select 1 as x" && ! row_has "select 2 as y"'

# ---- PATH 里有 pbcopy（lib.sh 的假工具）时 C-y 调它，不经过终端（F3.38，照 nvim 的 clipboard provider）
t set -g set-clipboard on
clear_all; sql "$q"; e2e_keys C-y; sleep 0.5
check "有 pbcopy 时 C-y：（假的）系统剪贴板里是同一份 CSV，tmux 的 buffer 里没有" eval 'wait_for 3 eval "[[ \"\$(clip; echo .)\" == \"\$want\" ]]" && ! t show-buffer >/dev/null 2>&1 || { clip | od -c | head -3; false; }'

e2e_done
