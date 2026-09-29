#!/usr/bin/env bash
# F3.21 字段的前置校验（specs/m3-console/task.md F3.21；tech-design §10.7、§10.1）
# 每类写法的合法 / 不合法由 app/edit.go 的单测和集成测试（select '<写法>'::<类型>）覆盖，提示框与浮层的画法由 golden 覆盖。
# 这里在真实终端里测：波浪下划线（SGR 4:3 和下划线色由 tmux 原样转发）、不合法时每条离开编辑的途径都被挡住、esc / C-c 放弃、
# 没改过的文字不检查、提示框和选项浮层叠放不出屏幕；放行的几种写法真的存进 PG（前置校验只是提示，以数据库为准）。
# 自建库：t_types 放 seed 里没有的类型；连接的 TimeZone 是 Asia/Shanghai，1900 年的 timestamptz 带秒级偏移 +08:05:43。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -c "create table t_types (id int primary key, i int, r real, d double precision, u uuid, js jsonb, tm time, ts timestamptz)" \
  -c "insert into t_types values (1, 1, 1, 1, null, null, '10:00', '1900-01-01 00:00:00+00')"
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&timezone=Asia/Shanghai"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
. "$(dirname "$0")/palette.sh"
psql_n() { psql "$E2E_DB" -At -c "$1"; }
col_x() { e2e_find "$1" "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }'; }
row_y() { echo $(( $(grid_y) + $1 )); }
cell() { local x; x=$(col_x "$1"); e2e_text "$x" $((x + ${3:-10})) "$(row_y "$2")" | sed 's/ *│.*//; s/ *$//; s/^ *//'; }   # NAME N [W]
editing() { [[ $(bar) == *"-- editing $1 --"* ]] && mode_is INSERT || { echo "  not editing $1: $(e2e_text 60 160 "$(H)")"; false; }; }
hint() { e2e_plain | grep -oE '│ (不是有效|超出)[^│]*│' | head -1 | sed 's/^│ //; s/ *│$//'; }   # 提示框里的字
hint_is() { [[ $(hint) == "$1" ]] || { echo "  hint: '$(hint)', want '$1'"; false; }; }
# wavy Y：这一行有 error 色的波浪下划线（SGR 4:3，下划线色 58;2;247;118;142 即 #f7768e）
wavy() { local l; l=$(e2e_cap -e | sed -n "${1}p"); [[ $l == *$'\e[4:3m'* && $l == *$'\e[58;2;247;118;142m'* ]]; }
edit() { key Enter; e2e_type "$1"; sleep 0.3; }                       # 进入编辑，输入的第一个字替换全选的内容
boxes() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }'; }      # 浮层的 X Y W H，一行一个
hint_box() { local b; while read -r b; do set -- $b; [[ $(e2e_text $(($1 + 2)) $(($1 + $3 - 2)) $(($2 + 1))) =~ 不是有效|超出 ]] && { echo "$b"; return; }; done <<<"$(boxes)"; }

start -C "$D/own"; open_table t_order; wait_for 8 settled

# ---- amount（numeric(10,2)）输入 10d：波浪下划线和提示框，↵ 不提交；改成 10 后 ↵ 提交；esc 回到原值（task.md F3.21 验收）
key 3 l; edit 10d
check "amount 输入 10d：提示「不是有效的数字」，输入框的字有 error 色的波浪下划线" eval 'editing amount && hint_is 不是有效的数字 && wavy $(row_y 1)'
key Enter
check "↵：不提交，留在编辑里，提示还在" eval 'editing amount && hint_is 不是有效的数字 && [[ $(cell amount 1) == 10d* ]]'
e2e_click "$(col_x status)" "$(row_y 5)"; sleep 0.3
check "点别的格：不提交，光标不动" eval 'editing amount && pos_is 1,4 && hint_is 不是有效的数字'
e2e_wheel 100 20 down; sleep 0.3
check "滚轮：不提交、不滚动" eval 'editing amount && [[ $(e2e_text 35 45 $(row_y 1) | tr -d " ") == 1│1* ]]'
key C-p
check "C-p：不开面板" eval 'editing amount && ! palette_open'
key C-s
check "C-s：不保存" eval 'editing amount && [[ $(qb) != *已保存* && $(psql_n "select amount from t_order where id = 1") == 1.99 ]]'
key C-h
check "C-h（换焦点）：焦点不动" eval 'editing amount && [[ $(focused) == 1 ]]'
key BSpace
check "退格成 10：提示和波浪线都没了" eval 'editing amount && [[ -z $(hint) ]] && ! wavy $(row_y 1)'
key Enter
check "改成 10 后 ↵：提交，回到 NORMAL，格子是 10" eval 'mode_is NORMAL && [[ $(cell amount 1) == 10 ]]'
edit x; key Escape
check "再进编辑输入 x、esc：放弃这一次，回到进入之前的 10（改过的值，不是库里的 1.99）" eval 'mode_is NORMAL && [[ $(cell amount 1) == 10 && -z $(hint) ]]'
edit y; key C-c
check "C-c 等同 esc：回到 10，不弹退出提示" eval 'mode_is NORMAL && [[ $(cell amount 1) == 10 ]] && ! screen_has "再按一次"'
key j; key Enter; key BSpace; sleep 0.3
check "清空非空的数字格：提示「不是有效的数字」（写 NULL 要用选项）" eval 'editing amount && hint_is 不是有效的数字'
key Escape
check "esc：回到原值 2.99" eval 'mode_is NORMAL && [[ $(cell amount 2) == 2.99 ]]'
edit "$(printf '5\xe3\x80\x80')"
check "5 后面跟全角空格 U+3000：不算空白，提示「不是有效的数字」" hint_is 不是有效的数字
key Escape
edit 123456789
check "123456789：提示「超出 numeric(10,2) 的范围」" hint_is "超出 numeric(10,2) 的范围"
clear_in; e2e_type 99999999.994; sleep 0.3
check "99999999.994：按 2 位舍入后是 99999999.99，放行" eval '[[ -z $(hint) ]]'
key Enter; key C-s; wait_for 8 eval '[[ $(qb) == *已保存* ]]'
check "保存：两处修改都进了库（10.00、99999999.99）" eval '[[ $(psql_n "select amount from t_order where id in (1, 2) order by id" | tr "\n" " ") == "10.00 99999999.99 " ]]'

# ---- boolean（paid）：唯一前缀放行，其他的挡住
key 1 l; edit maybe
check "paid 输入 maybe：提示「不是有效的布尔值」" eval 'editing paid && hint_is 不是有效的布尔值'
clear_in; e2e_type ye; sleep 0.3
check "ye（yes 的前缀）：放行" eval '[[ -z $(hint) ]]'
key Escape

# ---- 其他类型：挡住的写法给各自的提示，放行的写法一次保存进 PG，数据库也收
open_table t_types; wait_for 8 settled
checks() { local col=$1 bad=$2 want=$3 good=$4; key g g; key 0; key $(( $(awk -v c="$col" 'BEGIN { split("id i r d u js tm ts", a); for (n in a) if (a[n] == c) print n - 1 }') )) l
  edit "$bad"; hint_is "$want" || { key Escape; return 1; }; clear_in; e2e_type "$good"; sleep 0.3; [[ -z $(hint) ]] || { echo "  $good: hint '$(hint)'"; key Escape; return 1; }; key Enter; mode_is NORMAL; }
check "int：2147483648 超出 int4 的范围；+0x1F 放行" checks i 2147483648 "超出 int4 的范围" +0x1F
check "real：1e-50 超出 float4 的范围；-0x1.8 放行" checks r 1e-50 "超出 float4 的范围" -0x1.8
check "double：1e-400 超出 float8 的范围；0x10 放行" checks d 1e-400 "超出 float8 的范围" 0x10
check "uuid：只有一边花括号不行；两边花括号、不带连字符放行" checks u "{a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11" "不是有效的 UUID" "{a0eebc999c0b4ef8bb6d6bb9bd380a11}"
check "jsonb：{\"a\": 不完整；{\"a\": 1} 放行" checks js '{"a":' "不是有效的 JSON" '{"a": 1}'
check "time：25:00 挡住；24:00:00 放行" checks tm 25:00 "不是有效的日期 / 时间" 24:00:00
key C-s; wait_for 8 eval '[[ $(qb) == *已保存* ]]'
types_row() { psql_n "select concat_ws('|', i, r, d, u, js, tm) from t_types where id = 1"; }
check "保存：库里是 31、-1.5、16、a0eebc99-…、{\"a\": 1}、24:00:00" eval '[[ $(types_row) == "31|-1.5|16|a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11|{\"a\": 1}|24:00:00" ]] || { types_row; false; }'

# ---- 没改过的文字不检查：1900 年 Asia/Shanghai 的 timestamptz 带秒级偏移，进编辑后直接 ↵ 就能出去（reviewer 在 F3.21 实测）
key g g; key '$'
check "ts 显示成 1900-01-01 08:05:43+08:05:43（PG 的输出）" eval '[[ $(cell ts 1 30) == "1900-01-01 08:05:43+08:05:43" ]]'
key Enter; key Enter
check "进编辑不改，直接 ↵：出去了，没有提示，也不算修改" eval 'mode_is NORMAL && [[ -z $(hint) && $(qb) != *"$(printf "\xef\x83\x87") 1"* ]]'
edit 2026-02-30
check "ts：2026-02-30 挡住" hint_is "不是有效的日期 / 时间"
clear_in; e2e_type 2026-09-01T10:00:00+08:05:43; sleep 0.3
check "2026-09-01T10:00:00+08:05:43（T 分隔、时区带秒）放行" eval '[[ -z $(hint) ]]'
key Enter; key C-s; wait_for 8 eval '[[ $(qb) == *已保存* ]]'
check "保存：PG 收下，UTC 是 01:54:17（10:00 减去 8 小时 5 分 43 秒）" eval '[[ $(psql_n "select ts at time zone '"'UTC'"' from t_types where id = 1") == "2026-09-01 01:54:17" ]]'

# ---- 提示框和选项浮层叠放：提示框紧贴输入框，浮层在外侧，都不出屏幕（30 行高的窗口；最底一行、第一行；时间列、数字列）
start -y 30 -C "$D/own"; open_table t_order; wait_for 8 settled
stacked() {   # IN_Y：提示框贴着第 IN_Y 行的输入框，另一个浮层贴着提示框的外侧，两个都在第 1 行到状态栏上一行之间
  local in=$1 h o b; h=($(hint_box)); o=($(boxes | grep -v "^${h[0]} ${h[1]} ")); [[ -n ${h[0]} && -n ${o[0]} ]] || { echo "  boxes: $(boxes | tr "\n" ,)"; return 1; }
  for b in "${h[1]} ${h[3]}" "${o[1]} ${o[3]}"; do set -- $b; (($1 >= 1 && $1 + $2 - 1 <= $(H) - 1)) || { echo "  box at y $1 h $2 is off the screen"; return 1; }; done
  if ((h[1] > in)); then ((h[1] == in + 1 && o[1] == h[1] + h[3])); else ((h[1] + h[3] == in && o[1] + o[3] == h[1])); fi || { echo "  input y $in, hint [${h[*]}], other [${o[*]}]"; false; }
}
key G; key 8 l; key Enter; key BSpace; e2e_type 2026-02-30; sleep 0.4
check "时间列、最底一行：提示框在输入框正上方，时间浮层在它上面" eval 'hint_is "不是有效的日期 / 时间" && stacked $(( $(tab_y 1) - 1 ))'
key Escape; key g g; key Enter; key BSpace; e2e_type 2026-02-30; sleep 0.4
check "时间列、第一行：提示框在输入框正下方，时间浮层在它下面" eval 'stacked $(row_y 1)'
key Escape; key 0; key G; key Enter; e2e_type x; sleep 0.4
check "数字列 id（有 DEFAULT 选项）、最底一行：同样叠放" eval 'hint_is 不是有效的整数 && stacked $(( $(tab_y 1) - 1 ))'
key Escape; key g g; key Enter; e2e_type x; sleep 0.4
check "数字列 id、第一行：同样叠放" eval 'stacked $(row_y 1)'
key Escape

# ---- :wq 保存途中开始编辑一格：保存完成后 tab 不关，输入的内容还在（reviewer 在 F3.21 实测）
start -C "$D/own"; open_table t_order; wait_for 8 settled
key 3 l; edit 5; key Enter
psql "$E2E_DB" -q -c "begin" -c "select 1 from t_order where id = 1 for update" -c "select pg_sleep(60)" -c "commit" >/dev/null 2>&1 & LOCKER=$!
wait_for 5 eval '[[ $(psql_n "select count(*) from pg_locks l join pg_class c on c.oid = l.relation where c.relname = '"'t_order'"' and l.mode = '"'RowShareLock'"' and l.granted") -ge 1 ]]'
key :; e2e_type wq; sleep 0.3; key Enter; sleep 0.5
key j; edit 6
psql_n "select pg_terminate_backend(pid) from pg_stat_activity where datname = current_database() and query like '%pg_sleep(60)%' and pid <> pg_backend_pid()" >/dev/null; wait $LOCKER 2>/dev/null
wait_for 8 eval '[[ $(psql_n "select amount from t_order where id = 1") == 5.00 ]]'; sleep 0.5
check ":wq 等锁时开始编辑第 2 行：保存完成后 tab 不关，还在编辑，输入的 6 还在" eval 'editing amount && [[ $(e2e_text 34 160 1) == *" t_order ─"* && $(cell amount 2) == 6* ]]'
key Escape

e2e_done
