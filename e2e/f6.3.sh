#!/usr/bin/env bash
# F6.3 持久化（specs/m6-config/task.md F6.3；tech-design §14「state.json」）
# 写一轮读回来、写到一半被杀掉原文件完好有单测；这里在真实进程上做：执行 WHERE、收藏、快速 SQL、开表之后 kill -9，
# state.json 仍是完整的 JSON，里面有 §14 列的四类内容；用同一份 state 重启，历史 / 收藏 / 最近使用都在。只读，用共用库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
SJ=$D/sqlmux/state.json
hist() { local g y; g=($(e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }')); [[ -n ${g[0]} ]] || return 0
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | sed 's/^ *//; s/ *$//'; done; }

start -S "$D"; open_table t_order; wait_for 8 settled
key /; e2e_type "id < 50"; key Enter; wait_for 8 settled
key /; key C-r; sleep 0.4; key C-f; key Escape; key Escape; key Escape
e2e_keys C-p; sleep 0.3; e2e_type ";select 1"; key Enter; sleep 1; key Escape; key Escape
sleep 0.6
pid=$(pgrep -P "$(t display -p -t t '#{pane_pid}')" | head -1)
check "（准备）找到 sqlmux 的进程" eval '[[ -n $pid ]]'
kill -9 "$pid"; sleep 0.5
check "kill -9 之后：state.json 还是完整的 JSON，权限 0600" eval 'python3 -c "import json,sys; json.load(open(sys.argv[1]))" "$SJ" && [[ $(fmode "$SJ") == 600 ]]'
check "里面有 §14 列的四类：recent（最近使用）、tables（WHERE 历史与收藏）、sql（快速 SQL 历史）、queries（query 的连接）" eval 'python3 - "$SJ" <<PY
import json, sys
d = json.load(open(sys.argv[1]))
t = d["tables"]["doraemon/public.t_order"]
assert any(h["where"] == "id < 50" for h in t["history"]), t
assert t["favorites"], t
assert d["recent"] and d["sql"], d
assert "queries" in d, d
PY'
e2e_stop
start -S "$D"; open_table t_order; wait_for 8 settled
key /; key C-r; sleep 0.5
check "用同一份 state 重启：C-r 里有收藏的 id < 50（收藏在前）" eval 'hist | head -3 | grep -q "id < 50"'
key Escape; key Escape; key Escape
e2e_keys C-p; sleep 0.5
check "命令面板的最近使用：t_order 在前" eval 'list | head -2 | grep -q "t_order"'
key Escape

e2e_done
