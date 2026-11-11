#!/usr/bin/env bash
# F22.7 收起的侧栏占 0 列，展开按钮在状态栏最左（specs/m22-menus-keys/task.md F22.7；tech-design §7.8「折叠后占 0 列」「状态栏」）
# 收起后第 0 列是 pane 的边框、状态栏最左是 » 块；点它（第 2 列）展开并聚焦树；展开后没有这一块；缩放中点它退出缩放并聚焦树。只读。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
DB=$D/shop.db; sqlite3 "$DB" <"$E2E_ROOT/testdata/seed/sqlite.sql"
bar() { e2e_text 1 40 "$(H)"; }
first_col() { e2e_text 1 1 2; }
focus_tree() { [[ $(focused) == 0 ]]; }

e2e_start "$E2E_BIN $DB"; wait_for 5 flag_is alternate_on 1; sleep 1
check "（准备）展开着：状态栏最左是 session 块，没有 »" eval '[[ $(bar) != " » "* && $(first_col) == "│" ]]'
key C-h; key C-h; sleep 0.3; key Space; key b; sleep 0.5
check "收起（树上 SPC b）：第 0 列是 pane 的边框，不是细栏" eval '[[ $(e2e_text 1 1 1) == "┌" && $(first_col) == "│" ]]' || e2e_plain | head -4 | cut -c1-60
check "状态栏最左是 » 块" eval '[[ $(bar) == " » "* ]]' || bar
e2e_click 2 "$(H)"; sleep 0.6
check "点状态栏第 2 列（»）：展开，焦点在树上，» 没了" eval 'focus_is 0 && [[ $(e2e_text 1 1 1) == "┌" && $(bar) != " » "* ]] && e2e_plain | grep -qF "tables"' || { focused; bar; }
key C-l; sleep 0.3; key Space; key z; sleep 0.5
check "缩放 pane：状态栏有 »" eval '[[ $(bar) == " » "* ]]' || bar
e2e_click 2 "$(H)"; sleep 0.6
check "点 »：退出缩放，焦点在树上，» 没了" eval 'focus_is 0 && [[ $(bar) != " » "* ]] && [[ $(e2e_panes | wc -l) -ge 2 ]]' || { focused; bar; }
e2e_done
