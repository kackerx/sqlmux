#!/usr/bin/env bash
# F5.10 连接模型与连接树（specs/m5-workspace/task.md F5.10；tech-design §5「一个 session 多个连接」、§7.8「连接节点」「状态栏」、§12）
# 连接节点的四种状态、状态栏的画法由 golden 覆盖；这里在真实 PG 上测：用到时才连、password_cmd 在后台跑、连不上不影响别的、
# 表 tab 属于连接、当前连接跟着焦点走（地址块、快速 SQL）、面板列出各个连接的表、sqlmux <名字> 选启动时的连接。
# connections.toml 里四个连接：doraemon（共用的 sqlmux 库）、seed（同一个库，password_cmd 先睡 2 秒）、bad（库不存在）、
# multi（localhost:1，解析出两个地址、驱动报两行错）。只读，不用自建库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
APP_A=e2e-f510a-$$ APP_B=e2e-f510b-$$ ERROR=#f7768e DIM=#565f89
# seed 的 DSN 去掉密码，密码由 password_cmd 给（§13）
read -r SEED PW < <(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(p._replace(netloc=p.netloc.replace(":" + p.password + "@", "@")).geturl(), p.password)' "$SQLMUX_TEST_PG")
mkdir -p "$D/c"; (umask 077; cat >"$D/c/connections.toml" <<EOF
[[connection]]
name = "doraemon"
engine = "postgres"
dsn = "$SQLMUX_TEST_PG&application_name=$APP_A"

[[connection]]
name = "seed"
engine = "postgres"
dsn = "$SEED&application_name=$APP_B"
password_cmd = "sleep 2; echo $PW"

[[connection]]
name = "bad"
engine = "postgres"
dsn = "${SQLMUX_TEST_PG%%\?*}_nosuchdb?sslmode=disable"

[[connection]]
name = "multi"
engine = "postgres"
dsn = "postgres://nobody@localhost:1/x?sslmode=disable"
EOF
)
conns() { psql "$SQLMUX_TEST_PG" -At -c "select count(*) from pg_stat_activity where application_name = '$1'"; }
nth_row() { local y n=0; for y in $(seq 4 42); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && ((++n == $2)) && { echo "$y"; return; }; done; }   # NAME N：侧栏里第 N 个 NAME 的行
node_x() { e2e_find "$1" "$2" | tr ' ' '\n' | awk '$1 < 32 { print; exit }'; }       # NAME Y：侧栏里它的列
node_is() { [[ $(e2e_text 1 32 "$(nth_row "$1" 1)" | noicon) == *"$2 $1 "* ]] || { echo "  $1: '$(e2e_text 1 32 "$(nth_row "$1" 1)" | noicon)', want $2"; false; }; }   # NAME ▾|▸（中间的引擎图标 noicon 去掉了）
click_node() { local y; y=$(nth_row "$1" "${2:-1}"); e2e_click "$(node_x "$1" "$y")" "$y"; sleep 0.4; }
addr() { bar | grep -oE '[^ ]+ · [^ ]+@[^ ]+' | tail -1; }                              # 状态栏的地址块 <连接> · <用户>@<host>:<port>
addr_is() { [[ $(addr) == "$1 · "* ]] || { echo "  address block: '$(addr)', want $1 · …"; false; }; }
tables_n() { e2e_text 1 32 2 | grep -oE '[0-9]+ tables'; }
mark() { T0=$(psql "$SQLMUX_TEST_PG" -At -c "select clock_timestamp()"); }
fetched() { [[ -n $(psql "$SQLMUX_TEST_PG" -At -c "select 1 from pg_stat_activity where application_name = '$1' and query_start > '$T0' and query like '%t_order%'") ]]; }

start -C "$D/c"
check "启动：doraemon 连上并展开，seed、bad 折叠着（▸）、没有子节点" eval 'node_is doraemon ▾ && node_is seed ▸ && node_is bad ▸ && (( $(nth_row bad 1) - $(nth_row seed 1) == 1 ))'
check "只有 doraemon 建了连接，seed 一条都没有；侧栏 11 tables" eval '(( $(conns "$APP_A") >= 1 )) && [[ $(conns "$APP_B") == 0 && $(tables_n) == "11 tables" ]]'
check "状态栏的地址块是启动时的连接：doraemon · sqlmux@localhost:55432" eval 'addr_is doraemon && [[ $(addr) == "doraemon · sqlmux@localhost:55432" ]]'

# ---- 展开 seed：去连（password_cmd 在后台跑），连上后展开 current_schema() 和 Tables
click_node seed
check "单击 seed：连接中，名字后面跟 dim 色的三点转圈（F19.2，原来是 …），界面照常能动" eval 'y=$(nth_row seed 1); x=$(e2e_plain | sed -n "${y}p" | python3 -c "import re, sys; m = re.search(\"[●•]{3}\", sys.stdin.read()); print(m.start() + 1 if m else \"\")"); [[ -n $x ]] && (( x < 32 )) && style_has "$x" "$y" fg=$DIM && running || { echo "  y=$y x=$x row=[$(e2e_plain | sed -n ${y}p | cut -c1-40)] style=$(e2e_style "${x:-0}" "$y")"; false; }'
# 树光标停在连接中的 seed 上，当前连接沿用 doraemon；这时打开的面板就按 doraemon，seed 在面板开着时连上也不换（审查时定）
pal '\;'; e2e_type "select current_setting('application_name') as app"; sleep 0.3
check "连接中：面板照常能打开、能输入" eval 'is_open && [[ $(input) == ";select current_setting('"'application_name'"') as app" ]]'
check "连上之后：seed 展开，下面是 public 的 Tables（同一个库，两份 t_order），侧栏 22 tables，seed 建了连接" wait_for 6 eval '[[ -n $(nth_row t_order 2) && $(tables_n) == "22 tables" ]] && (( $(conns "$APP_B") >= 1 ))'
e2e_keys Enter; wait_for 5 eval '[[ -n $(title) && $(title) != "… 行"* ]]'
check "面板是 seed 连上之前打开的：快速 SQL 照样走 doraemon" eval '[[ $(grid) == "$APP_A" ]] || { grid; false; }'
e2e_keys C-e; sleep 0.5
check "C-e 开的 query 也在 doraemon 上（① 的 query_2，标题 doraemon · sqlmux.public ▾）" eval 'closed && tabs_are 1 "1:query_2*" && [[ $(e2e_text 34 103 1) == *" doraemon · sqlmux.public ▾"* ]] || { e2e_text 34 103 1; false; }'

# ---- 从 seed 打开表：表 tab 属于 seed，取数走 seed 的连接
mark; click_node t_order 2; wait_for 8 settled
check "单击 seed 下的 t_order：① 开出 t_order，取数走 seed 的连接" eval 'tabs_are 1 "1:query_2- │ 2:t_order*" && fetched "$APP_B"'
check "地址块跟着焦点：seed · sqlmux@localhost:55432" addr_is seed
pal "@t_order"
check "面板的表范围列出两个连接的 t_order：位置 seed.public、doraemon.public，当前连接 seed 的排前面；bad 没连上，不列" eval '[[ $(list | cut -d"|" -f1 | grep "^t_order  " | tr "\n" /) == "t_order  seed.public  表/t_order  doraemon.public  表/" ]] || { list; false; }'
pick() { local i; for ((i = 0; i < 12; i++)); do [[ $(selected) == "$1"* ]] && return; e2e_keys Down; sleep 0.1; done; }
pick "t_order  doraemon.public"; mark; e2e_keys Enter; wait_for 8 settled
check "选 doraemon 的 t_order ↵：另开一个 tab（两个连接上的同名表是两个 tab），取数走 doraemon" eval 'tabs_are 1 "1:query_2 │ 2:t_order- │ 3:t_order*" && fetched "$APP_A" && addr_is doraemon'
key g T
check "gT 回到 seed 的 t_order：地址块是 seed" addr_is seed
pal '\;'; e2e_type "select current_setting('application_name') as app"; sleep 0.3; e2e_keys Enter; wait_for 5 eval '[[ -n $(title) && $(title) != "… 行"* ]]'
check "快速 SQL 走当前连接（seed）的 Meta" eval '[[ $(grid) == "$APP_B" ]] || { grid; false; }'
e2e_keys Escape; sleep 0.3; key g t
pal '\;'; e2e_type "select current_setting('application_name') as app"; sleep 0.3; e2e_keys Enter; wait_for 5 eval '[[ -n $(title) && $(title) != "… 行"* ]]'
check "切到 doraemon 的 t_order 再执行：走 doraemon" eval '[[ $(grid) == "$APP_A" ]] || { grid; false; }'
e2e_keys Escape; sleep 0.3

# ---- 连不上：节点 error 色，toast「bad：<驱动的错误>」，别的连接照常；R 重试
# 树里列着集群所有的库（F15.1），并行跑的别的脚本的私有库会把 bad 挤出屏幕：先把 doraemon 和 seed 折起来
key C-h; key g g; key h; key j; key h; key g g
click_node bad
check "单击 bad：连不上，名字 error 色，toast「bad：…」（数据库不存在），别的不受影响" wait_for 5 eval 'y=$(nth_row bad 1); style_has "$(node_x bad "$y")" "$y" fg=$ERROR >/dev/null && screen_has "bad：" && screen_has "nosuchdb" && running && [[ $(tables_n) == "22 tables" ]]'
sleep 3.5                                                                         # 等 toast 消失；单击已经把树的光标放在 bad 上
key R
check "光标在失败的 bad 上按 R：重试，又是 toast「bad：…」" wait_for 5 eval 'screen_has "bad："'
sleep 3.5; click_node multi
check "multi 连不上、驱动的错误有两行：toast 只占一行，换行变成「；」" wait_for 5 eval 'y=$(( $(H) - 1 )); l=$(e2e_text 1 "$(e2e_flag pane_width)" "$y"); [[ $l == *"multi："*"；"* && $(e2e_text 1 "$(e2e_flag pane_width)" $((y - 1))) != *"localhost"* ]] || { echo "  $l"; false; }'

# ---- sqlmux <名字>：启动时只连这一个并展开它
e2e_start -C "$D/c" "$E2E_BIN seed"; wait_for 8 flag_is alternate_on 1; sleep 2.5
check "sqlmux seed：seed 连上并展开，doraemon 折叠、没有连" wait_for 6 eval 'node_is seed ▾ && node_is doraemon ▸ && [[ -n $(nth_row t_order 1) && $(conns "$APP_A") == 0 ]] && addr_is seed'

# ---- 连接名重复：启动报错
printf '[[connection]]\nname = "a"\nengine = "postgres"\ndsn = "%s"\n\n[[connection]]\nname = "a"\nengine = "postgres"\ndsn = "%s"\n' "$SQLMUX_TEST_PG" "$SQLMUX_TEST_PG" >| "$D/c/connections.toml"
e2e_start -C "$D/c" "$E2E_BIN"
check "connections.toml 里有两个 a：启动报错退出，指出重名的 a" eval 'wait_for 5 screen_has "[e2e-exit 1]" && screen_has "\"a\""'

e2e_done
