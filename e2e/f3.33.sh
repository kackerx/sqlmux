#!/usr/bin/env bash
# F3.33 新行里用 Tab 切换字段（specs/m3-console/task.md F3.33；tech-design §10.6、§6.4 newrow）
# 在还没保存的新行里编辑时 Tab / S-Tab 提交这一格、直接编辑下一个 / 上一个字段；已有的行照旧（F3.15 的 Tab 在浮层里选，见 f2.3.sh）。
# 自建库：整行要存进去。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
psql_n() { psql "$E2E_DB" -At -c "$1"; }
editing() { [[ $(bar) == *"-- editing $1 --"* ]] && mode_is INSERT || { echo "  not editing $1: $(e2e_text 60 160 "$(H)")"; false; }; }
col_x() { e2e_find "$1" "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }'; }
row_y() { echo $(( $(grid_y) + $1 )); }
cell() { local x; x=$(col_x "$1"); e2e_text "$x" $((x + 8)) "$(row_y "$2")" | sed 's/ *│.*//; s/ *▾.*//; s/ *$//; s/^ *//'; }
tab() { e2e_keys "$1"; sleep 0.4; }

start -C "$D/own"; open_table t_order; wait_for 8 settled

# ---- o 之后输入一格、Tab：直接在下一格编辑；S-Tab 回到上一格；C-s 整行存进去（task.md 验收）
key o; key l; key Enter; e2e_type 3; sleep 0.2
tab Tab
check "新行里 user_id 输入 3、Tab：提交这一格，直接编辑下一格 status" eval 'editing status && [[ $(cell user_id 2) == 3 ]]'
e2e_type done; sleep 0.3; tab Tab
check "status 输入 done、Tab：不在选项浮层里选，提交文字，编辑 amount" eval 'editing amount && [[ $(cell status 2) == done ]]'
e2e_type 5.5; sleep 0.2; tab BTab
check "S-Tab：提交 amount，回到 status 编辑" eval 'editing status && [[ $(cell amount 2) == 5.5 ]]'
tab Tab; key BSpace; e2e_type zz; sleep 0.3; tab Tab
check "amount 改成 zz 再 Tab：不合法，挡住，还在 amount（F3.21）" eval 'editing amount && e2e_plain | grep -q "不是有效的数字"'
clear_in; e2e_type 5.5; sleep 0.2; key Escape
key C-s; wait_for 8 eval '[[ $(qb) == *已保存* ]]'
check "C-s：整行存进去（user_id 3、status done、amount 5.50）" eval '[[ $(psql_n "select user_id, status, amount from t_order where id > 6000") == "3|done|5.50" ]]'

# ---- Tab 切到看不见的字段：视图跟着横向滚动（reviewer 在 F3.33 实测）
header() { e2e_text 35 159 "$(hy)"; }
check "准备：160 宽时最后一列 deleted_at 只露出一截" eval '[[ $(header) != *deleted_at* ]]'
key o; key Enter; for i in 1 2 3 4 5 6 7 8 9; do tab Tab; done
check "新行里从 id 连按 9 次 Tab：在编辑 deleted_at，视图滚到它完整可见" eval 'editing deleted_at && [[ $(header) == *deleted_at* ]] || { echo "  $(header)"; false; }'
key Escape; key r; key 0

# ---- 最后一个字段上 Tab、第一个字段上 S-Tab：提交并退出编辑，光标留在那一格，不绕回
key o; key '$'; key Enter; tab Tab
check "新行的最后一个字段 deleted_at 上 Tab：退出编辑，光标留在这一格（第 10 列）" eval 'mode_is NORMAL && [[ $(bar) == *" +,10 "* ]]'
key 0; key Enter; tab BTab
check "第一个字段 id 上 S-Tab：退出编辑，光标留在第 1 列" eval 'mode_is NORMAL && [[ $(bar) == *"+,1 "* ]]'

# ---- 已有的行照旧：Tab 不切字段
key r; key g g; key 3 l; key Enter; tab Tab
check "已有的行里编辑 amount 按 Tab：不切到下一格，还在编辑 amount" editing amount
key Escape

e2e_done
