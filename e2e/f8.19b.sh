#!/usr/bin/env bash
# F8.19 断线重连，MySQL（specs/m8-feedback/task.md F8.19；tech-design §8.2「断线重连」）
# 用 KILL <id> 杀掉服务端的连接（等价于 tsh 代理断开 / wait_timeout 空闲断开；不停、不 pause 共用的容器）：表格 R 刷新自动恢复；
# query 的语句服务端杀连接后已发出，读写都不重跑（错误栏原错误 + toast「已重连，请重新执行」），再执行就成功；选过的 database 重放；
# wait_timeout 到了同一条路径；重拨失败（password_cmd 读到错密码）节点变红、改回来恢复；手动事务里被杀（写语句不跑、commit 提交结果未知、
# rollback 成功、表格重新加载）；只读连接重连后仍只读。写库只在自建库 sqlmux_e2e_<pid> 上。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_mydb || { echo "e2e: could not create the private MySQL database" >&2; exit 1; }
OWN=sqlmux_e2e_$$ ERROR=#f7768e
read -r NOPW PW < <(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(p._replace(netloc=p.netloc.replace(":" + p.password + "@", "@")).geturl(), p.password)' "$E2E_MYDB")
mkdir -p "$D/c" "$D/ro"
printf '%s\n' "$PW" >"$D/pw"
(umask 077
 printf '[[connection]]\nname = "my"\nengine = "mysql"\ndsn = "%s"\npassword_cmd = "cat %s"\n' "$NOPW" "$D/pw" >"$D/c/connections.toml"
 printf '[[connection]]\nname = "ro"\nengine = "mysql"\ndsn = "%s"\nread_only = true\n' "$E2E_MYDB" >"$D/ro/connections.toml")

my() { e2e_mysql "$OWN" <<<"$1"; }
ids() { my "select id from information_schema.processlist where db <=> '$OWN' and id <> connection_id() and user = substring_index(user(), '@', 1)"; }   # sqlmux 的连接（我自己这条不算）
kill_all() { local i; for i in $(ids); do my "kill $i" 2>/dev/null; done; wait_for 5 eval '[[ -z $(ids) ]]'; }
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
setsql() { key Escape; key g g; key d G; key i; paste "$1"; key Escape; key g g; }
done_run() { wait_for 8 eval '[[ $(bar) != *busy* ]]'; sleep 0.4; }
run() { key Enter; done_run; }
rval() { local g; g=($(geom 3)); e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $((g[1] + 3)) | awk -F'│' '{ gsub(/ /, "", $2); print $2 }'; }
ctitle() { local g; g=($(geom 2)); e2e_text "${g[0]}" $((g[0] + g[2] - 1)) "${g[1]}"; }
toast_row() { e2e_text 1 "$(e2e_flag pane_width)" $(($(H) - 1)); }
toast_text() { toast_row | sed 's/[ │└┘─┌┐]//g'; }
trow() { local y; for y in $(seq 3 43); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && { echo "$y"; return; }; done; }
node_red() { local y c; y=$(trow "$1"); [[ -n $y ]] || return 1; c=$(e2e_find "$1" "$y" | cut -d' ' -f1); [[ " $(e2e_style "$c" "$y") " == *" fg=$ERROR "* ]]; }
txstate() { bar | grep -oE '事务中|事务出错'; }
toasted() { e2e_recorded "$1"; }
fresh_rec() { : >|"$D/rec"; }
row2() { e2e_plain | grep -E "│ +2 │ +2 │"; }
rows() { e2e_plain | grep -cE "│ +[0-9]+ │ +[0-9]+ │ user_"; }
open_own() { key C-p; e2e_type "@$1 $OWN"; sleep 0.4
  [[ $(selected) == "$1  ${2:-my}.$OWN  表" ]] || { echo "e2e: palette picked '$(selected)', not $OWN.$1; stopping" >&2; e2e_stop; exit 1; }
  key Enter; wait_for 8 eval '[[ -n $(grid_y) && $(e2e_plain | head -1) == *" $1 ─"* ]]'; }

start -x 200 -C "$D/c"; e2e_record "$D/rec"; open_own t_user; wait_for 8 settled
CNT=$(my "select count(*) from t_user")
check "（准备）t_user 打开，my 有两条连接，树节点不是红的" eval '[[ $(ids | wc -l | tr -d " ") == 2 ]] && ! node_red my'

# ---- Meta：杀掉连接后 R 刷新
kill_all; fresh_rec
key R; wait_for 8 settled; sleep 1
check "杀掉两条连接后在表格上按 R：数据回来，没有错误栏、没有 toast，树节点不变红，Meta 已经重连" eval '(( $(rows) >= 5 )) && [[ -z $(errbar 1 | head -1) && -z $(toast_text) && -n $(ids) ]] && ! node_red my'

# ---- query：已发出的读写语句都不重跑；再执行就成功
key C-l; setsql "select count(*) from t_user"
kill_all; fresh_rec; run
check "杀掉连接后 query 执行 select：已发出，不重跑，错误栏有原来的错误；toast「my：已重连，请重新执行」" eval '[[ -n $(errbar 2 | head -1) ]] && toasted "my：已重连，请重新执行" || { echo "  err=[$(errbar 2 | head -1)]"; false; }'
run
check "再执行一次：成功，结果是 ${CNT}，错误栏没了，树节点不变红" eval '[[ $(rval) == "$CNT" && -z $(errbar 2 | head -1) ]] && ! node_red my'
setsql "update t_user set name = 'zz819' where id = 1"
kill_all; fresh_rec; run
check "杀掉连接后 query 执行 update：不重跑，错误栏有原来的错误；toast「已重连，请重新执行」" eval '[[ -n $(errbar 2 | head -1) ]] && toasted "my：已重连，请重新执行"'
check "那条 update 没有执行（id 1 的 name 不是 zz819）" eval '[[ $(my "select name from t_user where id = 1") != zz819 ]]'
run
check "再按一次执行：成功，id 1 的 name 是 zz819" eval '[[ $(my "select name from t_user where id = 1") == zz819 && -z $(errbar 2 | head -1) ]]'

# ---- 重连后 query 选过的 database 还在（USE 重放）
key g s; e2e_type sqlmux; sleep 0.4; key Enter; sleep 1.2
setsql "select database()"; run
check "（准备）query 选了共用的 sqlmux 库：select database() 是 sqlmux" eval '[[ $(rval) == sqlmux ]] || { echo "  got: $(rval) title=$(ctitle)"; false; }'
kill_all; run; run
check "杀掉连接、重连后再执行：database 还是 sqlmux（选过的重放了）" eval '[[ $(rval) == sqlmux ]] || { echo "  got: $(rval)"; false; }'

# ---- wait_timeout：服务端把空闲的会话关了
setsql "set session wait_timeout = 2"; run
sleep 4
setsql "select database()"; fresh_rec; run
check "wait_timeout 到了、会话被服务端关掉：下一次执行报原错误，toast「已重连，请重新执行」" eval '[[ -n $(errbar 2 | head -1) ]] && toasted "my：已重连，请重新执行"'
run
check "再执行一次：成功（database 仍是 sqlmux）" eval '[[ $(rval) == sqlmux && -z $(errbar 2 | head -1) ]]'

# ---- 重拨失败：password_cmd 读到错密码 → 节点变红、toast、tab 留着；改回来恢复
kill_all; fresh_rec
printf 'wrong-%s\n' "$$" >|"$D/pw"
key C-h; key R; sleep 2.5
check "密码改错、杀掉连接后按 R：树节点 my 变红" eval 'node_red my'
check "toast「my：…」带连接名和错误（看录下来的输出）" toasted "my："
check "tab 都留着（t_user、query_1 还在）" eval '[[ $(e2e_plain | head -1) == *" t_user ─"* && $(tabbar 2) == *query_1* ]]'
printf '%s\n' "$PW" >|"$D/pw"
key R; wait_for 10 settled; sleep 1
check "密码改回来再按 R：数据回来，树节点不再红" eval '! node_red my && (( $(rows) >= 5 )) && [[ -n $(ids) ]]'

# ---- 手动事务里被杀：模式不变，写语句不跑，commit 提交结果未知，rollback 成功，表格重新加载
key C-l; key g s; e2e_type "$OWN"; sleep 0.4; key Enter; sleep 1.2                # 回到自建库（上面选过共用库）
setsql "select 1"; run; run
g=($(geom 2)); x=$(e2e_find "Tx:" "${g[1]}" | tr ' ' '\n' | awk -v l="${g[0]}" '$1 > l { print; exit }'); e2e_click "$x" "${g[1]}"; sleep 0.4; e2e_type 手动; sleep 0.3; key Enter
setsql "update t_user set name = 'tx1' where id = 2"; run
check "（准备）手动模式：update 在事务里没提交，状态栏「事务中」" eval '[[ $(txstate) == 事务中 && $(my "select name from t_user where id = 2") != tx1 ]]'
key C-h; key R; wait_for 8 settled; sleep 0.6
check "（准备）在表格上 R：Main 在事务里取数，表格显示着未提交的 tx1" eval 'row2 | grep -q tx1'
key C-l
kill_all; fresh_rec
setsql "update t_user set name = 'tx2' where id = 3"; run
check "事务里连接被杀、再执行一条 update：不跑，错误栏「连接断开，事务已丢失，这条没有执行」；toast「my：连接断开，事务已丢失」" eval '[[ $(errbar 2 | head -1) == *"连接断开，事务已丢失，这条没有执行"* ]] && toasted "my：连接断开，事务已丢失" || { echo "  err=[$(errbar 2 | head -1)]"; false; }'
check "id 2、3 都没有改，模式还是手动，状态栏没有「事务中」" eval '[[ $(my "select name from t_user where id in (2,3) order by id" | tr "\n" ,) == "user_2,user_3," && $(ctitle) == *"Tx: 手动"* && -z $(txstate) ]] || { echo "  title=[$(ctitle)] tx=[$(txstate)]"; false; }'
check "表格重新加载了：id 2 那一行是 user_2，不再显示 tx1（等重载完）" eval 'wait_for 6 eval "row2 | grep -q user_2" || { echo "  row 2: $(row2)"; false; }'
run
check "再执行一次：这回跑了（新事务，状态栏「事务中」）" eval '[[ $(txstate) == 事务中 && -z $(errbar 2 | head -1) ]]'
kill_all; fresh_rec
setsql "commit"; run
check "事务里连接被杀、手敲 commit：报「连接断开，提交结果未知：请先查一下数据」" eval '[[ $(errbar 2 | head -1) == *"提交结果未知"* ]] || { echo "  err=[$(errbar 2 | head -1)]"; false; }'
check "库里没有提交（id 3 还是 user_3）" eval '[[ $(my "select name from t_user where id = 3") == user_3 ]]'
setsql "update t_user set name = 'tx3' where id = 3"; run
kill_all; fresh_rec
setsql "rollback"; run
check "事务里连接被杀、手敲 rollback：按成功（没有错误栏），状态栏没有「事务中」，id 3 没改" eval '[[ -z $(errbar 2 | head -1) && -z $(txstate) && $(my "select name from t_user where id = 3") == user_3 ]] || { echo "  err=[$(errbar 2 | head -1)] tx=[$(txstate)]"; false; }'

# ---- 只读连接：重连之后仍然只读
start -x 200 -C "$D/ro"; e2e_record "$D/rec"; key C-l
setsql "select 1"; run; kill_all; fresh_rec; run; run
setsql "select @@session.transaction_read_only"; run
check "只读连接（ro）被杀、重连之后：transaction_read_only 仍是 1" eval '[[ $(rval) == 1 ]] || { echo "  got: $(rval)"; false; }'

e2e_done
