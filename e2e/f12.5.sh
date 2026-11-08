#!/usr/bin/env bash
# F12.5 连接连不上不退出、表单的测试按钮、tab 移到相邻 pane（specs/m12-grid/task.md F12.5；tech-design §14「恢复」、§7.8「新建连接表单」「把 tab 移到相邻 pane」）
# 启动连接连不上（--layout、sqlmux <连接名>）：进界面，节点标失败，toast 驱动的错误；挂起的表 tab 留着、居中画错误页；连上之后 R 取到数据；
# conn.manage 能建新连接；表单 C-t 测试（错端口是错误行、改对后是「已连接」，不保存）；SPC wn / wp 把 tab 移到下一个 / 上一个 pane。
# 连接经一个本地转发（python）连 PG，转发停了就是「没人听的端口」；只读，用共用库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/paging.sh"

read -r DSN PGU PW PGH PGP <<<"$(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(p._replace(netloc=f"{p.username}:{p.password}@127.0.0.1:PORT").geturl(), p.username, p.password, p.hostname, p.port)' "$SQLMUX_TEST_PG")"
LP=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; fwd_stop; rm -rf "$D"' EXIT
cat >"$D/fwd.py" <<'PY'
import socket, sys, threading
lp, host, port = int(sys.argv[1]), sys.argv[2], int(sys.argv[3])
srv = socket.socket(); srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); srv.bind(("127.0.0.1", lp)); srv.listen(16)
def pipe(a, b):
    try:
        while (d := a.recv(65536)):
            b.sendall(d)
    except OSError: pass
    for s in (a, b):
        try: s.shutdown(socket.SHUT_RDWR)
        except OSError: pass
while True:
    c, _ = srv.accept(); u = socket.create_connection((host, port))
    threading.Thread(target=pipe, args=(c, u), daemon=True).start(); threading.Thread(target=pipe, args=(u, c), daemon=True).start()
PY
FWD=
fwd_start() { python3 "$D/fwd.py" "$LP" "$PGH" "$PGP" & FWD=$!; sleep 0.6; }
fwd_stop() { [[ -n $FWD ]] && { kill "$FWD" 2>/dev/null; wait "$FWD" 2>/dev/null; FWD=; }; }
mkdir -p "$D/c"; printf '[[connection]]\nname = "play"\nengine = "postgres"\ndsn = "%s"\n' "${DSN/PORT/$LP}" >"$D/c/connections.toml"; chmod 600 "$D/c/connections.toml"
save_layout() { e2e_keys C-p; sleep 0.3; e2e_type ">save layout"; sleep 0.2; for i in $(seq 20); do e2e_keys BSpace; done; e2e_type ">保存布局"; sleep 0.5; key Enter; sleep 0.4; e2e_type "$1"; key Enter; sleep 0.6; }
quit_app() { e2e_type ":"; sleep 0.3; e2e_type qa; key Enter; wait_for 8 exited; sleep 0.4; }
tree_y() { local y; for y in $(seq 3 43); do [[ $(e2e_text 1 32 $y) == *"$1"* ]] && { echo $y; return; }; done; }
ERR=#f7768e

# ---- 做一个有表 tab 的布局（连接是活的），退出
fwd_start
SOLO=1 start -C "$D/c" -S "$D/st" -D "$D/data"; open_table t_order; wait_for 8 settled
save_layout lay
cp -R "$E2E_TMP/config/sqlmux/layouts" "$D/c/"
quit_app; fwd_stop

# ---- --layout：连接连不上（转发停了）
e2e_start -C "$D/c" -S "$D/st2" -D "$D/data2" "$E2E_BIN --layout lay"; wait_for 6 flag_is alternate_on 1; sleep 3
check "--layout 的连接连不上：进界面，没有退出（不是 [e2e-exit]）" eval 'running && ! screen_has "[e2e-exit"'
check "树里 play 节点标失败（error 色）" eval 'y=$(tree_y play); [[ -n $y ]] && c=$(e2e_find play $y | cut -d" " -f1) && style_has "$c" "$y" fg=$ERR'
check "toast 或错误页里有驱动的错误原文（failed to connect to … 带地址）" eval 'screen_has "failed to connect"'
check "表 tab 还在（标签 1: t_order，没有被关掉），查询条照旧画着" eval '[[ $(tabbar 1) == *t_order* ]] && qb_has "ORDER"'
g=($(geom 1)); cx=$((g[0] + g[2] / 2))
check "内容区居中画错误页：第一行是连接名 play，下面是错误原文（failed to connect）" eval 'y=$(e2e_plain | awk "/^│.*│ +play +│/ && NR > 8 { print NR; exit }"); [[ -n $y ]] && c=$(e2e_find play $y | tr " " "\n" | awk -v l=${g[0]} "\$1 > l + 20 { print; exit }") && (( c > cx - 10 && c < cx + 10 )) && e2e_plain | sed -n "$((y + 1)),$((y + 3))p" | grep -q "failed to connect" || { echo "  y=$y c=$c cx=$cx"; false; }'
check "错误页是 error 色（play 那一行）" eval 'y=$(e2e_plain | awk "/^│.*│ +play +│/ && NR > 8 { print NR; exit }"); c=$(e2e_find play $y | tr " " "\n" | awk -v l=${g[0]} "\$1 > l + 20 { print; exit }"); style_has "$c" "$y" fg=$ERR'
e2e_keys C-p; sleep 0.3; e2e_type ">表的 DDL"; sleep 0.6; key Enter; sleep 0.8
check "错误页上从命令面板开「表的 DDL」：不崩，toast「连接还没连上」或照常报错，界面还在" eval 'running && ! screen_has "panic" && ! screen_has "[e2e-exit"'
key Escape; sleep 0.3
# ---- 连接恢复（转发起来）：R 重试，取到数据，错误页没了
fwd_start
key R; sleep 2.5; wait_for 8 settled
check "转发起来之后 R：重试，取到数据（表头有 user_id，错误页没了）" eval 'e2e_plain | grep -q "user_id" && ! screen_has "failed to connect"'
check "树里 play 节点不再是失败色" eval 'y=$(tree_y play); c=$(e2e_find play $y | cut -d" " -f1); [[ $(e2e_style "$c" "$y") != *fg=$ERR* ]]'
quit_app; fwd_stop

# ---- sqlmux <连接名>：连不上也进界面（默认布局），toast，query tab 在
e2e_start -C "$D/c" -S "$D/st3" -D "$D/data3" "$E2E_BIN play"; wait_for 6 flag_is alternate_on 1; sleep 0.8
check "sqlmux play 连不上：进界面，toast 里是驱动的错误" eval 'running && screen_has "failed to connect"'
sleep 3
check "默认布局的 query tab 照旧在（② 里有 query_1），树里 play 是失败色" eval '[[ $(tabbar 2) == *query_1* ]] && y=$(tree_y play) && c=$(e2e_find play $y | cut -d" " -f1) && style_has "$c" "$y" fg=$ERR'

fwd_start
key C-l; sleep 0.3
setsql "select 1 as one"; key Enter; sleep 3
check "转发起来之后在 query 里第一次执行：触发重拨（树里 play 有表了），这一次不跑" eval '[[ $(e2e_plain | grep -c "t_order") -ge 1 ]] && ! e2e_plain | grep -qE "│ +one +│"'
key Enter; sleep 1.5; wait_for 8 settled
check "连上之后再执行：结果 tab 出来（列 one）" eval 'e2e_plain | grep -qE "│ +one +│"'
fwd_stop
# ---- 管理连接能开，新建连接能建（连不上的 play 还在）
e2e_keys C-p; sleep 0.3; e2e_type ">manage conn"; sleep 0.2; for i in $(seq 20); do e2e_keys BSpace; done; e2e_type ">管理连接"; sleep 0.6; key Enter; sleep 0.8
check "conn.manage（管理连接）能开：列表里有连不上的 play 和「新建」" eval 'e2e_plain | grep -q "┌─ 连接" && e2e_plain | grep -qE "│ [^│]*play[^│]* │ " && e2e_plain | grep -qE "│ [^│]*新建[^│]* │ "'
key Escape; sleep 0.4
e2e_keys C-p; sleep 0.3; e2e_type ">new conn"; sleep 0.2; for i in $(seq 20); do e2e_keys BSpace; done; e2e_type ">新建连接"; sleep 0.6; key Enter; sleep 0.8
key Down; sleep 0.3
e2e_type okconn; key Tab; key Tab; key Tab; e2e_type 127.0.0.1; key Tab; for i in 1 2 3 4 5 6; do e2e_keys BSpace; done; e2e_type "$PGP"; key Tab; e2e_type "$PGU"; key Tab; e2e_type "$PW"; key Tab; e2e_type sqlmux; sleep 0.3
key Enter; sleep 2.5
check "连接管理里新建 okconn（真端口）：保存后树里有 okconn 节点" eval 'running && [[ -n $(tree_y okconn) ]]'
key Escape; sleep 0.4
quit_app

# ---- 表单测试按钮：C-t
start; sleep 1
e2e_keys C-p; sleep 0.3; e2e_type ">new conn"; sleep 0.2; for i in $(seq 20); do e2e_keys BSpace; done; e2e_type ">新建连接"; sleep 0.6; key Enter; sleep 0.8
key Down; sleep 0.3
check "表单底部有 [ 保存 ] [ 测试 ] [ 取消 ] 三个按钮" eval 'e2e_plain | grep -qF "[ 保存 ] [ 测试 ] [ 取消 ]"'
e2e_type tt; key Tab; key Tab; key Tab; e2e_type 127.0.0.1; key Tab; for i in 1 2 3 4 5 6; do e2e_keys BSpace; done; e2e_type 1; key Tab; e2e_type "$PGU"; key Tab; e2e_type "$PW"; key Tab; e2e_type sqlmux; sleep 0.3
e2e_keys C-t; wait_for 14 eval 'e2e_plain | grep -q "failed to connect"'
check "C-t（端口 1）：底栏上方一行是驱动的错误（failed to connect，带 127.0.0.1:1），表单还开着" eval 'e2e_plain | grep -q "failed to connect" && e2e_plain | grep -q "127.0.0.1:1" && e2e_plain | grep -q "┌─ 连接"'
for i in 1 2 3; do e2e_keys BTab; sleep 0.15; done; for i in 1 2 3 4 5 6; do e2e_keys BSpace; done; e2e_type "$PGP"; sleep 0.2; e2e_keys C-t; sleep 3.5
check "改成真端口再 C-t：结果行是「已连接 · PostgreSQL <版本> · <耗时>ms」" eval 'e2e_plain | grep -qE "已连接 · PostgreSQL [0-9.]+ .*· [0-9]+ms"' || e2e_plain | grep -E '已连接|failed' | head -3
check "测试不保存：树里没有 tt 节点，connections.toml 里也没有（取消表单后）" eval 'key Escape; sleep 0.4; key Escape; sleep 0.4; ! grep -q "name = \"tt\"" "$E2E_TMP/config/sqlmux/connections.toml" && [[ -z $(tree_y " tt") ]]'
_e2e_kill

# ---- SPC wn / wp：把 tab 移到下一个 / 上一个 pane
SOLO= start; open_table t_order; wait_for 8 settled
check "（准备）① 里是 t_order，② 里是 query_1，焦点在 ①" eval '[[ $(tabbar 1) == *t_order* && $(tabbar 2) == *query_1* ]] && focus_is 1'
key Space; key w n; sleep 0.8
check "SPC wn：t_order 挪到 ②（排在 query_1 后面，成为当前 tab），焦点跟过去" eval '[[ $(tabbar 2) == *query_1*t_order* ]] && focus_is 2 && ! [[ $(tabbar 1) == *t_order* ]]'
key Space; key w p; sleep 0.8
check "SPC wp：挪回 ①，焦点回 ①，② 里只剩 query_1" eval '[[ $(tabbar 1) == *t_order* && $(tabbar 2) == *query_1* && $(tabbar 2) != *t_order* ]] && focus_is 1'
_e2e_kill
# 目标只有引导页时替换它；来源空了留引导页
start; sleep 1; key C-l; sleep 0.3
check "（准备）默认布局：① 引导页（没有 tab），② query_1，焦点在 ②" eval 'focus_is 2 && [[ $(tabbar 2) == *query_1* ]]'
key Space; key w p; sleep 0.8
check "② 的 query_1 用 SPC wp 移到 ①：① 的引导页被换掉（tab 栏里是 query_1），焦点在 ①，② 空了留引导页" eval '[[ $(tabbar 1) == *query_1* ]] && focus_is 1 && [[ $(tabbar 2) != *query_1* ]]'
_e2e_kill
# zoom 时不做事
SOLO= start; open_table t_order; wait_for 8 settled
key Space; key z; sleep 0.6; key Space; key w n; sleep 0.8
key Space; key z; sleep 0.6
check "zoom 时 SPC wn 不做事：t_order 还在 ①，焦点还在 ①" eval '[[ $(tabbar 1) == *t_order* ]] && focus_is 1'
_e2e_kill
# 只有一个普通 pane 时不做事
SOLO=1 start; open_table t_order; wait_for 8 settled
key Space; key w n; sleep 0.8
check "只有一个普通 pane：SPC wn 不做事，t_order 还在，没崩" eval 'running && [[ $(tabbar 1) == *t_order* ]]'
e2e_done
