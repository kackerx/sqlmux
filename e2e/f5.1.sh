#!/usr/bin/env bash
# F5.1 多 session 与新建连接（specs/m5-workspace/task.md F5.1；tech-design §5、§6.8、§7.8「session 列表」「新建 session」「新建连接表单」「状态栏」、§12、§14）
# session 列表、表单的画法由 golden 覆盖；这里在真实终端和 PG 上测：新建 session（名字、默认布局、自己的一套连接、连不上不建）、
# 两个 session 来回切界面不变、列表的 l / h / ↵ / $ / x、关掉最后一个等于退出、后台 session 的应答不动当前界面、面板的会话范围，
# （新建连接的表单和写进 connections.toml 在 f7.4.sh。）
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
APP=e2e-f51-$$ APP2=e2e-f51x-$$ APP3=e2e-f51s-$$ ERROR=#f7768e FOCUS=#9ece6a
# 用户手写的 connections.toml：带注释，最后一行后面没有换行
mkdir -p "$D/c"; (umask 077; printf '# my connections, keep this comment\n[[connection]]\nname = "doraemon"   # the seed\nengine = "postgres"\ndsn = "%s"\n# the end, no newline after this line' "$SQLMUX_TEST_PG&application_name=$APP" >"$D/c/connections.toml")

conns() { psql "$SQLMUX_TEST_PG" -At -c "select count(*) from pg_stat_activity where application_name = '${1:-$APP}'"; }
sname() { bar | sed 's/^ //; s/ ▾.*//'; }                                    # 状态栏 session 块里的名字
sname_is() { [[ $(sname) == "$1" ]] || { echo "  session block: '$(sname)', want '$1'"; false; }; }
wins() { bar | sed -E 's/^ [^▾]*▾  //; s/ {3,}.*//'; }                      # 状态栏的 window 列表：0: data*  1: data-2
box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }      # 浮层（session 列表 / 表单）的 X Y W H
slist_open() { e2e_plain | grep -q "┌─ session"; }
# srows：session 列表的每一行「文字|选中 0/1」，3 个以上的空格压成两个；window 行前面缩进两格
srows() { local g; slist_open || return 0; g=($(box))
  e2e_rows $((g[0] + 2)) $((g[0] + g[2] - 2)) $((g[1] + 1)) $((g[1] + g[3] - 2)) $((g[0] + 2)) | awk -F'|' '{ sub(/ +$/, "", $1); gsub(/   +/, "  ", $1); print $1 "|" ($2 == "#364a82" ? 1 : 0) }'; }
rows_are() { local got; got=$(srows | tr '\n' ';'); [[ $got == "$1" ]] || { echo "  rows: '$got'"; echo "  want  '$1'"; false; }; }
sl() { e2e_keys Space; e2e_type s; sleep 0.4; }                             # SPC s
layout() { e2e_keys Space; e2e_type %; sleep 0.4; key c; }   # F8.18 起新 session 只有一个空 pane：分一个出来、c 新建 query，回到旧的默认布局
new_session() { key n; sleep 0.4; key Enter; wait_for 6 eval '[[ $(sname) == "$1" ]]'; sleep 0.5; }   # NAME：列表里按 n，等它 attach
clr() { local i; for ((i = 0; i < 30; i++)); do e2e_keys BSpace; done; sleep 0.2; }
title1() { e2e_plain | head -1; }
nrow() { local y; for y in $(seq 3 30); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && { echo "$y"; return; }; done; }   # 侧栏里 NAME 的行
toast_row() { e2e_text 1 "$(e2e_flag pane_width)" $(($(H) - 1)); }
panes() { e2e_panes | awk '$1 != "-" { print $1 }' | tr '\n' ' '; }
kinds() { list | cut -d'|' -f1 | awk '{ print $NF }' | sort -u | tr '\n' ' '; }
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; }   # 焦点所在 query 的内容换成 SQL

# ---- 新建 session：名字、默认布局、自己的一套连接
start -C "$D/c"; open_table t_order; key 3 j; key l
check "启动：一个 session doraemon，它的连接有 Meta、Main 两条" eval 'sname_is doraemon && [[ $(conns) == 2 ]]'
sl
check "SPC s：session 列表，一行「doraemon  1 个窗口」，光标在当前 session 上" eval 'slist_open && rows_are "doraemon  1 个窗口|1;"'
key n; sleep 0.4; key Enter
check "n 再 ↵（F10.2：先要名字，预填 1）：列表关掉（toast「正在连接 doraemon…」连上就消失，F17.4，所以不再看它：慢连接的那条在 f17.4）" eval '! slist_open'
wait_for 6 eval '[[ $(sname) == 1 ]]'; sleep 0.5
check "连上之后 attach 新 session：名字 1（最小的没用过的数字），一个 window 0: data*" eval 'sname_is 1 && [[ $(wins) == "0: data*" ]]'
check "新 session 只有侧栏和一个空 pane ①（引导页），没有 query（F8.18）" eval '[[ $(e2e_panes | awk "\$1 != \"-\" { print \$1 }" | tr "\n" " ") == "0 1 " ]] && [[ $(tabbar 1) != *query* ]] && e2e_plain | grep -q "打开表"'
layout
check "（准备）分出 ②、c 新建 query（n 遍历所有 session，query_1 在 doraemon 里，所以是 query_2），焦点在 ②" eval '[[ $(e2e_panes | awk "\$1 != \"-\" { print \$1 }" | tr "\n" " ") == "0 1 2 " ]] && tabs_are 2 "1:query_2*" && focus_is 2'
key C-h
check "新 session 连上当前连接并展开（侧栏有 Tables (6)），它有自己的一套连接：库里多了两条" eval 'e2e_plain | head -12 | grep -q "Tables (6)" && [[ $(conns) == 4 ]]'

# ---- 两个 session 来回切：各自的界面不变
open_table t_user; key 2 j
sl
check "两个 session，光标在当前的 1 上" rows_are "doraemon  1 个窗口|0;1  1 个窗口|1;"
key k; key Enter
check "k ↵：attach doraemon、列表关掉；它的界面原样：① t_order、光标 4,2，② query_1" eval '! slist_open && sname_is doraemon && [[ $(title1) == *" t_order ─"* ]] && pos_is 4,2 && tabs_are 2 "1:query_1*"'
e2e_click 3 "$(H)"; sleep 0.4
check "单击状态栏的 session 块：打开列表" slist_open
g=($(box)); e2e_click $((g[0] + 3)) $((g[1] + 2)); sleep 0.4
check "单击 1 那一行：attach 1、列表关掉；它的界面原样：① t_user、光标 3,1，② query_2" eval '! slist_open && sname_is 1 && [[ $(title1) == *" t_user ─"* ]] && pos_is 3,1 && tabs_are 2 "1:query_2*"'

# ---- l / h：session 下面是它的 window；停在 window 上 ↵ 还切到这个 window
key Space; e2e_type c; sleep 0.5
check "（准备）SPC c：1 有两个 window，当前是新的那个" eval '[[ $(wins) == "0: data  1: data-2*" ]]'
sl; key k; key Enter; sl; key j; key l
check "l：展开 1，下面一级是它的两个 window（缩进）" rows_are "doraemon  1 个窗口|0;1  2 个窗口|1;  0: data|0;  1: data-2|0;"
key j; key h
check "window 行上 h：回到它的 session 行并收起" rows_are "doraemon  1 个窗口|0;1  2 个窗口|1;"
key l; key k; key l
check "两个都展开：每个 session 都标出它自己的当前 window（focus 色）：doraemon 的 0、1 的 1" eval 'rows_are "doraemon  1 个窗口|1;  0: data|0;1  2 个窗口|0;  0: data|0;  1: data-2|0;" && g=($(box)) && style_has $((g[0] + 4)) $((g[1] + 2)) fg=$FOCUS && style_has $((g[0] + 4)) $((g[1] + 5)) fg=$FOCUS && ! style_has $((g[0] + 4)) $((g[1] + 4)) fg=$FOCUS >/dev/null'
key j; key j; key j; key Enter
check "停在 1 的 window 0 上 ↵：attach 1 并切到 window 0" eval '! slist_open && sname_is 1 && [[ $(wins) == "0: data*  1: data-2" ]]'
e2e_keys Space; e2e_type l; sleep 0.4
check "之后 SPC l 回得去：回到 1 原来的 window 1" eval '[[ $(wins) == "0: data  1: data-2*" ]]'

# ---- $ 原地重命名
sl; e2e_type '$'; sleep 0.3
check "\$：这一行原地变成输入框，模式 INSERT" eval 'mode_is INSERT && [[ $(srows | sed -n 2p) == 1* ]]'
clr; e2e_type doraemon; key Enter
check "改成已有的 doraemon：toast「已有同名 session」，名字不变" eval 'toast_is "已有同名 session" && rows_are "doraemon  1 个窗口|0;1  2 个窗口|1;" && sname_is 1'
e2e_type '$'; clr; key Enter
check "清空后 ↵：等于放弃，名字不变" rows_are "doraemon  1 个窗口|0;1  2 个窗口|1;"
e2e_type '$'; e2e_type xyz; key Escape
check "esc：放弃，名字不变，列表还开着" eval 'slist_open && rows_are "doraemon  1 个窗口|0;1  2 个窗口|1;" && mode_is COMMAND'
e2e_type '$'; clr; e2e_type work; key Enter
check "改成 work（k 进了输入框，不是上移）：这一行和状态栏的 session 块都变成 work" eval 'rows_are "doraemon  1 个窗口|0;work  2 个窗口|1;" && sname_is work'
key l; key j; e2e_type '$'; sleep 0.3
check "window 行上 \$：不做事" eval 'mode_is COMMAND && rows_are "doraemon  1 个窗口|0;work  2 个窗口|0;  0: data|1;  1: data-2|0;"'
key h; key Escape

# ---- 面板的会话范围（§12）
pal '$'
check "\$ 范围：只列 session，名称是 session 名、所在位置是 window 数、类型标签「会话」；底栏「↵ 切换」" eval '[[ $(kinds) == "会话 " && $(list | cut -d"|" -f1 | tr "\n" ";") == "doraemon  1 个窗口  会话;work  2 个窗口  会话;" && $(footer) == *"↵ 切换 " ]] || { echo "  footer: $(footer)"; false; }'
clear_input; sleep 0.3
check "「所有」范围也列 session" eval 'list | grep -q "^doraemon  1 个窗口  会话|" && list | grep -q "^work  2 个窗口  会话|"'
e2e_type '$dor'; sleep 0.3; key Enter
check "在 doraemon 上 ↵：attach，面板关掉" eval 'closed && sname_is doraemon && [[ $(title1) == *" t_order ─"* ]]'
pal '$doraemon'; key Enter
check "在当前 session 上 ↵：只关面板" eval 'closed && sname_is doraemon'

# ---- x 关闭；名字取最小的没用过的数字
sl; new_session 1; sl; new_session 2
check "再建两个：名字 1、2，库里 4 个 session 共 8 条连接" eval 'sname_is 2 && [[ $(conns) == 8 ]]'
sl; key k; key x; wait_for 3 eval '[[ $(conns) == 6 ]]'
check "x 关掉不是当前的 1：列表还开着、光标落到下一行（2），当前还是 2，它的两条连接关掉了" eval 'slist_open && rows_are "doraemon  1 个窗口|0;work  2 个窗口|0;2  1 个窗口|1;" && sname_is 2 && [[ $(conns) == 6 ]]'
new_session 1
check "关掉 1 之后再 n：新 session 又叫 1，排在最后" eval 'sname_is 1 && { sl; rows_are "doraemon  1 个窗口|0;work  2 个窗口|0;2  1 个窗口|0;1  1 个窗口|1;"; }'
key k; key k; key Enter; sl
check "（准备）attach work，光标在第 2 行" rows_are "doraemon  1 个窗口|0;work  2 个窗口|1;2  1 个窗口|0;1  1 个窗口|0;"
key x
check "x 关掉当前的 work：attach 它的上一个 doraemon，列表还开着、光标留在第 2 行" eval 'slist_open && sname_is doraemon && rows_are "doraemon  1 个窗口|0;2  1 个窗口|1;1  1 个窗口|0;"'
key k; key x
check "x 关掉当前的、排第一的 doraemon：attach 它的下一个 2" eval 'slist_open && sname_is 2 && rows_are "2  1 个窗口|1;1  1 个窗口|0;"'

# ---- 有未保存的修改先确认；关之前把 query 写盘
key Escape; layout; key C-h; open_table t_user; key l; key Enter; e2e_type x; key Enter   # 2 的 ① t_user 改一格
key C-l; key i; e2e_type "select 42"; key Escape; cf=$(tabbar 2 | sed 's/^1://; s/\*$//')   # 2 的 ② query 写一句
sl; e2e_type x; sleep 0.4
check "x 关掉有修改的 2：确认框「2 里有 1 处修改未保存，关闭会丢弃。」y 关闭" eval 'e2e_plain | grep -q "│ *2 里有 1 处修改未保存，关闭会丢弃。" && e2e_plain | grep -q "y 关闭   n 取消"'
key n
check "n：取消，两个 session 都在" eval '! e2e_plain | grep -q 处修改未保存 && rows_are "2  1 个窗口|1;1  1 个窗口|0;"'
e2e_type x; sleep 0.3; key y
check "再按 x、y：关掉 2，attach 1；2 的 query 写了盘" eval 'sname_is 1 && rows_are "1  1 个窗口|1;" && grep -q "select 42" "$E2E_TMP/data/sqlmux/queries/$cf.sql"'
key x
check "x 关掉最后一个 session：等于退出，连接都关了" eval 'wait_for 3 exited && wait_for 3 eval "[[ \$(conns) == 0 ]]"'

# ---- 后台 session 的应答：按应答里的 tab 处理，不动当前 session 的焦点和布局
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
# 第二个连接 slow：DSN 不带密码，password_cmd 先睡 2 秒（没缓存过，第一次建连就慢）
read -r OWN_NOPW OWN_PW < <(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(p._replace(netloc=p.netloc.replace(":" + p.password + "@", "@")).geturl(), p.password)' "$E2E_DB")
mkdir -p "$D/own"; (umask 077; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n\n[[connection]]\nname = "slow"\nengine = "postgres"\ndsn = "%s"\npassword_cmd = "sleep 2; echo %s"\n' "$E2E_DB&application_name=$APP2" "$OWN_NOPW&application_name=$APP3" "$OWN_PW" >"$D/own/connections.toml")
start -C "$D/own"; key C-l; setsql "select count(*) from t_order"
e2e_lock t_order; key Enter; sleep 0.5
check "（准备）doraemon 的 query 在等锁：状态栏 busy" eval '[[ $(bar) == *busy* ]]'
sl; new_session 1; layout; key C-h
check "新 session 1 不忙：busy 跟着当前连接，1 用的是自己的连接" eval 'sname_is 1 && [[ $(bar) != *busy* ]]'
e2e_unlock; sleep 1.5
check "doraemon 的查询回来了：1 的界面不动（没有结果区、焦点还在 ①）" eval '[[ $(e2e_panes | awk "\$1 != \"-\" { print \$1 }" | tr "\n" " ") == "0 1 2 " ]] && focus_is 1 && sname_is 1'
sl; key k; key Enter
check "回到 doraemon：结果进了它的结果区（6000）" eval 'sname_is doraemon && g=($(geom 3)) && e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $((g[1] + 3)) | grep -q 6000'

# ---- 关掉在事务里的 session：确认，关掉它的连接，服务端回滚
sl; key j; key Enter; key C-l
setsql "begin"; key Enter; wait_for 8 eval '[[ $(bar) != *busy* ]]'; setsql "delete from t_log"; key Enter; wait_for 8 eval '[[ $(bar) != *busy* ]]'; sleep 0.3
check "（准备）1 的 query 里 begin、delete from t_log 没提交：库里 t_log 还是 3 行，1 的连接停在事务里" eval '[[ $(psql "$E2E_DB" -At -c "select count(*) from t_log") == 3 && $(psql "$E2E_DB" -At -c "select count(*) from pg_stat_activity where application_name = '"'$APP2'"' and state = '"'idle in transaction'"'") == 1 ]]'
sl; e2e_type x; sleep 0.4
check "x 关掉 1：确认框「doraemon 有未提交的事务，关闭会回滚。」（F5.3 起带连接名）" eval 'e2e_plain | grep -q "│ *doraemon 有未提交的事务，关闭会回滚。" && e2e_plain | grep -q "y 关闭   n 取消"'
key y; wait_for 3 eval '[[ $(conns "$APP2") == 2 ]]'
check "y：关掉 1、attach doraemon，它的连接断开，事务回滚（没有 idle in transaction 的连接，t_log 还是 3 行）" eval 'sname_is doraemon && [[ $(conns "$APP2") == 2 && $(psql "$E2E_DB" -At -c "select count(*) from t_log") == 3 && $(psql "$E2E_DB" -At -c "select count(*) from pg_stat_activity where datname = current_database() and state = '"'idle in transaction'"'") == 0 ]]'
key Escape

# ---- 关掉有查询、取数在跑的 session：不另确认，存活的 session 里没有任何提示或变化
before=$(panes)
sl; new_session 1; layout; setsql "select count(*) from t_order"
e2e_lock t_order; key Enter; key C-h; pal "@t_order"; key Enter; sleep 0.5               # ② 的查询、① 的取数都在等锁
check "（准备）1 里 query 和表格都在等锁：busy" eval 'sname_is 1 && [[ $(bar) == *busy* ]]'
sl; e2e_type x; sleep 0.6
check "x 关掉 1：不另确认，attach doraemon，列表留着，没有「查询已取消」" eval 'slist_open && rows_are "doraemon  1 个窗口|1;" && ! e2e_plain | grep -q 关闭会 && [[ $(toast_row) != *取消* ]]'
key Escape; e2e_unlock; sleep 1.5
check "锁放开、迟到的应答回来：doraemon 里没有提示、没有 busy，布局不变" eval '[[ $(toast_row) != *取消* && $(toast_row) != *"："* && $(bar) != *busy* && $(panes) == "$before" ]] && sname_is doraemon && [[ $(conns "$APP2") == 2 ]]'

# ---- 关掉有连接正在建连的 session：建好的连接随即关掉，不留在库里
sl; new_session 1; y=$(nrow slow); e2e_click "$(e2e_find slow "$y" | cut -d' ' -f1)" "$y"; sleep 0.3   # 1 里点 slow：password_cmd 睡 2 秒
check "（准备）1 里 slow 在建连，库里还没有它的连接" eval '[[ $(e2e_text 1 32 "$(nrow slow)") =~ [●•]{3} && $(conns "$APP3") == 0 ]]'
sl; e2e_type x; sleep 4
check "这时关掉 1：slow 迟到的连接随即关掉，库里没有 slow 的连接；doraemon 里没有提示" eval '[[ $(conns "$APP3") == 0 && $(toast_row) != *"slow："* ]] && sname_is doraemon'
key Escape

# ---- 新 session 连不上：toast，不建
psql "$SQLMUX_TEST_PG" -qAt -c "alter database sqlmux_e2e_$$ allow_connections false"
sl; key n; sleep 0.4; key Enter; wait_for 6 eval 'e2e_text 1 160 $(($(H) - 1)) | grep -q "doraemon："'
check "库不让连：toast「doraemon：<驱动的错误>」，不建 session" eval 'e2e_text 1 160 $(($(H) - 1)) | grep -q "doraemon：.*FATAL: database \"sqlmux_e2e_$$\" is not curr" && sname_is doraemon && { sl; rows_are "doraemon  1 个窗口|1;"; }'
psql "$SQLMUX_TEST_PG" -qAt -c "alter database sqlmux_e2e_$$ allow_connections true"
key Escape

# ---- n 建连期间：新加的连接新 session 里也有；建好之后 attach 关掉所有浮层
# 自建库上的 login 事件触发器让 doraemon（APP2）的每条新连接先睡 7 秒（填表单要三四秒）（PG 17），psql 的连接不受影响
psql "$E2E_DB" -q -c "create table public.e2e_slow (secs float)" \
  -c "create function public.e2e_slow_login() returns event_trigger language plpgsql as \$\$ begin if current_setting('application_name') = '$APP2' then perform pg_sleep(coalesce((select max(secs) from public.e2e_slow), 0)); end if; end \$\$" \
  -c "create event trigger e2e_slow_login on login execute function public.e2e_slow_login()" -c "insert into public.e2e_slow values (7)"
sl; key n; sleep 0.4; key Enter; pal ">conn.new"; key Enter; form_dsn late "$SQLMUX_TEST_PG"; key C-p
check "（准备）建连还没回来：还是 doraemon；建连期间新加了 late；面板开着" eval 'sname_is doraemon && [[ -n $(nrow late) ]] && palette_open'
wait_for 20 eval '[[ $(sname) == 1 ]]'; sleep 0.5
check "建连回来 attach 新 session 1：面板关掉（attach 关掉所有浮层）" eval 'sname_is 1 && ! palette_open'
check "建连期间新加的 late，新 session 1 的树里也有" eval '[[ -n $(nrow late) ]]'
psql "$E2E_DB" -q -c "delete from public.e2e_slow"

# ---- 后台 session 里表格的自动刷新照常：不在屏幕上就跳过这一轮，不会停掉
pal '$doraemon'; key Enter; open_table t_order; pal ">自动刷新"; key Enter; e2e_type 2s; sleep 0.3; key Enter
pal '$^1'; key Enter; sleep 2.5; psql "$E2E_DB" -qAt -c "update t_order set amount = 47 where id = 1"; sleep 2.5
check "（准备）在 session 1 里待了 5 秒，期间库里 id 1 改成 47" eval 'sname_is 1'
pal '$doraemon'; key Enter
check "切回 doraemon：自动刷新没停，几秒内 t_order 的 id 1 变成 47.00" wait_for 5 eval 'e2e_plain | grep -E "│ +1 │ +1 │" | grep -q "47.00"'

# 新建连接的表单（F5.1 原来的 13 项，M7 F7.3 / F7.4 起重做成分字段、密码进钥匙串）测在 f7.4.sh

e2e_done
