#!/usr/bin/env bash
# F1.14 补全：默认选中、循环切换、智能回车（specs/m1-browse/task.md F1.14；tech-design §9.7「交互」）
# 在真实终端里按键，看选中了哪一项、↵ 是接受还是执行。选中项只能从底色读出（select 底）。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
SELECT=#364a82
# boxN N：第 N 个浮层的 X Y W H；WHERE 的补全列表是第 1 个，面板里的是第 2 个（面板本身是第 1 个）
boxN() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | sed -n "${1}p"; }
# items N：第 N 个浮层里每一行「候选|底色」，底色取行尾的留白
items() { local g y; g=($(boxN "$1")); [[ -n ${g[0]} ]] || return 0
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do
    printf '%s|%s\n' "$(e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | awk '{ print $1 }')" "$(e2e_style $((g[0] + g[2] - 2)) $y | grep -oE 'bg=#[0-9a-f]+' | cut -c4-)"; done; }
names() { items "${1:-1}" | cut -d'|' -f1 | tr '\n' ' '; }
sel() { items "${1:-1}" | awk -F'|' -v c=$SELECT '$2 == c { print $1 }'; }
sel_is() { [[ $(sel) == "$1" ]] || { echo "  selected '$(sel)', want $1 ($(names))"; false; }; }
typ() { e2e_type "$1"; sleep 0.4; }
edit() { key /; clear_in; }

start; open_table t_order; wait_for 8 settled

# ---- 弹出时第一项就是选中的；↵ 接受选中项
edit; typ sta
check "WHERE 输入 sta：弹出时 status 已经选中（select 底）" sel_is status
key Enter
check "↵ 接受：输入框是 status，列表关闭，仍在输入" eval '[[ $(where_in) == status && -z $(boxN 1) ]] && mode_is INSERT'

# ---- Tab / C-n / ↓ 下一项，S-Tab / C-p / ↑ 上一项，到头绕回
edit; typ n
check "输入 n：候选 note、not、null，第一项 note 选中" eval '[[ $(names) == "note not null " ]] && sel_is note'
key Tab;  check "第一次 Tab 就移到第二项 not" sel_is not
key Tab;  key Tab
check "最后一项 null 再 Tab：回到第一项 note" sel_is note
key BTab; check "第一项 S-Tab：到最后一项 null" sel_is null
key C-n;  check "C-n 同 Tab（null → note，绕回）" sel_is note
key C-p;  check "C-p 同 S-Tab（note → null，绕回）" sel_is null
key Down; check "↓ 同 Tab" sel_is note
key Up;   check "↑ 同 S-Tab" sel_is null

# ---- 候选的第一个字符必须和输入的第一个字符相同（不分大小写），其余照 fzf
edit; typ at
check "at：只有 a 开头的候选（amount），没有 status、created_at" eval '[[ $(names) == *amount* && $(names) != *status* && $(names) != *created_at* ]] || { echo "  $(names)"; false; }'
edit; typ Am
check "Am：首字符不分大小写，仍出现 amount" eval '[[ $(names) == *amount* ]] || { echo "  $(names)"; false; }'
edit; typ STA
check "STA：补全整串不分大小写（不用 smartcase，docs ab3c0b8），出现 status" eval '[[ $(names) == *status* ]] || { echo "  $(names)"; false; }'

# ---- 智能回车：接受之后文字不变时照常执行；列表没弹出时照常执行
edit; typ "status is not null"
check "输入 status is not null：列表开着，选中的就是 null" sel_is null
key Enter; wait_for 8 settled
check "↵：文字不变，直接执行（NORMAL，6000 行）" eval 'mode_is NORMAL && [[ $(where_in) == "status is not null" && $(cnt) == 6000 ]]'
edit; typ "status is not NULL"; key Enter; wait_for 8 settled
check "NULL 和候选 null 只差大小写：也算文字不变，直接执行，不改成小写" eval 'mode_is NORMAL && [[ $(where_in) == "status is not NULL" && $(cnt) == 6000 ]]'
pal ";select 42 as x"; sleep 0.3
check "快速 SQL 输入 select 42 as x：x 没有 x 开头的候选，列表不弹" eval '[[ -z $(boxN 2) ]]'
key Enter; wait_for 8 eval 'e2e_plain | grep -q " 只读"'
check "↵ 直接执行，语句不变" eval 'input_is ";select 42 as x" && e2e_plain | grep -q "│ 1 │ 42"'
clear_input; e2e_type ";select * from tord"; sleep 0.5
check "快速 SQL 输入 tord：仍出现 t_order（首字符 t 相同，其余模糊）" eval '[[ $(names 2) == *"t_order "* ]] || { echo "  $(names 2)"; false; }'
clear_input; e2e_type ";SELECT * FROM T_OR"; sleep 0.5
check "快速 SQL 大写输入 T_OR：同样出现 t_order" eval '[[ $(names 2) == *"t_order "* ]] || { echo "  $(names 2)"; false; }'
key Escape; key Escape

e2e_done
