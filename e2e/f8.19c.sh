#!/usr/bin/env bash
# F8.19 审查时定：已连上的连接在连接管理里改了 dsn，重拨用的是新配置（tech-design §8.2「断线重连」）
# 改的是 dsn 里的 application_name（连接管理里 dsn 写法的连接在「高级」里改 dsn），杀掉旧连接后表格 R 刷新：新连上的后端叫新名字。
# 钥匙串不碰（密码走 password_cmd 读文件）。自建库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
APP=e2e-f819c-$$ DB=sqlmux_e2e_$$
read -r NOPW PW < <(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(p._replace(netloc=p.netloc.replace(":" + p.password + "@", "@")).geturl(), p.password)' "$E2E_DB")
mkdir -p "$D/c"; printf '%s\n' "$PW" >"$D/pw"
(umask 077; printf '[[connection]]\nname = "rw"\nengine = "postgres"\ndsn = "%s"\npassword_cmd = "cat %s"\n' "$NOPW&application_name=$APP" "$D/pw" >"$D/c/connections.toml")
admin() { psql "$SQLMUX_TEST_PG" -qAt -c "$1"; }
conns() { admin "select count(*) from pg_stat_activity where application_name = '$1' and datname = '$DB'"; }
kill_all() { admin "select count(pg_terminate_backend(pid)) from pg_stat_activity where application_name = '$1' and datname = '$DB'" >/dev/null; wait_for 5 eval '[[ $(conns "$1") == 0 ]]'; }
toast_text() { e2e_text 1 "$(e2e_flag pane_width)" $(( $(H) - 1 )) | sed 's/[ │└┘─┌┐]//g'; }
rows() { e2e_plain | grep -cE "│ +[0-9]+ │ +[0-9]+ │ user_"; }

SOLO= start -x 200 -C "$D/c"; e2e_record "$D/rec"; open_table t_user; wait_for 8 settled
check "（准备）rw 用 application_name=$APP 连着，两条" eval '[[ $(conns "$APP") == 2 ]]'
pal ">conn.manage"; key Enter; sleep 0.7
key Tab; for i in 1 2 3 4 5 6 7; do key Tab; done; key End; e2e_type b; sleep 0.3
check "（准备）连接管理里高级的 dsn 末尾改成 …${APP}b" eval 'e2e_plain | grep -q "application_name=${APP}b"'
key Enter; sleep 1
check "保存：toast「下次连接时生效」，连接没有马上重连（旧名字的两条还在）" eval 'e2e_recorded "下次连接时生效" && [[ $(conns "$APP") == 2 ]]'
key Escape; key Escape; sleep 0.3
check "（准备）连接管理已关闭，焦点回到表格" eval '! e2e_plain | grep -q "┌─ 连接" && [[ $(focused) == 1 ]]'
kill_all "$APP"
e2e_click 60 8; key R; wait_for 8 settled; sleep 1.5
check "杀掉旧连接后按 R：数据回来；重拨用的是新 dsn，服务端看到的后端叫 ${APP}b，旧名字没有再连" eval '(( $(rows) >= 5 )) && (( $(conns "${APP}b") >= 1 )) && [[ $(conns "$APP") == 0 ]] || { echo "  rows=$(rows) new=$(conns "${APP}b") old=$(conns "$APP")"; false; }'
e2e_done
