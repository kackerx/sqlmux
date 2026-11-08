#!/usr/bin/env bash
# F10.7 表格选择模式（specs/m10-feedback3/task.md F10.7；tech-design §6.4「select」、§7.6「选择」）
# m 标记当前记录，v 进 VISUAL 扩选（esc / 再按 v 并入选中集），d 把选中的记录标记删除（C-s 才真删）、y 复制成 JSON 数组，
# 都不等 timeoutlen；序号格用 visual 底，chip 行加「N selected」；刷新清空选中，转置不清。高亮的画法由 golden 覆盖，这里看真实终端里的按键和结果。
# 自建库 t_sel（8 行，要真删）。
. "$(dirname "$0")/lib.sh"
SOLO=1
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -c "create table t_sel (id int primary key, v text)" -c "insert into t_sel select g, 'v' || g from generate_series(1, 8) g" >/dev/null
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
VISUAL=#2d3f76 SELECTED=#5c4a1e   # F12.12：正在选的范围是 visual 底，esc 确认 / m 标记的是 selected 底（黄褐）
row_y() { echo $(( $(grid_y) + $1 )); }
hl() { local st; st=" $(e2e_style 36 "$(row_y "$1")") "; [[ $st == *" bg=$VISUAL "* || $st == *" bg=$SELECTED "* ]]; }   # 第 N 行的序号格是 visual（范围）或 selected（确认）底
hl_c() { e2e_style 36 "$(row_y "$1")" | tr " " "\n" | grep "^bg=" | cut -d= -f2; }   # 第 N 行序号格的底色
hl_rows() { local n out=""; for n in $(seq 1 8); do hl "$n" && out+="$n,"; done; echo "$out"; }   # 高亮着的行："1,3,"
hl_is() { [[ $(hl_rows) == "$1" ]] || { echo "  highlighted: '$(hl_rows)', want '$1'"; false; }; }
mark_of() { e2e_text 35 38 "$(row_y "$1")" | sed 's/[ │]//g'; }                                   # 序号格的字：行号，标记删除的是 −
marks() { local n out=""; for n in $(seq 1 8); do out+="$(mark_of "$n"),"; done; echo "$out"; }
sel_is() { local want=$1; if [[ $want == 0 ]]; then [[ $(qb) != *已选* ]] || { echo "  query bar: '$(qb)'"; false; }; else qb_has "已选 $want "; fi; }   # 统计里「已选 N」（zh；en 是 N selected）
ids() { psql "$E2E_DB" -At -c "select string_agg(id::text, ',' order by id) from t_sel"; }
pj() { clip | python3 -c "$1"; }

start -C "$D/own"; open_table t_sel; wait_for 8 settled
check "（准备）8 行，没有选中" eval 'sel_is 0 && hl_is "" && [[ $(cnt) == 8 ]]'

# ---- m：标记当前记录，不进 VISUAL
key m
check "m：第 1 行的序号格是 selected 底（黄褐，不是 visual 蓝），chip 行「1 selected」，模式还是 NORMAL" eval 'hl_is "1," && [[ $(hl_c 1) == "$SELECTED" ]] && sel_is 1 && mode_is NORMAL'
key m
check "再 m：取消标记" eval 'hl_is "" && sel_is 0'
key m; key j; key j; key m
check "m j j m：第 1、3 行高亮，「2 selected」，第 2 行没有" eval 'hl_is "1,3," && sel_is 2'

# ---- d：立刻把选中的记录标记删除（不等 timeoutlen），清空选中
key d; sleep 0.3
check "d（不等超时）：第 1、3 行的序号格是 −（标记删除），选中清空，保存计数 2" eval '[[ $(marks) == "−,2,−,4,5,6,7,8," ]] && hl_is "" && sel_is 0 && qb | grep -q "2 "' || marks
key C-s; sleep 1.2
check "C-s：真删了 id 1 和 3（库里剩 2,4,5,6,7,8）" eval '[[ $(ids) == "2,4,5,6,7,8" ]] || { echo "  db: $(ids)"; false; }'
key R; wait_for 8 settled; sleep 0.4                                                  # 刷新：去掉「已保存 N 行」，统计区回来
check "刷新后表格 6 行" eval '[[ $(cnt) == 6 ]]'

# ---- v：VISUAL 扩选，esc 并入选中集，y 复制成 JSON 数组
key g g; key v
check "v：模式块 VISUAL，当前记录算选中（第 1 行高亮）" eval 'mode_is VISUAL && hl_is "1," && sel_is 1'
key 2 j
check "v 2j：第 1–3 行选中，VISUAL，范围是 visual 底，「3 selected」" eval 'mode_is VISUAL && hl_is "1,2,3," && [[ $(hl_c 2) == "$VISUAL" ]] && sel_is 3'
key Escape
check "esc：回到 NORMAL，三行仍选中（范围并入选中集，变成 selected 底）" eval 'mode_is NORMAL && hl_is "1,2,3," && [[ $(hl_c 2) == "$SELECTED" ]] && sel_is 3'
key y; sleep 0.6
check "y：剪贴板是 3 条记录的 JSON 数组（id 2、4、5，键按列顺序），选中清空、仍是 NORMAL" eval 'pj "import json, sys; a = json.load(sys.stdin); print(\"ok\" if isinstance(a, list) and [r[\"id\"] for r in a] == [2, 4, 5] and list(a[0]) == [\"id\", \"v\"] else \"bad\")" | grep -q ok && hl_is "" && sel_is 0 && mode_is NORMAL' || clip

# ---- 再按 v 退出 VISUAL；NORMAL 下 esc 清空选中
key g g; key v; key j; key v
check "v j v：VISUAL 退出，两行选中" eval 'mode_is NORMAL && hl_is "1,2," && sel_is 2'
key Escape
check "NORMAL 有选中时 esc：清空" eval 'hl_is "" && sel_is 0 && mode_is NORMAL'

# ---- m 和 VISUAL 合并（非连续）；全部已标记时 d 取消标记
key g g; key m; key j; key j; key v; key j; key Escape
check "m 标记第 1 行，再 j j v j esc：第 3、4 行并进来，共 3 行（1,3,4）" eval 'hl_is "1,3,4," && sel_is 3'
key d; sleep 0.3
check "d：1、3、4 三行都标记删除（序号格是 −），其余行号不变" eval '[[ $(marks) == "−,2,−,−,5,6,,," ]] || { echo "  marks: $(marks)"; false; }'
key g g; key m; key j; key j; key m; key j; key m; key d; sleep 0.3
check "对已全部标记的记录再选中、d：全部取消标记，计数清掉" eval '[[ $(marks) != *−* ]] || { marks; false; }'

# ---- 无选中时 dd 照旧；选中不等超时的反面：dd 仍要两下
key g g; key d d; sleep 0.3
check "没有选中时 dd 照旧：标记当前行删除" eval '[[ $(mark_of 1) == − ]]'
key d d; sleep 0.3
check "再 dd：取消标记" eval '[[ $(mark_of 1) != − ]]'

# ---- 刷新清空选中，转置不清
key m; key j; key m
check "（准备）两行选中" eval 'sel_is 2'
key T; sleep 0.6
check "转置：选中不清，chip 行仍是「2 selected」" eval 'sel_is 2'
key Escape; key g g; key 0; key v; key l; sleep 0.3
check "转置下清掉选中后 v l：VISUAL，选两条记录（列），「已选 2」" eval 'mode_is VISUAL && sel_is 2'
key Escape; key Escape; key T; sleep 0.5
key m; key R; wait_for 8 settled; sleep 0.4
check "R 刷新：选中清空" eval 'sel_is 0 && hl_is ""'
e2e_done
