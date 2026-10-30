#!/usr/bin/env bash
# F10.7 表格选择的细节（specs/m10-feedback3/task.md F10.7；tech-design §7.6「选择」、§6.4 select）
# 审查中定的：选中的记录里夹着新增行时 d 只取消标记、新增行留着（以前会把新增行扔掉）；VISUAL 里单击把范围扩到那一行；VISUAL 仍是 NORMAL 上下文（SPC b 能用）；
# 结果 tab 也能选、能 y，d 不做事、选中保留；有选中或在 VISUAL 里时自动刷新暂停（别人改了数据，选中期间表格不动，清掉选中后马上刷新）。
# 基本流程在 f10.7.sh。用自建库的 t_sel（改数据）和共用库的 t_sku（只读）。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -c "create table t_sel (id int primary key, v text)" -c "insert into t_sel select g, 'v' || g from generate_series(1, 8) g" >/dev/null
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
VISUAL=#2d3f76 SELECTED=#5c4a1e   # F12.12：m / esc 确认的是 selected 底
row_y() { echo $(( $(grid_y) + $1 )); }
hl() { local st; st=" $(e2e_style 36 "$(row_y "$1")") "; [[ $st == *" bg=$VISUAL "* || $st == *" bg=$SELECTED "* ]]; }
hl_rows() { local n out=""; for n in $(seq 1 8); do hl "$n" && out+="$n,"; done; echo "$out"; }
hl_is() { [[ $(hl_rows) == "$1" ]] || { echo "  highlighted: '$(hl_rows)', want '$1'"; false; }; }
mark_of() { e2e_text 35 38 "$(row_y "$1")" | python3 -c "import re, sys; print(re.sub(r'[^0-9+−-]', '', sys.stdin.read()))"; }
sel_is() { local want=$1; if [[ $want == 0 ]]; then [[ $(qb) != *已选* ]] || { echo "  query bar: '$(qb)'"; false; }; else qb_has "已选 $want"; fi; }
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; }
done_run() { wait_for 8 eval '[[ $(bar) != *busy* ]]'; sleep 0.4; }
run() { key Enter; done_run; }
RP=3
rgeom() { geom $RP; }
rrow_y() { local g; g=($(rgeom)); echo $(( g[1] + 2 + $1 )); }               # 结果区第 N 条数据所在的屏幕行
rhl() { local g st; g=($(rgeom)); st=" $(e2e_style $((g[0] + 2)) "$(rrow_y "$1")") "; [[ $st == *" bg=$VISUAL "* || $st == *" bg=$SELECTED "* ]]; }
save_count_is_1() { qb | python3 -c "import re, sys; sys.exit(0 if re.search(r'│[^│0-9]*1[^│0-9]*│', sys.stdin.read()) else 1)"; }   # 保存按钮组里的未保存数（图标后的 1）
rhl_rows() { local n out=""; for n in 1 2 3 4; do rhl "$n" && out+="$n,"; done; echo "$out"; }

# ---- 选中的里面夹着新增行：d 只取消标记，新增行留着（共用库，不保存）
SOLO=1 start; open_table t_sku; wait_for 8 settled
key g g; key d d; key j; key j; key d d; key k; key o; sleep 0.4; key Escape; sleep 0.4
check "（准备）第 1、4 行标记删除，第 3 行是新增行（+）" eval '[[ $(mark_of 1) == − && $(mark_of 3) == + && $(mark_of 4) == − ]]'
key g g; key m; key j; key j; key m; key j; key m
check "选中第 1、3（新增）、4 行" eval 'hl_is "1,3,4," && sel_is 3'
key d; sleep 0.4
check "d：第 1、4 行取消标记，新增行还在（+），保存计数 1，选中清空" eval 'm1=$(mark_of 1); m3=$(mark_of 3); m4=$(mark_of 4); [[ $m1 == 1 && $m3 == + && $m4 == 3 ]] && save_count_is_1 && sel_is 0 || { echo "  marks: [$m1] [$m3] [$m4]; qb: [$(qb)]"; false; }'

# ---- VISUAL：单击扩选、仍是 NORMAL 上下文
_e2e_kill; SOLO=1 start; open_table t_sku; wait_for 8 settled
key g g; key v
e2e_click 36 "$(row_y 4)"; sleep 0.4
check "VISUAL 里单击第 4 行的序号格：范围扩到第 1–4 行（「已选 4」）" eval 'mode_is VISUAL && hl_is "1,2,3,4," && sel_is 4'
e2e_keys Space; e2e_type b; sleep 0.6
e2e_keys Space; e2e_type b; sleep 0.6   # F19.5：第一个 SPC b 聚焦树，第二个才收起
check "VISUAL 里 SPC b（两次，F19.5）：normal 层照常，侧栏收起，仍是 VISUAL，选中不变" eval '[[ -z $(geom 0) ]] && mode_is VISUAL && sel_is 4'
e2e_keys Space; e2e_type b; sleep 0.6
check "再 SPC b：侧栏回来（展开时焦点到树上，选中不变）" eval '[[ -n $(geom 0) ]]'
key C-l; sleep 0.3; key Escape; key Escape; sleep 0.3

# ---- 结果 tab：能选、能 y，d 不做事、选中保留
_e2e_kill; start -C "$D/own"; key C-l
setsql "select id, v from t_sel where id <= 4 order by id"; run
key C-j; sleep 0.3
key m; key j; key m; sleep 0.3
check "结果 tab：m j m 选中第 1、2 行，序号格 selected 底" eval 'rhl_rows | grep -q "^1,2,$" || { echo "  [$(rhl_rows)]"; false; }'
key d; sleep 0.4
check "d：不做事（结果只读），选中保留" eval '[[ $(rhl_rows) == "1,2," ]]'
key y; sleep 0.6
check "y：剪贴板是 2 条记录的 JSON 数组（id 1、2）" eval 'clip | python3 -c "import json, sys; a = json.load(sys.stdin); print(\"ok\" if [r[\"id\"] for r in a] == [1, 2] else \"bad\")" | grep -q ok'
check "y 之后选中清空" eval '[[ -z $(rhl_rows) ]]'

# ---- 自动刷新：有选中或在 VISUAL 里时暂停（自建库，另一个连接改数据）
_e2e_kill; SOLO=1 start -C "$D/own"; open_table t_sel; wait_for 8 settled
pal ">自动刷新"; sleep 0.6; key Enter; sleep 0.6; e2e_type 2; sleep 0.3; key Enter; sleep 0.6
psql "$E2E_DB" -q -c "update t_sel set v = 'aaa' where id = 1" >/dev/null
check "（准备）自动刷新 2s：别的连接改了数据，几秒内表格自己刷新（v 变成 aaa）" eval 'wait_for 8 eval "e2e_text 35 80 \$(row_y 1) | grep -q aaa"'
key m
psql "$E2E_DB" -q -c "update t_sel set v = 'bbb' where id = 1" >/dev/null
sleep 6
check "有选中：自动刷新暂停，表格还是 aaa，选中还在" eval 'e2e_text 35 80 $(row_y 1) | grep -q aaa && sel_is 1'
key Escape
check "esc 清掉选中：马上恢复刷新，表格变成 bbb" eval 'wait_for 8 eval "e2e_text 35 80 \$(row_y 1) | grep -q bbb"'
key v
psql "$E2E_DB" -q -c "update t_sel set v = 'ccc' where id = 1" >/dev/null
sleep 6
check "在 VISUAL 里：同样暂停，表格还是 bbb" eval 'e2e_text 35 80 $(row_y 1) | grep -q bbb && mode_is VISUAL'
key Escape; key Escape
check "退出 VISUAL、清掉选中：恢复刷新，变成 ccc" eval 'wait_for 8 eval "e2e_text 35 80 \$(row_y 1) | grep -q ccc"'
e2e_done
