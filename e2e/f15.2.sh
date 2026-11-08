#!/usr/bin/env bash
# F15.2 SSH 隧道（specs/m15-pg-databases/task.md F15.2；tech-design §8.4「SSH 隧道」、§14）
# connections.toml 的 ssh = "user@host"：每次拨号起本机的 `ssh -W host:port -o BatchMode=yes -o ExitOnForwardFailure=yes user@host`，它的
# stdin / stdout 当 net.Conn，连接关了进程退，重拨再起一个，ssh 失败时 stderr 原文进连接错误；SQLite 忽略；表单「高级」里有 ssh 一项。
# PATH 里放一个假 ssh（记下参数和 pid，再把 stdin / stdout 接到本机的 PG / MySQL 端口：host 是只有 ssh 另一头认得的名字），真 ssh 不碰。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
command -v nc >/dev/null || { echo "e2e: no nc"; exit 1; }

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
mkdir -p "$D/bin" "$D/c" "$D/f"
cat >"$D/bin/ssh" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >>"$SSHLOG"
case "$*" in
  *me@bad*) echo "me@bad: Permission denied (publickey)." >&2; echo "second line of ssh's stderr" >&2; exit 255 ;;
  *me@polite*) trap 'kill $P 2>/dev/null; exit 0' TERM; echo $$ >>"$SSHPIDS"; exec 3<&0; nc localhost "${2##*:}" <&3 & P=$!; wait $P; exit 0 ;;   # ssh that leaves with exit status 0 when it is terminated, as ssh -W does
  *me@gone*) echo "ssh: Could not resolve hostname gone" >&2; exit 255 ;;
esac
echo $$ >>"$SSHPIDS"
exec nc localhost "${2##*:}"
SH
chmod +x "$D/bin/ssh"; export SSHLOG=$D/ssh.log SSHPIDS=$D/pids; : >"$SSHLOG"; : >"$SSHPIDS"
[[ $SQLMUX_TEST_MYSQL =~ ^([^:]*):([^@]*)@tcp\(([^:]*):([0-9]+)\)/(.*)$ ]] && MYPORT=${BASH_REMATCH[4]} MYURL=mysql://${BASH_REMATCH[1]}:${BASH_REMATCH[2]}@my.internal:${BASH_REMATCH[4]}/${BASH_REMATCH[5]}
PGURL=$(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(p._replace(netloc=p.netloc.replace("localhost", "pg.internal")).geturl())' "$SQLMUX_TEST_PG")
sqlite3 "$D/lite.db" "create table t_lite (id integer primary key, v text); insert into t_lite values (1, 'lite-row');"
(umask 077; cat >"$D/c/connections.toml" <<TOML
[[connection]]
name = "ok"
engine = "postgres"
dsn = "$SQLMUX_TEST_PG"

[[connection]]
name = "viapg"
engine = "postgres"
dsn = "$PGURL"
ssh = "me@bastion"

[[connection]]
name = "viapolite"
engine = "postgres"
dsn = "$PGURL"
ssh = "me@polite"

[[connection]]
name = "viamy"
engine = "mysql"
dsn = "$MYURL"
ssh = "me@bastion"

[[connection]]
name = "badssh"
engine = "postgres"
dsn = "$PGURL"
ssh = "me@bad"

[[connection]]
name = "lite"
engine = "sqlite"
dsn = "$D/lite.db"
ssh = "me@bastion"
TOML
)
ARGS="-W pg.internal:55432 -o BatchMode=yes -o ExitOnForwardFailure=yes me@bastion"
nlog() { wc -l <"$SSHLOG" | tr -d ' '; }
alive() { local p n=0; for p in $(cat "$SSHPIDS"); do kill -0 "$p" 2>/dev/null && n=$((n + 1)); done; echo $n; }   # the ssh (nc) processes still running
grid_has() { e2e_plain | grep -qF -- "$1"; }
nth_row() { local y n=0; for y in $(seq 4 42); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && ((++n == $2)) && { echo "$y"; return; }; done; }
node_x() { e2e_find "$1" "$2" | tr ' ' '\n' | awk '$1 < 32 { print; exit }'; }
click_node() { local y; y=$(nth_row "$1" "${2:-1}"); e2e_click "$(node_x "$1" "$y")" "$y"; sleep 0.4; }
addr() { bar | grep -oE '[^ ]+ · [^ ]+@[^ ]+' | tail -1; }
quit_app() { e2e_type ":"; sleep 0.4; e2e_type qa; key Enter; wait_for 8 exited; sleep 0.5; }
sstart() { e2e_start $SX -C "$D/c" -P "$D/bin:$PATH" "$E2E_BIN $1"; wait_for 8 flag_is alternate_on 1; sleep 2; }

# ---- PG 经 ssh：参数、数据、重拨、退出
sstart viapg
check "sqlmux viapg：连上了，状态栏地址是 pg.internal:<端口>（DSN 里的名字，不是 localhost）" eval '[[ $(addr) == "viapg · sqlmux@pg.internal:"* ]]' || addr
check "假 ssh 收到的参数：-W pg.internal:<端口> -o BatchMode=yes -o ExitOnForwardFailure=yes me@bastion（每次拨号一行）" eval '(( $(nlog) >= 1 )) && ! grep -qvxF -e "$ARGS" "$SSHLOG"' || cat "$SSHLOG"
open_table t_user; wait_for 8 settled
check "表 t_user 经隧道打开，有数据（user_ 开头的行）" eval 'grid_has "user_"' || e2e_plain | sed -n 3,8p
N0=$(nlog); PIDS0=$(cat "$SSHPIDS" | tr '\n' ' ')
check "隧道进程在跑（至少一个 nc）" eval '(( $(alive) >= 1 ))'
kill $PIDS0 2>/dev/null; sleep 1
key R; wait_for 12 settled; sleep 2
check "杀掉隧道进程后 R：重拨又起了新的 ssh（日志多了行），数据回来" eval '(( $(nlog) > N0 )) && grid_has "user_"' || { echo "  log $N0 -> $(nlog)"; e2e_plain | sed -n 1,10p | cut -c1-150; }
quit_app
check "退出 sqlmux：每个隧道进程都退了" eval 'wait_for 5 eval "[[ \$(alive) == 0 ]]"' || { echo "  alive: $(alive)"; }
_e2e_kill

# ---- MySQL 经 ssh
: >"$SSHLOG"; sstart viamy
check "MySQL：ssh 参数里是 -W my.internal:<MySQL 端口>" eval 'grep -q "^-W my.internal:$MYPORT -o BatchMode=yes -o ExitOnForwardFailure=yes me@bastion$" "$SSHLOG"' || cat "$SSHLOG"
open_table t_user; wait_for 8 settled
check "MySQL 的表 t_user 经隧道打开" eval 'grid_has "user_"' || e2e_plain | sed -n 3,8p
N0=$(nlog); kill $(cat "$SSHPIDS") 2>/dev/null; sleep 1
key R; wait_for 12 settled; sleep 2
check "MySQL：杀掉隧道进程后 R：第一次就重拨（日志多了行），数据回来" eval '(( $(nlog) > N0 )) && grid_has "user_"' || { echo "  log $N0 -> $(nlog)"; e2e_plain | sed -n 1,10p | cut -c1-150; }
quit_app; _e2e_kill

# ---- ssh 收到 SIGTERM 时退出码是 0（真的 ssh -W 就是这样）：也算连接断了，下一次请求重拨
: >"$SSHLOG"; : >"$SSHPIDS"; sstart viapolite
open_table t_user; wait_for 8 settled
N0=$(nlog); kill $(cat "$SSHPIDS") 2>/dev/null; sleep 1
key R; wait_for 12 settled; sleep 2
check "ssh 以退出码 0 退出（被 SIGTERM）：R 重拨（日志多了行），数据回来" eval '(( $(nlog) > N0 )) && grid_has "user_"' || { echo "  log $N0 -> $(nlog)"; e2e_plain | sed -n 1,10p | cut -c1-150; }
quit_app; _e2e_kill

# ---- SQLite 忽略 ssh
: >"$SSHLOG"; sstart lite
open_table t_lite; wait_for 8 settled
check "SQLite 连接写了 ssh：忽略，表照常打开，假 ssh 一次也没被调用" eval 'grid_has "lite-row" && [[ $(nlog) == 0 ]]' || { cat "$SSHLOG"; e2e_plain | sed -n 1,8p | cut -c1-120; }
quit_app; _e2e_kill

# ---- ssh 失败：错误文本是 ssh 的 stderr 原文（多行变「；」），别的连接不受影响；R 重试
SX="-x 300" sstart ok   # wide: the toast is one line, and the whole error should show
click_node badssh
check "单击 badssh（ssh 退出码 255）：toast「badssh：…」带 ssh 的原文 Permission denied (publickey).，名字 error 色" wait_for 6 eval 'y=$(nth_row badssh 1); style_has "$(node_x badssh "$y")" "$y" fg=#f7768e >/dev/null && screen_has "badssh：" && screen_has "Permission denied (publickey)."' || e2e_cap | tail -4
check "多行的 stderr 在 toast 里变成「；」连着（second line of ssh's stderr 也在）" eval 'y=$(( $(H) - 1 )); l=$(e2e_text 1 "$(e2e_flag pane_width)" "$y"); [[ $l == *"Permission denied (publickey).； second line of ssh"* ]] || { echo "  $l"; false; }'
check "别的连接照常（ok 的表数还在、程序没退）" eval 'running && e2e_plain | grep -qE "[0-9]+ tables"'
sleep 3.5; N1=$(nlog); key R
check "光标在失败的 badssh 上按 R：重试，又起了一次 ssh，又是 toast" wait_for 6 eval '(( $(nlog) > N1 )) && screen_has "badssh："'
quit_app; _e2e_kill

# ---- 表单：高级里有 ssh 一项；保存写进 connections.toml；不合法的拒绝；SQLite 没有这一项；测试按钮走隧道
mkdir -p "$D/f"; (umask 077; printf '[[connection]]\nname = "viapg"\nengine = "postgres"\ndsn = "%s"\n' "$PGURL" >"$D/f/connections.toml")
mgr() { pal ">conn.manage"; key Enter; sleep 0.7; }
tabs() { local i; for ((i = 0; i < $1; i++)); do key Tab; done; }
# the form's ssh row (the icon before its label off): its text, status 1 when there is no such row
ssh_row() { e2e_plain | python3 -c "
import re, sys
for l in sys.stdin:
    m = re.search(r'│ +ssh(?: +(.*?))? *│', re.sub('[\ue000-\uf8ff] ?', '', l))
    if m: print(m.group(1) or ''); sys.exit(0)
sys.exit(1)"; }
form_has() { e2e_plain | python3 -c "
import re, sys
t = re.sub('[\ue000-\uf8ff] ?', '', sys.stdin.read())
sys.exit(0 if re.search(r'│ +' + sys.argv[1] + r'( |\n)', t) else 1)" "$1"; }
e2e_start -C "$D/f" -P "$D/bin:$PATH" "$E2E_BIN"; wait_for 8 flag_is alternate_on 1; sleep 2
mgr
check "连接管理：高级（展开）里有 ssh 一行，在 password_cmd 之后，现在是空的" eval 'form_has password_cmd && [[ -z $(ssh_row) ]] && ssh_row >/dev/null' || e2e_plain | sed -n 14,30p | cut -c1-140
tabs 10; e2e_type "me@bastion"; sleep 0.3
check "Tab 到 ssh 一项，打 me@bastion，输入框里是它" eval '[[ $(ssh_row) == "me@bastion" ]]' || ssh_row
key C-s; sleep 1
check "C-s 保存：connections.toml 里是 ssh = \"me@bastion\"" eval 'grep -qx "ssh = \"me@bastion\"" "$E2E_TMP/config/sqlmux/connections.toml"' 
key Escape; sleep 0.3; mgr
check "再打开连接管理：ssh 一项读回 me@bastion" eval '[[ $(ssh_row) == "me@bastion" ]]' || ssh_row
# 测试按钮
by=$(for r in $(seq 1 45); do [[ $(e2e_text 1 160 $r) == *"[ 测试 ]"* ]] && echo $r; done | tail -1); bx=$(e2e_find "[ 测试 ]" "$by" | cut -d' ' -f1)
N2=$(nlog); e2e_click $((bx + 3)) "$by"; sleep 3
check "测试按钮：表单里带 ssh 时走隧道（假 ssh 又被调用），结果「已连接 · PostgreSQL …」" eval '(( $(nlog) > N2 )) && screen_has "已连接 · PostgreSQL"' || { e2e_plain | sed -n 25,31p | cut -c60-140; }
key Escape; sleep 0.3
# 不合法的值：以 - 开头 / 含空格
for bad in "-oProxyCommand=x" "a b"; do
  _e2e_kill; rm -f "$D/f/connections.toml"; (umask 077; printf '[[connection]]\nname = "viapg"\nengine = "postgres"\ndsn = "%s"\n' "$PGURL" >"$D/f/connections.toml")
  e2e_start -C "$D/f" -P "$D/bin:$PATH" "$E2E_BIN"; wait_for 8 flag_is alternate_on 1; sleep 2
  mgr; tabs 10; e2e_type "$bad"; key C-s; sleep 1
  check "ssh 写成「$bad」：保存被拒，表单上写「写成 user@host，不能以 - 开头，也不能含空格」" eval 'e2e_plain | grep -qF "写成 user@host，不能以 - 开头"'
  check "被拒之后 connections.toml 没有 ssh（文件没动）" eval '[[ -s "$E2E_TMP/config/sqlmux/connections.toml" ]] && ! grep -q "^ssh" "$E2E_TMP/config/sqlmux/connections.toml"'
done
# SQLite：引擎换成 sqlite，ssh 一行没有了
_e2e_kill; e2e_start -C "$D/f" -P "$D/bin:$PATH" "$E2E_BIN"; wait_for 8 flag_is alternate_on 1; sleep 2
mgr
ey=$(for r in $(seq 1 45); do [[ $(e2e_text 1 160 $r) == *"engine"* ]] && echo $r; done | tail -1); sx=$(e2e_find "sqlite" "$ey" | cut -d' ' -f1)
e2e_click $((sx + 1)) "$ey"; sleep 0.7
check "引擎选 sqlite：表单里没有 ssh 一行（也没有 host / port / user / password）" eval '! ssh_row >/dev/null && ! form_has host && ! form_has port && ! form_has user' || e2e_plain | sed -n 14,30p | cut -c60-140

e2e_done
