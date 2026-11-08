#!/usr/bin/env bash
# F15.2 SSH 隧道的补充（specs/m15-pg-databases/task.md F15.2；reviewer 点名）：PG 和 MySQL 的 ssh 失败文本、两个连接同时经同一台跳板机失败、
# 对面不说话时到 DSN 的 timeout 放弃（报超时，不是 signal: killed）、取消执行中的查询（PG 的取消请求、MySQL 的 KILL QUERY）也走隧道、
# 表单里清空 ssh、错误标在 ssh 那一行、connections.toml 里写坏的 ssh 启动时报「第 N 个连接」。假 ssh 的做法同 f15.2.sh。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/paging.sh"
command -v nc >/dev/null || { echo "e2e: no nc"; exit 1; }

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
mkdir -p "$D/bin" "$D/c" "$D/f"
cat >"$D/bin/ssh" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >>"$SSHLOG"
case "$*" in
  *me@bad*) sleep 1; echo "me@bad: Permission denied (publickey)." >&2; exit 255 ;;     # both connections to it fail at the same time
  *me@silent*) exit 3 ;;                                                                    # ssh dies with an exit code and not a word
  *me@mute*) echo $$ >>"$SSHPIDS"; exec sleep 120 ;;                                       # the far end never answers
esac
echo $$ >>"$SSHPIDS"
exec nc localhost "${2##*:}"
SH
chmod +x "$D/bin/ssh"; export SSHLOG=$D/ssh.log SSHPIDS=$D/pids; : >"$SSHLOG"; : >"$SSHPIDS"
[[ $SQLMUX_TEST_MYSQL =~ ^([^:]*):([^@]*)@tcp\(([^:]*):([0-9]+)\)/(.*)$ ]] && MYURL=mysql://${BASH_REMATCH[1]}:${BASH_REMATCH[2]}@my.internal:${BASH_REMATCH[4]}/${BASH_REMATCH[5]}
PGURL=$(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(p._replace(netloc=p.netloc.replace("localhost", "pg.internal")).geturl())' "$SQLMUX_TEST_PG")
(umask 077; cat >"$D/c/connections.toml" <<TOML
[[connection]]
name = "ok"
engine = "postgres"
dsn = "$SQLMUX_TEST_PG"

[[connection]]
name = "badpg"
engine = "postgres"
dsn = "$PGURL"
ssh = "me@bad"

[[connection]]
name = "badmy"
engine = "mysql"
dsn = "$MYURL"
ssh = "me@bad"

[[connection]]
name = "silentpg"
engine = "postgres"
dsn = "$PGURL"
ssh = "me@silent"

[[connection]]
name = "silentmy"
engine = "mysql"
dsn = "$MYURL"
ssh = "me@silent"

[[connection]]
name = "mutepg"
engine = "postgres"
dsn = "$PGURL&connect_timeout=2"
ssh = "me@mute"

[[connection]]
name = "mutemy"
engine = "mysql"
dsn = "$MYURL?timeout=2s"
ssh = "me@mute"

[[connection]]
name = "mutemy10"
engine = "mysql"
dsn = "$MYURL"
ssh = "me@mute"
TOML
)
nlog() { wc -l <"$SSHLOG" | tr -d ' '; }
alive() { local p n=0; for p in $(cat "$SSHPIDS"); do kill -0 "$p" 2>/dev/null && n=$((n + 1)); done; echo $n; }
nth_row() { local y n=0; for y in $(seq 4 42); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && ((++n == $2)) && { echo "$y"; return; }; done; }
node_x() { e2e_find "$1" "$2" | tr ' ' '\n' | awk '$1 < 32 { print; exit }'; }
click_node() { local y; y=$(nth_row "$1" "${2:-1}"); e2e_click "$(node_x "$1" "$y")" "$y"; sleep 0.2; }
# collect SECS：每 0.3 秒记一次 toast 那一行（状态栏上一行），去重后一行一条，放进 $D/toasts
collect() { local i; : >"$D/toasts"; for ((i = 0; i < $1 * 3; i++)); do e2e_text 1 "$(e2e_flag pane_width)" $(( $(H) - 1 )) | sed 's/[─┘└┐┌│ ]\{2,\}/ /g' >>"$D/toasts"; sleep 0.3; done; sort -u "$D/toasts" -o "$D/toasts"; }
saw() { python3 -c "import sys; sys.exit(0 if sys.argv[1] in open(sys.argv[2], errors='replace').read() else 1)" "$1" "$D/toasts"; }
quit_app() { e2e_type ":"; sleep 0.4; e2e_type qa; key Enter; wait_for 8 exited; sleep 0.5; }

# ---- ① ssh 失败的原文（PG 和 MySQL）；两个连接经同一台跳板机同时失败，两边都带原文
start -x 300 -C "$D/c" -P "$D/bin:$PATH"; sleep 2
click_node badpg; sleep 0.3; click_node badmy
collect 5
check "badpg 经跳板机失败：toast「badpg：…」带 ssh 的原文 Permission denied (publickey).（PG）" eval 'saw "badpg：" && saw "ssh me@bad: me@bad: Permission denied (publickey)."' || cat "$D/toasts"
twice() { python3 -c "
import sys; t = open(sys.argv[1], errors='replace').read()
sys.exit(0 if 'badmy：' in t and t.count('Permission denied (publickey).') >= 2 else 1)" "$D/toasts"; }
check "badmy 经同一台跳板机、同时失败：toast「badmy：…」也带 ssh 的原文（MySQL；两条 toast 里原文各一份）" twice || cat "$D/toasts"
check "两个连接各拨了一次（日志里至少两行，都是 me@bad）" eval '(( $(nlog) >= 2 )) && ! grep -qv "me@bad$" "$SSHLOG"' || cat "$SSHLOG"

# ssh 没说话就退了：toast 只有退出码，「ssh me@silent: exit status 3」（PG 和 MySQL）
click_node silentpg; collect 4
check "silentpg：ssh 退出码 3、没有 stderr：toast「silentpg：ssh me@silent: exit status 3」" eval 'saw "silentpg：" && saw "ssh me@silent: exit status 3"' || cat "$D/toasts"
click_node silentmy; collect 4
check "silentmy：同样「silentmy：ssh me@silent: exit status 3」" eval 'saw "silentmy：" && saw "ssh me@silent: exit status 3"' || cat "$D/toasts"

# ---- ② 对面不说话：到 DSN 的 timeout 放弃，报超时
: >"$SSHLOG"
click_node mutepg; collect 6
check "PG：connect_timeout=2，对面不说话：toast 报超时（deadline exceeded），不是 signal: killed" eval 'saw "mutepg：" && saw "deadline exceeded" && ! saw "killed"' || cat "$D/toasts"
click_node mutemy; collect 6
check "MySQL：timeout=2s：toast 报「mutemy：context deadline exceeded」，不是 signal: killed" eval 'saw "mutemy：" && saw "deadline exceeded" && ! saw "killed"' || cat "$D/toasts"
t0=$SECONDS; click_node mutemy10; collect 14
check "MySQL 没写 timeout：10 秒左右放弃（toast 出现在 collect 的后半段），也是超时" eval 'saw "mutemy10：" && saw "deadline exceeded" && ! saw "killed"' || cat "$D/toasts"
check "超时放弃之后，那几个不说话的 ssh 进程都被收掉了" eval 'wait_for 5 eval "[[ \$(alive) == 0 ]]"' || echo "  alive $(alive)"
quit_app; _e2e_kill

# ---- ③ 取消执行中的查询也走隧道：PG 的取消请求、MySQL 的 KILL QUERY 都再拨一次 ssh
: >"$SSHLOG"; : >"$SSHPIDS"
cancel_case() {   # NAME SQL DESC
  e2e_start -C "$D/c2" -P "$D/bin:$PATH" "$E2E_BIN $1"; wait_for 8 flag_is alternate_on 1; sleep 2
  key C-l; sleep 0.5; setsql "$2"; key Enter; sleep 2
  check "（准备）$1：$2 在跑（状态栏 busy）" eval '[[ $(bar) == *busy* ]]'
  local n0 t0; n0=$(nlog); t0=$SECONDS; key C-c
  wait_for 20 eval '[[ $(bar) != *busy* ]]'
  check "$3：C-c 之后马上返回（$((SECONDS - t0)) 秒），取消又拨了一次 ssh（日志 $n0 → $(nlog) 行）：没有绕过隧道" eval '(( SECONDS - t0 <= 6 )) && (( $(nlog) > n0 ))'
  quit_app; _e2e_kill
}
mkdir -p "$D/c2"; (umask 077; cat >"$D/c2/connections.toml" <<TOML
[[connection]]
name = "viapg"
engine = "postgres"
dsn = "$PGURL"
ssh = "me@bastion"

[[connection]]
name = "viamy"
engine = "mysql"
dsn = "$MYURL"
ssh = "me@bastion"
TOML
)
cancel_case viapg "select pg_sleep(60)" "PG 的取消请求走隧道"
cancel_case viamy "select sleep(60)" "MySQL 的 KILL QUERY 走隧道"
check "退出之后没有残留的隧道进程" eval 'wait_for 5 eval "[[ \$(alive) == 0 ]]"' || echo "  alive $(alive)"

# ---- ④ 表单：清空 ssh；错误标在 ssh 那一行；写坏的 connections.toml 启动时报第几个连接
mgr() { pal ">conn.manage"; key Enter; sleep 0.7; }
tabs() { local i; for ((i = 0; i < $1; i++)); do key Tab; done; }
ssh_row() { e2e_plain | python3 -c "
import re, sys
for l in sys.stdin:
    m = re.search(r'│ +ssh(?: +(.*?))? *│', re.sub('[-] ?', '', l))
    if m: print(m.group(1) or ''); sys.exit(0)
sys.exit(1)"; }
fresh_form() { rm -f "$D/f/connections.toml"; (umask 077; printf '[[connection]]\nname = "viapg"\nengine = "postgres"\ndsn = "%s"\nssh = "me@bastion"\n' "$PGURL" >"$D/f/connections.toml")
  e2e_start -C "$D/f" -P "$D/bin:$PATH" "$E2E_BIN"; wait_for 8 flag_is alternate_on 1; sleep 2; mgr; }
T="$E2E_TMP"   # 应用写的是 e2e_start 拷贝出来的那份
fresh_form; tabs 10
check "编辑已有的连接：ssh 一行读出 me@bastion" eval '[[ $(ssh_row) == "me@bastion" ]]' || ssh_row
clear_in; sleep 0.3
check "清空输入框：ssh 一行是空的" eval '[[ -z $(ssh_row) ]]' || ssh_row
key C-s; sleep 1
check "保存：connections.toml 里这个连接没有 ssh 了，其余字段还在" eval '! grep -q "^ssh" "$E2E_TMP/config/sqlmux/connections.toml" && grep -q "^dsn" "$E2E_TMP/config/sqlmux/connections.toml"' || cat "$E2E_TMP/config/sqlmux/connections.toml"
_e2e_kill
fresh_form; tabs 10; clear_in; e2e_type "-oProxyCommand=x"; key C-s; sleep 1
err_under_ssh() { e2e_plain | python3 -c '
import re, sys
ls = [re.sub("[\ue000-\uf8ff] ?", "", l) for l in sys.stdin.read().split("\n")]
i = next((i for i, l in enumerate(ls) if re.search(r"│ +ssh +-oProxyCommand=x", l)), None)
sys.exit(0 if i is not None and "写成 user@host" in ls[i + 1] else 1)'; }
check "写了 -oProxyCommand=x 保存：错误就在 ssh 那一行的下面（ssh = …：写成 user@host，…），文件里的 ssh 还是原来的 me@bastion" eval 'err_under_ssh && grep -qx "ssh = \"me@bastion\"" "$E2E_TMP/config/sqlmux/connections.toml"' || e2e_plain | sed -n 22,30p | cut -c60-150
_e2e_kill
(umask 077; printf '[[connection]]\nname = "ok"\nengine = "postgres"\ndsn = "%s"\n\n[[connection]]\nname = "evil"\nengine = "postgres"\ndsn = "%s"\nssh = "-oProxyCommand=touch %s/pwned"\n' "$SQLMUX_TEST_PG" "$SQLMUX_TEST_PG" "$D" >"$D/f/connections.toml")
e2e_start -C "$D/f" "$E2E_BIN"; sleep 3
bad_start() { e2e_plain | python3 -c '
import sys
import re
t = re.sub(r"\s", "", sys.stdin.read())   # a wide char at the wrap edge leaves a blank cell: compare without any whitespace
sys.exit(0 if "第2个连接：ssh=" in t and "不能以-开头" in t and "[e2e-exit1]" in t else 1)'; }
check "connections.toml 里手写了 ssh = \"-oProxyCommand=…\"：启动报「第 2 个连接：ssh = …：写成 user@host，不能以 - 开头」，退出码 1" bad_start || e2e_plain | head -4
check "也没有真的去执行那个选项（没有 pwned 文件）" eval '[[ ! -e "$D/pwned" ]]'

e2e_done
