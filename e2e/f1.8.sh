#!/usr/bin/env bash
# F1.8 查询条按钮与 ORDER 方向（specs/m1-browse/task.md F1.8；tech-design §7.8「查询条」、§7.7）
# 图标间距、颜色、悬停整块高亮的布局由 golden 和单测覆盖（TestQueryBarIconColors、TestOrderToggle、TestQueryBarIconCutOff）；
# 这里测真实终端里的悬停、点击命中区，和点击之后发出的 SQL（锁住表，读等锁的语句）。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
APP=e2e-f18-$$
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$E2E_DB" "$APP" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
SELECT=#364a82
REFRESH=$(printf '\xef\x80\xa1') TRANSPOSE=$(printf '\xef\x83\xac') SAVE=$(printf '\xef\x83\x87')
at() { local c; c=$(e2e_find "$1" "${2:-3}"); echo "${c%% *}"; }           # TEXT [Y] — 第一次出现的列
dd() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }'; }             # 下拉框的 X Y W H
lit() { style_has "$1" 3 bg=$SELECT >/dev/null; }                          # X：查询条第二行这一格是 select 底
# sent TABLE CMD...：锁住 TABLE 时执行 CMD，SQL 是它发出、正在等锁的取数语句；然后放锁、等表格回来
sent() { local tb=$1; shift; e2e_lock "$tb"; "$@"; wait_for 5 eval '[[ -n $(e2e_waiting $APP) ]]'; SQL=$(e2e_waiting $APP); e2e_unlock; wait_for 8 settled; sleep 0.3; }
sql_has() { [[ $SQL == *"$1"* ]] || { echo "  sent: $SQL"; echo "  want: …$1…"; false; }; }

start -C "$D/own"; open_table t_order; wait_for 8 settled
check "默认排序：ORDER chip 是 id 加升序图标" eval '[[ $(qb) == *"ORDER id $ASC "* ]] || { echo "  $(qb)"; false; }'

# ---- 悬停各亮各的：方向图标是一个按钮，chip 的其余部分是另一个
ix=$(at "$ASC"); ox=$(at ORDER)
e2e_move "$ix" 3; sleep 0.3
check "指针在方向图标上：只亮图标，chip 的其余部分不亮" eval 'lit $ix && ! lit $ox && ! lit $((ix - 2))'
e2e_move "$ox" 3; sleep 0.3
check "指针在 ORDER 上：亮 chip，方向图标不亮" eval 'lit $ox && lit $((ix - 2)) && ! lit $ix'

# ---- 点击方向图标：翻转，回到第 1 页；默认排序时得到行标识列降序
key ']'; wait_for 8 settled
sent t_order e2e_click "$ix" 3
check "默认排序、第 2 页时点方向图标：order by \"id\" desc，回到第 1 页，图标变成降序" eval 'sql_has "order by \"id\" desc limit 101 offset 0" && qb_has "PAGE 1/60" && [[ $(qb) == *"ORDER id $DESC "* ]]'
sent t_order e2e_click "$(at "$DESC")" 3
check "再点一次：回到升序，图标也回来" eval 'sql_has "order by \"id\" asc limit 101 offset 0" && [[ $(qb) == *"ORDER id $ASC "* ]]'
e2e_click "$ox" 3; sleep 0.3
check "点 chip 的列名部分：打开下拉框，排序不变" eval '[[ -n $(dd) ]] && [[ $(qb) == *"ORDER id $ASC "* ]]'
key Escape

# ---- 图标按钮画成 " <图标> "：悬停整个按钮亮，命中区覆盖整个按钮
# F3.23 起工具按钮有三组七个，70 列的 ① 放不下（整组让位）：先关掉 console，让 ① 占满侧栏右边
solo
rx=$(at "$REFRESH"); tx=$(at "$TRANSPOSE")
e2e_move $((rx - 1)) 3; sleep 0.3
check "指针在刷新按钮左边的空格上：整个按钮（3 列）亮" eval 'lit $((rx - 1)) && lit $rx && lit $((rx + 1)) && ! lit $((rx + 2))'
e2e_click $((tx + 1)) 3; sleep 0.4
check "点转置按钮右边的空格：同样转置" eval '[[ $(e2e_text 35 159 $(hy)) =~ │\ 1\ +│\ 2\  ]]'
key T

# ---- 复合键 t_event（主键 occurred_at, id）：点方向图标、在下拉框选 occurred_at，都得到 occurred_at 降序
open_table t_event; wait_for 8 settled
sent t_event e2e_click "$(at "$ASC")" 3
check "t_event 默认排序时点方向图标：order by \"occurred_at\" desc, \"id\"" sql_has 'order by "occurred_at" desc, "id" limit 101'
e2e_click "$(at "$DESC")" 3; wait_for 8 settled                        # 回到默认的升序
key g o; e2e_type occurred_at; sleep 0.3
sent t_event key Enter
check "在下拉框选 occurred_at：同样 order by \"occurred_at\" desc, \"id\"" sql_has 'order by "occurred_at" desc, "id" limit 101'

# ---- 没有行标识列（t_log）：chip 是 —，没有方向图标
open_table t_log; wait_for 8 settled
check "t_log：ORDER —，查询条上没有方向图标" eval '[[ $(qb) == *"ORDER —"* && $(qb) != *"$ASC"* && $(qb) != *"$DESC"* ]] || { echo "  $(qb)"; false; }'

# ---- 放不下的 chip 整个让位，不画半截（F3.23，§7.8「第二行放不下时谁让位」；原来截断的 chip 上的命中区见 fdd2b79）
start -x 70 -y 24 -C "$D/own"; open_table t_event; wait_for 8 settled; two_panes
g=($(e2e_panes | awk '$1 == 1 { print $2, $3, $4, $5 }')); last=$((g[0] + g[2] - 2))
check "70 列、三个 pane：① 只有 10 列，连 ORDER chip 也让位，第二行是空的" eval '[[ -z $(e2e_text $((g[0] + 1)) $last 3 | tr -d " ") ]] || { echo "  $(e2e_text ${g[0]} $((last + 1)) 3)"; false; }'
e2e_move "$last" 3; sleep 0.3; e2e_click "$last" 3; sleep 0.3
check "点那一行：没有命中区，不开下拉框" eval '[[ -z $(dd) ]] && ! lit $last'

# ---- 主题的 [icon] 给 save、refresh、transpose、sort_asc 写 fg 后生效（§7.7）
mkdir -p "$D/themed/themes"; cp "$D/own/connections.toml" "$D/themed/"
printf 'theme = "x"\n' >"$D/themed/config.toml"
printf '[icon]\nsave = { fg = "#010203" }\nrefresh = { fg = "#040506" }\ntranspose = { fg = "#070809" }\nsort_asc = { fg = "#0a0b0c" }\n' >"$D/themed/themes/x.toml"
SOLO=1 start -C "$D/themed"; open_table t_order; wait_for 8 settled
check "[icon] 的 fg：save / refresh / transpose / sort_asc 各用各的颜色" eval 'style_has $(at "$SAVE") 3 fg=#010203 && style_has $(at "$REFRESH") 3 fg=#040506 && style_has $(at "$TRANSPOSE") 3 fg=#070809 && style_has $(at "$ASC") 3 fg=#0a0b0c'

e2e_done
