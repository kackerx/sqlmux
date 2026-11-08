#!/usr/bin/env bash
# F8.19 断线重连，PG（specs/m8-feedback/task.md F8.19；tech-design §8.2「断线重连」）
# 判定、重拨、重跑的分档由 db 层单测和集成测试覆盖；这里在真实终端里用 pg_terminate_backend 杀掉服务端的连接，等价于 tsh 代理断开
# （不停、不 pause 共用的容器）：Meta 上的请求（表格 R）自动重连重跑；query 的语句不论读写，服务端杀掉连接后都已发出，
# 不重跑，错误栏显示原错误 + toast「已重连，请重新执行」，再执行就成功；search_path 重放；idle_session_timeout 断开；
# 重拨失败（password_cmd 读到错密码）节点变红 + toast，密码改回来再请求就恢复；手动事务里被杀：模式不变、写语句不跑、commit 报提交结果未知、
# rollback 按成功、表格重新加载；只读连接重连后仍只读。全在自建库上做。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
APP=e2e-f819-$$ APPRO=e2e-f819ro-$$ ERROR=#f7768e DB=sqlmux_e2e_$$
read -r NOPW PW < <(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(p._replace(netloc=p.netloc.replace(":" + p.password + "@", "@")).geturl(), p.password)' "$E2E_DB")
mkdir -p "$D/c" "$D/ro"
printf '%s\n' "$PW" >"$D/pw"
(umask 077
 printf '[[connection]]\nname = "rw"\nengine = "postgres"\ndsn = "%s"\npassword_cmd = "cat %s"\n' "$NOPW&application_name=$APP" "$D/pw" >"$D/c/connections.toml"
 printf '[[connection]]\nname = "ro"\nengine = "postgres"\ndsn = "%s"\nread_only = true\n' "$E2E_DB&application_name=$APPRO" >"$D/ro/connections.toml")

psql_n() { psql "$E2E_DB" -At -c "$1"; }
admin() { psql "$SQLMUX_TEST_PG" -qAt -c "$1"; }
conns() { admin "select count(*) from pg_stat_activity where application_name = '${1:-$APP}' and datname = '$DB'"; }
kill_all() { admin "select count(pg_terminate_backend(pid)) from pg_stat_activity where application_name = '${1:-$APP}' and datname = '$DB'" >/dev/null
  wait_for 5 eval '[[ $(conns "${1:-$APP}") == 0 ]]'; }                 # 等服务端真的把它们都关了
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
toasted() { e2e_recorded "$1"; }                                  # toast 只显示 3 秒：看录下来的输出，不靠截屏的时机
fresh_rec() { : >|"$D/rec"; }
row2() { e2e_plain | grep -E "│ +2 │ +2 │"; }                                 # ① 表格里 id 2 那一行（③ 的日志里也有 tx1 的字样，不能整屏找）
rows() { e2e_plain | grep -cE "│ +[0-9]+ │ +[0-9]+ │ user_"; }

start -x 200 -C "$D/c"; e2e_record "$D/rec"; open_table t_user; wait_for 8 settled
check "（准备）t_user 打开，rw 有 Meta、Main 两条连接，树节点不是红的" eval '[[ $(conns) == 2 ]] && ! node_red rw'
CNT=$(psql_n "select count(*) from t_user")

# ---- Meta：杀掉连接后 R 刷新，数据回来，没有错误
kill_all; fresh_rec
key R; wait_for 8 settled; sleep 1
check "杀掉两条连接后在表格上按 R：数据回来，没有错误栏、没有 toast，树节点不变红" eval '(( $(rows) >= 5 )) && [[ -z $(errbar 1 | head -1) && -z $(toast_text) ]] && ! node_red rw && (( $(conns) >= 1 ))'

# ---- query：服务端杀连接后请求已发出，读写语句都不重跑；再执行就成功
key C-l; setsql "select count(*) from t_user"
kill_all; fresh_rec; run
check "杀掉连接后 query 执行 select：已发出，不重跑，错误栏有原来的错误（57P01）" eval '[[ $(errbar 2 | head -1) == *57P01* ]] || { echo "  err=[$(errbar 2 | head -1)]"; false; }'
check "toast「rw：已重连，请重新执行」（看录下来的输出）" toasted "rw：已重连，请重新执行"
run
check "再执行一次：成功，结果是 ${CNT}，错误栏没了，树节点不变红" eval '[[ $(rval) == "$CNT" && -z $(errbar 2 | head -1) ]] && ! node_red rw'
setsql "update t_user set name = 'zz819' where id = 1"
kill_all; fresh_rec; run
check "杀掉连接后 query 执行 update：不重跑，错误栏有原来的错误；toast「已重连，请重新执行」" eval '[[ -n $(errbar 2 | head -1) ]] && toasted "rw：已重连，请重新执行"'
check "那条 update 没有执行（id 1 的 name 不是 zz819）" eval '[[ $(psql_n "select name from t_user where id = 1") != zz819 ]]'
run
check "再按一次执行：成功，id 1 的 name 是 zz819" eval '[[ $(psql_n "select name from t_user where id = 1") == zz819 && -z $(errbar 2 | head -1) ]]'

# ---- 重连后 query 选过的 schema 还在（search_path 重放）
key g s; sleep 0.4; key Enter; sleep 0.4; e2e_type agen; sleep 0.4; key Enter; sleep 1.2
setsql "show search_path"; run
check "（准备）query 选了 agentable：search_path 是 agentable" eval '[[ $(rval) == \"agentable\"* ]] || { echo "  got: $(rval)"; false; }'
kill_all; run; run
check "杀掉连接、重连后再执行：search_path 还是 agentable（选过的 schema 重放了）" eval '[[ $(rval) == \"agentable\"* ]] || { echo "  got: $(rval)"; false; }'

# ---- 空闲超时：服务端把空闲的会话关了，同一条路径
setsql "set idle_session_timeout = '2s'"; run
sleep 4
setsql "show search_path"; fresh_rec; run
check "idle_session_timeout 到了、会话被服务端关掉：下一次执行报原错误，toast「已重连，请重新执行」" eval '[[ -n $(errbar 2 | head -1) ]] && toasted "rw：已重连，请重新执行"'
run
check "再执行一次：成功（search_path 仍是 agentable）" eval '[[ $(rval) == \"agentable\"* && -z $(errbar 2 | head -1) ]]'

# ---- 重拨失败：password_cmd 读到错密码（重拨会再执行 password_cmd）→ 节点变红、toast、tab 都留着；密码改回来，再请求就恢复
kill_all; fresh_rec
printf 'wrong-%s\n' "$$" >|"$D/pw"
key C-h; key R; sleep 2.5
check "密码改错、杀掉连接后按 R：树节点 rw 变红" eval 'node_red rw'
check "toast「rw：…」带连接名和错误（看录下来的输出）" toasted "rw："
check "tab 都留着（t_user、query_1 还在）" eval '[[ $(e2e_plain | head -1) == *" t_user ─"* && $(tabbar 2) == *query_1* ]]'
printf '%s\n' "$PW" >|"$D/pw"
key R; wait_for 10 settled; sleep 1
check "密码改回来再按 R：数据回来，树节点不再红，库里有连接" eval '! node_red rw && (( $(rows) >= 5 )) && (( $(conns) >= 1 ))'

# ---- 手动事务里被杀：模式不变，写语句不跑，表格重新加载
key C-l; setsql "select 1"; run; run                                       # Main 在上一段被杀过还没用：第一次执行报「请重新执行」，再来一次
g=($(geom 2)); x=$(e2e_find "Tx:" "${g[1]}" | tr ' ' '\n' | awk -v l="${g[0]}" '$1 > l { print; exit }'); e2e_click "$x" "${g[1]}"; sleep 0.4; e2e_type 手动; sleep 0.3; key Enter
setsql "update t_user set name = 'tx1' where id = 2"; run
check "（准备）手动模式：update 在事务里没提交，状态栏「事务中」" eval '[[ $(txstate) == 事务中 && $(psql_n "select name from t_user where id = 2") != tx1 ]]'
key C-h; key R; wait_for 8 settled; sleep 0.6
check "（准备）在表格上 R：Main 在事务里取数，表格显示着未提交的 tx1" eval 'row2 | grep -q tx1'
key C-l
kill_all; fresh_rec
setsql "update t_user set name = 'tx2' where id = 3"; run
check "事务里连接被杀、再执行一条 update：不跑，错误栏「连接断开，事务已丢失，这条没有执行」" eval '[[ $(errbar 2 | head -1) == *"连接断开，事务已丢失，这条没有执行"* ]] || { echo "  err=[$(errbar 2 | head -1)]"; false; }'
check "toast「rw：连接断开，事务已丢失」（看录下来的输出）" toasted "rw：连接断开，事务已丢失"
check "id 2、3 都没有改（tx1 随连接丢了，tx2 没有执行），模式还是手动，状态栏没有「事务中」" eval '[[ $(psql_n "select name from t_user where id in (2,3) order by id" | tr "\n" ,) == "user_2,user_3," && $(ctitle) == *"Tx: 手动"* && -z $(txstate) ]] || { echo "  title=[$(ctitle)] tx=[$(txstate)]"; false; }'
check "表格重新加载了：id 2 那一行不再显示丢掉的未提交的 tx1，是 user_2（等重载完）" eval 'wait_for 6 eval "row2 | grep -q user_2" || { echo "  row 2: $(row2)"; false; }'
run
check "再执行一次：这回跑了（新事务，状态栏「事务中」），库里还没提交" eval '[[ $(txstate) == 事务中 && -z $(errbar 2 | head -1) && $(psql_n "select name from t_user where id = 3") != tx2 ]]'

# ---- 丢失后的 commit：已发出、回复前断开的，提交结果未知（服务端可能已经提交）；rollback 按成功
kill_all; fresh_rec
setsql "commit"; run
check "事务里连接被杀、手敲 commit：报「连接断开，提交结果未知：请先查一下数据」，不是「已丢失」" eval '[[ $(errbar 2 | head -1) == *"提交结果未知"* ]] || { echo "  err=[$(errbar 2 | head -1)]"; false; }'
check "库里没有提交（tx2 不在）" eval '[[ $(psql_n "select name from t_user where id = 3") == user_3 ]]'
setsql "update t_user set name = 'tx3' where id = 3"; run
kill_all; fresh_rec
setsql "rollback"; run
check "事务里连接被杀、手敲 rollback：按成功（日志里是 ROLLBACK，没有错误栏）" eval '[[ -z $(errbar 2 | head -1) ]] && e2e_plain | grep -q "ROLLBACK" || { echo "  err=[$(errbar 2 | head -1)]"; false; }'
check "（rollback 之后）状态栏没有「事务中」，库里 id 3 没改" eval '[[ -z $(txstate) && $(psql_n "select name from t_user where id = 3") == user_3 ]]'

# ---- 只读连接：重连之后仍然只读
key Escape
start -x 200 -C "$D/ro"; e2e_record "$D/rec"; key C-l
setsql "select 1"; run; kill_all "$APPRO"; fresh_rec; run; run
setsql "show default_transaction_read_only"; run
check "只读连接（ro）被杀、重连之后：default_transaction_read_only 仍是 on" eval '[[ $(rval) == on ]] || { echo "  got: $(rval)"; false; }'

e2e_done
