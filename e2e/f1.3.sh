#!/usr/bin/env bash
# F1.3 data pane 表格（只读）（specs/m1-browse/task.md F1.3；tech-design §7.6、§7.8 状态栏、§8.3、§8.5、§10.1）
# 网格样式见 f0.9.sh，按类型着色见 f0.12.sh，表格滚轮见 f0.8.sh；golden 由 go test 覆盖。
# 锁表、删表、空表在自建库 sqlmux_e2e_<pid> 里做（AGENTS.md「集成测试环境」）。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
FOCUS=#9ece6a DIM=#565f89 WARN=#e0af68 ERROR=#f7768e FUNC=#7aa2f7 NUMBER=#ff9e64
header() { e2e_text 35 159 "$(hy)"; }
col_x() { e2e_find "$1" "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }'; }   # ① 的表头里 NAME 的起始列（侧栏里可能也有这几个字）
row_y() { echo $(( $(grid_y) + $1 )); }                                 # 可见的第 N 行数据
rowno() { local c; c=$(e2e_find ┼ "$(grid_y)"); e2e_text 35 $((${c%% *} - 1)) "$1" | tr -d ' '; }   # Y 行的行号
last_y() { echo $(( $(H) - 3 )); }                                      # 最后一行数据（tab 栏上一行）
tree_open() { local y; key C-h; for ((y = 4; y < $(H) - 4; y++)); do [[ $(e2e_text 5 31 $y) == "$1 "* ]] && { e2e_click 10 $y; sleep 0.5; return; }; done; echo "  $1 not in the tree"; }

# ---- 第一页（§8.5、§10.1）：按行标识列排序，100 行，多取的一行不显示
start; open_table t_order
check "打开 t_order：第 1 行是 id 1，状态栏 1,1" eval '[[ $(rowno $(row_y 1)) == 1 && $(e2e_text 41 46 $(row_y 1)) == "    1 " ]] && pos_is 1,1'
key G
check "G：最后一行是第 100 行（id 100），再 j 不动（第 101 行只用来判断还有下一页）" eval 'pos_is 100,1 && [[ $(e2e_text 41 46 $(last_y)) == "  100 " ]] && { key j; pos_is 100,1; }'
check "当前行的行号为 focus 色，其他行不是" eval 'style_has 38 $(last_y) fg=$FOCUS && ! style_has 38 $(( $(last_y) - 1 )) fg=$FOCUS >/dev/null'
key g g

# ---- 移动（§7.6）：hjkl、gg、G、0、$，次数前缀；状态栏的行,列跟着光标
key 3 j; key 2 l; check "3j 2l → 4,3" pos_is 4,3
key 0;           check "0 → 4,1" pos_is 4,1
key 5 l; key h;  check "5l h → 4,5" pos_is 4,5
key G;           check "G → 100,5" pos_is 100,5
key 5 k;         check "5k → 95,5" pos_is 95,5
key g g;         check "gg → 1,5" pos_is 1,5
key 0

# ---- 横向（§7.6「视图跟随光标」）：光标出了视图，视图滚到光标所在的列完整可见；往回移动时视图不跳回
check "准备：160 宽时最后一列 deleted_at 只露出一截" eval '[[ $(header) == *"│ deleted_" && $(header) != *"deleted_at"* ]] || { echo "  $(header)"; false; }'
key '$'
check "\$：到最后一列（1,10），视图横向滚动，deleted_at 完整可见，id 滚出视图" eval 'pos_is 1,10 && [[ $(header) == *"deleted_at"* && $(header) != *" id "* ]] || { echo "  $(header)"; false; }'
h0=$(header); key h
check "h 回到 created_at：视图不跳回" eval 'pos_is 1,9 && [[ $(header) == "$h0" ]]'
key 0
check "0：回到第一列，id 又可见" eval 'pos_is 1,1 && [[ $(header) == *" id "* ]]'
key 9 l
check "9l：同样滚到 deleted_at 完整可见" eval 'pos_is 1,10 && [[ $(header) == *"deleted_at"* ]]'
key 0

# ---- 转置 T（G-05、§7.6）：光标不变；表头是记录的行号，第一列是字段名；j/k 字段、h/l 记录；列宽不收缩
key 3 j; key 5 l; key T
check "T：光标仍是 4,6，状态栏不变" pos_is 4,6
check "转置后表头是记录的行号，当前记录（4）为 focus 色" eval 'h=$(header); [[ $h == *"│ 1 "*"│ 4 "* ]] && c=$(e2e_find " 4 " $(hy) | tr " " "\n" | awk "\$1 > 34 { print; exit }") && style_has $((${c%% *} + 1)) $(hy) fg=$FOCUS'
check "第一列是字段名（func 色），主键 id 带钥匙图标" eval '[[ $(e2e_text 36 50 $(row_y 1)) == "$(printf "\xef\x82\x84") id"* && $(e2e_text 35 50 $(row_y 5)) == *" paid "* ]] && style_has 38 $(row_y 1) fg=$FUNC'
key j; check "j：下一个字段 → 4,7" pos_is 4,7
key l; check "l：下一条记录 → 5,7" pos_is 5,7
rec_widths() { local c p w=; for c in $(e2e_find ┼ "$(grid_y)"); do [[ -n $p ]] && w+="$((c - p)) "; p=$c; done; echo "$w"; }
check "列宽不按比例收缩：每条记录一列，取期望宽度（时间值 22 列 + 两侧留白 + 竖线），放不下就横向滚动" eval 'w=$(rec_widths); [[ $w == "25 25 25 "* && $(tr " " "\n" <<<"$w" | sort -u | grep -c .) == 1 ]] || { echo "  record column widths: $w"; false; }'
key 5 0 l
check "50l：横向滚动到第 55 条记录，它的行号在表头里可见" eval 'pos_is 55,7 && [[ $(header) == *"│ 55 "* && $(header) != *"│ 1 "* ]]'
key T
check "再按 T：切回，光标仍是 55,7" eval 'pos_is 55,7 && [[ $(header) == *" id "* || $(header) == *"note"* ]]'
key g g; key 0

# ---- [map.grid.normal]（§6.6）
printf '[map.grid.normal]\nL = "5l"\n' >"$D/config.toml"; start -c "$D/config.toml"; open_table t_order
key L; check '[map.grid.normal] L = "5l"：L 右移 5 列 → 1,6' pos_is 1,6

# ---- 控制字符（§7.6）：换行 → dim 的 ↵，Tab → 空格，ESC 等去掉；替换后再截断（列宽上限 40）
start; open_table t_log
y=$(row_y 2); x=$(col_x msg)
check "t_log 第 2 行：line one↵line two after tab [31mred[0m …（ESC 去掉，40 列处截断）" text_is $x $((x + 39)) $y "line one↵line two after tab [31mred[0m …"
check "↵ 为 dim 色，前后的文字不是" eval 'style_has $((x + 8)) $y fg=$DIM && ! style_has $((x + 7)) $y fg=$DIM >/dev/null'
check "屏幕没有错乱：每行 160 列，这一行的右边框在第 160 列" eval '[[ $(e2e_widths | sed "/^0$/d" | sort -u) == 160 && $(e2e_text 160 160 $y) == "│" ]]'
check "NULL 显示为 dim 色的 <null>（第 3 行）" eval 'text_is $x $((x + 5)) $(row_y 3) "<null>" && style_has $x $(row_y 3) fg=$DIM'

# ---- 滚轮（§7.4、§7.6）：纵向每格 3 行，滚到最后一行贴底；Shift+滚轮 / 横向滚轮每格 1 列；光标夹回视图
start; open_table t_order
e2e_wheel 60 20 down; sleep 0.3
check "下滚一格：第一行是第 4 行，光标被夹到 4,1" eval '[[ $(rowno $(row_y 1)) == 4 ]] && pos_is 4,1'
for i in $(seq 40); do e2e_wheel 60 20 down; done; sleep 0.5
check "一直下滚：最后一行（第 100 行）贴着底边就停住" eval '[[ $(rowno $(last_y)) == 100 ]]'
key g g
_sgr 69 60 20 M; sleep 0.3   # Shift + 滚轮向下（SGR 65 + Shift 4）
check "Shift+滚轮：横向 1 列，第一列变成 user_id，光标夹到 1,2" eval '[[ $(header) == *"│ user_id │"* && $(header) != *" id │ user_id"* ]] && pos_is 1,2 || { echo "  $(header)"; false; }'
_sgr 66 60 20 M; sleep 0.3   # 横向滚轮（按钮 6 → 66，向左）
check "横向滚轮向左：回到 id 列" eval '[[ $(header) == *" id │ user_id"* ]]'
_sgr 67 60 20 M; sleep 0.3   # 横向滚轮（按钮 7 → 67，向右）
check "横向滚轮向右：又滚过 1 列" eval '[[ $(header) != *" id │ user_id"* && $(header) == *"│ user_id │"* ]]'
_sgr 66 60 20 M; sleep 0.3
key C-h
e2e_click $(( $(col_x status) + 1 )) $(row_y 5); sleep 0.3
check "焦点在树上时点击单元格：① 获得焦点，光标移到 5,3（status）" eval '[[ $(focused) == 1 ]] && pos_is 5,3'

# ---- 以下用自建库：空串、空表、报错、busy 与取消、R
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -c "create table t_blank (id int primary key, s text)" -c "insert into t_blank values (1, ''), (2, 'x')" \
  -c "create table t_empty (id int primary key)" -c "create table t_gone (id int primary key)"
APP=e2e-f13-$$
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$E2E_DB" "$APP" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
# opened_sql NAME：先打开 NAME（列信息进缓存），再锁住它、按 R 重新取数，SQL 是正在等锁的取数语句
# （计数排在取数后面，pg_stat_activity 只留每条连接的最后一条；F3.37 起同一个 pane 里再打开它只会切过去，不再取数）
opened_sql() { open_table "$1"; e2e_lock "$1"; e2e_keys R
  wait_for 5 eval '[[ -n $(e2e_waiting $APP) ]]'; SQL=$(e2e_waiting $APP); e2e_unlock; wait_for 5 eval '[[ -n $(grid_y) ]]'; sleep 0.3; }
start -C "$D/own"

# 取数的 SQL（§8.5、§10.1）：pg_stat_activity 里 Meta 最后执行的语句
opened_sql t_order
check "t_order：order by 主键 \"id\"，limit 101 offset 0" eval '[[ $SQL == "select * from \"public\".\"t_order\" order by \"id\" limit 101 offset 0" ]] || { echo "  $SQL"; false; }'
opened_sql t_event
check "t_event：按主键的键顺序 \"occurred_at\", \"id\"" eval '[[ $SQL == *"from \"public\".\"t_event\" order by \"occurred_at\", \"id\" limit 101"* ]] || { echo "  $SQL"; false; }'
opened_sql t_sku
check "t_sku 没有主键：按一个不可空的唯一索引排" eval '[[ $SQL =~ from\ \"public\"\.\"t_sku\"\ order\ by\ \"(code|title|title\",\ \"code)\"\ limit ]] || { echo "  $SQL"; false; }'
opened_sql t_log
check "t_log 没有行标识列：不排序" eval '[[ $SQL == "select * from \"public\".\"t_log\" limit 101 offset 0" ]] || { echo "  $SQL"; false; }'

# 空串、空表；行,列只在焦点 data pane 有已加载的非空表时显示（§7.8）。F3.18 起 ↵ 一律新开 tab，上面开出了一排：重新开始
start -C "$D/own"; open_table t_blank
check "空串显示为空白，x 照常" eval 'x=$(col_x s); [[ $(e2e_text $x $x $(row_y 1)) == " " && $(e2e_text $x $x $(row_y 2)) == x ]]'
check "有已加载的表：状态栏显示 1,1" pos_is 1,1
key C-h; check "焦点在树上：不显示行,列" eval '[[ -z $(pos) ]]'
key C-l; key Space %; check "焦点在空 pane：不显示行,列" eval '[[ -z $(pos) ]]'
open_table t_empty
check "空表：只有表头，不显示行,列" eval '[[ $(header) == *" id "* && -z $(pos) ]]'
e2e_keys Space; e2e_type x; sleep 0.3

# 数据库报错（§7.6、F3.20）：显示在 pane 底部的错误栏，第一次打开就出错时不画表格
psql "$E2E_DB" -q -c "drop table t_gone"
key C-p; e2e_type "@t_gone"; sleep 0.3; key Enter; sleep 0.5
check "打开已被删掉的 t_gone：错误栏是 error 色的 [42P01] 数据库错误，不画表格" eval 'errbar_is 1 "[42P01] relation \"public.t_gone\" does not exist ×" && [[ -z $(grid_y) ]] && style_has 36 $(errbar_y 1) fg=$ERROR'

# busy 与取消（§8.3）：另一个会话锁住 t_user，打开它就一直在等
waiting() { psql "$E2E_DB" -At -c "select count(*) from pg_stat_activity where application_name = '$APP' and wait_event_type = 'Lock'"; }
open_table t_order
e2e_lock t_user
key C-p; e2e_type "@t_user"; sleep 0.3; key Enter
check "取数中：状态栏在模式块左边显示 warn 色的 busy · C-c 取消" eval '[[ $(bar) == *" busy · C-c 取消  NORMAL " ]] && c=$(e2e_find "busy" $(H)) && style_has ${c%% *} $(H) fg=$WARN || { echo "  $(bar)"; false; }'
check "取数中：不画「加载中」占位（↵ 新开了 t_user 的 tab，第一次打开，内容区空白）" eval '[[ -z $(grid_y) && -z $(e2e_text 35 159 4 | tr -d " ") && $(e2e_text 34 160 1) == *" t_user ─"* ]]'
check "服务端确实在等锁" eval '[[ $(waiting) == 1 ]]'
key C-c
check "C-c：取消，toast「查询已取消」，busy 消失，不弹退出提示" eval 'toast_is "查询已取消" && [[ $(bar) != *busy* ]] && ! screen_has "再按一次"'
check "服务端的查询也被取消了（不再等锁）" eval 'wait_for 3 eval "[[ \$(waiting) == 0 ]]"'
check "第一次打开时取消：tab 还是 t_user，是空表" eval '[[ -z $(grid_y) ]] && tabs_are 1 "1:t_blank │ 2:t_gone │ 3:t_order- │ 4:t_user*"'
key C-c
check "空闲时 C-c 照旧：「再按一次 C-c 退出」" toast_is "再按一次 C-c 退出"
sleep 2.2
key Space %; key C-p; e2e_type "@t_user"; sleep 0.3; key C-t   # 新 pane 里第一次打开（① 已有 t_user，↵ 会切过去，F1.6）
c=$(e2e_find busy $(H)); e2e_click $(( ${c%% *} + 1 )) $(H); sleep 0.3
check "点击 busy 提示也能取消；第一次打开时取消，就是空表（没有网格）" eval 'toast_is "查询已取消" && [[ $(bar) != *busy* ]] && [[ $(e2e_text 99 159 3) != *┼* ]]'
e2e_unlock
sleep 0.5; key R; sleep 0.5                                             # F3.37：② 里已有 t_user 的 tab，再打开只切过去，所以用 R 重取
check "锁释放后在 ② 的 t_user 上 R：正常显示，同一条 Meta 连接还能用" eval '[[ $(e2e_text 99 159 $(hy)) == *name* ]] || { echo "  $(e2e_text 99 159 $(hy))"; false; }'

# R（F1.2 的刷新）之后，已经打开的表仍保留类型颜色和钥匙图标
e2e_keys Space; e2e_type x; sleep 0.3; open_table t_order     # 关掉 ②，切到 ① 的 t_order（只开着一个，F1.6）
key C-h; key R; sleep 0.5; key C-l
check "R 之后 ① 的 t_order 仍有 number 色和钥匙图标" eval 'x=$(col_x id); style_has 45 $(row_y 1) fg=$NUMBER && [[ $(e2e_text $((x - 2)) $((x - 2)) $(hy)) == "$(printf "\xef\x82\x84")" ]]'

e2e_done
