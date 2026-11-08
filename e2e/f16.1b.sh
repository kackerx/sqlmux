#!/usr/bin/env bash
# F16.1 Redis 连接（二）（specs/m16-redis/task.md F16.1；tech-design §8.7「连接」、§14）
# 密码的来源盖过 URL 里的、连接被 CLIENT KILL 后自动重拨、经假 ssh 连、[redis] 段和连接的 delimiter 的校验、表单里的 redis 引擎
# （db 一栏、用户可以不填、PING 测试）、Redis 的 query 里不弹 SQL 的补全和提示。f16.1.sh 是连上、树、状态栏、连接数、ACL。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/redis.sh"
command -v nc >/dev/null || { echo "e2e: no nc"; exit 1; }

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1; redis_seed
redis_start A --requirepass pw1 --user alice on '>secret' '~*' '+@all' || exit 1
mkdir -p "$D/c" "$D/bin" "$D/f" "$D/v"
cat >"$D/bin/ssh" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >>"$SSHLOG"
case "$*" in
  *me@bad*) echo "me@bad: Permission denied (publickey)." >&2; exit 255 ;;
esac
echo $$ >>"$SSHPIDS"
exec nc localhost "${2##*:}"
SH
chmod +x "$D/bin/ssh"; export SSHLOG=$D/ssh.log SSHPIDS=$D/pids; : >"$SSHLOG"; : >"$SSHPIDS"
export F161_PW=pw1 F161_ALICE=secret
(umask 077; cat >"$D/c/connections.toml" <<TOML
[[connection]]
name = "rd"
engine = "redis"
dsn = "redis://localhost:$RPORT/0"

[[connection]]
name = "penv"
engine = "redis"
dsn = "redis://localhost:$APORT/0"
password_env = "F161_PW"

[[connection]]
name = "pcmd"
engine = "redis"
dsn = "redis://localhost:$APORT/0"
password_cmd = "echo pw1"

[[connection]]
name = "over"
engine = "redis"
dsn = "redis://alice:wrong@localhost:$APORT/0"
password_env = "F161_ALICE"

[[connection]]
name = "viassh"
engine = "redis"
dsn = "redis://redisbox.internal:$RPORT/0"
ssh = "me@bastion"

[[connection]]
name = "badssh"
engine = "redis"
dsn = "redis://redisbox.internal:$RPORT/0"
ssh = "me@bad"
TOML
)
nclients() { rc client list | grep -c "name=sqlmux"; }
aclients() { rca client list | grep -c "name=sqlmux"; }
ids() { rc client list | grep name=sqlmux | grep -o "^id=[0-9]*" | sort | tr '\n' ' '; }
side() { e2e_plain | python3 -c 'import re, sys; [print(re.sub("[\ue000-\uf8ff] ?", "", l[1:33]).strip("│ ")) for l in sys.stdin.read().split("\n")]'; }
dbrows() { side | python3 -c "
import re, sys
print(' '.join(m.group(1) for l in sys.stdin for m in [re.match(r'^[▸▾] +(db\d+)', l)] if m), end=' ')"; }
addr() { bar | grep -oE '[^ ]+ · [^ ]*(localhost|redisbox.internal):[0-9]+[^ ]*' | tail -1; }
nth_row() { local y n=0; for y in $(seq 4 42); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && ((++n == $2)) && { echo "$y"; return; }; done; }
node_x() { e2e_find "$1" "$2" | tr ' ' '\n' | awk '$1 < 32 { print; exit }'; }
click_node() { local y; y=$(nth_row "$1" "${2:-1}"); e2e_click "$(node_x "$1" "$y")" "$y"; sleep 0.4; }
runl() { key C-l; key g g; key d G; key i; e2e_type "$1"; key Escape; key Enter; sleep 1.5; }   # the console: replace its text with one line, run it
rrows() { local g y; g=($(geom 3)); for ((y = g[1] + 1; y < g[1] + g[3] - 2; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) "$y"; done | python3 -c "
import re, sys
for l in sys.stdin:
    t = re.sub(r'\\s+', ' ', re.sub('[\ue000-\uf8ff]', '', l.replace('│', ' ').replace('┼', ' ')).strip())
    if not t or set(t) <= set('─ '): continue
    print(t)"; }   # the result tab's table, squeezed
logline() { local g y; g=($(geom 3)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) "$y"; done | grep -a "$1" | tail -1; }
quit_app() { e2e_type ":"; sleep 0.4; e2e_type qa; key Enter; wait_for 8 exited; sleep 0.5; }
toast() { e2e_text 1 "$(e2e_flag pane_width)" "$(( $(H) - 1 ))" | python3 -c 'import sys; print(sys.stdin.read().strip("└─┘│ \n"))'; }
nlog() { wc -l <"$SSHLOG" | tr -d ' '; }
alive() { local p n=0; for p in $(cat "$SSHPIDS"); do kill -0 "$p" 2>/dev/null && n=$((n + 1)); done; echo $n; }
sstart() { e2e_start $SX -C "${DIR:-$D/c}" -P "@FAKES@:$D/bin:$PATH" "$E2E_BIN $1"; wait_for 8 flag_is alternate_on 1; sleep 2; }

# ---- 密码来源：password_env、password_cmd 给的盖过 URL 里的（连接的 user 还是 URL 里的）
sstart penv
check "password_env：DSN 里没有密码，环境变量 F161_PW 给了 pw1：连上（服务端要密码），地址块 penv · localhost:<端口>" eval '[[ $(addr) == "penv · localhost:$APORT" ]] && (( $(aclients) >= 2 ))' || { addr; toast; }
quit_app; _e2e_kill
sstart pcmd
check "password_cmd（echo pw1）：连上" eval '[[ $(addr) == "pcmd · localhost:$APORT" ]] && (( $(aclients) >= 2 ))' || { addr; toast; }
quit_app; _e2e_kill
sstart over
check "DSN 里的密码是错的（alice:wrong），password_env 给的 secret 盖过它：连上，是 alice" eval '[[ $(addr) == "over · alice@localhost:$APORT" ]] && rca client list | grep name=sqlmux | grep -q "user=alice"' || { addr; toast; }
quit_app; _e2e_kill

# ---- 连接被服务端关掉：下一次请求自动重拨（F8.19），树照旧
sstart rd
OLD=$(ids)
rc client kill type normal skipme yes >/dev/null; sleep 1
check "CLIENT KILL：服务端上 sqlmux 的连接都没了" eval '[[ $(nclients) == 0 ]]'
key C-h; key g g; key R; sleep 2
check "在树里 R：自动重拨，服务端上又有 sqlmux 的连接了，都是新的（id 和被杀的不重叠）" eval 'N=$(ids); [[ -n $N ]] && ! comm -12 <(tr " " "\n" <<<"$OLD") <(tr " " "\n" <<<"$N") | grep -q .' || { echo "  old: $OLD new: $(ids)"; }
check "重拨之后树里键还在（db0 · 29 个键），没有报错 toast" eval '[[ $(dbrows) == "db0 db1 " ]] && ! toast | grep -qE "EOF|reset|closed|broken|rd："' || { dbrows; toast; }
quit_app; _e2e_kill

# ---- 经假 ssh 连（host 是只有 ssh 的另一头认得的名字）
sstart viassh
check "viassh：假 ssh 收到 -W redisbox.internal:<端口> -o BatchMode=yes -o ExitOnForwardFailure=yes me@bastion，树连上了，地址块 viassh · redisbox.internal:<端口>" eval '(( $(nlog) >= 1 )) && ! grep -qvxF -e "-W redisbox.internal:$RPORT -o BatchMode=yes -o ExitOnForwardFailure=yes me@bastion" "$SSHLOG" && [[ $(addr) == "viassh · redisbox.internal:$RPORT" && $(dbrows) == "db0 db1 " ]]' || { cat "$SSHLOG"; addr; dbrows; }
check "服务端上的客户端在、隧道进程在" eval '(( $(nclients) >= 2 )) && (( $(alive) >= 1 ))'
N0=$(nlog); kill $(cat "$SSHPIDS") 2>/dev/null; sleep 1
tree_db0() { local i y; y=$(nth_row db0 1); key C-h; key g g; for ((i = 4; i < y; i++)); do key j; done; }   # the cursor on the db0 node (the only one: sqlmux NAME connects that connection alone)
tree_db0; key R; sleep 2.5
check "杀掉隧道进程后 R：又起了新的 ssh，树里的键回来" eval '(( $(nlog) > N0 )) && [[ $(dbrows) == "db0 db1 " ]]' || { echo "  log $N0 -> $(nlog)"; dbrows; toast; }
N0=$(nlog); kill $(cat "$SSHPIDS") 2>/dev/null; sleep 1
key C-l   # the tree had the focus (from the R above): ① first, runl goes on to ②
runl "GET str:plain"
check "杀掉隧道进程后在控制台跑只读命令 GET：自动起新的 ssh，重跑，结果回来（value / hello）" eval '(( $(nlog) > N0 )) && [[ $(rrows | tr "\n" "/") == "value/1 hello/" ]]' || { echo "  log $N0 -> $(nlog)"; rrows; logline GET; }
N0=$(nlog); kill $(cat "$SSHPIDS") 2>/dev/null; sleep 1
runl "SET zz 1"
echo "WRITE-LOG: $(logline "SET zz")"
check "杀掉隧道进程后跑写命令 SET zz 1：不自动重跑（zz 没有被写），日志里说连接断了、已重连" eval '[[ $(rc exists zz) == 0 && -n $(logline "SET zz") ]]' || { rc exists zz; logline "SET zz"; }
runl "SET zz 1"
check "再跑一次 SET zz 1：连接已重拨，写进去了（zz = 1）" eval '[[ $(rc get zz) == 1 ]]' || { rc exists zz; logline "SET zz"; }
quit_app
check "退出：隧道进程都退了，服务端上没有 sqlmux 的连接" eval 'wait_for 5 eval "[[ \$(alive) == 0 && \$(nclients) == 0 ]]"' || echo "  alive $(alive), clients $(nclients)"
_e2e_kill
SX="-x 200" sstart rd
click_node badssh
check "ssh 失败（退出码 255）：toast「badssh：…Permission denied (publickey).」" wait_for 6 eval '[[ $(toast) == *"badssh："*"Permission denied (publickey)."* ]]' || toast
quit_app; _e2e_kill

# ---- 配置文件的校验：[redis] 段、连接的 delimiter、engine
bad_start() {   # CONFIG-TOML CONNECTIONS-TOML WANT: starts with them, wants the error text on the screen and exit 1
  _e2e_kill; rm -rf "$D/v"; mkdir -p "$D/v"; printf '%b' "$1" >"$D/v/config.toml"; (umask 077; printf '%b' "$2" >"$D/v/connections.toml")
  e2e_start -C "$D/v" "$E2E_BIN"; sleep 2.5
  e2e_plain | python3 -c '
import sys
t = sys.stdin.read().replace("\n", "")
sys.exit(0 if sys.argv[1] in t and "[e2e-exit 1]" in t else 1)' "$3"
}
CONN='[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:1/0"\n'
check "[redis] delimiter = \"\"：启动报错「must not be empty」，退出码 1" bad_start '[redis]\ndelimiter = ""\n' "$CONN" '[redis] delimiter = "": must not be empty' || e2e_plain | head -4
check "[redis] scan_keys = 0：报「must be greater than 0」" bad_start '[redis]\nscan_keys = 0\n' "$CONN" '[redis] scan_keys = 0: must be greater than 0' || e2e_plain | head -4
check "[redis] load_all = -3：报「must be greater than 0」" bad_start '[redis]\nload_all = -3\n' "$CONN" '[redis] load_all = -3: must be greater than 0' || e2e_plain | head -4
check "connections.toml：postgres 连接写了 delimiter：报「delimiter 只对 redis 连接有用」，退出码 1" bad_start '' '[[connection]]\nname = "pg"\nengine = "postgres"\ndsn = "postgres://u:p@h/d"\ndelimiter = "/"\n' 'delimiter 只对 redis 连接有用' || e2e_plain | head -4
check "connections.toml：engine = \"redis2\"：报「只能是 postgres、mysql、sqlite、redis、mssql 或 exec」" bad_start '' '[[connection]]\nname = "x"\nengine = "redis2"\ndsn = "redis://h/0"\n' '只能是 postgres、mysql、sqlite、redis、mssql 或 exec' || e2e_plain | head -4
_e2e_kill; rm -rf "$D/v"; mkdir -p "$D/v"; printf '[redis]\ndelimiter = "/"\nscan_keys = 5\nload_all = 7\n' >"$D/v/config.toml"; (umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\ndelimiter = "-"\n' "$RPORT" >"$D/v/connections.toml")
DIR=$D/v sstart rd
check "[redis] 三项都写了、连接里写了 delimiter：照常启动，树连上了" eval 'running && [[ $(dbrows) == "db0 db1 " ]]' || { dbrows; e2e_plain | head -3; }
quit_app; _e2e_kill; DIR=

# ---- Redis 的 query：不弹 SQL 的补全，不画 SQL 的提示行
sstart rd
key C-l; key i; e2e_type "select * from t_"; sleep 1
check "在 Redis 连接的 query 里打 select * from t_：没有补全列表（没有「t_」开头的表、没有 SQL 关键字候选）" eval '! e2e_plain | grep -qE "┌─+┐" || ! e2e_plain | grep -qE "│ *(t_user|table|from) "' 
e2e_type "\nINSERT INTO a (id, "; sleep 1
check "再打 INSERT INTO a (id, ：没有签名提示行" eval '! e2e_plain | grep -qE "(id|…)[ ,]*│ *$|\bid, name\b"'
check "程序还在跑" running
quit_app; _e2e_kill

# ---- 表单：redis 引擎（db 一栏，用户可以不填），保存、测试
mgr() { pal ">conn.manage"; key Enter; sleep 0.7; }
box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }
fsep() { local g y; g=($(box)); for ((y = g[1] + 1; y < g[1] + g[3]; y++)); do [[ $(e2e_text $((g[0] + 28)) $((g[0] + 30)) $y) == "───" ]] && { echo "$y"; return; }; done; }
ferr() { local g; g=($(box)); e2e_text $((g[0] + 28)) $((g[0] + g[2] - 2)) $(( $(fsep) - 1 )) | sed 's/^ *//; s/ *$//'; }
form_open() { e2e_plain | grep -q "┌─ 连接"; }
frows() { local g y; g=($(box)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 28)) $((g[0] + g[2] - 2)) "$y" | python3 -c 'import re, sys; print(re.sub("[\ue000-\uf8ff] ?", "", sys.stdin.read()).strip())'; done; }
clr() { local i; for ((i = 0; i < 40; i++)); do e2e_keys BSpace; done; sleep 0.2; }
CT() { echo "$E2E_TMP/config/sqlmux/connections.toml"; }
test_btn() { local by bx; by=$(for r in $(seq 1 45); do [[ $(e2e_text 1 160 $r) == *"[ 测试 ]"* ]] && echo $r; done | tail -1); bx=$(e2e_find "[ 测试 ]" "$by" | cut -d' ' -f1); e2e_click $((bx + 3)) "$by"; sleep 2.5; }
fresh() { _e2e_kill; rm -rf "$D/f"; mkdir -p "$D/f"; (umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$RPORT" >"$D/f/connections.toml"); e2e_start -C "$D/f" -P "@FAKES@:$D/bin:$PATH" "$E2E_BIN"; wait_for 8 flag_is alternate_on 1; sleep 2; pal ">conn.new"; key Enter; sleep 0.6; }
fresh; e2e_type "nr"; key Tab; key Tab; key Right; key Right; key Right; sleep 0.3
check "engine 选 redis：port 预填 6379，数据库一栏的标签是 db，高级里 dsn 的例子是 redis://…" eval 'f=$(frows); grep -qE "^port +6379" <<<"$f" && grep -qE "^db( |$)" <<<"$f" && ! grep -qE "^database" <<<"$f"' || frows
key Tab; e2e_type localhost; key Tab; clr; e2e_type "$RPORT"
test_btn
check "只填 host 和 port（没有 user、没有 db），测试按钮：「已连接 · Redis <版本>」" eval 'e2e_plain | grep -qE "已连接 · Redis [0-9]"' || { e2e_plain | sed -n 14,32p | cut -c30-130; }
key C-s; sleep 2
check "C-s 保存：connections.toml 里是 engine = \"redis\"、dsn = \"redis://localhost:$RPORT/0\"（db 空着是 0，没有用户）" eval 'grep -qx "engine = \"redis\"" "$(CT)" && grep -qx "dsn = \"redis://localhost:$RPORT/0\"" "$(CT)" && grep -q "name = \"nr\"" "$(CT)"' || cat "$(CT)"
check "表单关了，树里有 nr（连上了：db0 在）" eval '! form_open && side | grep -qE "^[▸▾] +nr"' || { side | head -8; }
quit_app
fresh; e2e_type "nb"; key Tab; key Tab; key Right; key Right; key Right; key Tab; e2e_type localhost; key Tab; key Tab; key Tab; key Tab; e2e_type x
key Enter; sleep 0.5
check "db 填 x：保存被拒，「db 要是从 0 起的数字」" eval '[[ $(ferr) == *"db 要是从 0 起的数字"* ]] && form_open' || ferr
clr; e2e_type -1; key Enter; sleep 0.5
check "db 填 -1：同样被拒" eval '[[ $(ferr) == *"db 要是从 0 起的数字"* ]] && form_open' || ferr
clr; e2e_type 5; key Enter; sleep 1.5
check "db 填 5：保存了，DSN 的路径是 /5" eval '! form_open && grep -qE "^dsn = \"redis://localhost:6379/5\"" "$(CT)"' || { cat "$(CT)"; }
quit_app
fresh; e2e_type "nh"; key Tab; key Tab; key Right; key Right; key Right; key Enter; sleep 0.5
check "没填 host：「缺少 host」（user 和 db 不是必填的，没有「缺少 user」）" eval '[[ $(ferr) == *"缺少 host"* ]]' || ferr
quit_app
fresh; e2e_type "na"; key Tab; key Tab; key Right; key Right; key Right; key Tab; e2e_type localhost; key Tab; clr; e2e_type "$APORT"; key Tab; e2e_type alice; key Tab; e2e_type secret
test_btn
check "填了 user alice 和 password secret（服务端的 ACL 用户），测试按钮：「已连接 · Redis」" eval 'e2e_plain | grep -qE "已连接 · Redis [0-9]"' || { e2e_plain | sed -n 14,32p | cut -c30-130; }
key C-s; sleep 2
check "保存：DSN 里有用户没有密码（redis://alice@localhost:<端口>/0），密码走 password_cmd，文件里没有明文 secret" eval 'grep -qx "dsn = \"redis://alice@localhost:$APORT/0\"" "$(CT)" && grep -q "^password_cmd" "$(CT)" && ! grep -qE "secret([^-]|$)" "$(CT)"' || cat "$(CT)"
click_node na; sleep 1.5
check "点 na 连它：用 password_cmd 从钥匙串取的密码连上，服务端上有 alice 的 sqlmux 连接" eval 'wait_for 8 eval "rca client list | grep name=sqlmux | grep -q user=alice"' || { rca client list | cut -c1-150; toast; }
quit_app
# 编辑已有的 redis 连接：字段读回来
_e2e_kill; rm -rf "$D/f"; mkdir -p "$D/f"; (umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://bob@localhost:%s/4"\n' "$RPORT" >"$D/f/connections.toml")
e2e_start -C "$D/f" -P "@FAKES@:$D/bin:$PATH" "$E2E_BIN"; wait_for 8 flag_is alternate_on 1; sleep 2; mgr
check "连接管理里打开 rd：engine 在 redis 上，host localhost、port ${RPORT}、user bob、db 4" eval 'f=$(frows); grep -qE "^host +localhost" <<<"$f" && grep -qE "^port +$RPORT" <<<"$f" && grep -qE "^user +bob" <<<"$f" && grep -qE "^db +4" <<<"$f"' || frows
e2e_done
