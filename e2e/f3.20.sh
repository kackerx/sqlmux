#!/usr/bin/env bash
# F3.20 错误栏（specs/m3-console/task.md F3.20；tech-design §7.8「错误栏」、§7.6「数据库报错」、§10.3）
# 错误栏的画法（多行、截短、6 行上限）由 golden 覆盖；这里在真实 PG 上测哪些错误进错误栏、关掉它的途径、自动消失、
# 属于 tab，出错后 ORDER / LIMIT / PAGE 退回画着的那一份（发出的 SQL 看 pg_stat_activity），console 的位置换算与 R 重跑。
# 自建库：要改名表和列、锁行、写库。表格部分 SOLO（① 占满），console 部分用默认布局。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
APP=e2e-f320-$$
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$E2E_DB" "$APP" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
. "$(dirname "$0")/palette.sh"
ERROR=#f7768e SELECT=#364a82
psql_n() { psql "$E2E_DB" -At -c "$1"; }
col_x() { e2e_find "$1" "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }'; }
row_y() { echo $(( $(grid_y) + $1 )); }
nums() { local x l r c; x=$(col_x "$1"); for c in $(e2e_find │ "$(hy)"); do ((c < x)) && l=$c; ((c > x)) && [[ -z $r ]] && r=$c; done; e2e_text $((l + 1)) $((r - 1)) "$(row_y "$2")" | tr -d ' '; }
rowno() { local c; c=$(e2e_find ┼ "$(grid_y)"); e2e_text 35 $((${c%% *} - 1)) "$1" | tr -d ' '; }
where() { key /; clear_in; e2e_type "$1"; sleep 0.2; key Enter; wait_for 8 settled; sleep 0.2; }
result() { qb | sed 's/.*   //; s/^ *//; s/ *$//'; }                                   # 查询条右侧的文字
# mark / sent_since：记下服务端的时间，之后本次运行的连接发出过的语句（每条连接只留最后一条，取数在计数前面时看不到取数）
mark() { T0=$(psql_n "select clock_timestamp()"); }
sent_since() { psql_n "select query from pg_stat_activity where application_name = '$APP' and query_start > '$T0'" | tr '\n' ' '; }
sent_has() { [[ $(sent_since) == *"$1"* ]] || { echo "  sent since mark: $(sent_since)"; echo "  want …$1…"; false; }; }
dd() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }'; }
dd_sel() { local g y; g=($(dd)); for ((y = g[1] + 3; y < g[1] + g[3] - 1; y++)); do style_has $((g[0] + 3)) $y bg=$SELECT >/dev/null && e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | sed 's/ *$//'; done; }
x_close() { e2e_find × "$(errbar_y "$1")" | tr ' ' '\n' | tail -1; }                   # N：pane N 错误栏的 × 所在列
set_col() { key g g; key 0; [[ $1 -gt 1 ]] && key $(($1 - 1)) j; [[ $2 -gt 0 ]] && key $2 l; key Enter; e2e_type "$3"; sleep 0.2; key Escape; }

start -C "$D/own"; open_table t_order; wait_for 8 settled

# ---- WHERE 写错：错误栏在 pane 底部，表格照旧；视图跟着光标时让出错误栏；esc 关（§7.8，task.md F3.20 验收）
where "status = 'done'"
where "statu = 'done'"
check "WHERE 写错：错误栏第一行 error 色的 [42703]，第二行 HINT，error_bg 底；查询条右侧不报错" eval 'errbar_is 1 "[42703] column \"statu\" does not exist ×" && [[ $(errbar 1 | sed -n 2p) == "HINT: Perhaps you meant to reference the column \"t_order.status\"." ]] && style_has 36 $(errbar_y 1) fg=$ERROR && style_has 100 $(( $(errbar_y 1) + 1 )) bg=$ERROR_BG && [[ $(result) != *42703* && $(result) != *exist* ]] || { errbar 1; false; }'
check "表格照旧画着上一次（status = 'done'）的数据；失败的条件留在输入框里" eval '[[ $(nums id 1) == 2 && $(for n in 1 2 3; do e2e_text 35 159 $(row_y $n); done | grep -c " done ") == 3 && $(where_in) == "statu = '"'done'"'" ]]'
check "错误栏不盖住表格：最后一行数据紧贴错误栏上面，错误栏下面就是 tab 栏" eval '[[ $(e2e_text 35 159 $(( $(errbar_y 1) - 1 ))) == *" done "* && $(( $(errbar_y 1) + 2 )) == $(tab_y 1) ]]'
key G
check "G：视图跟着光标，最后一行（第 100 行）贴着错误栏的上沿，不被它盖住" eval 'pos_is 100,1 && y=$(( $(errbar_y 1) - 1 )) && [[ $(rowno $y) == 100 ]] || { echo "  row above the bar: $(e2e_text 35 60 $(( $(errbar_y 1) - 1 )))"; false; }'
key g g
mark; key R; sleep 1
check "R：带着失败的 WHERE 重试，又报错" eval 'sent_has "( statu = '"'done'"' )" && errbar_is 1 "[42703] column \"statu\" does not exist ×"'
key Escape
check "NORMAL 下 esc：关掉错误栏，表格画回那两行" eval '[[ -z $(errbar 1) && $(e2e_text 35 159 $(( $(tab_y 1) - 1 ))) == *" done "* ]]'
mark; key /; key Enter; sleep 1
check "/ 再 ↵：输入框里还是失败的条件，照样重试、报错" eval 'sent_has "( statu = '"'done'"' )" && errbar_is 1 "[42703] column \"statu\" does not exist ×" && mode_is NORMAL'
e2e_click "$(x_close 1)" "$(errbar_y 1)"; sleep 0.3
check "点 ×：关掉" eval '[[ -z $(errbar 1) ]]'
key /; key Enter; sleep 1; key C-p; e2e_type ">关闭错误栏"; sleep 0.3; key Enter; sleep 0.3
check "面板里执行「关闭错误栏」（pane.error.close）：关掉" eval '! palette_open && [[ -z $(errbar 1) ]]'
key /; key Enter; sleep 1
where "status = 'done'"
check "改对再 ↵：取数成功，错误栏自动消失" eval '[[ -z $(errbar 1) && $(cnt) == 1500 ]]'

# ---- 错误栏属于 tab：切走不显示，切回来还在；一个 tab 只有一条，后来的替换先前的
where "statu = 1"
key C-p; e2e_type "@t_user"; sleep 0.3; key C-t; wait_for 8 settled
check "切到新开的 t_user tab：没有错误栏" eval '[[ -z $(errbar 1) ]]'
key g T
check "切回 t_order：错误栏还在" eval 'errbar_is 1 "[42703] column \"statu\" does not exist ×"'
where "nosuch > 1"
check "又一次出错：替换先前的，只剩一条" eval 'errbar_is 1 "[42703] column \"nosuch\" does not exist ×" && [[ $(errbar 1 | grep -c 42703) == 1 ]]'
where "status = 'done'"

# ---- 出错之后 ORDER / LIMIT / PAGE 退回画着的那一份（§7.6，reviewer 在 F3.20 实测：chip 显示 id ↑，点箭头发出去的却是失败的那列）
key go; e2e_type raw; sleep 0.3; key Enter; sleep 1
check "按 json 列 raw 排序：错误栏 [42883]，chip 仍是 id ↑" eval 'errbar_is 1 "[42883] could not identify an ordering operator for type json ×" && qb_has "ORDER id $ASC"'
key go
check "ORDER 下拉框的当前项是「默认」（画着的数据按它排），不是 raw" eval '[[ $(dd_sel) == 默认 ]] || { echo "  selected: $(dd_sel)"; false; }'
key Escape
mark; e2e_click "$(e2e_find "$ASC" 3 | cut -d' ' -f1)" 3; wait_for 8 settled; sleep 0.3
check "点方向图标：发出 order by \"id\" desc（按画着的 id ↑ 翻转），成功后错误栏消失" eval 'sent_has "order by \"id\" desc limit 101 offset 0" && qb_has "ORDER id $DESC" && [[ -z $(errbar 1) ]]'
e2e_click "$(e2e_find "$DESC" 3 | cut -d' ' -f1)" 3; wait_for 8 settled
psql_n "alter table t_order rename to t_order_x" >/dev/null
key ']'; sleep 1
check "表被改名后按 ]：错误栏 [42P01]，PAGE 仍是 1/15，表格还是第 1 页" eval 'errbar_is 1 "[42P01] relation \"public.t_order\" does not exist ×" && qb_has "PAGE 1/15" && [[ $(rowno $(row_y 1)) == 1 ]]'
key gl; key C-n; key Enter; sleep 1
check "换 LIMIT 500 失败：chip 仍是 LIMIT 100" eval 'qb_has "LIMIT 100" && qb_has "PAGE 1/15" && [[ -n $(errbar 1) ]]'
psql_n "alter table t_order_x rename to t_order" >/dev/null
key ']'; wait_for 8 settled; sleep 0.3
check "改回表名再按 ]：到第 2 页（不是第 3 页），错误栏消失" eval 'qb_has "PAGE 2/15" && [[ $(rowno $(row_y 1)) == 101 && -z $(errbar 1) ]]'
key '['; wait_for 8 settled

# ---- 打了次数之后点 chip 开下拉框、开面板：esc 照样能关（reviewer 在 F3.20 实测）
key 3; e2e_click $(( $(e2e_find LIMIT 3 | cut -d' ' -f1) + 1 )) 3; sleep 0.3
check "grid 里打了 3 再点 LIMIT chip：打开下拉框" eval '[[ -n $(dd) ]]'
key Escape
check "esc：关掉下拉框，回到 NORMAL" eval '[[ -z $(dd) ]] && mode_is NORMAL'
key 3; key C-p
check "打了 3 再按 C-p：打开面板" palette_open
key Escape
check "esc：关掉面板" eval '! palette_open && mode_is NORMAL'

# ---- 保存失败进错误栏；先取消保存再失败，查询条右侧的「已取消，已回滚」清掉，只剩错误栏（reviewer 在 F3.20 实测）
where ""
set_col 1 1 999                                                      # user_id = 999：过得了前置校验，外键报 23503
psql "$E2E_DB" -q -c "begin" -c "select 1 from t_order where id = 1 for update" -c "select pg_sleep(60)" -c "commit" >/dev/null 2>&1 & LOCKER=$!
wait_for 5 eval '[[ $(psql_n "select count(*) from pg_locks l join pg_class c on c.oid = l.relation where c.relname = '"'t_order'"' and l.mode = '"'RowShareLock'"' and l.granted") -ge 1 ]]'
key C-s; sleep 0.8; key C-c; wait_for 5 eval '[[ $(result) == *已回滚* ]]'
check "保存中 C-c：「已取消，已回滚」仍在查询条右侧，不进错误栏" eval '[[ $(result) == "已取消，已回滚" && -z $(errbar 1) ]] || { echo "  $(result)"; false; }'
psql_n "select pg_terminate_backend(pid) from pg_stat_activity where datname = current_database() and query like '%pg_sleep(60)%' and pid <> pg_backend_pid()" >/dev/null; wait $LOCKER 2>/dev/null
key C-s; wait_for 8 eval '[[ -n $(errbar 1) ]]'
check "再保存、外键报错：错误栏「[23503] id = 1：…，已回滚」，查询条右侧的旧提示清掉了" eval '[[ $(errbar 1 | head -1) == "[23503] id = 1：insert or update on table"*"，已回滚 ×" && $(result) != *已回滚* ]] || { echo "  $(result)"; errbar 1; false; }'
key ']'; wait_for 8 settled
check "取数成功只清取数的错误：翻到第 2 页，保存的报错还在" eval 'qb_has "PAGE 2/60" && [[ $(errbar 1 | head -1) == "[23503] id = 1："* ]]'
key '['; wait_for 8 settled
set_col 1 1 3; key C-s; wait_for 8 eval '[[ $(result) == 已保存* ]]'
check "改成存在的 user 3 再保存：成功，错误栏消失，库里写进去" eval '[[ $(result) == "已保存 1 行"* && -z $(errbar 1) && $(psql_n "select user_id from t_order where id = 1") == 3 ]]'

# ---- console（默认布局，② console_1 在 [105,1] 56x26，文字从 x 111 起）：报错带 SQLSTATE 和换算成文件里的位置
paste() { t set-buffer -b e2e -- "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; }   # console 的内容换成 SQL，光标回到第 1 行
run() { key Enter; wait_for 8 eval '[[ $(bar) != *busy* ]]'; sleep 0.3; }
SOLO= start -C "$D/own"; key C-l
setsql $'select 1;\n\nselect id,\n  nosuch_a from t_order\nwhere id < 3;'
key G; run
check "console 执行出错：② 底部的错误栏 [42703]，下一行「位置：第 4 行第 3 列」（换算成文件里的行列）" eval 'errbar_is 2 "[42703] column \"nosuch_a\" does not exist ×" && [[ $(errbar 2 | sed -n 2p) == "位置：第 4 行第 3 列" ]] && style_has 107 $(errbar_y 2) fg=$ERROR || { errbar 2; false; }'
setsql 'select 1; select nosuch_b;'; key '$'; run
check "同一行的第二条语句：列号从行首算（第 1 行第 18 列）" eval '[[ $(errbar 2 | sed -n 2p) == "位置：第 1 行第 18 列" ]] || { errbar 2; false; }'
setsql $'-- 注释\nselect  \'中文\', nosuch_c'; key j; run
check "前面有注释行、同一行有中文：第 2 行第 15 列（按字符数，同 PG 的 Position）" eval 'errbar_is 2 "[42703] column \"nosuch_c\" does not exist ×" && [[ $(errbar 2 | sed -n 2p) == "位置：第 2 行第 15 列" ]] || { errbar 2; false; }'
setsql 'select 1/0'; run
check "没有 Position 的错误（22012）：只有一行，没有位置" eval 'errbar_is 2 "[22012] division by zero ×" && [[ $(errbar 2 | wc -l) -eq 1 ]]'
key v; key Escape
check "VISUAL 下 esc：照旧退出 VISUAL，错误栏还在" eval 'mode_is NORMAL && [[ -n $(errbar 2) ]]'
key d; key Escape; key 3; key Escape
check "d、次数 3 等着时 esc：交给编辑器取消，错误栏还在" eval 'mode_is NORMAL && [[ -n $(errbar 2) ]]'
key Escape
check "再按 esc：关掉" eval '[[ -z $(errbar 2) ]]'
setsql 'select nosuch_d'; run; setsql 'select 1 as ok'; run
check "下一次开始执行就清掉上一次的：成功之后没有错误栏" eval '[[ -z $(errbar 2) ]]'
# 光标在最后一行时出错：错误栏让 console 变矮，光标所在行仍然可见
setsql "$(for i in $(seq 29); do echo "select $i;"; done; echo 'select nosuch_e;')"; key G; run
check "30 行、光标在第 30 行出错：错误栏上面一行就是第 30 行（视图跟着变矮）" eval '[[ $(e2e_text 106 160 $(( $(errbar_y 2) - 1 ))) == *"30 select nosuch_e"* ]] || { e2e_text 106 160 $(( $(errbar_y 2) - 1 )); false; }'

# ---- 结果区 R 重跑出错：错误显示在来源 console 的 tab 上；来源 console 关了只记日志
setsql 'select id, note from t_order where id < 3'; run
psql_n "alter table t_order rename column note to note2" >/dev/null
key C-j; key R; wait_for 8 eval '[[ $(bar) != *busy* ]]'; sleep 0.3
check "③ 上 R 重跑出错：错误栏出在来源 console ② 上（带位置），③ 没有" eval 'errbar_is 2 "[42703] column \"note\" does not exist ×" && [[ $(errbar 2 | tail -1) == "位置：第 1 行第 12 列" && -z $(errbar 3) ]] || { errbar 2; false; }'
key C-k; key Space; e2e_type x; sleep 0.5                                         # 关掉 console ②，结果区变成 ②
key C-j; key g t; key R; wait_for 8 eval '[[ $(bar) != *busy* ]]'; sleep 0.3
check "来源 console 已关：R 出错只记日志，哪里都没有错误栏" eval '[[ -z $(errbar 1) && -z $(errbar 2) && $(e2e_plain | grep -c "column \"note\" does not exist") -ge 2 ]] && running'
psql_n "alter table t_order rename column note2 to note" >/dev/null

e2e_done
