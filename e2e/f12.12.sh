#!/usr/bin/env bash
# F12.12 选择的颜色和 V 取消选择（specs/m12-grid/task.md F12.12；tech-design §7.6「选择」、§7.7）
# m 标记的记录序号格是 selected 底（黄褐），VISUAL 里正在选的范围是 visual 底（蓝）；V 进 UNSET：模式块是 UNSET，esc 或再按 V 时范围里的记录从选中集里去掉，
# 范围里本来没选的不受影响；v 不变；chip 行的「已选 N」是 warn 色。自建库 t_sel（8 行）。
. "$(dirname "$0")/lib.sh"
SOLO=1
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -c "create table t_sel (id int primary key, v text)" -c "insert into t_sel select g, 'v' || g from generate_series(1, 8) g" >/dev/null
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
SELECTED=#5c4a1e VISUAL=#2d3f76 WARN=#e0af68
row_y() { echo $(( $(grid_y) + $1 )); }
bg_of() { e2e_style 36 "$(row_y "$1")" | tr ' ' '\n' | grep '^bg=' | cut -d= -f2; }
state_of() { case $(bg_of "$1") in "$SELECTED") echo S ;; "$VISUAL") echo V ;; *) echo . ;; esac; }
states() { local n out=""; for n in $(seq 1 8); do out+=$(state_of "$n"); done; echo "$out"; }   # 每行一个字符：S 确认选中、V 正在选的范围、. 没选
states_are() { [[ $(states) == "$1" ]] || { echo "  rows: $(states), want $1"; false; }; }
sel_text() { qb | grep -oE '已选 [0-9]+' ; }

start -C "$D/own"; open_table t_sel; wait_for 8 settled
check "（准备）没有选中：8 行都没有底色" eval 'states_are "........"'
key m; key j; key m
check "m 两行（1、2）：序号格是 selected 底（黄褐 $SELECTED），不是 visual 蓝" eval 'states_are "SS......" && [[ $(sel_text) == "已选 2" ]]'
check "chip 行的「已选 2」是 warn 色（$WARN）" eval 'x=$(e2e_find "已选" 3 | cut -d" " -f1); style_has "$x" 3 fg=$WARN'
# ---- V：UNSET 模式，范围是 visual 底，esc 把范围从选中集里去掉
key V; sleep 0.3
check "V：模式块是 UNSET，当前行（2，本来选着）画成范围的 visual 底，行 1 还是 selected" eval 'mode_is UNSET && states_are "SV......"'
key Escape; sleep 0.4
check "esc：范围里的记录（行 2）从选中集里去掉，只剩行 1（chip「已选 1」），回到 NORMAL" eval 'mode_is NORMAL && states_are "S......." && [[ $(sel_text) == "已选 1" ]]'
# ---- V 再按一次也结束：k 之后再按 V
key g g; key j; key m; key j; key m        # 选 2、3（现在 1、2、3 都选着，光标在 3）
check "（准备）1、2、3 选着" eval 'states_are "SSS....."'
key V; key k; sleep 0.3
check "V k：范围是 3、2 两行（visual 底），行 1 仍是 selected" eval 'mode_is UNSET && states_are "SVV....."'
key V; sleep 0.4
check "再按 V：范围去掉，只剩行 1，回到 NORMAL" eval 'mode_is NORMAL && states_are "S......." && [[ $(sel_text) == "已选 1" ]]'
# ---- V 覆盖本来没选的行：不受影响
key g g; key j; key j; key j; key j      # 光标到行 5
key V; key j; sleep 0.3
check "V j 覆盖行 5、6（本来没选的）：范围是 visual 底" eval 'states_are "S...VV.."'
key Escape; sleep 0.4
check "esc：行 5、6 本来没选，不受影响（还是没选），行 1 仍选着" eval 'states_are "S......." && [[ $(sel_text) == "已选 1" ]]'
# ---- v 不变：范围 esc 后并入选中集（selected 黄褐）
key g g; key j; key j; key j; key j; key v; key j; key j; sleep 0.3
check "v j j：正在选的范围是 visual 底（行 5、6、7）" eval 'mode_is VISUAL && states_are "S...VVV."'
key Escape; sleep 0.4
check "esc：并入选中集，变成 selected 底（行 1、5、6、7），chip「已选 4」" eval 'states_are "S...SSS." && [[ $(sel_text) == "已选 4" ]]'
# ---- UNSET 里 d：先结束模式再执行（按去掉之后的选中集算）
key g g; key j; key j; key j; key j; key V; key j; sleep 0.3      # 行 5、6 在范围里
key d; sleep 0.5
check "UNSET 里按 d：先把范围（行 5、6）从选中集去掉，再对剩下的（行 1、7）标记删除，不是删行 5、6" eval '[[ $(e2e_text 35 38 "$(row_y 1)" | sed "s/[ │]//g") == − ]] && [[ $(e2e_text 35 38 "$(row_y 7)" | sed "s/[ │]//g") == − ]] && [[ $(e2e_text 35 38 "$(row_y 5)" | sed "s/[ │]//g") != − ]] && [[ $(e2e_text 35 38 "$(row_y 6)" | sed "s/[ │]//g") != − ]]' || { for n in 1 5 6 7; do echo "  row $n: $(e2e_text 35 38 "$(row_y $n)")"; done; }
e2e_done
