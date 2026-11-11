#!/usr/bin/env bash
# F22.1 gj 退回表格后保留转置（specs/m22-menus-keys/task.md F22.1；tech-design §7.6「JSON 视图」）
# [grid] transpose = true 开表：gj 进 JSON 视图，esc / q / gj 三条路退回，都还是字段在左的转置布局；T 切回横置后同三条路退回还是横置。只读，用共用库。
. "$(dirname "$0")/lib.sh"
SOLO=1
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
printf '[grid]\ntranspose = true\n' >"$D/config.toml"
header() { e2e_text 35 159 "$(hy)"; }
in_json() { e2e_plain | sed -n 3p | grep -qF "JSON"; }
# 转置：表头是记录行号 1 2 3…，字段名（user_id、status）在左边一列
transposed() { ! in_json && [[ $(header) == *"│ 1 "*"│ 2 "* ]] && e2e_plain | noicon | grep -qE "│ +status +│"; }
across() { ! in_json && [[ $(header) == *"user_id"*"status"* ]]; }
# 进 JSON 视图、确认进了、按 $@ 退出
roundtrip() { key g j; wait_for 5 in_json || return 1; key "$@"; sleep 0.8; }

start -c "$D/config.toml"; open_table t_order; wait_for 8 settled
check "（准备）transpose = true：t_order 打开就是转置" transposed || header
for k in Escape q "g j"; do
  roundtrip $k
  check "转置的表 gj → ${k/ /}：退回后还是转置（表头 1 2 3…，字段名在左）" transposed || { header; e2e_plain | sed -n 3,8p | cut -c33-120; }
done
key T; sleep 0.6
check "（准备）T 切到横置" across || header
for k in Escape q "g j"; do
  roundtrip $k
  check "横置的表 gj → ${k/ /}：退回后还是横置（表头是字段名）" across || header
done
e2e_done
