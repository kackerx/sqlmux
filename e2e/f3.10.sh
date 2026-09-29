#!/usr/bin/env bash
# F3.10 console 的补全（specs/m3-console/task.md F3.10；tech-design §9.7「补全的上下文判断」「交互」）
# 上下文判断由 sqlkit 的单测覆盖；这里测真实终端里的自动弹出、接受、esc、粘贴不弹，以及列在第一次用到时从 PG 取。
# 默认布局；共用库只读。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }        # 补全列表的 X Y W H（快速 SQL 里面板也是浮层，补全画在最上面）
items() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | sed 's/^ *//; s/ *$//'; done; }
names() { items | awk '{ print $1 }' | tr '\n' ' '; }
has() { [[ " $(names) " == *" $1 "* ]] || { echo "  candidates: $(names)"; false; }; }
line() { e2e_text 111 159 $(($1 + 1)) | sed 's/ *$//'; }
mode() { bar | awk '{ print $NF }'; }
fresh() { key Escape; key Escape; key g g; key d G; key i; }                      # 清空 console，进 INSERT

start; key C-l; key i
e2e_type "select * from t_o"; sleep 0.5
check "select * from t_o：自动弹出，候选里有 t_order（表）" eval 'has t_order && items | grep -q "^t_order  *表$"'
check "弹出时第一项就是 t_order" eval '[[ $(names) == "t_order "* ]]'
key Enter
check "↵ 接受：补成 t_order，列表关掉，仍在 INSERT" eval '[[ $(line 1) == "select * from t_order" && -z $(pop) && $(mode) == INSERT ]]'
key Enter
check "列表没开时 ↵ 照常换行" eval '[[ $(line 1) == "select * from t_order" && $(e2e_flag cursor_y) == 2 ]]'

# ---- 别名 + .：列出那张表的列（列在第一次用到时从 Meta 取，与打开表共用缓存）
fresh; e2e_type "select * from t_user u where u."; sleep 1
check "t_user u 之后输入 u.：前缀为空也弹，列出 t_user 的列（带类型和表名）" eval 'has id && has name && has email && items | grep -q "^email .*· t_user$" && ! has t_user'
e2e_type e; sleep 0.3
check "接着输入 e：按首字符过滤，只剩 e 开头的列" eval '[[ $(names) == "email " ]] || { echo "  $(names)"; false; }'
key Escape
check "console 里列表开着时 esc：关掉列表，同时退出 INSERT（同 nvim-cmp，ce3126e）" eval '[[ -z $(pop) && $(mode) == NORMAL ]]'

# ---- CTE 的名字算作表
fresh; e2e_type "with recent as (select 1) select * from rec"; sleep 0.5
check "CTE 名 recent 出现在候选里" has recent

# ---- schema. 之后只列那个 schema 的表；C-n 手动唤起
fresh; e2e_type "select * from agentable."; sleep 0.8
check "agentable. 之后：前缀为空也弹，只列 agentable 的表（bbbb2cd）" eval 'has agent && has goal && ! has t_order'
key Escape; key a; key C-n; sleep 0.5
check "esc 关掉之后，回到 INSERT 按 C-n 手动唤起" has agent

# ---- 别名按子查询的深度解析；光标在词中间时前缀是光标之前那段；未闭合的字符串里不补全
fresh; e2e_type "select * from t_order o where exists (select 1 from t_user o where o."; sleep 1
check "子查询里的 o 是 t_user：o. 列的是 t_user 的列" eval 'has email && ! has amount'
fresh; e2e_type "select * from t_our"; key Left; key Left; key C-n; sleep 0.5
check "t_o|ur 按 C-n：只按 t_o 过滤（t_order 在，t_user 不在）" eval 'has t_order && ! has t_user'
fresh; e2e_type "select * from t_user where name = 'ab"; key C-n; sleep 0.5
check "未闭合的字符串里按 C-n：不弹" eval '[[ -z $(pop) ]]'
fresh; e2e_type "select * from "; key Escape; key 2; key a; e2e_type t_o; sleep 0.5; key Enter; key Escape
check "2a 输入 t_o、接受 t_order、esc：接受的文字随次数重复" eval '[[ $(line 1) == "select * from t_ordert_order" ]] || { echo "  $(line 1)"; false; }'

# ---- 粘贴不弹补全（§11「粘贴」）
fresh; paste "select * from t_o"
check "粘贴 select * from t_o：不弹补全" eval '[[ $(line 1) == "select * from t_o" && -z $(pop) ]]'

# ---- 快速 SQL 也用同一套上下文（CompletionContext）
key Escape; key Escape
key C-p; sleep 0.3; e2e_type ";select  from t_order o"; sleep 0.3
for i in $(seq 15); do e2e_keys Left; done; sleep 0.2; e2e_type o.; sleep 1
check "快速 SQL 里 select o. from t_order o：o. 之后列出 t_order 的列" eval 'has status && has amount && items | grep -q "^amount .*· t_order$"'
key Escape; key Escape

e2e_done
