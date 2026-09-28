#!/usr/bin/env bash
# F1.1 测试数据库与连接（specs/m1-browse/task.md F1.1；tech-design §8.1–§8.3、§13「凭据」、§14）
# 取消、Meta 只读、文本值、DateStyle 由 go test -tags integration 覆盖；这里测启动、凭据和界面。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

CFG=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); CFG=${CFG//\/\///}   # TMPDIR ends with /; sqlmux prints the cleaned path
trap 'e2e_stop; rm -rf "$CFG"' EXIT
# SQLMUX_TEST_PG 去掉密码后的 DSN，以及密码、用户、主机、端口
read -r DSN PW PGU PGH PGP <<<"$(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(p._replace(netloc=f"{p.username}@{p.hostname}:{p.port}").geturl(), p.password, p.username, p.hostname, p.port)' "$SQLMUX_TEST_PG")"
DIR=$CFG/x/sqlmux   # 当 XDG_CONFIG_HOME/sqlmux 用：cli 直接读，界面用例经 -C 拷过去
conn() { printf '[[connection]]\nname = "%s"\nengine = "postgres"\ndsn = "%s"\n%s' "$1" "$2" "$3"; }   # NAME DSN [EXTRA]
conns() { rm -rf "$DIR"; mkdir -p "$DIR"; printf '%s\n' "${@:2}" >"$DIR/connections.toml"; chmod "$1" "$DIR/connections.toml"; }   # MODE BODY...
# cli [ARGS]：不经 tmux 直接运行（启动失败的用例），OUT 为 stdout+stderr，CODE 为退出码；20 秒后强制结束
cli() { OUT=$(XDG_CONFIG_HOME="$CFG/x" XDG_STATE_HOME="$CFG/s" perl -e 'alarm 20; exec @ARGV' "$E2E_BIN" "$@" </dev/null 2>&1); CODE=$?; }
failed() {   # TEXT — 退出码 1、输出含 TEXT、没进界面、没 panic
  [[ $CODE == 1 && $OUT == *"$1"* && $OUT != *$'\e[?1049h'* && $OUT != *panic* && $OUT != *goroutine* ]] || { echo "  exit $CODE: $OUT"; false; }
}
# ui CMD：在 tmux 里运行 CMD（sh 语法），connections.toml 取自 conns
ui() { e2e_start -C "$DIR" "$1"; wait_for 5 flag_is alternate_on 1; sleep 0.3; }
# conns_of [APP]：postgres 库上 application_name 为 APP（默认 sqlmux）的连接数；别人跑的 sqlmux 连的是 sqlmux 库
conns_of() { psql "$SQLMUX_TEST_PG" -Atc "select count(*) from pg_stat_activity where datname = 'postgres' and application_name = '${1:-sqlmux}'"; }

# ---- 启动失败：终端打印错误，退出码 1，不进入界面（§14「启动时找不到连接」）
rm -rf "$DIR"; cli
check "没有 connections.toml：错误写明文件路径" failed "$DIR/connections.toml"
conns 600 '# nothing here'; cli
check "文件里没有 [[connection]]：错误写明文件路径" failed "$DIR/connections.toml"
conns 600 "$(conn a "$SQLMUX_TEST_PG")" "$(conn b "$SQLMUX_TEST_PG")"; cli nosuch
check "sqlmux <名字> 找不到：列出已有的连接名" failed 'a, b'
conns 600 "$(conn t "$DSN" 'password = "wrong"')"; cli
check "密码错误：打印数据库返回的错误，不 panic" failed 'password authentication failed'
conns 600 "$(conn t "$DSN")"; cli
check "DSN 没有密码、也没有密码来源：连不上（对照：下面三种来源是真的生效了）" failed 'password authentication failed'
conns 600 "$(conn t "$DSN" 'password_cmd = "echo no-keychain >&2; exit 3"')"; cli
check "password_cmd 非 0 退出：错误里带它的 stderr" failed 'no-keychain'
conns 600 "$(conn t "$DSN" 'password_env = "SQLMUX_E2E_UNSET"')"; cli
check "password_env 指向的变量没设置：错误里写明变量名" failed 'SQLMUX_E2E_UNSET'
conns 600 "$(conn t "postgres://$PGU@$PGH:1/sqlmux?sslmode=disable")"; cli
check "端口连不上：打印连接错误" failed 'connection refused'
# 连接超时：没写时补成 10s；DSN 里写了就按 DSN（§8.1）。10.255.255.1 不回包
conns 600 "$(conn t 'postgres://u@10.255.255.1:5432/x')"; s=$(date +%s); cli; took=$(( $(date +%s) - s ))
check "没写 connect_timeout：约 10 秒后放弃（${took}s）" eval 'failed timeout && ((took >= 9 && took <= 13))'
conns 600 "$(conn t 'postgres://u@10.255.255.1:5432/x?connect_timeout=2')"; s=$(date +%s); cli; took=$(( $(date +%s) - s ))
check "connect_timeout=2：约 2 秒后放弃（${took}s）" eval 'failed timeout && ((took <= 4))'

# ---- 三种密码来源都能连上（§13「凭据」）；session 名取连接的 name
conns 600 "$(conn viacmd "$DSN" "password_cmd = \"printf '%s\\\\n' '$PW'\"")"; ui "$E2E_BIN"
check "password_cmd：进入界面，状态栏 session 名为 viacmd" eval 'running && [[ $(bar) == *" viacmd ▾ "* ]]'
conns 600 "$(conn viaenv "$DSN" 'password_env = "SQLMUX_E2E_PW"')"; ui "SQLMUX_E2E_PW='$PW' $E2E_BIN"
check "password_env：进入界面" eval 'running && [[ $(bar) == *" viaenv ▾ "* ]]'
mkdir -p "$CFG/home"; printf '%s:%s:*:%s:%s\n' "$PGH" "$PGP" "$PGU" "$PW" >"$CFG/home/.pgpass"; chmod 600 "$CFG/home/.pgpass"
conns 600 "$(conn viapgpass "$DSN")"; ui "env -u PGPASSFILE HOME='$CFG/home' $E2E_BIN"
check "~/.pgpass：进入界面" eval 'running && [[ $(bar) == *" viapgpass ▾ "* ]]'

# ---- 选哪个连接：不指定时用第一个，sqlmux <名字> 用那一个
conns 600 "$(conn first "$SQLMUX_TEST_PG")" "$(conn second "$SQLMUX_TEST_PG")"
ui "$E2E_BIN"; check "不指定连接名：用第一个（first）" eval '[[ $(bar) == *" first ▾ "* ]]'
ui "$E2E_BIN second"; check "sqlmux second：用 second" eval '[[ $(bar) == *" second ▾ "* ]]'

# ---- 默认界面（§5、§7.8 状态栏）：一个 window data，侧栏 + 占满其余宽度的空 data pane
start
check "只有 ⓪ 侧栏和 ① data 两块，焦点在 ①" eval '[[ $(e2e_panes | awk "{ print \$1 }" | tr "\n" " ") == "0 1 " && $(focused) == 1 ]] && [[ $(e2e_find ┐ 1) == "32 160" ]]'
check "① 是空 pane：tab 栏只有 +" eval '[[ $(e2e_text 34 160 43 | tr -s " ") == "│ + │" ]]'
check "状态栏：连接名 doraemon、只有 0: data*、地址 <用户>@<host>:<port>" eval 'b=$(bar); [[ $b == *" doraemon ▾  0: data* "* && $b != *"1: "* && $b == *" $PGU@$PGH:$PGP  NORMAL " ]] || { echo "  $b"; false; }'

# ---- Main、Meta 两条连接，application_name 默认 sqlmux，退出时都关掉（§8.1、§8.2）
PGDB=$(python3 -c 'import sys, urllib.parse as u; print(u.urlsplit(sys.argv[1])._replace(path="/postgres").geturl())' "$SQLMUX_TEST_PG")
conns 600 "$(conn db "$PGDB")"; ui "$E2E_BIN"
check "application_name 没写：两条连接都是 sqlmux" eval '[[ $(conns_of) == 2 ]]'
e2e_type ':qa'; e2e_keys Enter
check ":qa 退出后两条连接都关掉" eval 'wait_for 3 screen_has "[e2e-exit 0]" && wait_for 3 eval "[[ \$(conns_of) == 0 ]]"'
conns 600 "$(conn db "$PGDB&application_name=e2e-f11-$$")"; ui "$E2E_BIN"
check "DSN 写了 application_name：照用" eval '[[ $(conns_of e2e-f11-$$) == 2 ]]'

# ---- 明文 password 且同组或其他用户可读：进入界面后 toast 警告，3 秒后消失（§13）
WARN='connections.toml 里有明文密码，且其他用户可读，建议 chmod 600'
conns 644 "$(conn t "$DSN" "password = \"$PW\"")"; ui "$E2E_BIN"
check "0644：toast 警告" toast_is "$WARN"
sleep 3
check "3 秒后 toast 消失" eval '! screen_has "明文密码"'
conns 640 "$(conn t "$DSN" "password = \"$PW\"")"; ui "$E2E_BIN"
check "0640（同组可读）：toast 警告" toast_is "$WARN"
conns 600 "$(conn t "$DSN" "password = \"$PW\"")"; ui "$E2E_BIN"
check "0600：没有警告" eval 'running && ! screen_has "明文密码"'

e2e_done
