#!/usr/bin/env bash
# F18.13 表单拼的 mssql DSN 连得上，错误行不截断（specs/m18-windows-mssql/task.md F18.13；tech-design §7.8「新建连接表单」、§8.1「SQL Server」）
# 新建连接表单选 mssql、填 host / port / user / password / database：「测试」连的是填的 host:port（不是退回 localhost:1433），成功行带版本，保存后
# connections.toml 里是 sqlserver:// 开头；连不上时错误行按表单宽度折成几行、全文可见、没有 …；手写 mssql:// 开头的 DSN 也连得上。
# 连用户 Windows 主机上的 SQL Server 容器（SQLMUX_TEST_MSSQL），只读（测试、连上看库）。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

[[ -n $SQLMUX_TEST_MSSQL ]] || { echo "e2e: SQLMUX_TEST_MSSQL is not set, skipped"; echo "== 0 passed, 0 failed"; exit 0; }
read -r MS_USER MS_PW MS_HOST MS_PORT MS_DB < <(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); q = u.parse_qs(p.query); print(p.username, p.password, p.hostname, p.port, q.get("database", ["master"])[0])' "$SQLMUX_TEST_MSSQL")
D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'chmod -R u+w "$D" 2>/dev/null; e2e_stop; rm -rf "$D"' EXIT
ct() { echo "$E2E_TMP/config/sqlmux/connections.toml"; }
box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5; exit }'; }
frows() { local g y; g=($(box)); for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 28)) $((g[0] + g[2] - 2)) "$y" | sed 's/^ *//; s/ *$//'; done; }   # 表单（右半边）的每一行
# 底栏上方的结果 / 错误行：「高级 ▸ / ▾」那行（或高级展开后的最后一个字段）之后、分隔线之前的全部行
result_rows() { frows | python3 -c '
import sys
on = False
for l in sys.stdin.read().split("\n"):
    l = l.strip()
    if "高级" in l: on = True; continue
    if l and set(l) == {"─"}: on = False
    if on and l: print(l)'; }
clr() { local i; for ((i = 0; i < 40; i++)); do e2e_keys BSpace; done; sleep 0.2; }
open_form() { pal ">新建连接"; key Enter; sleep 0.8; }
# 表单：name → group → engine → host → port → user → password → database
fill() { # NAME HOST PORT USER PW DB（engine 先选到 mssql）
  e2e_type "$1"; key Tab; key Tab; for i in 1 2 3 4; do e2e_keys Space; sleep 0.2; done; key Tab
  clr; e2e_type "$2"; key Tab; clr; e2e_type "$3"; key Tab; e2e_type "$4"; key Tab; e2e_type "$5"; key Tab; e2e_type "$6"; sleep 0.3; }

start; sleep 1

# ---- 连不上的 host：错误是填的 host:port 的，折行、全文可见
open_form
fill bad 127.0.0.1 1 sa pw master
e2e_keys C-t; sleep 6
check "连不上的 host：测试连的是填的 127.0.0.1:1（错误里有 '127.0.0.1:1'，不是 localhost:1433）" eval 'result_rows | tr "\n" " " | grep -q "127.0.0.1:1"'
check "错误行折成两行以上，全文可见（末尾的 dial tcp 127.0.0.1:1 在里面），没有 …" eval '[[ $(result_rows | wc -l | tr -d " ") -ge 2 ]] && result_rows | tr "\n" " " | grep -q "dial tcp 127.0.0.1:1" && ! result_rows | grep -q "…" || { result_rows; false; }'
check "表单还开着，没有保存（connections.toml 里没有 bad）" eval 'e2e_plain | grep -q "┌─ 连接" && ! grep -q "bad" "$(ct)" 2>/dev/null'
key Escape; sleep 0.4; key Escape; sleep 0.4

# ---- 窗口压矮：框取窗口高度，只画得下的行，按钮还在
e2e_stop
start -y 17; sleep 1
open_form
fill bad 127.0.0.1 1 sa pw master
e2e_keys C-t; sleep 6
check "窗口只有 17 行（表单本身 16 行，加上两行错误放不下）：表单框不超出窗口（框底在状态栏之前），按钮行「[ 保存 ] [ 测试 ] [ 取消 ]」还在，程序没崩" eval 'g=($(box)); (( g[1] + g[3] <= 17 )) && frows | grep -qF "[ 保存 ] [ 测试 ] [ 取消 ]" && running || { echo "  box $(box)"; frows; false; }'
check "矮窗口里错误行画了放得下的第一行（unable to open tcp connection…）" eval 'frows | grep -q "unable to open tcp connection"'
e2e_stop
start; sleep 1

# ---- 连得上的（Windows 主机上的容器）：表单填 host / port / user / password / database，测试成功，行里带版本；保存后文件里是 sqlserver://
open_form
fill msf "$MS_HOST" "$MS_PORT" "$MS_USER" "$MS_PW" "$MS_DB"
e2e_keys C-t; sleep 6
check "填 $MS_HOST:$MS_PORT 的 SQL Server：测试成功，结果行「已连接 · SQL Server <版本> · <耗时>ms」" eval 'result_rows | grep -qE "已连接 · .*SQL Server.* [0-9]+(\.[0-9]+)+.*· [0-9]+ms" || { result_rows; false; }'
key Enter; sleep 3
check "保存：connections.toml 里 msf 的 dsn 以 sqlserver:// 开头，带填的 host:port 和 database" eval 'grep -A4 "name = \"msf\"" "$(ct)" | grep -q "dsn = \"sqlserver://.*$MS_HOST:$MS_PORT.*database=$MS_DB"'
check "树里有 msf 节点，程序没崩" eval 'e2e_plain | grep -q "msf" && running'
key Escape; sleep 0.4

# ---- 手写 mssql:// 开头的 DSN 也认（F18.13 a）
e2e_stop
mkdir -p "$D/c"
(umask 077; printf '[[connection]]\nname = "hand"\nengine = "mssql"\ndsn = "mssql://%s:%s@%s:%s?database=%s&TrustServerCertificate=true"\n' "$MS_USER" "$MS_PW" "$MS_HOST" "$MS_PORT" "$MS_DB" >"$D/c/connections.toml")
SOLO= start -C "$D/c" -S "$D/st2"; sleep 1
wait_for 20 eval 'e2e_plain | grep -q "dbo"'
check "手写 mssql://user:pw@host:port?database=… 的连接：连上了，树里有 dbo / Tables" eval 'e2e_plain | grep -q "dbo" && e2e_plain | grep -q "Tables ("'
e2e_done
