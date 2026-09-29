#!/usr/bin/env bash
# F3.8 执行与结果区（specs/m3-console/task.md F3.8；tech-design §11「执行」「结果区」「工具行」、§5 默认布局）
# 结果区标题与按钮的舍弃、日志 tab 的画法由 golden 覆盖；这里测真实 PG 上的执行、替换、固定、出错、取消、截断、DDL、导出。
# 默认布局；第一次执行后 ③ 结果区在底部全宽 [34,27] 127x18，② console 在 [105,1] 56x26（文字从 x 111 起）。自建库：要建表、改数据。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
APP=e2e-f38-$$
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$E2E_DB" "$APP" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
ERROR=#f7768e FOCUS=#9ece6a DIM=#565f89
RP=3                                                                        # 结果区的 ⟨n⟩（前面的 pane 关掉后会变）
psql_n() { psql "$E2E_DB" -At -c "$1"; }
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; }   # console 的内容换成 SQL，光标回到第 1 行
rtitle() { local g; g=($(geom $RP)); e2e_text "${g[0]}" $((g[0] + g[2] - 1)) "${g[1]}" | noicon; }
rtabs() { tabbar $RP | sed -E 's/ {3,}.*//; s/- │/ │/g; s/-$//'; }             # "1:日志 │ 2:console_1 #1*"（不看「上一个」的 -）
rtabs_are() { [[ $(rtabs) == "$1" ]] || { echo "  ③ tabs: '$(rtabs)', want '$1'"; false; }; }
rrow() { local g; g=($(geom $RP)); e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $((g[1] + $1)) | sed 's/^ //; s/ *$//'; }   # ③ 内容区第 N 行
logs() { local g y; g=($(geom $RP)); for ((y = g[1] + 1; y < g[1] + g[3] - 2; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/^ //; s/ *$//'; done | sed '/^$/d'; }
pending() { bar | grep -oE '[^ ]+ +[^ ]+ [^ ]+@.*$' | awk '{ print $1 }'; }   # 状态栏的待输入一块（空闲时是 ·）
done_run() { wait_for 8 eval '[[ $(bar) != *busy* ]]'; sleep 0.3; }
run() { key Enter; done_run; }

start -C "$D/own"
key C-l
setsql "select id, status from t_order where id <= 3 order by id"
run
check "第一次执行：底部出现全宽的结果区 ③（40% 高），①、② 在上面" eval 'geom_is 3 "34 27 127 18" && geom_is 2 "105 1 56 26" && geom_is 1 "34 1 70 26"'
check "焦点留在 console，结果区切到这次的结果 tab" eval 'focus_is 2 && rtabs_are "1:日志 │ 2:console_1 #1*"'
check "标题 ③ console_1 #1，右边 3 行 · <耗时>" eval '[[ $(rtitle) =~ ^┌─\ ③\ console_1\ \#1\ ─+\ 3\ 行\ ·\ [0-9]+ms\  ]] || { echo "  $(rtitle)"; false; }'
check "结果与 psql 一致" eval '[[ $(rrow 3) == *" 1 │ running"* && $(rrow 4) == *" 2 │ done"* && $(rrow 5) == *" 3 │ failed"* ]] || { echo "  $(rrow 3)"; false; }'

# ---- 替换与固定（§11「tab 的组织」）
run
check "再执行：替换原来的结果 tab，序号加 1" rtabs_are "1:日志 │ 2:console_1 #2*"
key C-j; key P; key C-k
run
check "P 固定之后再执行：另开一个 tab，固定的留着" rtabs_are "1:日志 │ 2:console_1 #2 │ 3:console_1 #3*"
run
check "再执行：只替换没固定的那个" rtabs_are "1:日志 │ 2:console_1 #2 │ 3:console_1 #4*"

# ---- 多条语句：每个结果集一个 tab，写语句只进日志
setsql $'select 1 as a;\nupdate t_order set note = note where id <= 2;\nselect 2 as b;'
key V; key G; run
check "选中三条执行：两个结果集各一个 tab（#5、#5·2），替换没固定的 #4" rtabs_are "1:日志 │ 2:console_1 #2 │ 3:console_1 #5* │ 4:console_1 #5·2"
check "执行后退出 VISUAL" eval '[[ $(bar) == *" NORMAL " ]]'
key C-j; key 1; key g; key t; key C-k
check "日志：每条一行，写语句记影响行数 UPDATE 2" eval 'l=$(logs); [[ $l == *"console_1  select 1 as a  1 行 · "*ms* && $l == *"console_1  update t_order set note = note where id <= 2  UPDATE 2 · "*ms* && $l == *"console_1  select 2 as b  1 行 · "* ]] || { echo "$l"; false; }'
check "日志行以时间开头 HH:MM:SS" eval 'logs | tail -1 | grep -qE "^[0-9]{2}:[0-9]{2}:[0-9]{2}  console_1  "'
setsql "update t_order set note = note where id = 1"
run
check "只有写语句：保留上一次的结果 tab，切到日志" eval 'rtabs_are "1:日志* │ 2:console_1 #2 │ 3:console_1 #5 │ 4:console_1 #5·2" && [[ $(logs | tail -1) == *"UPDATE 1 · "* ]]'

# ---- 出错：切到日志，出错语句的 ▶ 变红，前面成功的照常出 tab，后面的不执行
setsql $'select 11 as x;\nselect nosuch from t_log;\ninsert into t_log(logged_at, msg) values (now(), \'e2e\');'
key V; key G; run
check "第二条出错：切到日志，第一条照常出 tab（#7）" eval '[[ $(rtabs) == "1:日志* │ 2:console_1 #2 │ 3:console_1 #7" ]] || { echo "  ③ tabs: $(rtabs)"; false; }'
check "日志里是 error 色的 ERROR: <Message>" eval 'y=$(for i in $(seq 1 14); do [[ $(rrow $i) == *"ERROR: column \"nosuch\" does not exist"* ]] && echo $i; done | tail -1); [[ -n $y ]] && x=$(e2e_find ERROR: $((27 + y)) | cut -d" " -f1) && style_has $x $((27 + y)) fg=$ERROR'
check "出错语句（第 2 行）的 ▶ 是 error 色，别的 ▶ 照旧" eval 'style_has 106 3 fg=$ERROR && style_has 106 2 fg=$FOCUS && style_has 106 4 fg=$FOCUS'
check "第三条没有执行" eval '[[ $(psql_n "select count(*) from t_log where msg = '"'e2e'"'") == 0 ]]'
key g g; run
check "再执行（不改缓冲区）第 1 条：开始时先清掉旧的红 ▶（2c36829）" style_has 106 3 fg=$FOCUS
key j; run
check "再执行出错的第 2 条：又标红" style_has 106 3 fg=$ERROR
key G; key A; e2e_type " "; key Escape
check "缓冲区一改，红色 ▶ 就恢复" style_has 106 3 fg=$FOCUS
psql "$E2E_DB" -q -c "create table t_gone (i int)"
setsql "select * from t_gone"; run
psql "$E2E_DB" -q -c "drop table t_gone"; key A; e2e_type " "; key Escape
key C-j; key R; done_run; key C-k
check "改过文字之后重跑、出错：不崩，日志记错误，▶ 不标红（出错的已不是屏幕上这段文字）" eval 'running && [[ $(logs | tail -1) == *"ERROR: relation \"t_gone\" does not exist"* ]] && style_has 106 2 fg=$FOCUS || { logs | tail -2; false; }'
psql "$E2E_DB" -q -c "create function e2e_hint() returns int language plpgsql as \$\$ begin raise exception 'bad' using detail = 'the detail', hint = 'the hint'; end \$\$"
setsql "select e2e_hint()"
run
check "DETAIL / HINT 另起一行" eval 'l=$(logs); [[ $l == *"ERROR: bad"* && $l == *"the detail"* && $l == *"the hint"* ]] || { echo "$l"; false; }'

# ---- 截断：最多 max_rows（1000）行，多取一行判断还有没有
setsql "select g from generate_series(1, 1001) g"
run
check "1001 行的查询：显示 1000+ 行" eval '[[ $(rtitle) == *" 1000+ 行 · "* ]] || { echo "  $(rtitle)"; false; }'

# ---- DDL 之后重新加载 catalog（§11「执行」）
setsql "create table t_e2e (id int primary key)"
run
check "create table 之后树里出现 t_e2e" eval 'e2e_plain | cut -c1-32 | grep -q " t_e2e "'

# ---- 导出 CSV（§11「工具行」）：当前目录下的 console_1-<序号>.csv，toast 显示路径
setsql "select id, status, note from t_order where id in (9, 10) order by id"
run
seq_n=$(rtitle | grep -oE '#[0-9]+' | tr -d '#')
read x y <<<"$(g=($(geom 3)); echo $((g[0] + g[2] - 9)) "${g[1]}")"    # 5 个按钮各 4 列，导出是倒数第二个
e2e_click $x $y; sleep 0.5; key C-k                                       # 点按钮让 ③ 获得了焦点，回到 ②
CSV=$E2E_TMP/console_1-$seq_n.csv
check "点导出：写到当前目录的 console_1-$seq_n.csv，toast 显示路径" eval '[[ -f $CSV ]] && screen_has "已导出 " && screen_has "/${E2E_TMP##*/}/console_1-$seq_n.csv"'
check "CSV 内容与 psql 的 copy 一致（NULL 写空串）" eval '[[ $(cat "$CSV") == "$(psql "$E2E_DB" -c "copy (select id, status, note from t_order where id in (9, 10) order by id) to stdout with (format csv, header)")" ]] || { cat "$CSV"; false; }'

# ---- 执行中：占位每秒刷新、状态栏 busy、再按 ↵ 忽略、C-c 取消（§11、§8.3）
setsql "select pg_sleep(8)"
key Enter; sleep 1.3
secs() { rrow 1 | sed -nE 's/^执行中 · ([0-9]+)s · C-c 取消$/\1/p'; }
check "执行中：内容区第一行是 dim 色的「执行中 · Ns · C-c 取消」，状态栏 busy" eval '[[ -n $(secs) && $(bar) == *busy* ]] && x=$(e2e_find 执行中 28 | cut -d" " -f1) && style_has $x 28 fg=$DIM || { echo "  $(rrow 1)"; false; }'
s1=$(secs); sleep 1.1
check "每秒刷新：秒数在涨" eval '(( $(secs) > s1 )) || { echo "  $s1 → $(rrow 1)"; false; }'
key Enter; sleep 0.3
check "执行中再按 ↵：忽略（只有一条在跑）" eval '[[ $(psql_n "select count(*) from pg_stat_activity where application_name = '"'$APP'"' and query like '"'%pg_sleep(8)%'"' and state = '"'active'"'") == 1 ]]'
key v; key C-c
check "执行中在 VISUAL 下按 C-c：等同 esc，回到 NORMAL，查询照跑（cd4729f）" eval '[[ $(bar) == *" NORMAL " && $(bar) == *busy* ]]'
key d; key C-c
check "按了 d 在等后续按键时 C-c：只清掉待输入，查询照跑" eval '[[ $(pending) == · && $(bar) == *busy* ]]'
key C-c; wait_for 3 eval '[[ $(bar) != *busy* ]]'
check "C-c：取消，toast「查询已取消」，切到日志记「已取消」，▶ 不变红" eval 'toast_is "查询已取消" && [[ $(rtabs) == "1:日志*"* && $(logs | tail -1) == *"select pg_sleep(8)  已取消"* ]] && style_has 106 2 fg=$FOCUS'
setsql "select 42 as answer"
run
check "取消之后 Main 照常能用" eval '[[ $(rtitle) == *" 1 行 · "* && $(rrow 3) == *42* ]]'

# ---- 执行的单位（§11「执行」）：gutter 的 ▶、块选区；重跑用当时的 SQL
setsql $'select 1 as one;\nselect 2 as two;\nselect 3 as three;'
e2e_click 106 3; done_run
check "点第 2 行的 ▶：执行那一条，光标不动（还在第 1 行）" eval '[[ $(rrow 1) == *" two"* && $(e2e_flag cursor_y) == 1 ]] || { echo "  $(rrow 1)"; false; }'
key j; key l; key C-v; key j; key l; run
check "块选区（第 2、3 行各两列）：执行覆盖到的整行，两条各出一个 tab" eval '[[ $(rtabs) == *"#"*"* │ "*"#"*"·2" ]] && [[ $(rrow 1) == *" two"* ]] || { echo "  $(rtabs) / $(rrow 1)"; false; }'
setsql "select 'old' as v"
run; setsql "select 'new' as v"
key C-j; key R; done_run; key C-k
check "结果区按 R 重跑：用这组结果当时的 SQL（old），不是 console 现在的内容" eval '[[ $(rrow 3) == *old* ]] || { echo "  $(rrow 3)"; false; }'

# ---- 两个 console 共用 Main：后执行的在 Worker 的锁上排队，占位照样显示
# console_2 的语句等 t_log 上的锁（e2e_lock），Main 一直忙着，console_1 的执行只能排队；放锁后依次跑完
key C-h; key c; setsql "select count(*) as slow from t_log"
e2e_lock t_log; key Enter; wait_for 5 eval '[[ -n $(e2e_waiting $APP) ]]'
key C-l; setsql "select 'queued' as q"
key Enter; sleep 1
check "console_2 在跑（等锁）时执行 console_1：console_1 的 tab 是占位「执行中」" eval '[[ $(rrow 1) == 执行中* && $(rtabs) == *"console_1 #"*"*"* ]] || { echo "  $(rtabs) / $(rrow 1)"; false; }'
check "console_1 的语句还没发出去（服务端只有 console_2 那一条）" eval '[[ $(psql_n "select count(*) from pg_stat_activity where application_name = '"'$APP'"' and query like '"'%queued%'"'") == 0 ]]'
e2e_unlock; wait_for 10 eval '[[ $(rrow 3) == *queued* ]]'
check "放锁后 console_2 跑完，console_1 接着执行，结果回来" eval '[[ $(rrow 3) == *queued* && $(rtabs) == *"console_2 #"* ]] || { echo "  $(rtabs) / $(rrow 3)"; false; }'

# ---- 结果表格只读；x 等同 q 关闭结果 tab；关掉结果区，下次执行再出现
key C-j
check "C-j 进结果区：显示 行,列" eval 'focus_is 3 && [[ -n $(pos) ]]'
key i
check "i / ↵ 不做事（只读）" eval '[[ $(bar) == *" NORMAL " ]]'
before=$(rtabs); key x
check "x 关闭当前结果 tab" eval '[[ $(rtabs) != "$before" && $(rtabs) != *"#"*"answer"* ]]'
key Space; e2e_type x; sleep 0.3
check "SPC x 关掉结果区：①、② 占回全高" eval '[[ -z $(geom 3) ]] && geom_is 2 "105 1 56 44"'
key C-l; run
check "再执行：结果区重新出现" eval 'geom_is 3 "34 27 127 18" && [[ $(rtabs) == *"*" ]]'
setsql "select pg_sleep(2), 'late' as w"
key Enter; sleep 0.5; key C-j; key Space; e2e_type x; sleep 0.3
check "执行中关掉结果区：结果区先没了" eval '[[ -z $(geom 3) ]]'
wait_for 8 eval '[[ -n $(geom 3) ]]'; done_run
check "结果回来：结果区重新出现，只有日志和这次的结果，关掉的旧 tab 不带回来" eval '[[ $(rtabs) =~ ^1:日志\ │\ 2:console_1\ \#[0-9]+\*$ && $(rrow 3) == *late* ]] || { echo "  $(rtabs)"; false; }'

# ---- 日志 tab 关不掉；结果区在的时候，唯一的普通 pane 也关不掉（2c36829）
key C-j; key 1; key g; key t
key x; key :; e2e_type q; sleep 0.3; key Enter
check "日志 tab 上 x、:q 都关不掉它" eval '[[ $(rtabs) == "1:日志*"* && -n $(geom 3) ]]'
key C-k; key :; e2e_type q; sleep 0.3; key Enter                         # 关掉 console_1，② 随之关掉
key C-k; key Space; e2e_type x; sleep 0.3
check "只剩 ① 一个普通 pane 和结果区：SPC x 不做事" eval '[[ $(e2e_panes | awk "{ print \$1 }" | tr "\n" " ") == "0 1 2 " && $(e2e_text 34 103 1) == *"┌─ ①"* ]] && geom_is 2 "34 27 127 18" || { e2e_panes; false; }'

RP=2; key C-j; key 2; key g; key t; n0=$(rtabs | grep -oE '#[0-9]+' | tail -1 | tr -d '#')
key R; done_run; sleep 2.5
check "来源 console_1 已关：结果区按 R 照样重跑，结果进它原来那一组（序号加 1）" eval '[[ $(rtabs) =~ ^1:日志\ │\ 2:console_1\ \#$((n0 + 1))\*$ && $(rrow 3) == *late* ]] || { echo "  $(rtabs) / $(rrow 3)"; false; }'

e2e_done
