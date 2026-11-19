#!/usr/bin/env bash
# F22.7 收起的侧栏：第 0 列不再是拖动柄、» 的悬停和 80 列（specs/m22-menus-keys/task.md F22.7；tech-design §7.8「折叠后占 0 列」「状态栏」）
# 收起后在第 1 列（pane 的左边框）上按下拖到第 20 列、单击：侧栏不出来，展开后还是 32 列；悬停 » 是 select 底；80 列下 » 和 session 块都在。SQLite 文件，只读。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
DB=$D/shop.db; sqlite3 "$DB" <"$E2E_ROOT/testdata/seed/sqlite.sql"
L() { e2e_keys Space; e2e_type "$1"; sleep 0.4; }

e2e_start "$E2E_BIN $DB"; wait_for 5 flag_is alternate_on 1; sleep 1
check "（准备）侧栏 32 列" eval '[[ $(geom 0 | cut -d" " -f3) == 32 ]]'
key C-h; sleep 0.3; L b   # 焦点在树上：一次 SPC b 就收起（F19.5）
check "（准备）收起" folded
e2e_down 1 20; e2e_drag_to 10 20; e2e_drag_to 20 20; e2e_up 20 20; sleep 0.5
check "在第 1 列（① 的左边框）按下拖到第 20 列：侧栏没出来，① 还从第 1 列开始" eval 'folded && [[ $(geom 1 | cut -d" " -f1) == 1 ]]' || e2e_panes
e2e_click 1 20; sleep 0.4
check "单击第 1 列：也没有展开" folded
unfold_click; sleep 0.5
check "点 » 展开：侧栏还是 32 列（没被那次拖动改宽）" eval '[[ $(geom 0 | cut -d" " -f3) == 32 ]]' || e2e_panes
L b
check "（准备）焦点在树上 SPC b 再收起" folded
e2e_move 2 "$(H)"; sleep 0.4
check "悬停 »：select 底（#364a82），移开后不是" eval '[[ $(e2e_style 2 "$(H)") == *bg=#364a82* ]] && { e2e_move 60 20; sleep 0.4; [[ $(e2e_style 2 "$(H)") != *bg=#364a82* ]]; }' || e2e_style 2 "$(H)"
e2e_resize 80 24; sleep 0.6
check "80x24：状态栏最左 »，后面是 session 块（shop）" eval '[[ $(e2e_text 1 30 24) == " » "*shop* ]]' || e2e_text 1 80 24
e2e_done
