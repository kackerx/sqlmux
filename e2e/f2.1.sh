#!/usr/bin/env bash
# F2.1 单元格编辑与待提交标记（specs/m2-edit/task.md F2.1；tech-design §10.1、§7.6、§7.8「查询条」）
# 行内输入框的位置、宽度由 golden 覆盖；这里测真实终端里的进入 / 提交方式、粘贴（bracketed paste）、
# 修改样式里的点状下划线（SGR 4:4 由 tmux 原样转发）和保存按钮上的计数。在自建库里做，F2.1 不写库。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
. "$(dirname "$0")/palette.sh"
WARN=#e0af68
SAVE=$(printf '\xef\x83\x87')                                                 # U+F0C7
col_x() { e2e_find "$1" "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }'; }
row_y() { echo $(( $(grid_y) + $1 )); }
cell_raw() { local x; x=$(col_x "$1"); e2e_text "$x" $((x + ${3:-10})) "$(row_y "$2")" | sed 's/ *│.*//; s/ *$//'; }
cell() { cell_raw "$@" | sed 's/^ *//'; }                                      # NAME N [W]：第 N 行 NAME 列的文字（数值右对齐，去掉前面的空格）
saves() { local q; q=$(qb); q=${q#*"$SAVE"}; q=${q%%[!\ 0-9]*}; echo $q; }    # 保存按钮上的修改数（没有就是空）
editing() { [[ $(bar) == *"-- editing $1 --"* ]] && mode_is INSERT; }
# marked NAME N：这一格是修改样式——文字是 warn 字，前面有 SGR 4:4（点状下划线）
marked() { local raw lead y; raw=$(cell_raw "$1" "$2"); lead=${raw%%[! ]*}; y=$(row_y "$2")
  style_has $(( $(col_x "$1") + ${#lead} )) "$y" fg=$WARN >/dev/null && e2e_cap -e | sed -n "${y}p" | grep -qE $'\e\\[4:4m(\e\\[[0-9;:]*m)*'"${raw#"$lead"}"; }
paste() { t set-buffer -- "$1"; t paste-buffer -p -t t; sleep 0.4; }          # TEXT：bracketed paste
typ() { e2e_type "$1"; sleep 0.3; }

start -C "$D/own"; open_table t_order; wait_for 8 settled
orig=$(psql "$E2E_DB" -At -c "select amount from t_order where id = 1")

# ---- 进入编辑：↵；输入替换全选的内容；esc 提交；修改样式、保存按钮上的计数
# 用 amount（不可空、没有默认值、不是枚举 / 布尔）：它没有 F2.3 的选项浮层，只看 F2.1 的输入框。
# numeric(10,2) 有 F3.21 的前置校验：输入合法的数（12.34、23.45），不合法的写法在 f3.21.sh
key 3 l; key Enter
check "↵ 进入编辑：状态栏 -- editing amount --，INSERT" editing amount
typ 12.34; key Escape
check "输入 12.34、esc 提交：回到 NORMAL，光标不动，格子是 12.34" eval 'mode_is NORMAL && pos_is 1,4 && [[ $(cell amount 1) == 12.34 ]]'
check "修改过的格：warn 字、点状下划线（SGR 4:4）" marked amount 1
check "保存按钮显示修改数 1" eval '[[ $(saves) == 1 ]] || { echo "  $(qb)"; false; }'
key Enter; typ "$orig"; key Escape
check "改回原值：修改删掉，样式消失，保存按钮上没有计数" eval '[[ $(cell amount 1) == "$orig" && -z $(saves) ]] && ! marked amount 1'

# ---- 翻页、改 WHERE 都保留修改
key Enter; typ 12.34; key Escape
key ']'; wait_for 8 settled; key '['; wait_for 8 settled
check "翻到第 2 页再回来：修改还在，仍是修改样式，计数 1" eval '[[ $(cell amount 1) == 12.34 && $(saves) == 1 ]] && marked amount 1'
key /; clear_in; typ "id < 3"; key Enter; wait_for 8 settled
check "改 WHERE 之后这一行还在页里：照样标记" eval 'qb_has "PAGE 1/1" && [[ $(cell amount 1) == 12.34 ]] && marked amount 1'
key /; clear_in; key Enter; wait_for 8 settled

# ---- 其他进入方式：i、双击；转置视图下编辑光标所在的格
key j; key i
check "i：同样进入编辑" editing amount
key Escape
e2e_dclick "$(col_x amount)" "$(row_y 3)"; sleep 0.4
check "双击 amount 第 3 行：进入这一格的编辑" eval 'editing amount && pos_is 3,4'
key Escape
key T; key Enter
check "转置视图下 ↵：编辑光标所在的格（状态栏是列名）" eval 'mode_is INSERT && [[ $(bar) == *"-- editing "*" --"* ]]'
key Escape; key T

# ---- 提交：点击别处（先提交，再把光标移过去，不进入编辑）、全局键、滚轮
key g g; key 0; key 3 l; key j; key Enter; typ 23.45
e2e_click "$(col_x amount)" "$(row_y 5)"; sleep 0.4
check "编辑中点击别的格：先提交（23.45 记下），光标移到 5,4，不进入编辑" eval 'mode_is NORMAL && pos_is 5,4 && [[ $(cell amount 2) == 23.45 && $(saves) == 2 ]]'
key Enter; typ 7.77; key C-p
check "编辑中按全局键 C-p：先提交，再打开命令面板" eval 'is_open && [[ $(saves) == 3 ]]'
key Escape
key Enter; typ 8.88; e2e_wheel 100 20 down; sleep 0.4
check "编辑中滚轮：先提交，再滚动" eval 'mode_is NORMAL && [[ $(saves) == 3 ]]'   # 同一格改成另一个值，仍是 3 处
e2e_wheel 100 20 up; sleep 0.3

# ---- 文字没变就什么都不记：原值是 NULL 的格进去再出来，不变成空字符串
key g g; key 0; key 7 l
check "note 第 1 行原值是 NULL（列窄，画成 <nul…）" eval '[[ $(cell note 1) == "<nul"* && -z $(psql "$E2E_DB" -At -c "select note from t_order where id = 1") ]]'
key Enter; key Escape; mode_is NORMAL >/dev/null || key Escape                 # 可空列有 F2.3 的选项浮层：esc 可能先关浮层
check "NULL 的格 ↵ 再 esc：什么都不记（仍是 <null>，计数不变）" eval '[[ $(cell note 1) == "<nul"* && $(saves) == 3 ]] && ! marked note 1'

# ---- 行标识列：没有主键、也没有全部列都非空的唯一索引的表只读；只有唯一索引的表可以编辑
key C-p; sleep 0.3; e2e_type "@t_log"; sleep 0.3; key Enter; wait_for 8 settled   # 当前 tab 有修改：新开 tab（F2.2）
key Enter
check "t_log 上 ↵：不进入编辑，toast 说明原因" eval 'mode_is NORMAL && e2e_text 40 160 $(( $(H) - 1 )) | grep -q "t_log 没有主键，也没有全部列都非空的唯一索引，只读"'
paste abc
check "t_log 上粘贴：同样不进入编辑" mode_is NORMAL
key C-p; sleep 0.3; e2e_type "@t_sku"; sleep 0.3; key Enter; wait_for 8 settled
key Enter
check "t_sku（只有唯一索引）：可以编辑" editing code
key Escape

# ---- 粘贴：NORMAL 下粘贴以粘贴的内容开始编辑；所有单行输入框都插到光标处，换行换成空格
paste $'pa\nste'
check "NORMAL 下粘贴（含换行）：进入编辑，内容是 pa ste" eval 'editing code && [[ $(cell code 1 14) == "pa ste "* ]]'   # 输入框盖住了右边的竖线
key Escape
key /; clear_in; typ "id  3"; key Left; key Left; paste "<"
check "WHERE 输入框里粘贴：插到光标处" eval '[[ $(where_in) == "id < 3" ]] || { echo "  $(where_in)"; false; }'
key Escape
pal "@t_"; paste $'sk\nu'
check "面板里粘贴：插到光标处，换行换成空格" input_is "@t_sk u"
key Escape

e2e_done
