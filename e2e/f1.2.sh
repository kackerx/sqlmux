#!/usr/bin/env bash
# F1.2 catalog 与 schema 树（specs/m1-browse/task.md F1.2；tech-design §7.8「schema 侧栏」、§8.4、§8.6、§12）
# catalog 的主键、唯一索引、枚举、可空、默认值由 go test -tags integration 覆盖；这里测树和面板。
# F1.12 起树是层级树，schema 下拉框去掉了；展开、列节点、跨 schema 过滤、工作区在 f1.12.sh。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
. "$(dirname "$0")/palette.sh"
FOCUS=#9ece6a SELECT=#364a82 ROW=#292e42 PANE_BG=#24283b FUNC=#7aa2f7 DIM=#565f89
NF_TABLE=$(printf '\xef\x83\x8e')   # U+F0CE
typ() { e2e_type "$1"; sleep 0.3; }
last_y() { echo $(( $(H) - 4 )); }   # 树的最后一行：下面是分隔线、提示行、下边框和状态栏
# tree：侧栏的树逐行「名字 [注释]」，去掉缩进、▸ / ▾ 和图标（F1.12 起是层级树）
tree() { e2e_rows 2 31 4 "$(last_y)" 3 | cut -d'|' -f1 | python3 -c '
import sys
for l in sys.stdin:
    l = "".join(c for c in l if not (0xE000 <= ord(c) <= 0xF8FF) and c not in "▸▾")
    l = " ".join(l.split())
    if l: print(l)'; }
item_y() { local y; for ((y = 4; y <= $(last_y); y++)); do [[ $(e2e_text 2 31 $y) =~ (^|\ )$1(\ |$) ]] && { echo $y; return; }; done; }
cursor_on() {   # NAME — 光标行（select 底或 row 底）是 NAME，且只有这一行
  local y got=; for ((y = 4; y <= $(last_y); y++)); do [[ $(e2e_style 30 $y) == *bg=$SELECT* || $(e2e_style 30 $y) == *bg=$ROW* ]] && got+="$(e2e_text 2 31 $y | python3 -c 'import sys; print(" ".join("".join(c for c in sys.stdin.read() if not (0xE000 <= ord(c) <= 0xF8FF) and c not in "▸▾").split()))') "; done
  [[ $got == "$1 "* && $(wc -w <<<"$got") -le $(( $(wc -w <<<"$1") + 1 )) ]] || { echo "  cursor rows: '$got', want '$1'"; false; }
}
lit() { local y c; y=$(item_y "$1"); c=$(e2e_find "$NF_TABLE" "$y"); style_has "${c%% *}" $y fg=$FOCUS && style_has $(e2e_find "$1" "$y" | cut -d' ' -f1) $y fg=$FOCUS; }   # NAME 是「当前打开的表」
none_lit() { local y; for ((y = 4; y <= $(last_y); y++)); do [[ $(e2e_style 14 $y) != *fg=$FOCUS* ]] || { echo "  row $y lit: $(e2e_text 2 31 $y)"; return 1; }; done; }
data_title() { e2e_text 34 160 1; }
data_tabs() { e2e_text 34 159 43 | noicon | sed 's/  .*//'; }   # 到第一处连续空格为止（右边是键位提示）
mclick() { _sgr 1 "$1" "$2" M; _sgr 1 "$1" "$2" m; }   # 中键
psql_n() { psql "$SQLMUX_TEST_PG" -At -c "$1"; }
# want KINDS SCHEMA：期望的一组节点，按 §7.8 的规则由 reltuples 算出行数量级；分区子表不列；按名字排；普通视图（v）没有行数
want() {
  psql "$SQLMUX_TEST_PG" -At -F' ' -c "select c.relname, c.relkind, c.reltuples from pg_class c join pg_namespace n on n.oid = c.relnamespace where n.nspname = '$2' and c.relkind = any('{$1}') and not c.relispartition" | python3 -c '
import math, sys
def mag(n):
    if n < 0: return "?"
    if n < 1000: return str(int(n))
    for s, d in (("b", 1e9), ("m", 1e6), ("k", 1e3)):
        if n >= d:
            v = n / d
            return (f"{math.floor(v * 10) / 10:.1f}" if v < 10 else str(math.floor(v))) + s
for name, kind, n in sorted(l.split() for l in sys.stdin if l.strip()): print(name if kind == "v" else f"{name} {mag(float(n))}")'
}
between() { tree | awk -v a="$1" -v b="$2" '$0 == b { f = 0 } f { print } $0 == a { f = 1 }'; }   # A B：树里 A 和 B 之间的行

# ---- 树（§7.8、§8.4）：Tables 下按名字排，分区子表不列；行数量级；视图在 Views 下
start
n=$(psql_n "select count(*) from pg_class c join pg_namespace n on n.oid = c.relnamespace where c.relkind in ('r','p','v','m','f') and not c.relispartition and n.nspname not in ('pg_catalog', 'information_schema') and n.nspname !~ '^pg_toast'")
check "第一行显示 $n tables（所有 schema 的表和视图，F1.12）" text_is 4 14 2 " $n tables "
check "public 的 Tables：按名字排，行数量级按 §7.8（t_order 6.0k、t_order_item 400），分区子表不列" eval 'w=$(want r,p,f public); [[ $(between "Tables ($(grep -c . <<<"$w"))" "Views (2)") == "$w" && $w == *"t_order 6.0k"* && $w == *"t_order_item 400"* && $w != *t_event_20* ]] || { echo "  tree: $(tree | tr "\n" ",")"; echo "  want: $(tr "\n" "," <<<"$w")"; false; }'
key C-h; for ((i = 0; i < 12; i++)); do [[ $(e2e_text 2 31 $(item_y "Views")) == *Views* ]] && cursor_on "Views (2)" >/dev/null && break; key j; done; key Enter; key C-l
check "展开 Views (2)：物化视图有行数（名字太长时整个不显示），普通视图 v_paid_order 没有" eval '[[ $(between "Views (2)" "工作区") == "mv_order_by_status"*$'"'\n'"'"v_paid_order" ]] || { between "Views (2)" "工作区"; false; }'
check "表项：图标 func 色" eval 'y=$(item_y t_log); c=$(e2e_find "$NF_TABLE" $y); style_has ${c%% *} $y fg=$FUNC'

# ---- 光标（§7.8）：树聚焦时 select 底、失焦时 row 底；j/k 支持次数，gg / G；悬停 row 底、不动光标
key C-h; key g g; key C-l
check "失焦：光标行（第一个节点 doraemon）为 row 底" eval 'cursor_on doraemon && style_has 30 4 bg=$ROW'
key C-h
check "C-h 聚焦树：光标行为 select 底" eval 'cursor_on doraemon && style_has 30 4 bg=$SELECT'
key 3 j; check "3j：下移 3 个节点（Tables (6)）" cursor_on "Tables (6)"
key k;   check "k：上移 1 个（public）" cursor_on public
key G;   check "G：最后一个节点（工作区的 pane-1，F3.19）" cursor_on pane-1
key g g; check "gg：第一个节点" cursor_on doraemon
e2e_move 20 $(item_y t_sku); sleep 0.3
check "悬停 t_sku：row 底，光标不动" eval 'style_has 30 $(item_y t_sku) bg=$ROW && style_has 30 4 bg=$SELECT'
e2e_move 80 20; sleep 0.2

# ---- / 过滤（§7.8，nvim-tree 的 live filter）
key 5 j; typ /
check "/：进入 INSERT" mode_is INSERT
typ ord
check "输入 ord：过滤行显示 <图标> ord 和 dim 色的 4/${n}，终端光标在文字后面" eval 'text_is 3 7 2 "$(e2e_text 3 3 2) ord" && text_ends 1 32 2 " 4/$n │" && at_c=$(e2e_find 4/$n 2) && style_has $at_c 2 fg=$DIM && flag_is cursor_flag 1 && [[ $(e2e_flag cursor_x) == 7 && $(e2e_flag cursor_y) == 1 ]]'
check "只剩匹配的表和视图及其上级，按树的顺序（不按分数），工作区隐藏" eval '[[ $(tree | tr "\n" ",") == "doraemon,public,Tables (6),t_order 6.0k,t_order_item 400,Views (2),mv_order_by_status,v_paid_order," ]] || { echo "  $(tree | tr "\n" ",")"; false; }'
key Enter
check "↵：回到 NORMAL，保留过滤，光标在第一个匹配 t_order 上" eval 'mode_is NORMAL && text_ends 1 32 2 " 4/$n │" && cursor_on t_order'
key j; typ /; key Escape
check "esc：清空过滤、回到 NORMAL，光标仍在 t_order_item" eval 'mode_is NORMAL && text_is 4 14 2 " $n tables " && cursor_on t_order_item'
typ /sku; key C-c
check "C-c 等同 esc：清空过滤，光标在 t_sku，不弹退出提示" eval 'text_is 4 14 2 " $n tables " && cursor_on t_sku && ! screen_has "再按一次" && running'

# ---- 打开表（§7.8、§12）：↵、t、单击、中键都新开 tab（F3.18 起 ↵ 和单击不再替换当前 tab，只替换引导 tab）；打开后焦点移到 data pane
key k k; key Enter
check "↵ 打开 t_order：① 标题显示 t_order，焦点移到 ①" eval '[[ $(data_title) == "┌─ ① "?" t_order ─"* && $(focused) == 1 && $(data_tabs) == "│ 1:t_order* │ +" ]] || { echo "  $(data_title) | $(data_tabs) | focus $(focused)"; false; }'
check "当前打开的表 t_order：图标和表名 focus 色；树失焦，光标行为 row 底" eval 'lit t_order && style_has 30 $(item_y t_order) bg=$ROW'
key C-h; key j
check "光标和当前打开的表分开画：t_order 仍是 focus 色、不是光标底色，光标在 t_order_item" eval 'lit t_order && style_has 30 $(item_y t_order) bg=$PANE_BG && cursor_on t_order_item'
key t
check "t 在新 tab 打开 t_order_item，焦点移到 ①" eval '[[ $(data_tabs) == "│ 1:t_order- │ 2:t_order_item* │ +" && $(focused) == 1 ]] || { echo "  $(data_tabs) focus $(focused)"; false; }'
e2e_click $(e2e_find t_sku $(item_y t_sku) | cut -d' ' -f1) $(item_y t_sku); sleep 0.3
check "单击 t_sku：新开 tab（F3.18），焦点在 ①" eval '[[ $(data_tabs) == "│ 1:t_order │ 2:t_order_item- │ 3:t_sku* │ +" && $(focused) == 1 && $(data_title) == *" t_sku ─"* ]] || { echo "  $(data_tabs) focus $(focused)"; false; }'
mclick $(e2e_find t_user $(item_y t_user) | cut -d' ' -f1) $(item_y t_user); sleep 0.3
check "中键 t_user：在新 tab 打开，焦点在 ①" eval '[[ $(data_tabs) == "│ 1:t_order │ 2:t_order_item │ 3:t_sku- │ 4:t_user* │ +" && $(focused) == 1 ]] || { echo "  $(data_tabs) focus $(focused)"; false; }'
check "当前打开的表跟着 ① 的当前 tab：t_user 高亮，t_order / t_sku 不再高亮" eval 'lit t_user && ! lit t_order >/dev/null && ! lit t_sku >/dev/null'
key Space %
check "焦点在空的 ②：↵ 会打开到 ②，树里没有高亮的表" none_lit
key C-h; key C-h
check "焦点在树上：↵ 打开到最近聚焦过的 data pane ①（从 ② 经过 ① 回来），高亮 t_user" lit t_user

# ---- 面板 @ 的顺序跟着「树当前的 schema」（光标所在节点所属的 schema，F1.12）
key g g; key j                                                                   # 光标到 agentable
pal "@"
check "光标在 agentable 上，面板 @：agentable 的表在前，其余按 schema、表名；所在位置是 doraemon.<schema>" eval 'o=$(list | cut -d"|" -f1 | head -5 | tr -s " " | tr "\n" ","); [[ $o == "agent doraemon.agentable 表,agent_version doraemon.agentable 表,goal doraemon.agentable 表,mv_order_by_status doraemon.public 表,t_event doraemon.public 表," ]] || { echo "  $o"; false; }'
key Escape; key j                                                                 # 光标到 public
pal "@"
check "光标回到 public：面板 @ 里 public 在前" eval '[[ $(list | head -1 | cut -d" " -f1) == mv_order_by_status ]]'
key Escape

# ---- 滚轮（§7.4、§7.8）：12 行高时树放 5 行；每格 3 行，最多滚到最后一个节点贴着底边；光标被夹回视图
start -y 12
key C-h
e2e_wheel 10 6 down; sleep 0.3
check "滚一格：视图下移 3 个节点（Tables (6) 起）；光标被夹到 Tables (6)" eval '[[ $(tree | head -1) == "Tables (6)" ]] && cursor_on "Tables (6)"'
for i in 1 2 3 4; do e2e_wheel 10 6 down; sleep 0.2; done
check "滚到底：最后一个节点 pane-1 贴着底边，不再往下" eval '[[ $(tree | tail -1) == pane-1 && $(e2e_text 2 31 $(last_y)) == *pane-1* ]]'
for i in 1 2 3 4 5 6; do e2e_wheel 10 6 up; sleep 0.2; done
check "往回滚：回到顶部" eval '[[ $(tree | head -1) == doraemon ]]'

# ---- R 刷新（§7.8、§6.8）：Meta 连接重新执行表列表的查询
APP=e2e-f12-$$
printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$SQLMUX_TEST_PG" "$APP" >"$D/connections.toml"; chmod 600 "$D/connections.toml"
tables_q() { psql "$SQLMUX_TEST_PG" -At -c "select max(query_start) from pg_stat_activity where application_name = '$APP' and query like '%relispartition%'"; }
start -C "$D"
t0=$(tree); q0=$(tables_q); key C-h; key R; sleep 0.5
check "R：Meta 上重新查了表列表，树不变" eval '[[ -n $q0 && $(tables_q) > $q0 && $(tree) == "$t0" ]] || { echo "  query_start $q0 → $(tables_q)"; false; }'

e2e_done
