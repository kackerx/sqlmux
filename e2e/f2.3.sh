#!/usr/bin/env bash
# F2.3 选项浮层：NULL、DEFAULT、原值、布尔、枚举（specs/m2-edit/task.md F2.3；tech-design §10.2）
# 浮层的样子由 golden 覆盖；这里在自建库里按键、点击，看出现哪些选项、应用之后格子和库里是什么。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
. "$(dirname "$0")/palette.sh"
SELECT=#364a82
psql_n() { psql "$E2E_DB" -At -c "$1"; }
col_x() { e2e_find "$1" "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }'; }
row_y() { echo $(( $(grid_y) + $1 )); }
cell() { local x; x=$(col_x "$1"); e2e_text "$x" $((x + ${3:-10})) "$(row_y "$2")" | sed 's/ *│.*//; s/^ *//; s/ *$//'; }
opts_box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }     # 选项浮层的 X Y W H（没有就是空）
opts() { local g y; g=($(opts_box)); [[ -n ${g[0]} ]] || return 0              # 每一项的文字，用 / 连起来
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | tr -s ' ' | sed 's/^ //; s/ $//'; done | tr '\n' /; }
opt_y() { local g y; g=($(opts_box)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do [[ $(e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y) == *"$1"* ]] && { echo $y; return; }; done; }
picked() { local g y; g=($(opts_box)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do style_has $((g[0] + g[2] - 2)) $y bg=$SELECT >/dev/null && e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | tr -s ' ' | sed 's/^ //; s/ $//'; done; }
saves() { local q s; s=$(printf '\xef\x83\x87'); q=$(qb); q=${q#*"$s"}; q=${q%%[!\ 0-9]*}; echo $q; }
goto() { key g g; key 0; [[ $2 -gt 1 ]] && key $(($2 - 1)) j; local n; n=$(e2e_text 35 $(( $(col_x "$1") - 1 )) $(hy) | tr -cd '│' | wc -m); n=$((n - 1)); [[ $n -gt 0 ]] && key "$n" l; true; }   # COL ROW：光标移到这一格（前面有几条竖线就是第几列）

start -C "$D/own"; open_table t_order; wait_for 8 settled

# ---- 按列属性给选项（§10.2 的表）：顺序是 布尔 / 枚举 → ∅ NULL → DEFAULT → ↺ 原值
goto status 1; key Enter
check "status（枚举，不可空，有默认值）：全部枚举值 + DEFAULT，没有 NULL，不预先选中" eval '[[ $(opts) == "pending 值/queued 值/running 值/done 值/failed 值/DEFAULT/" && -z $(picked) ]] || { echo "  $(opts)"; false; }'
e2e_type d; sleep 0.3
check "输入 d：枚举值随输入过滤（没有 running），DEFAULT 不参与过滤，仍在后面" eval '[[ $(opts) != *running* && $(opts) == *"done 值"* && $(opts) == *"/DEFAULT/" ]] || { echo "  $(opts)"; false; }'
key BSpace; e2e_type running; sleep 0.2; key Enter                               # 改回原文再提交：不记修改（esc 会把 d 记下来）
check "改回原文 running、没选中就 ↵：不记修改" eval 'mode_is NORMAL && [[ -z $(saves) ]]'
goto paid 1; key Enter
check "paid（布尔，可空，没有默认值）：true / false + ∅ NULL，没有 DEFAULT" eval '[[ $(opts) == "true 值/false 值/∅ NULL/" ]] || { echo "  $(opts)"; false; }'
key Escape
goto note 10; key Enter
check "note（文本，可空，没有默认值）：只有 ∅ NULL" eval '[[ $(opts) == "∅ NULL/" ]] || { echo "  $(opts)"; false; }'
key Escape
goto amount 1; key Enter
check "amount（数值，不可空，没有默认值）：没有任何选项，不弹浮层" eval '[[ -z $(opts_box) ]] && mode_is INSERT'
key Escape

# ---- 没有选中时 ↵ 提交文字，不会误写成 NULL；C-n / C-p 移动、到头绕回，↑ / ↓ 也移动；↵ 应用选中项
goto note 10; key Enter; e2e_type zz; sleep 0.2; key Enter
check "note 输入 zz、没选中就 ↵：提交文字 zz（不是 NULL）" eval 'mode_is NORMAL && [[ $(cell note 10) == zz && $(saves) == 1 ]]'
key Enter
check "再编辑已修改的格：选项多了 ↺ 原值，排在最后" eval '[[ $(opts) == "∅ NULL/↺ 原值/" ]] || { echo "  $(opts)"; false; }'
key C-n; check "C-n：选中第一项 ∅ NULL" eval '[[ $(picked) == "∅ NULL" ]]'
key C-n; key C-n; check "C-n 到头绕回（原值 → NULL）" eval '[[ $(picked) == "∅ NULL" ]]'
key C-p; check "C-p 绕到最后一项 ↺ 原值" eval '[[ $(picked) == "↺ 原值" ]]'
key Up;  check "↑ 同样移动（→ ∅ NULL）" eval '[[ $(picked) == "∅ NULL" ]]'
key Down; check "↓ 同样移动（→ ↺ 原值）" eval '[[ $(picked) == "↺ 原值" ]]'
key Enter
check "↵ 应用 ↺ 原值：删掉这条修改并退出（note 10 回到原文 note…，计数清零）" eval 'mode_is NORMAL && [[ $(cell note 10) == note* && $(cell note 10) != zz && -z $(saves) ]]'

# ---- 鼠标：悬停只高亮、不改选中项；点击立即应用；点输入框右端的 ▾ 收起 / 展开
goto paid 2; key Enter
y=$(opt_y "∅ NULL"); g=($(opts_box)); e2e_move $((g[0] + 3)) "$y"; sleep 0.3
check "悬停 ∅ NULL：只高亮成 row 底，不是选中的 select 底（§10.2，docs c6623f6）" eval 'style_has $((g[0] + g[2] - 2)) $y bg=#292e42 && [[ -z $(picked) ]]'
key Enter
check "悬停之后 ↵：仍是提交文字（paid 2 不变，没有写成 NULL）" eval 'mode_is NORMAL && [[ $(cell paid 2) == t && -z $(saves) ]]'
e2e_move 100 30
goto status 3; key Enter
y=$(opt_y "done"); g=($(opts_box)); e2e_click $((g[0] + 3)) "$y"; sleep 0.4
check "点击枚举值 done：立即应用，退出编辑，status 3 是 done（修改样式）" eval 'mode_is NORMAL && [[ $(cell status 3) == done && $(saves) == 1 ]]'
goto paid 1; key Enter
v=$(e2e_find ▾ $(row_y 1) | tr ' ' '\n' | awk -v x=$(col_x paid) '$1 >= x { print; exit }'); e2e_click "$v" "$(row_y 1)"; sleep 0.3
check "点输入框右端的 ▾：收起浮层，仍在编辑" eval '[[ -z $(opts_box) ]] && mode_is INSERT'
e2e_click "$v" "$(row_y 1)"; sleep 0.3
check "再点 ▾：展开" eval '[[ -n $(opts_box) ]]'
key C-n; key Enter
check "C-n 选 true、↵：paid 1（原值 f）写成 true" eval 'mode_is NORMAL && [[ $(cell paid 1) == t* && $(saves) == 2 ]]'
goto paid 5; key Enter; key C-n; key C-n; key Enter
check "原值 f 的格选 false：和原值相同，不记修改" eval 'mode_is NORMAL && [[ $(cell paid 5) == f* && $(saves) == 2 ]]'
goto status 4; key Enter; key C-p; key Enter
check "C-p 选中最后的 DEFAULT、↵：格子显示 <default>（列窄，<defa…）" eval '[[ $(cell status 4) == "<defa"* && $(saves) == 3 ]]'

# ---- 面板里的「设为 NULL」：作用于光标所在的格
goto note 10
pal ">设为 NULL"; key Enter
check "面板执行「设为 NULL」：note 10 显示 <null>（修改样式），计数 +1" eval '! is_open && [[ $(cell note 10) == "<nul"* && $(saves) == 4 ]] || { echo "  $(cell note 10) $(saves)"; false; }'
goto amount 1; pal ">设为 NULL"; key Enter
check "不可空的 amount 上「设为 NULL」：不做事" eval '[[ $(cell amount 1) == 1.99 && $(saves) == 4 ]]'

# ---- 保存：DEFAULT 写成关键字，NULL 写成 NULL
key C-s; wait_for 8 eval '[[ $(qb) == *已保存* ]]'
check "C-s：status 4 写成默认值 pending，note 10 写成 NULL，paid 1 写成 true，status 3 写成 done" eval '[[ $(psql_n "select status from t_order where id = 4") == pending && -z $(psql_n "select note from t_order where id = 10") && $(psql_n "select paid from t_order where id = 1") == t && $(psql_n "select status from t_order where id = 3") == done ]]'

e2e_done
