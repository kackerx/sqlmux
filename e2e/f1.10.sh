#!/usr/bin/env bash
# F1.10 ; 直接打开快速 SQL（specs/m1-browse/task.md F1.10；tech-design §12、§6.8）
# 注意 tmux 把单独的 ; 参数当命令分隔符，按键要写成 '\;'。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
semi() { e2e_keys '\;'; sleep 0.4; }
in_sql() { is_open && input_is ";" && [[ $(e2e_text $(($(left) + 2)) $(($(right) - 2)) $(tabs_y)) == *"SQL ;"* ]]; }

start; open_table t_order; wait_for 8 settled
semi
check "焦点在表格上按 ;：面板打开，输入预填 ;（SQL 范围）" in_sql
e2e_type "select 1"; sleep 0.3; key Enter
check "接着输入 select 1 按 ↵：执行，结果 1 行" eval 'wait_for 8 eval "e2e_plain | grep -q \"1 行 · .* · 只读\""'
key Escape
key C-h; semi
check "焦点在树上按 ;：同样打开在 SQL 范围" eval 'in_sql && [[ $(focused) == "" || $(focused) == 0 ]]'
key Escape; key C-l
key Space %; semi
check "焦点在空 pane 上按 ;：同样打开在 SQL 范围" in_sql
key Escape; key C-h
key /; semi
check "在 WHERE 输入框里 ; 就是字符，不打开面板" eval '! is_open && [[ $(e2e_text 35 60 2) == *"WHERE ; "* ]] && mode_is INSERT'
key Escape

e2e_done
