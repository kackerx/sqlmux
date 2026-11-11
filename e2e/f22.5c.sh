#!/usr/bin/env bash
# F22.5 e 范围行放不下时横向滚动（specs/m22-menus-keys/task.md F22.5 e；tech-design §12「范围与前缀」，docs 7fbc0c5）
# 80 列：直接敲前缀 ; 跳到被截掉的那头，「SQL ;」看得见、左头 ‹N；删掉前缀回「所有」，所有看得见、右头 N›；Tab / S-Tab 来回切当前范围都看得见；
# 窗口放宽到 160 列，整行放得下、没有 ‹ ›；再缩回 80，当前范围还看得见；? 提示时没有标签高亮。只读。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

srow() { e2e_text "$(left)" $(( $(right) - 1 )) "$(tabs_y)"; }
seen() { local s; s=$(srow); [[ $s == *"$1"* ]] || { echo "  want [$1] in [$s]"; false; }; }
cur_seen() { [[ $(current_scope_text) == "$1" ]] || { echo "  current [$(current_scope_text)], want [$1]; row [$(srow)]"; false; }; }
# 高亮的那个范围标签（select 底不是面板底色的那段）的文字
current_scope_text() { local y l r x s=; y=$(tabs_y); l=$(left); r=$(right)
  for ((x = l + 1; x < r; x++)); do [[ $(e2e_style $x $y) == *bg=#9ece6a* ]] && s+=$(e2e_text $x $x $y); done; echo "$s" | sed 's/^ *//; s/ *$//'; }

start -x 80; pal
check "80 列：当前是「所有」，看得见、高亮；右头 N›" eval 'cur_seen 所有 && [[ $(srow) =~ [0-9]› ]]'
e2e_type '\;'; sleep 0.4   # tmux 把单独的 ; 当命令分隔符
check "直接敲 ;：跳到被截掉的 SQL，「SQL ;」看得见、高亮，左头 ‹N" eval 'cur_seen "SQL ;" && [[ $(srow) =~ ‹[0-9] ]]'
e2e_keys BSpace; sleep 0.4
check "删掉 ;：回「所有」，看得见、高亮" eval 'cur_seen 所有'
bad=; want=(会话 窗口·Pane 窗口 Tab 表 键 命令 SQL 所有)
for w in "${want[@]}"; do e2e_keys Tab; sleep 0.25; [[ $(current_scope_text) == "$w"* ]] || bad+="$w:[$(current_scope_text)] "; done
check "Tab 一路走一圈（会话 … SQL → 所有）：每一步高亮的范围都看得见" eval '[[ -z $bad ]] || { echo "  $bad"; false; }'
bad=; for w in SQL 命令 键 表 Tab 窗口 窗口·Pane 会话 所有; do e2e_keys BTab; sleep 0.25; [[ $(current_scope_text) == "$w"* ]] || bad+="$w:[$(current_scope_text)] "; done
check "S-Tab 倒着走一圈：每一步高亮的范围都看得见" eval '[[ -z $bad ]] || { echo "  $bad"; false; }'
e2e_type ":"; sleep 0.3
e2e_resize 160 45; sleep 0.6
check "放宽到 160 列：整行放得下（所有 … SQL ; 都在），没有 ‹ ›，「键 :」高亮" eval 'seen "所有" && seen "SQL ;" && [[ $(srow) != *‹* && $(srow) != *›* ]] && cur_seen "键 :"'
e2e_resize 80 45; sleep 0.6
check "缩回 80 列：「键 :」还看得见、高亮，又有 ‹ / › 标记" eval 'cur_seen "键 :" && [[ $(srow) == *‹* || $(srow) == *›* ]]'
e2e_keys BSpace; e2e_type "?"; sleep 0.4
check "敲 ?（特性提示，不是范围标签）：没有标签高亮，范围行照常画" eval '[[ -z $(current_scope_text) && -n $(srow) ]]'
e2e_done
