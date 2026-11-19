#!/usr/bin/env bash
# F22.1 JSON 视图开着时关 tab / 退出，转置照样留下（specs/m22-menus-keys/task.md F22.1；tech-design §7.6「JSON 视图」、§14 last.toml）
# 转置的表 gj 进 JSON 视图后：关 tab、tab.reopen 重开，还是转置；:qa 退出、同一份 state 再起，last.toml 恢复的 tab 还是转置。只读，用共用库。
. "$(dirname "$0")/lib.sh"
SOLO=1
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
# JSON 视图是只读的 vim：x 和 : 归编辑器，所以关 tab 和退出绑到全局的 F 键上
printf '[keys.global]\n"<F4>" = "tab.reopen"\n"<F5>" = "tab.close"\n"<F6>" = "quit"\n' >"$D/k.toml"
mkdir -p "$D/st"
header() { e2e_text 35 159 "$(hy)"; }
in_json() { e2e_plain | sed -n 3p | grep -qF "JSON"; }
transposed() { ! in_json && [[ $(header) == *"│ 1 "*"│ 2 "* ]] && e2e_plain | noicon | grep -qE "│ +status +│"; }

# ---- x 关 tab → tab.reopen
start -K "$D/k.toml"; open_table t_order; wait_for 8 settled
key T; sleep 0.5; key g j; wait_for 5 in_json
check "（准备）T 转置后 gj：在 JSON 视图里" in_json
key F5; sleep 0.6; key F4; wait_for 8 settled; sleep 0.5
check "JSON 视图开着时关 tab（tab.close）、tab.reopen 重开：回到表格，还是转置" transposed || { header; e2e_plain | sed -n 1,8p | cut -c33-130; }

# ---- quit → 同一份 state 再起
start -K "$D/k.toml" -S "$D/st"; open_table t_order; wait_for 8 settled
key T; sleep 0.5; key g j; wait_for 5 in_json
key F6; wait_for 8 exited; sleep 0.5
check "（准备）JSON 视图开着时退出（quit），last.toml 写下了" eval 'find "$D/st" -name last.toml | grep -q .' || find "$D/st"
start -S "$D/st"; wait_for 8 settled; sleep 1
check "再起：恢复的 t_order 是转置（不是横置，也不在 JSON 视图）" transposed || { header; e2e_plain | sed -n 1,8p | cut -c33-130; }
e2e_done
