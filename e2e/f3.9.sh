#!/usr/bin/env bash
# F3.9 格式化（specs/m3-console/task.md F3.9；tech-design §9.5）
# 格式化的结果由 sqlkit 的 golden 覆盖，首次耗时由单测覆盖；这里测真实终端里的 gq、撤销、配置加载和外部命令 formatprg。
# 默认布局，② console_1 的文字从 x 111 起，第 y 行是第 y−1 行文字。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
line() { e2e_text 111 159 $(($1 + 1)) | sed 's/ *$//'; }
lines() { local i; for ((i = 1; i <= $1; i++)); do line $i; done | tr '\n' '|'; }   # 前 N 行，用 | 连起来
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
SQL="select id,status from t_order where id<3"
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; }
conf() { rm -rf "$D/c"; mkdir -p "$D/c"; printf "$1" >"$D/c/config.toml"; }

start; key C-l; setsql "$SQL"
key g; key q
check "gq 是操作符（b8bf335）：只按 gq 时在等移动，缓冲区不变" eval '[[ $(lines 2) == "$SQL||" ]]'
key q; wait_for 3 eval '[[ -n $(line 2) ]]'
check "gqq：用内置的 sql-formatter 把当前语句排成多行（关键字默认小写）" eval '[[ $(lines 7) == "select|  id,|  status|from|  t_order|where|  id < 3|" ]] || { echo "  $(lines 7)"; false; }'
check "gq 之后光标在格式化文字的最后一行的第一个非空白字符（同 vim）" eval '[[ $(e2e_flag cursor_y) == 7 && $(e2e_flag cursor_x) == 112 ]]'
key u
check "整次格式化是一个撤销步：u 一次恢复原文" eval '[[ $(lines 2) == "$SQL||" ]] || { echo "  $(lines 2)"; false; }'
setsql $'select 1;\nselect  a,b from t;\nselect 3'
key j; key g; key q; key q; sleep 1
check "gqq 只格式化光标所在的语句：; 留在原处，前后两条不动" eval '[[ $(lines 7) == "select 1;|select|  a,|  b|from|  t;|select 3|" ]] || { echo "  $(lines 7)"; false; }'

setsql $'select 1;\n\nselect  a,b from t;\n\nselect 3'
key j; key j; key g; key q; key a; key p; sleep 1
check "gqap：格式化光标所在段落碰到的语句，不进 INSERT（a / p 不被当成插入）" eval '[[ $(lines 9) == "select 1;||select|  a,|  b|from|  t;||select 3|" && $(bar) == *" NORMAL " ]] || { echo "  $(lines 9)"; false; }'
key u; key g g; key V; key j; key g; key q; sleep 1
check "V j 再 gq：按选区（覆盖到的整行）格式化，退出 VISUAL" eval '[[ $(lines 4) == "select|  1;||select  a,b from t;|" && $(bar) == *" NORMAL " ]] || { echo "  $(lines 4)"; false; }'

# ---- 配置：keyword_case（§9.5），同时管类型名（dataTypeCase）
conf 'keyword_case = "upper"\n'; start -C "$D/c"; key C-l; setsql "select cast(id as integer) from t"
key g; key q; key q; sleep 1
check "keyword_case = \"upper\"：关键字和类型名大写，函数名（cast）照原样" eval '[[ $(line 1) == SELECT && $(line 2) == "  cast(id AS INTEGER)" && $(line 3) == FROM ]] || { echo "  $(lines 4)"; false; }'
conf 'keyword_case = "shout"\n'; e2e_start -C "$D/c" "$E2E_BIN"; wait_for 3 screen_has '[e2e-exit 1]'
check "keyword_case 写错：启动报错退出，指出这一项" eval 'screen_has "[e2e-exit 1]" && e2e_plain | tr -d "\n" | grep -q keyword_case'

# ---- formatprg：sh -c，stdin 进 stdout 出；失败时 toast 显示 stderr 第一行，5s 超时；缓冲区都不变
printf '#!/bin/sh\ntr a-z A-Z\n' >"$D/up"; chmod +x "$D/up"
conf "formatprg = \"$D/up\"\n"; start -C "$D/c"; key C-l; setsql "$SQL"
key g; key q; key q; sleep 1
check "formatprg：改用外部命令（这里是转大写）" eval '[[ $(line 1) == "SELECT ID,STATUS FROM T_ORDER WHERE ID<3" ]] || { echo "  $(line 1)"; false; }'
printf '#!/bin/sh\necho first failure line >&2\necho second >&2\nexit 3\n' >"$D/bad"; chmod +x "$D/bad"
conf "formatprg = \"$D/bad\"\n"; start -C "$D/c"; key C-l; setsql "$SQL"
key g; key q; key q; sleep 1
check "formatprg 失败：toast 显示 stderr 的第一行，缓冲区不变" eval 'e2e_text 1 160 44 | grep -q "first failure line" && ! e2e_text 1 160 44 | grep -q second && [[ $(lines 2) == "$SQL||" ]]'
printf '#!/bin/sh\nsleep 30\n' >"$D/slow"; chmod +x "$D/slow"
conf "formatprg = \"$D/slow\"\n"; start -C "$D/c"; key C-l; setsql "$SQL"
key g; key q; key q; sleep 4
check "formatprg 还没回来（5s 之内）：缓冲区不变，照常能用" eval '[[ $(lines 2) == "$SQL||" ]] && running'
wait_for 4 eval 'e2e_text 1 160 44 | grep -q 格式化超时'
check "5s 超时（脚本派生的 sleep 还在跑也一样）：toast「格式化超时（5s）」，缓冲区不变" eval 'e2e_text 1 160 44 | grep -q "格式化超时（5s）" && [[ $(lines 2) == "$SQL||" ]]'

# ---- 格式化结果回来时编辑器已经不在 NORMAL（按了 A）：这次结果丢掉
printf '#!/bin/sh\nsleep 2\ntr a-z A-Z\n' >"$D/late"; chmod +x "$D/late"
conf "formatprg = \"$D/late\"\n"; start -C "$D/c"; key C-l; setsql "$SQL"
key g; key q; key q; key A; sleep 3
check "结果回来时在 INSERT：丢掉结果，缓冲区不变，仍在 INSERT" eval '[[ $(lines 2) == "$SQL||" && $(bar) == *" INSERT " ]]'
key Escape

# ---- formatprg 没有输出：按失败处理，不把 SQL 删成空（b8bf335）
conf 'formatprg = "cat >/dev/null"\n'; start -C "$D/c"; key C-l; setsql "$SQL"
key g; key q; key q; sleep 1
check "formatprg 没有输出：toast「格式化失败：没有输出」，缓冲区不变" eval 'e2e_text 1 160 44 | grep -q "格式化失败：没有输出" && [[ $(lines 2) == "$SQL||" ]]'

# ---- 内置格式化也有 5s 超时（goja 跑大语句很慢，§9.5）：打断后 VM 还能继续用
start; key C-l; setsql "insert into t values $(for i in $(seq 400); do printf "(%d, 'v%d'), " $i $i; done)(0, 'end')"
key 0; t0=$(date +%s); key g; key q; key q
wait_for 9 eval 'e2e_text 1 160 44 | grep -q "格式化超时"'
check "400 行的 insert：约 5s 后 toast「格式化超时（5s）」，缓冲区不变" eval 'e2e_text 1 160 44 | grep -q "格式化超时（5s）" && (( $(date +%s) - t0 <= 8 )) && [[ $(line 1) == "insert into t values (1, '"'v1'"'), (2,"* && -z $(line 2) ]]'
setsql "$SQL"; key g; key q; key q; sleep 1.5
check "超时之后内置格式化照常能用" eval '[[ $(line 1) == select && $(line 4) == from ]] || { echo "  $(lines 4)"; false; }'

e2e_done
