#!/usr/bin/env bash
# F1.4 查询条（specs/m1-browse/task.md F1.4；tech-design §7.8「查询条」「COMMAND 模式」、§9.6、§8.5、§8.3、§7.6）
# 全部在自建库里做：发出的 SQL 靠锁住 t_order、在 pg_stat_activity 里看等锁的那条语句；golden 由 go test 覆盖。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -c "create table t_big as select g as id from generate_series(1, 1100000) g" -c "analyze t_big" \
  -c 'create table t_zh (id int primary key, "默认" int)' -c 'insert into t_zh select g, 10 - g from generate_series(1, 5) g'
APP=e2e-f14-$$
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$E2E_DB" "$APP" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
KEYWORD=#bb9af7 DIM=#565f89 FG=#c0caf5 SEP=#2f3549 SELECT=#364a82 ROW=#292e42 ERROR=#f7768e
header() { e2e_text 35 159 "$(hy)"; }
col_x() { e2e_find "$1" "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }'; }
row_y() { echo $(( $(grid_y) + $1 )); }
rowno() { local c; c=$(e2e_find ┼ "$(grid_y)"); e2e_text 35 $((${c%% *} - 1)) "$1" | tr -d ' '; }
val() { local x; x=$(col_x "$1"); e2e_text "$x" $((x + ${3:-8})) "$(row_y "$2")" | sed 's/ .*//'; }   # NAME N [W] — 第 N 行 NAME 列的值（到第一个空格）
# nums NAME N：第 N 行 NAME 列整格的内容（两条竖线之间），去掉空格；数值右对齐也能取到
nums() { local x l r c; x=$(col_x "$1"); for c in $(e2e_find │ "$(hy)"); do ((c < x)) && l=$c; ((c > x)) && [[ -z $r ]] && r=$c; done; e2e_text $((l + 1)) $((r - 1)) "$(row_y "$2")" | tr -d ' '; }
where() { key /; clear_in; e2e_type "$1"; sleep 0.2; key Enter; wait_for 8 settled; sleep 0.2; }
psql_n() { psql "$E2E_DB" -At -c "$1"; }
# sent KEYS...：锁住 t_order 时执行 KEYS，SQL 是它发出、正在等锁的那条语句；然后放锁、等表格回来
sent() { e2e_lock t_order; "$@"; wait_for 5 eval '[[ -n $(e2e_waiting $APP) ]]'; SQL=$(e2e_waiting $APP); e2e_unlock; wait_for 8 settled; sleep 0.3; }
sql_is() { [[ $SQL == "$1" ]] || { echo "  sent: $SQL"; echo "  want: $1"; false; }; }
dd() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }'; }   # 浮层的 X Y W H
dd_rows() { local g y; g=($(dd)); for ((y = g[1] + 3; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | sed 's/ *$//'; done; }
dd_sel() { local g y; g=($(dd)); for ((y = g[1] + 3; y < g[1] + g[3] - 1; y++)); do style_has $((g[0] + 3)) $y bg=$SELECT >/dev/null && e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | sed 's/ *$//'; done; }
chip_x() { e2e_find "$1" 3 | cut -d' ' -f1; }

start -C "$D/own"; open_table t_order

# ---- 布局（§7.8「查询条」）
check "第一行：keyword 色的 WHERE，输入框为空" eval 'text_is 36 40 2 WHERE && style_has 36 2 fg=$KEYWORD && [[ -z $(where_in) ]]'
check "第二行：ORDER id ↑ / LIMIT 100 / PAGE 1/60 / COLS 10/10，右边 auto · 6000 行 · <耗时>" eval '[[ $(qb) =~ ^\ \ ORDER\ id\ $ASC\ \ \ LIMIT\ 100\ \ \ PAGE\ 1/60\ \ \ COLS\ 10/10\ .*auto\ ·\ 6000\ 行\ ·\ [0-9]+ms\ $ ]] || { echo "  $(qb)"; false; }'
check "chip：标签 dim、值 fg、sep 底" eval 'x=$(chip_x ORDER); style_has $x 3 fg=$DIM && style_has $x 3 bg=$SEP && style_has $((x + 6)) 3 fg=$FG && style_has $((x - 1)) 3 bg=$SEP'
check "三个图标按钮：保存 U+F0C7、刷新 U+F021、转置 U+F0EC" eval 'q=$(qb); [[ $q == *"$(printf "\xef\x83\x87")"* && $q == *"$(printf "\xef\x80\xa1")"* && $q == *"$(printf "\xef\x83\xac")"* ]]'
check "查询条和表格之间没有分隔线：表头在第 4 行" eval '[[ $(grid_y) == 5 ]]'
e2e_move $(( $(chip_x LIMIT) + 1 )) 3; sleep 0.3
check "悬停 LIMIT chip：select 底" style_has $(chip_x LIMIT) 3 bg=$SELECT
e2e_move 100 30; sleep 0.2

# ---- WHERE（§9.6、§7.8）
key /
check "/：INSERT，状态栏显示 -- editing WHERE --，终端光标在输入框开头" eval 'mode_is INSERT && [[ $(bar) == *"-- editing WHERE --"* ]] && flag_is cursor_flag 1 && [[ $(e2e_flag cursor_x) == 41 && $(e2e_flag cursor_y) == 1 ]]'
e2e_type "status = 'done'"; sleep 0.2; key Enter; wait_for 8 settled
check "↵：按条件过滤，回到 NORMAL、焦点在表格；计数与数据库一致，PAGE 1/15" eval 'mode_is NORMAL && pos_is 1,1 && [[ $(cnt) == $(psql_n "select count(*) from t_order where status = '"'done'"'") ]] && qb_has "PAGE 1/15" && [[ $(for n in 1 2 3 20; do val status $n; done | sort -u) == done ]]'
key ']'; qb_has "PAGE 2/15" >/dev/null
where "status = 'done' and id < 400"
check "换条件后回到第 1 页，重新计数（100 行，PAGE 1/1）" eval 'qb_has "PAGE 1/1 " && [[ $(cnt) == 100 ]]'
key /; e2e_type " and false"; sleep 0.2; key Escape; key Escape   # 第一次 esc 先关 F1.5 的补全列表（false 是关键字）
check "esc：回到表格，输入框恢复成当前生效的条件，数据不变" eval 'mode_is NORMAL && [[ $(where_in) == "status = '"'done'"' and id < 400" && $(cnt) == 100 ]]'
e2e_click 100 2; sleep 0.3
check "点击 WHERE 那一行：开始输入" eval 'mode_is INSERT && [[ $(bar) == *"-- editing WHERE --"* ]]'
key Escape
where "stauts = 1"
check "写错的条件：内容区第一行是 error 色的数据库错误，不画表格，不显示行,列" eval 'l=$(e2e_text 35 159 4); [[ $l == *"stauts"*"does not exist"* && -z $(grid_y) && -z $(pos) ]] && style_has 36 4 fg=$ERROR || { echo "  row 4: $l"; false; }'
key /
check "报错后查询条仍可编辑" eval 'mode_is INSERT && [[ $(where_in) == "stauts = 1" ]]'
key Escape
where "status = 'done' -- 备注"
check "末尾带 -- 注释：正常执行，仍按 id 排，第 1 行 id 2" eval '[[ $(cnt) == 1500 && $(nums id 1) == 2 ]] && qb_has "ORDER id $ASC" && qb_has "PAGE 1/15"'
key ']'; wait_for 8 settled
check "翻到第 2 页：行号从 101 起，id 402（第 101 个 done）" eval '[[ $(rowno $(row_y 1)) == 101 && $(nums id 1) == 402 ]] && qb_has "PAGE 2/15"'
where ""

# 单行输入框（F0.13 的组件）：按字素簇退格、光标可见、超宽时正在输入的位置可见
key /; clear_in
e2e_type "$(printf 'e\xcc\x81')"; sleep 0.2; e2e_keys BSpace; sleep 0.2
ok1=$([[ -z $(where_in) ]] && echo 1)
e2e_type "$(printf '\xf0\x9f\x91\x8d\xf0\x9f\x8f\xbd')"; sleep 0.2; e2e_keys BSpace; sleep 0.2
check "é（e+U+0301）、👍🏽 各按一次退格：整个字删掉" eval '[[ $ok1 == 1 && -z $(where_in) ]] && flag_is cursor_flag 1'
e2e_type "$(printf 'x%.0s' $(seq 150))END"; sleep 0.3
end_shown() { [[ $(e2e_text 42 159 2) == *END* ]] && c=$(e2e_find END 2) && [[ $(e2e_flag cursor_x) == $((${c%% *} + 2)) ]] && (( $(e2e_flag cursor_x) < 159 )); }
check "输入超出宽度：结尾 END 可见，光标在它后面、仍在 pane 里" wait_for 3 end_shown   # 负载高时 150 个字要一会儿才画完
key Escape

# ---- ORDER（go）：通用下拉框，第一项「默认」；同列 ↵ 翻转；行标识列作 tiebreaker；换了回第 1 页
key go
check "go：下拉框在 ORDER chip 下方，状态栏 COMMAND，第一项是「默认」，列出全部 10 列" eval 'g=($(dd)); [[ ${g[0]} == $(( $(chip_x ORDER) - 1 )) && ${g[1]} == 4 ]] && mode_is COMMAND && [[ $(dd_rows | head -1) == 默认 ]] || { echo "  dropdown [$(dd)] rows: $(dd_rows | tr "\n" ,)"; false; }'
key C-n; s1=$(dd_sel); key Down; s2=$(dd_sel); key C-p; s3=$(dd_sel); key Up; s4=$(dd_sel)
check "C-n / ↓ / C-p / ↑ 移动选中项" eval '[[ "$s1 $s2 $s3 $s4" == "id user_id id 默认" ]] || { echo "  $s1 $s2 $s3 $s4"; false; }'
key Escape
check "esc 关闭，排序不变" eval '[[ -z $(dd) ]] && qb_has "ORDER id $ASC" && mode_is NORMAL'
key ']'; wait_for 8 settled
sent eval 'key go; e2e_type status; sleep 0.3; e2e_keys Enter'
check "选 status：chip 为 status ↑，回到第 1 页，SQL 以 id 作 tiebreaker" eval 'qb_has "ORDER status $ASC" && qb_has "PAGE 1/60" && sql_is "select * from \"public\".\"t_order\" order by \"status\" asc, \"id\" limit 101 offset 0"'
sent eval 'key go; e2e_type status; sleep 0.3; e2e_keys Enter'
check "在当前排序列上再 ↵：翻转为 status ↓（order by \"status\" desc, \"id\"）" eval 'qb_has "ORDER status $DESC" && sql_is "select * from \"public\".\"t_order\" order by \"status\" desc, \"id\" limit 101 offset 0"'
check "status ↓：第一行是 failed（枚举的最后一个值），同值按 id 升序（3, 7）" eval '[[ $(val status 1) == failed && $(nums id 1) == 3 && $(nums id 2) == 7 ]]'
sent eval 'key go; e2e_type 默认; sleep 0.3; e2e_keys Enter'
check "选「默认」：回到按行标识列排（id ↑）" eval 'qb_has "ORDER id $ASC" && sql_is "select * from \"public\".\"t_order\" order by \"id\" limit 101 offset 0"'
key go
check "下拉框打开时选中的是当前排序列（默认排序时是「默认」）" eval '[[ $(dd_sel) == 默认 ]]'
key Escape
sent eval 'key go; e2e_type id; sleep 0.3; e2e_keys Enter'
check "默认排序时 chip 上的 id 就是当前排序列：选 id 直接翻转为 id ↓，SQL 里 id 不重复，第一行 id 6000" eval 'qb_has "ORDER id $DESC" && sql_is "select * from \"public\".\"t_order\" order by \"id\" desc limit 101 offset 0" && [[ $(nums id 1) == 6000 ]]'
e2e_click $(( $(chip_x ORDER) + 1 )) 3; sleep 0.3
check "点击 ORDER chip：打开同一个下拉框" eval '[[ $(dd_rows | head -1) == 默认 ]] && mode_is COMMAND'
e2e_type 默认; sleep 0.3; key Enter; wait_for 8 settled

# ---- LIMIT（gl）：100 / 500 / 1000，换了回第 1 页
key ']'; wait_for 8 settled
key gl
check "gl：下拉框列出 100 / 500 / 1000" eval '[[ $(dd_rows | tr "\n" " ") == "100 500 1000 " ]] || { echo "  $(dd_rows | tr "\n" ,)"; false; }'
sent eval 'e2e_keys C-n; sleep 0.2; e2e_keys Enter'
check "选 500：chip LIMIT 500、PAGE 1/12，SQL limit 501" eval 'qb_has "LIMIT 500" && qb_has "PAGE 1/12" && sql_is "select * from \"public\".\"t_order\" order by \"id\" limit 501 offset 0"'
key G; check "G：这一页最后一行是第 500 行" pos_is 500,1
e2e_click $(( $(chip_x LIMIT) + 1 )) 3; sleep 0.3; key C-p; key Enter; wait_for 8 settled; key g g
check "点击 LIMIT chip 打开下拉框，选回 100" eval 'qb_has "LIMIT 100" && qb_has "PAGE 1/60"'

# ---- PAGE：] / [，边界不起作用；行号接着上一页；gp 原地变输入框
key '['; check "第 1 页按 [：不起作用" eval 'qb_has "PAGE 1/60" && [[ $(rowno $(row_y 1)) == 1 ]]'
key 5 j; key 2 l
sent eval 'e2e_keys "]"'
check "]：第 2 页，行号从 101 起，光标的行、列保留（106,3），SQL offset 100" eval 'qb_has "PAGE 2/60" && [[ $(rowno $(row_y 1)) == 101 ]] && pos_is 106,3 && sql_is "select * from \"public\".\"t_order\" order by \"id\" limit 101 offset 100"'
key gp
check "gp：chip 原地变成 PAGE [2]/60 输入框，INSERT" eval 'qb_has "PAGE [2]/60" && mode_is INSERT'
key BSpace; e2e_type 7; key Enter; wait_for 8 settled
check "输入 7 ↵：跳到第 7 页（行号 601 起）" eval 'qb_has "PAGE 7/60" && [[ $(rowno $(row_y 1)) == 601 ]]'
key gp; key BSpace; e2e_type 999; key Enter; wait_for 8 settled
check "输入 999：夹到最后一页 60/60（行号 5901 起）" eval 'qb_has "PAGE 60/60" && [[ $(rowno $(row_y 1)) == 5901 ]]'
key ']'; check "最后一页按 ]：不起作用" eval 'qb_has "PAGE 60/60" && [[ $(rowno $(row_y 1)) == 5901 ]]'
key gp; key BSpace; e2e_type 3; key Escape
check "gp 后 esc：放弃，仍是 60/60" eval 'qb_has "PAGE 60/60" && mode_is NORMAL'
e2e_click $(( $(chip_x PAGE) + 1 )) 3; sleep 0.3
check "点击 PAGE chip：同 gp" eval 'qb_has "PAGE [60]/60" && mode_is INSERT'
key BSpace; key BSpace; e2e_type 1; key Enter; wait_for 8 settled; key g g; key 0

# ---- COLS（gc）：焦点在列表；j/k、space、a/A；/ 进过滤框；esc 两步；隐藏列按 tab 记住
key 2 l                                          # 光标在 status（第 3 列）
key gc
check "gc：列表打开，COMMAND，右上角 10/10，每行 [x] 列名 … 类型，底部 a 全选 · A 全不选" eval 'mode_is COMMAND && g=($(dd)) && [[ $(e2e_text ${g[0]} $((g[0] + g[2] - 1)) $((g[1] + 1))) == *"10/10 │" && $(dd_rows | head -1) == "[x] id"*bigint* && $(dd_rows | grep -c "^\[x\]") == 10 && $(dd_rows | tail -1) == "a 全选 · A 全不选" ]] || { echo "  $(dd_rows | tr "\n" ,)"; false; }'
key j; key j; key Space
check "j j space：取消 status，表格里 status 列消失，chip COLS 9/10" eval '[[ $(dd_rows | sed -n 3p) == "[ ] status"* && $(header) != *" status "* ]] && qb_has "COLS 9/10"'
cur_col() { local x y=$(row_y 1); for ((x = 35; x < 160; x++)); do style_has $x $y bg=#3d59a1 >/dev/null && { e2e_text $x $((x + 12)) "$(hy)" | awk '{ print $1 }'; return; }; done; }   # 光标格所在列的表头
check "隐藏的是光标所在列：光标挪到相邻的可见列" eval 'c=$(cur_col); [[ $c == user_id || $c == amount ]] || { echo "  cursor on \"$c\", pos $(pos)"; false; }'
key /; e2e_type at; sleep 0.3
check "/at：过滤框（仍是 COMMAND），只剩 status / amount / created_at / deleted_at，4/10" eval 'mode_is COMMAND && [[ $(dd_rows | grep "^\[" | sed "s/^\[.\] //; s/ .*//" | tr "\n" " ") == "status amount created_at deleted_at " ]] && g=($(dd)) && [[ $(e2e_text ${g[0]} $((g[0] + g[2] - 1)) $((g[1] + 1))) == *"4/10 │" ]]'
key Enter; key A
check "↵ 回列表（过滤保留），A：过滤出来的 4 列全不选，COLS 6/10" eval '[[ $(dd_rows | grep -c "^\[ \]") == 4 ]] && qb_has "COLS 6/10" && [[ $(header) != *created_at* ]]'
key a
check "a：这 4 列全选，COLS 10/10" eval 'qb_has "COLS 10/10" && [[ $(header) == *" status "* ]]'
key Escape
check "esc 第一次：清空过滤，列表回到 10 行，浮层还在" eval '[[ -n $(dd) && $(dd_rows | grep -c "^\[") == 10 ]]'
key Escape
check "esc 第二次：关闭" eval '[[ -z $(dd) ]] && mode_is NORMAL'
key gc; g=($(dd)); e2e_click $((g[0] + 6)) $((g[1] + 3 + 4)); sleep 0.3   # 第 5 行：paid
check "点击一行：切换这一列（paid），COLS 9/10" eval '[[ $(dd_rows | sed -n 5p) == "[ ] paid"* ]] && qb_has "COLS 9/10"'
e2e_click $((g[0] + 3)) $((g[1] + g[3] - 2)); sleep 0.3
check "点击底部的 a 全选：10/10" qb_has "COLS 10/10"
key /; e2e_type paid; key Enter; key Space; key Escape; key Escape   # 隐藏 paid
key C-p; e2e_type "@t_user"; sleep 0.3; key C-t; wait_for 8 settled
check "新 tab 的 t_user 有自己的 COLS 5/5" qb_has "COLS 5/5"
e2e_type ':q'; key Enter; wait_for 5 settled
check "回到 t_order：paid 仍隐藏（COLS 9/10）" eval 'qb_has "COLS 9/10" && [[ $(header) != *" paid "* ]]'
key gc; key a; key Escape

# ---- 计数（§8.5、§8.3）：取数之后才计数，先 …；3 秒超时 ?；大表不带 WHERE 用估计值 ~n；不算 busy
key /; clear_in; e2e_type "pg_sleep(0.001) is not null"; key Enter
check "计数中：先显示 …，状态栏没有 busy" eval 'wait_for 3 eval "[[ \$(cnt) == … ]]" && [[ $(bar) != *busy* && -n $(grid_y) ]]'
check "3 秒超时：显示 ?，PAGE 1/?" eval 'wait_for 6 eval "[[ \$(cnt) == \"?\" ]]" && qb_has "PAGE 1/?"'
where ""
key /; clear_in; e2e_type "pg_sleep(0.001) is not null and id < 500"; sleep 0.2; e2e_keys Enter; sleep 0.05; e2e_keys ']'
check "WHERE ↵ 后马上按 ]：新条件照样计数（499 行），停在第 2 页" eval 'wait_for 8 eval "[[ \$(cnt) == 499 ]]" && qb_has "PAGE 2/5" || { echo "  $(qb)"; false; }'
where ""
open_table t_big
check "t_big（估计 110 万行，没有 WHERE）：显示 ~1.1m 行、PAGE 1/~" eval 'wait_for 3 eval "[[ \$(cnt) == ~1.1m ]]" && qb_has "PAGE 1/~" || { echo "  $(qb)"; false; }'
check "用估计值时不发计数：Meta 最后一条是取数" eval '[[ $(psql_n "select query from pg_stat_activity where application_name = '"'$APP'"' order by query_start desc limit 1") == "select * from \"public\".\"t_big\""* ]]'
where "id < 10"
check "带 WHERE 时照常精确计数（9 行）" eval '[[ $(cnt) == 9 ]]'
open_table t_order

# ---- 悬停行、点击行号（§7.6，G-06 / G-04）
key 3 l
e2e_move 100 $(row_y 5); sleep 0.3
check "悬停第 5 行：row 底，光标不动" eval 'style_has 100 $(row_y 5) bg=$ROW && pos_is 1,4'
e2e_click 37 $(row_y 5); sleep 0.3
check "点击第 5 行的行号：光标移到第 5 行，列不变" pos_is 5,4
e2e_move 100 30; key T
c=$(e2e_find " 3 " $(hy)); e2e_click $((${c%% *} + 1)) $(hy); sleep 0.3
check "转置后点击表头上的记录号 3：光标移到第 3 条记录，字段不变" pos_is 3,4
key T; key g g; key 0

# ---- 三个按钮：保存只占位；刷新 = R；转置 = T（§7.8）
bx() { local q c; q=$(qb); c=$(e2e_find "$(printf "$1")" 3); echo "${c%% *}"; }
before=$(e2e_plain | md5)
e2e_click $(bx '\xef\x83\x87') 3; sleep 0.5
check "点击保存：什么都不做" eval '[[ $(e2e_plain | md5) == "$before" ]]'
sent eval 'e2e_click $(bx "\xef\x80\xa1") 3'
check "点击刷新：按当前条件重取当前页" sql_is "select * from \"public\".\"t_order\" order by \"id\" limit 101 offset 0"
e2e_click $(bx '\xef\x83\xac') 3; sleep 0.3
check "点击转置：同 T（表头变成记录号）" eval '[[ $(header) == *"│ 1 "*"│ 2 "* ]]'
key T

# ---- 重新取数时保留上一次的数据（§7.6「取数中」，F1.3 留下的）：R、翻页
e2e_lock t_order
key R
check "R 取数中：busy，表格仍是原来的数据" eval '[[ $(bar) == *busy* && $(nums id 1) == 1 ]]'
key C-c
check "取消后数据还在" eval 'toast_is "查询已取消" && [[ $(nums id 1) == 1 && $(bar) != *busy* ]]'
check "取消的 R 不会让计数一直停在 …" eval 'sleep 1; [[ $(cnt) != … ]] || { echo "  $(qb)"; false; }'
key ']'
check "] 取数中：表格仍是第 1 页的数据，行号和 PAGE 也还是第 1 页（行号 1 对应 id 1）" eval '[[ $(bar) == *busy* && $(rowno $(row_y 1)) == 1 && $(nums id 1) == 1 ]] && qb_has "PAGE 1/" || { echo "  $(qb) | $(pos) | 行号 $(rowno $(row_y 1)) id $(nums id 1)"; false; }'
key C-c
check "取消翻页：仍是第 1 页（PAGE 1/…、行号 1、状态栏 1,1）" eval 'qb_has "PAGE 1/" && [[ $(rowno $(row_y 1)) == 1 ]] && pos_is 1,1 || { echo "  $(qb) | 行号 $(rowno $(row_y 1)) id $(nums id 1)"; false; }'
key gl; key C-n; key Enter
check "换 LIMIT 取数中：chip 仍是 LIMIT 100" eval '[[ $(bar) == *busy* ]] && qb_has "LIMIT 100"'
key C-c
check "取消后 LIMIT 仍是 100" eval 'qb_has "LIMIT 100" && qb_has "PAGE 1/"'
key /; e2e_type "id < 10"; key Enter
check "改 WHERE 取数中：数据还是原来的" eval '[[ $(bar) == *busy* && $(nums id 1) == 1 ]]'
key C-c
check "取消后 WHERE 输入框恢复成原来生效的条件（空）" eval '[[ -z $(where_in) && $(nums id 1) == 1 ]] && mode_is NORMAL'
e2e_unlock; sleep 0.5
key ']'; wait_for 8 settled
check "放锁后再按 ]：到第 2 页（不跳页）" eval 'qb_has "PAGE 2/60" && [[ $(rowno $(row_y 1)) == 101 && $(nums id 1) == 101 ]]'
key '['; wait_for 8 settled

# ---- 主题里的 [icon] 覆盖对查询条按钮生效；有一列叫「默认」时 ORDER 能按它排
mkdir -p "$D/own/themes"; printf 'theme = "x"\n' >"$D/own/config.toml"; printf '[icon]\nrefresh = { fg = "#ff0000" }\n' >"$D/own/themes/x.toml"
start -C "$D/own"; open_table t_zh
check "[icon] refresh = { fg = \"#ff0000\" }：查询条的刷新按钮变色" eval 'x=$(bx "\xef\x80\xa1"); [[ -n $x ]] && style_has $x 3 fg=#ff0000'
key go; e2e_type 默认; sleep 0.3
check "t_zh 的 ORDER 列表里有两项「默认」：第一项是默认排序，第二项是这一列" eval '[[ $(dd_rows | tr "\n" " ") == "默认 默认 " ]] || { echo "  $(dd_rows | tr "\n" ,)"; false; }'
key C-n; key Enter; wait_for 8 settled
check "选第二项：按「默认」列排（第 1 行 id 5），chip 为 默认 ↑" eval 'qb_has "ORDER 默认 $ASC" && [[ $(nums id 1) == 5 ]]'

e2e_done
