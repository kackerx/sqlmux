#!/usr/bin/env bash
# F1.2 catalog 与 schema 树（specs/m1-browse/task.md F1.2；tech-design §7.8「schema 侧栏」、§8.4、§8.6、§12）
# catalog 的主键、唯一索引、枚举、可空、默认值由 go test -tags integration 覆盖；这里测树、下拉框和面板。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
. "$(dirname "$0")/palette.sh"
H() { e2e_flag pane_height; }
FOCUS=#9ece6a SELECT=#364a82 ROW=#292e42 PANE_BG=#24283b WARN=#e0af68 FUNC=#7aa2f7 DIM=#565f89 PK=#73daca
key() { e2e_keys "$@"; sleep 0.3; }
typ() { e2e_type "$1"; sleep 0.3; }
last_y() { echo $(( $(H) - 4 )); }   # 表列表的最后一行：下面是分隔线、提示行、下边框和状态栏
# tree：侧栏表列表逐行「表名 行数」
tree() { local y; for ((y = 4; y <= $(last_y); y++)); do e2e_text 5 31 $y | tr -s ' ' | sed 's/^ //; s/ $//'; done | sed '/^$/d'; }
item_y() { local y; for ((y = 4; y <= $(last_y); y++)); do [[ $(e2e_text 5 31 $y) == "$1 "* ]] && { echo $y; return; }; done; }
cursor_on() {   # NAME — 光标行（select 底或 row 底）是 NAME，且只有这一行
  local y got=; for ((y = 4; y <= $(last_y); y++)); do [[ $(e2e_style 20 $y) == *bg=$SELECT* || $(e2e_style 20 $y) == *bg=$ROW* ]] && got+="$(e2e_text 5 22 $y | sed 's/ *$//') "; done
  [[ $got == "$1 " ]] || { echo "  cursor rows: '$got', want '$1'"; false; }
}
lit() { local y=$(item_y "$1"); style_has 3 $y fg=$FOCUS && style_has 5 $y fg=$FOCUS; }   # NAME 是「当前打开的表」
none_lit() { local y; for ((y = 4; y <= $(last_y); y++)); do [[ $(e2e_style 5 $y) != *fg=$FOCUS* ]] || { echo "  row $y lit: $(e2e_text 5 22 $y)"; return 1; }; done; }
title_is() { local t; t=$(e2e_text 1 32 1); [[ $t == "┌─ ⓪ "?" $1 ▾ "* ]] || { echo "  sidebar title: $t"; false; }; }
mode_is() { [[ $(e2e_text 150 160 45) == *" $1 " ]] || { echo "  mode: $(e2e_text 140 160 45)"; false; }; }
data_title() { e2e_text 34 160 1; }
data_tabs() { e2e_text 34 159 43 | sed 's/  .*//'; }   # 到第一处连续空格为止（右边是键位提示）
mclick() { _sgr 1 "$1" "$2" M; _sgr 1 "$1" "$2" m; }   # 中键
dropdown() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }'; }   # 下拉框的 X Y W H
dd_rows() { local g y; g=($(dropdown)); for ((y = g[1] + 3; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | sed 's/ *$//'; done; }
dd_y() { local g y; g=($(dropdown)); for ((y = g[1] + 3; y < g[1] + g[3] - 1; y++)); do [[ $(e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y) == "$1 "* ]] && echo $y; done; }
dd_sel() { local y; y=$(dd_y "$1"); style_has 10 $y bg=$SELECT; }
# 期望的树：按 §7.8 的规则由 reltuples 算出行数量级；分区子表不列；按名字排
want_tree() {
  psql "$SQLMUX_TEST_PG" -At -F' ' -c "select c.relname, c.reltuples from pg_class c join pg_namespace n on n.oid = c.relnamespace where n.nspname = '$1' and c.relkind in ('r','p','v','m','f') and not c.relispartition" | python3 -c '
import math, sys
def mag(n):
    if n < 0: return "?"
    if n < 1000: return str(int(n))
    for s, d in (("b", 1e9), ("m", 1e6), ("k", 1e3)):
        if n >= d:
            v = n / d
            return (f"{math.floor(v * 10) / 10:.1f}" if v < 10 else str(math.floor(v))) + s
rows = sorted(l.split() for l in sys.stdin if l.strip())
for name, n in rows: print(name, mag(float(n)))'
}

# ---- 列表（§7.8、§8.4）：初始 schema 取 current_schema()；分区子表不列，视图、物化视图在；行数量级
start
check "侧栏标题为 current_schema()（public）" title_is public
check "第一行显示 8 tables" text_is 4 13 2 " 8 tables "
check "public 的 8 张表按名字排，行数量级按 §7.8（t_order 6.0k、t_order_item 400、视图 ?），分区子表不列" eval 'w=$(want_tree public); [[ $(tree) == "$w" && $w == *"t_order 6.0k"* && $w == *"t_order_item 400"* && $w == *"v_paid_order ?"* && $w != *t_event_20* ]] || { echo "  tree: $(tree | tr "\n" ",")"; echo "  want: $(tr "\n" "," <<<"$w")"; false; }'
check "表项：图标 func 色" style_has 3 4 fg=$FUNC

# ---- 光标（§7.8）：树聚焦时 select 底、失焦时 row 底；j/k 支持次数，gg / G；悬停 row 底、不动光标
check "失焦：光标行（第一项）为 row 底" eval 'cursor_on mv_order_by_status && style_has 20 4 bg=$ROW'
key C-h
check "C-h 聚焦树：光标行为 select 底" eval 'cursor_on mv_order_by_status && style_has 20 4 bg=$SELECT'
key 3 j; check "3j：下移 3 项" cursor_on t_order
key k;   check "k：上移 1 项" cursor_on t_log
key G;   check "G：最后一项" cursor_on v_paid_order
key g g; check "gg：第一项" cursor_on mv_order_by_status
e2e_move 20 $(item_y t_sku); sleep 0.3
check "悬停 t_sku：row 底，光标不动" eval 'style_has 20 $(item_y t_sku) bg=$ROW && style_has 20 4 bg=$SELECT'
e2e_move 80 20; sleep 0.2

# ---- / 过滤（§7.8，nvim-tree 的 live filter）
key 5 j; typ /
check "/：进入 INSERT" mode_is INSERT
typ ord
check "输入 ord：过滤行显示 <图标> ord 和 dim 色的 4/8，终端光标在文字后面" eval 'text_is 3 7 2 "$(e2e_text 3 3 2) ord" && text_ends 1 32 2 " 4/8 │" && at_c=$(e2e_find 4/8 2) && style_has $at_c 2 fg=$DIM && flag_is cursor_flag 1 && [[ $(e2e_flag cursor_x) == 7 && $(e2e_flag cursor_y) == 1 ]]'
check "只剩匹配的表，按名字顺序（不按分数）" eval '[[ $(tree | cut -d" " -f1 | tr "\n" " ") == "mv_order_by_status t_order t_order_item v_paid_order " ]] || { echo "  $(tree | tr "\n" ",")"; false; }'
check "匹配到的字符为 warn 底（t_order 的 ord）" eval 'y=$(item_y t_order); style_has 7 $y bg=$WARN && style_has 9 $y bg=$WARN && ! style_has 6 $y bg=$WARN >/dev/null && ! style_has 10 $y bg=$WARN >/dev/null'
key Enter
check "↵：回到 NORMAL，保留过滤，光标在第一个匹配上" eval 'mode_is NORMAL && text_ends 1 32 2 " 4/8 │" && cursor_on mv_order_by_status'
key j; typ /; key Escape
check "esc：清空过滤、回到 NORMAL，光标仍在 t_order" eval 'mode_is NORMAL && text_is 4 13 2 " 8 tables " && (( $(tree | wc -l) == 8 )) && cursor_on t_order'
typ /sku; key C-c
check "C-c 等同 esc：清空过滤，光标在 t_sku，不弹退出提示" eval 'text_is 4 13 2 " 8 tables " && cursor_on t_sku && ! screen_has "再按一次" && running'

# ---- 打开表（§7.8、§12）：↵ 当前 tab，t 新 tab，单击当前 tab，中键新 tab；打开后焦点移到 data pane
key g g; key 3 j; key Enter
check "↵ 打开 t_order：① 标题显示 t_order，焦点移到 ①" eval '[[ $(data_title) == "┌─ ① "?" t_order ─"* && $(focused) == 1 && $(data_tabs) == "│ 1:t_order* │ +" ]] || { echo "  $(data_title) | $(data_tabs) | focus $(focused)"; false; }'
check "当前打开的表 t_order：图标和表名 focus 色；树失焦，光标行为 row 底" eval 'lit t_order && style_has 20 $(item_y t_order) bg=$ROW'
key C-h; key j
check "光标和当前打开的表分开画：t_order 仍是 focus 色、不是光标底色，光标在 t_order_item" eval 'lit t_order && style_has 20 $(item_y t_order) bg=$PANE_BG && cursor_on t_order_item'
key t
check "t 在新 tab 打开 t_order_item，焦点移到 ①" eval '[[ $(data_tabs) == "│ 1:t_order- │ 2:t_order_item* │ +" && $(focused) == 1 ]] || { echo "  $(data_tabs) focus $(focused)"; false; }'
e2e_click 10 $(item_y t_sku); sleep 0.3
check "单击 t_sku：在当前 tab 打开，焦点在 ①" eval '[[ $(data_tabs) == "│ 1:t_order- │ 2:t_sku* │ +" && $(focused) == 1 && $(data_title) == *" t_sku ─"* ]] || { echo "  $(data_tabs) focus $(focused)"; false; }'
mclick 10 $(item_y t_user); sleep 0.3
check "中键 t_user：在新 tab 打开，焦点在 ①" eval '[[ $(data_tabs) == "│ 1:t_order │ 2:t_sku- │ 3:t_user* │ +" && $(focused) == 1 ]] || { echo "  $(data_tabs) focus $(focused)"; false; }'
check "当前打开的表跟着 ① 的当前 tab：t_user 高亮，t_order / t_sku 不再高亮" eval 'lit t_user && ! lit t_order >/dev/null && ! lit t_sku >/dev/null'
key Space %
check "焦点在空的 ②：↵ 会打开到 ②，树里没有高亮的表" none_lit
key C-h; key C-h
check "焦点在树上：↵ 打开到第一个 data pane ①，高亮 t_user" lit t_user

# ---- schema 下拉框（§8.6）：gs 或点击标题；输入进过滤框；C-n / C-p / ↑ / ↓；↵ 选中；esc、点外部关闭
typ /t_; key Enter          # 带着过滤切 schema：切过去后要清空
key g s
check "gs 打开下拉框：在标题下方从第 3 列起，右边框与侧栏右边框（第 32 列）对齐" eval '[[ $(dropdown) == "3 2 30 "* ]] || { echo "  dropdown at [$(dropdown)]"; false; }'
check "列出非系统 schema：agentable、public" eval '[[ $(dd_rows | tr "\n" " ") == "agentable public " ]] || { echo "  $(dd_rows | tr "\n" ",")"; false; }'
check "当前 schema public 为 pk 色，初始选中它" eval 'y=$(dd_y public); style_has 5 $y fg=$PK && dd_sel public'
check "下拉框打开时是 COMMAND（输入进过滤框，§7.8「COMMAND 模式」）" mode_is COMMAND
typ jk
check "j / k 被当成文字：过滤框里是 jk，没有匹配" eval '[[ $(e2e_text 5 12 3) == *jk* && -z $(dd_rows) ]]'
key BSpace; key BSpace
key C-p; check "C-p：上移到 agentable" dd_sel agentable
key C-n; check "C-n：下移到 public" dd_sel public
key Up;  check "↑：上移到 agentable" dd_sel agentable
key Down; check "↓：下移到 public" dd_sel public
e2e_move 10 $(dd_y agentable); sleep 0.3
check "悬停 agentable：row 底" style_has 10 $(dd_y agentable) bg=$ROW
e2e_move 100 20; sleep 0.2; key Escape
check "esc 关闭，不切换" eval '[[ -z $(dropdown) ]] && title_is public && mode_is NORMAL'
e2e_click 10 1; sleep 0.3
check "点击侧栏标题也打开下拉框" eval '[[ -n $(dropdown) ]]'
e2e_click 100 20; sleep 0.3
check "点击下拉框外部：关闭，不切换" eval '[[ -z $(dropdown) ]] && title_is public'
key C-h; key g s; key C-p; key Enter
check "↵ 选中 agentable：标题 agentable ▾，列出 3 张表，过滤清空，光标回到第一项" eval 'title_is agentable && text_is 4 13 2 " 3 tables " && [[ $(tree) == "$(want_tree agentable)" ]] && cursor_on agent'
pal "@"
check "面板 @：树所在的 agentable 的表在前，其余按 schema、表名；所在位置是 doraemon.<schema>" eval 'o=$(list | cut -d"|" -f1 | head -5 | tr -s " " | tr "\n" ","); [[ $o == "agent doraemon.agentable 表,agent_version doraemon.agentable 表,goal doraemon.agentable 表,mv_order_by_status doraemon.public 表,t_event doraemon.public 表," ]] || { echo "  $o"; false; }'
key Escape
e2e_click 10 1; sleep 0.3; e2e_click 10 $(dd_y public); sleep 0.3
check "点选 public：切回 public" eval 'title_is public && [[ $(tree) == "$(want_tree public)" ]]'
pal "@"
check "切回后面板 @ 里 public 在前" eval '[[ $(list | head -1 | cut -d" " -f1) == mv_order_by_status ]]'
key Escape

# ---- 滚轮（§7.4、§7.8）：12 行高时列表放 5 项；每格 3 行，最多滚到最后一项贴着底边；光标被夹回视图
start -y 12
key C-h
e2e_wheel 10 6 down; sleep 0.3
check "滚一格：视图下移 3 项（t_order 起），v_paid_order 贴着底边；光标被夹到 t_order" eval '[[ $(tree | head -1) == "t_order 6.0k" && $(e2e_text 5 22 $(last_y) | sed "s/ *$//") == v_paid_order ]] && cursor_on t_order'
e2e_wheel 10 6 down; sleep 0.3
check "再滚：已经到底，不动" eval '[[ $(tree | head -1) == "t_order 6.0k" ]]'
e2e_wheel 10 6 up; sleep 0.3
check "往回滚：回到顶部，光标仍在 t_order" eval '[[ $(tree | head -1) == "mv_order_by_status 4" ]] && cursor_on t_order'

# ---- R 刷新表列表（§7.8、§6.8）：Meta 连接重新执行表列表的查询
APP=e2e-f12-$$
printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$SQLMUX_TEST_PG" "$APP" >"$D/connections.toml"; chmod 600 "$D/connections.toml"
tables_q() { psql "$SQLMUX_TEST_PG" -At -c "select max(query_start) from pg_stat_activity where application_name = '$APP' and query like '%relispartition%'"; }
start -C "$D"
q0=$(tables_q); key C-h; key R; sleep 0.5
check "R：Meta 上重新查了表列表，列表不变" eval '[[ -n $q0 && $(tables_q) > $q0 && $(tree) == "$(want_tree public)" ]] || { echo "  query_start $q0 → $(tables_q)"; false; }'

e2e_done
