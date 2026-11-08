#!/usr/bin/env bash
# F8.16 复测补充（reviewer 提测要点）：窄屏上的键位提示、长列表、只读（含符号链接、零连接 + 只读配置目录）、写盘不留 probe 文件。
# 英文下（默认语言，E2E_NOLANG=1）80×24：新建和修改两种表单的提示都看得见；30 多个连接按 j 翻：选中项一直停在列表最后一行；
# 钥匙串一律用 lib.sh 的假 security。只读 PG，不写库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'chmod -R u+w "$D" "$E2E_TMP/config" 2>/dev/null; e2e_stop; rm -rf "$D"' EXIT
ct() { echo "$E2E_TMP/config/sqlmux/connections.toml"; }
box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }
mgr_open() { e2e_plain | grep -qE "┌─ (连接|Connections)"; }
clr() { local i; for ((i = 0; i < 40; i++)); do e2e_keys BSpace; done; sleep 0.2; }
cmdp() { pal ">$1"; key Enter; sleep 0.7; }
frows() { local g y; g=($(box)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 28)) $((g[0] + g[2] - 2)) "$y" | noicon | sed 's/^ *//; s/ *$//'; done; }
read -r PG_USER PG_PW PG_HOST PG_PORT PG_DB < <(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(p.username, p.password, p.hostname, p.port, p.path[1:])' "$SQLMUX_TEST_PG")
# 英文下的 fill：同 f7.4 的 fill（name→engine→host→port→user→password→database）
fill() { e2e_type "$1"; key Tab; key Tab; key Tab; e2e_type "$2"; key Tab; clr; e2e_type "$3"; key Tab; e2e_type "$4"; key Tab; e2e_type "$5"; key Tab; e2e_type "$6"; sleep 0.3; }

# ---- 英文 80×24：新建（零连接启动，浮层自动开）
E2E_NOLANG=1 E2E_NOCONN=1 start -x 80 -y 24; sleep 1.5
check "英文 80×24 新建：浮层在屏幕里（不出界）" eval 'g=($(box)); (( g[0] >= 1 && g[1] >= 1 && g[0] + g[2] <= 81 && g[1] + g[3] <= 25 ))'
check "英文 80×24 新建：按钮行看得见，键位提示（esc）也看得见（放不下同一行时在按钮下一行）" eval 'p=$(frows); grep -q "Save" <<<"$p" && grep -qi "esc" <<<"$p" || { echo "$p" | tail -4; false; }'
key Escape; sleep 0.3

# ---- 英文 80×24：修改已有连接（多一个 Delete 按钮）
(umask 077; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$SQLMUX_TEST_PG" >|"$D/one.toml"); mkdir -p "$D/c1"; cp "$D/one.toml" "$D/c1/connections.toml"
E2E_NOLANG=1 start -x 80 -y 24 -C "$D/c1"; sleep 0.5
cmdp conn.manage
check "英文 80×24 修改：浮层在屏幕里，按钮行有 Save / Cancel / Delete，键位提示也看得见" eval 'g=($(box)); p=$(frows); (( g[0] >= 1 && g[1] >= 1 && g[0] + g[2] <= 81 && g[1] + g[3] <= 25 )) && grep -q "Save" <<<"$p" && grep -q "Delete" <<<"$p" && grep -qi "esc" <<<"$p" || { echo "$p" | tail -5; false; }'
key Escape; sleep 0.3

# ---- 长列表：35 个连接，80 列下按 j 往下翻
mkdir -p "$D/c35"; (umask 077; for i in $(seq -w 1 35); do printf '[[connection]]\nname = "c%s"\nengine = "postgres"\ndsn = "%s"\n\n' "$i" "$SQLMUX_TEST_PG"; done >|"$D/c35/connections.toml")
start -x 80 -y 24 -C "$D/c35"; sleep 0.5
cmdp conn.manage
sel_y() { local g y; g=($(box)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do style_has $((g[0] + 20)) "$y" bg=#364a82 >/dev/null && { echo $y; return; }; done; }
fname() { frows | awk '$1 == "name" { print $2; exit }'; }
for i in $(seq 25); do key j; done; sleep 0.3
y25=$(sel_y)
for i in $(seq 5); do key j; done; sleep 0.3
y30=$(sel_y)
check "35 个连接、80 列按 j 往下翻：选中项（c26 → c31）一直停在同一行（列表最后一行），不错一行" eval '[[ -n $y25 && $y25 == $y30 ]] && [[ $(fname) == c31 ]] || { echo "  y25=$y25 y30=$y30 name=$(fname)"; false; }'
for i in $(seq 4); do key j; done; sleep 0.3
check "再翻到最后一个：表单是 c35" eval '[[ $(fname) == c35 ]]'
for i in $(seq 40); do key k; done; sleep 0.3
check "按 k 回到第一个：表单是 c01" eval '[[ $(fname) == c01 ]]'
key Escape; sleep 0.3

# ---- 写盘：新建、修改保存成功后配置目录里没有 probe-* 文件
write_one() { E2E_NOLANG=1 start -C "$D/c1"; sleep 0.5; }
write_one
cmdp conn.new; fill pr1 "$PG_HOST" "$PG_PORT" "$PG_USER" "$PG_PW" "$PG_DB"; key Enter; sleep 1.2
check "新建保存成功：connections.toml 里有 pr1，配置目录里没有 probe-* 文件" eval 'grep -q "^name = \"pr1\"" "$(ct)" && [[ -z $(ls -A "$E2E_TMP/config/sqlmux" | grep "^probe") ]] || { ls -A "$E2E_TMP/config/sqlmux"; false; }'
key Escape; key Escape; sleep 0.3
cmdp conn.manage; key Tab; key Tab; key Tab; key Tab; key Tab; e2e_keys Space; key C-s; sleep 1.2
check "修改保存成功（doraemon 勾上 read_only）：配置目录里仍没有 probe-* 文件" eval 'grep -q "^read_only = true" "$(ct)" && [[ -z $(ls -A "$E2E_TMP/config/sqlmux" | grep "^probe") ]]'
key Escape; key Escape; sleep 0.3

# ---- 只读：符号链接指向只读文件
write_one
cp "$D/one.toml" "$D/real.toml"; chmod 444 "$D/real.toml"; rm -f "$(ct)"; ln -s "$D/real.toml" "$(ct)"
cmdp conn.new; fill rosym "$PG_HOST" "$PG_PORT" "$PG_USER" "pw-sym" "$PG_DB"; key Enter; sleep 1
e2e_skip_if_root "connections.toml 是指向只读文件的符号链接：保存被拒绝（浮层不关、有错误），文件没变，链接还是链接" || check "connections.toml 是指向只读文件的符号链接：保存被拒绝（浮层不关、有错误），文件没变，链接还是链接" eval 'mgr_open && cmp -s "$D/real.toml" "$D/one.toml" && [[ -L "$(ct)" ]]'
e2e_skip_if_root "…而且没有碰钥匙串（没有 sqlmux-rosym）" || check "…而且没有碰钥匙串（没有 sqlmux-rosym）" eval '[[ ! -e "$E2E_TMP/keychain/sqlmux-rosym" ]]'
key Escape; key Escape; sleep 0.3
chmod 644 "$D/real.toml"

# ---- 只读：零连接启动 + 只读的配置目录
E2E_NOLANG= E2E_NOCONN=1 start; sleep 1.5
chmod 555 "$E2E_TMP/config/sqlmux"
fill rodir "$PG_HOST" "$PG_PORT" "$PG_USER" "pw-dir" "$PG_DB"; key Enter; sleep 1
e2e_skip_if_root "还没有 connections.toml、配置目录是只读：保存被拒绝（浮层不关），没有建出文件" || check "还没有 connections.toml、配置目录是只读：保存被拒绝（浮层不关），没有建出文件" eval 'mgr_open && [[ ! -e "$(ct)" ]]'
e2e_skip_if_root "…而且没有碰钥匙串（没有 sqlmux-rodir）" || check "…而且没有碰钥匙串（没有 sqlmux-rodir）" eval '[[ ! -e "$E2E_TMP/keychain/sqlmux-rodir" ]]'
chmod 755 "$E2E_TMP/config/sqlmux"
key Escape; sleep 0.3
e2e_done
