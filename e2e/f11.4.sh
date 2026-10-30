#!/usr/bin/env bash
# F11.4 结果区滚动卡顿（specs/m11-distribution/task.md F11.4；tech-design §7.6「列宽」、§15「性能」）
# 结果 tab 一页 500 行 × 15 列（一半是中文，有 JSON）：每秒 30 个 j 持续 3 秒，最后一个键之后 100ms 内光标在第 91 行；
# 期间 sqlmux 的 CPU（ps cputime 前后差 / 墙钟）不超过一个核的 E2E_CPU_MAX%（默认 20，机器忙时放宽并在结论里写明）。
# 滚轮同样；表格 pane（100 行）的 j 也量一遍。列宽缓存是按值失效的，这里看它的结果：滚动、G / gg 不改列的位置，
# 转置再转回、隐藏列再显示、翻页、刷新（库里的值变了）、编辑单元格之后列宽都跟着值走。自建库 t_wide（15 列，1203 行）。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/paging.sh"
CPU_MAX=${E2E_CPU_MAX:-20} BURST_MAX=${E2E_BURST_MAX:-750}

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q >/dev/null <<'SQL'
create table t_wide as
select g as id,
  case when g % 2 = 0 then '任务名称' || g || '：数据库迁移与校验' else 'task name ' || g end as name,
  case when g % 3 = 0 then '负责人' || (g % 7) else 'owner' || (g % 7) end as owner,
  jsonb_build_object('id', g, 'desc', repeat('描述abc', 8), 'tags', jsonb_build_array('a', 'b', g)) as payload,
  case when g % 2 = 0 then '状态' else 'state' end as state,
  g * 3 as qty, (g * 1.25)::numeric(10,2) as price, g % 2 = 0 as ok,
  now() - (g || ' minutes')::interval as created_at,
  repeat('备注', 10 + g % 20) as note,
  'c11-' || g as c11, '中文c12-' || g as c12, 'c13' as c13, g::text || '末' as c14, repeat('x', g % 30) as c15
from generate_series(1, 1203) g;
alter table t_wide add primary key (id);
SQL
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"

# ---- 量：N 个 KEY（tmux 键名）或滚轮（KEY 是 wheel:X:Y）按 RATE/秒发出去，最后一个键之后 100ms 读状态栏的行,列；
# 输出 "行,列 CPU% 秒数"（CPU% 是 sqlmux 的 cputime 差除以墙钟，一个核是 100）
hold() {
  local pid; pid=$(pgrep -P "$(t list-panes -t t -F '#{pane_pid}')" | head -1)
  python3 - "$E2E_SOCK" "$pid" "$1" "$2" "$3" <<'PY'
import re, subprocess, sys, time
sock, pid, n, rate, key = sys.argv[1], sys.argv[2], int(sys.argv[3]), float(sys.argv[4]), sys.argv[5]
def cpu():
    t = subprocess.run(["ps", "-o", "cputime=", "-p", pid], capture_output=True, text=True).stdout.strip()
    m, s = t.split(":"); return int(m) * 60 + float(s)
def tmux(*a): return subprocess.run(["tmux", "-L", sock, *a], capture_output=True, text=True).stdout
if key.startswith("wheel:"):
    _, x, y = key.split(":"); send = lambda: tmux("send-keys", "-t", "t", "-l", f"\x1b[<65;{x};{y}M")
else:
    send = lambda: tmux("send-keys", "-t", "t", key)
c0, t0 = cpu(), time.monotonic()
for i in range(n):
    send()
    if i < n - 1:
        d = t0 + (i + 1) / rate - time.monotonic()
        if d > 0: time.sleep(d)
time.sleep(0.1)
last = tmux("capture-pane", "-p", "-t", "t").rstrip("\n").split("\n")[-1]
c1, t1 = cpu(), time.monotonic()
m = re.search(r" (\d+,\d+) ", last)
print(m.group(1) if m else "?", round((c1 - c0) / (t1 - t0) * 100, 1), round(t1 - t0, 1))
PY
}
# 一口气 N 个 KEY（一次 send-keys，事件排成队）：光标到第 N+1 行用了多少毫秒——每个事件一次 Update + View，队列排空的时间就是按住不放时的延迟
burst() {
  python3 - "$E2E_SOCK" "$1" "$2" <<'PY'
import re, subprocess, sys, time
sock, n, key = sys.argv[1], int(sys.argv[2]), sys.argv[3]
def tmux(*a): return subprocess.run(["tmux", "-L", sock, *a], capture_output=True, text=True).stdout
t0 = time.monotonic()
tmux("send-keys", "-t", "t", *([key] * n))
want = f"{n + 1},1"
while time.monotonic() - t0 < 30:
    m = re.search(r" (\d+,\d+) ", tmux("capture-pane", "-p", "-t", "t").rstrip("\n").split("\n")[-1])
    if m and m.group(1) == want: break
    time.sleep(0.005)
print(int((time.monotonic() - t0) * 1000))
PY
}
pos_of() { echo "${1%% *}"; }
cpu_of() { local r=($1); echo "${r[1]}"; }
cpu_ok() { awk -v c="$(cpu_of "$1")" -v m="$CPU_MAX" 'BEGIN { exit !(c <= m) }'; }
# 列的位置：表头那一行 │ 所在的列号（pane 内）
seps() { e2e_plain | python3 -c '
import sys, unicodedata
line = sys.stdin.read().split("\n")[int(sys.argv[1]) - 1]
col, out = 0, []
for ch in line:
    col += 2 if unicodedata.east_asian_width(ch) in "WF" else 1
    if ch == "│": out.append(str(col))
print(",".join(out))' "$1"; }

# 每列的宽度（相邻 │ 的间距，去掉行号栏）：行数变少时行号栏会变窄，整体位置跟着移，列宽才是不变的
colw() { seps "$1" | python3 -c 'import sys; a = [int(x) for x in sys.stdin.read().strip().split(",")]; print(",".join(str(b - c) for b, c in zip(a[2:], a[1:])))'; }

# ---- 表格 pane（一页 100 行）：打开 t_wide，j 按住 3 秒
start -C "$D/own"; sleep 1; open_table t_wide; wait_for 8 settled
HY=$(hy)
S0=$(seps "$HY"); W0=$(colw "$HY")
r=$(hold 90 30 j)
echo "  表格 pane：j × 90 @30/s → $r"
check "表格 pane：每秒 30 个 j 持续 3 秒，最后一个键之后 100ms 内光标在第 91 行" eval '[[ $(pos_of "$r") == "91,1" ]] || { echo "  光标 $(pos_of "$r")"; false; }'
check "表格 pane：这 3 秒里 sqlmux 的 CPU ≤ 一个核的 ${CPU_MAX}%（实测 $(cpu_of "$r")%）" cpu_ok "$r"
check "滚动没有改列的位置：表头的 │ 和滚动前一样" eval '[[ $(seps "$HY") == "$S0" ]] || { echo "  $S0"; echo "  $(seps "$HY")"; false; }'
key G; key g; key g
check "G / gg 回到顶部，列的位置不变" eval '[[ $(seps "$HY") == "$S0" ]] && pos_is 1,1'

# ---- 转置再转回；隐藏列再显示：回到原来的列宽
key T; sleep 0.4
check "转置：表头变成记录号（第一列是字段名），列的位置和竖直视图不同" eval '[[ $(seps "$HY") != "$S0" ]] && e2e_plain | grep -q "│ name"'
key T; sleep 0.4
check "转回：列的位置和原来一样" eval '[[ $(seps "$HY") == "$S0" ]]'
key g; key c; sleep 0.5; key j; key Space; key Escape; sleep 0.5
check "COLS 里取消 id 后：表头没有 id、列的位置变了（少一列）" eval '[[ $(seps "$HY") != "$S0" ]] && ! e2e_text 35 159 "$HY" | grep -qE "│ id +│"'
key g; key c; sleep 0.5; key j; key Space; key Escape; sleep 0.5
check "再勾上：列的位置回到原来的" eval '[[ $(seps "$HY") == "$S0" ]]'

# ---- 翻页：第 2 页（id 101 起）的值不同，列按这一页的值重新算
key "]"; wait_for 8 settled; sleep 0.3
check "] 翻到第 2 页：第一行 id 101，光标回到合理的行" eval 'e2e_plain | grep -qE "│ +101 │ +101 │"'
key "["; wait_for 8 settled; sleep 0.3
check "[ 回第 1 页：列的位置和原来一样（缓存没有把别的页的宽带回来）" eval '[[ $(seps "$HY") == "$S0" ]]'

# ---- 刷新：库里的值变了（第 1 行 name 变得很长），R 之后列宽跟着变；改回来再 R，回到原来的
psql "$E2E_DB" -q -c "update t_wide set owner = repeat('长名字', 12) where id = 1" >/dev/null
key R; wait_for 8 settled; sleep 0.4
check "刷新后：owner 这一页最宽的值变了，列的位置变了，新值（截断）在屏幕上" eval '[[ $(seps "$HY") != "$S0" ]] && e2e_plain | grep -q "长名字长名字"'
psql "$E2E_DB" -q -c "update t_wide set owner = 'owner1' where id = 1" >/dev/null
key R; wait_for 8 settled; sleep 0.4
check "改回来再刷新：列的位置回到原来的" eval '[[ $(seps "$HY") == "$S0" ]]'

# ---- WHERE 去掉的行里有最宽的值：列缩窄；再去掉 WHERE：宽回来（缓存不能停在旧页）
psql "$E2E_DB" -q -c "update t_wide set owner = repeat('长名字', 12) where id = 50" >/dev/null
key R; wait_for 8 settled; sleep 0.4
S1=$(seps "$HY"); W1=$(colw "$HY")
check "（准备）第 50 行的 owner 很长：这一页的 owner 列变宽，列的位置变了" eval '[[ $S1 != "$S0" ]]'
key /; sleep 0.3; e2e_type "id < 10"; key Enter; wait_for 8 settled; sleep 0.4
ow() { colw "$HY" | cut -d, -f5; }   # owner 列的宽度（id 列的宽度随行数位数变，所以不比整行）
check "WHERE id < 10：最长的值在被去掉的行里，owner 列缩回去，宽度同没有那个值时（$(echo "$W0" | cut -d, -f5)，不是 $(echo "$W1" | cut -d, -f5)）" eval '[[ $(ow) == $(echo "$W0" | cut -d, -f5) && $(ow) != $(echo "$W1" | cut -d, -f5) ]] || { echo "  W0 $W0"; echo "  W1 $W1"; echo "  now $(colw "$HY")"; false; }'
key /; sleep 0.3; clear_in; key Enter; wait_for 8 settled; sleep 0.4
check "去掉 WHERE：第 50 行回来，列又变宽（同 S1）" eval '[[ $(seps "$HY") == "$S1" ]]'
psql "$E2E_DB" -q -c "update t_wide set owner = 'owner' || (50 % 7) where id = 50" >/dev/null
key R; wait_for 8 settled; sleep 0.4
check "库里改回来再刷新：列的位置回到 S0" eval '[[ $(seps "$HY") == "$S0" ]]'

# ---- 隐藏一列、WHERE 一行都不匹配、取消隐藏：不崩，列回到原来的
key g; key c; sleep 0.5; key j; key Space; key Escape; sleep 0.4
key /; sleep 0.3; e2e_type "id < 0"; key Enter; wait_for 8 settled; sleep 0.5
check "隐藏 id 后 WHERE id < 0（0 行）：还在跑，查询条「0 行」（F19.4：窄 pane 里 COLS 先让位，不再显示 COLS 14/15）" eval 'flag_is alternate_on 1 && ! screen_has "[e2e-exit" && qb_has "0 行" && ! e2e_plain | grep -qE "│ +1 │"'
key g; key c; sleep 0.5; key j; key Space; key Escape; sleep 0.5
check "0 行时取消隐藏 id：没有崩（还在跑，COLS 15/15）" eval 'flag_is alternate_on 1 && ! screen_has "[e2e-exit" && qb_has "0 行"'
key /; sleep 0.3; clear_in; key Enter; wait_for 8 settled; sleep 0.5
check "去掉 WHERE：100 行回来，列的位置回到 S0" eval '[[ $(seps "$HY") == "$S0" ]]'

# ---- COLS 里调列顺序（J）：列宽跟着列走
key g; key c; sleep 0.5; key J; key Escape; sleep 0.5
check "COLS 里把 id 下移一位（J）：表头顺序变了（name 在 id 前），列的位置变了" eval '[[ $(seps "$HY") != "$S0" ]] && [[ $(e2e_text 35 159 "$HY") =~ name\ +│[^│]*id ]]'
key g; key c; sleep 0.5; key j; key K; key Escape; sleep 0.5
check "K 移回去：列的位置回到 S0" eval '[[ $(seps "$HY") == "$S0" ]]'

# ---- 编辑单元格（还没保存）：新值进了列宽，r 还原后回去
key l; key l; key i; sleep 0.3; e2e_type "$(printf '长名字%.0s' 1 2 3 4 5 6 7 8)"; key Enter; sleep 0.5
check "编辑 owner 后（未保存）：列的位置变了，新值在屏幕上" eval '[[ $(seps "$HY") != "$S0" ]] && e2e_plain | grep -q "长名字长名字"'
key r; sleep 0.4
check "r 还原：列的位置回到原来的" eval '[[ $(seps "$HY") == "$S0" ]]'
_e2e_kill

# ---- 结果区（一页 500 行）：j 按住 3 秒、k 按住回来、滚轮
start -C "$D/own"; sleep 1; key C-l
setsql "select * from t_wide order by id"; run
key C-j; sleep 0.5
check "（准备）结果 tab 500+ 行，光标在 1,1" eval 'title_has "500+ 行" && pos_is 1,1'
RY=$(grid_y); RHY=$((RY - 1))
R0=$(seps "$RHY")
r=$(hold 90 30 j)
echo "  结果区：j × 90 @30/s → $r"
check "结果区：每秒 30 个 j 持续 3 秒，最后一个键之后 100ms 内光标在第 91 行" eval '[[ $(pos_of "$r") == "91,1" ]] || { echo "  光标 $(pos_of "$r")"; false; }'
check "结果区：这 3 秒里 sqlmux 的 CPU ≤ 一个核的 ${CPU_MAX}%（实测 $(cpu_of "$r")%）" cpu_ok "$r"
check "结果区：滚动之后列的位置不变" eval '[[ $(seps "$RHY") == "$R0" ]]'
r=$(hold 90 30 k)
echo "  结果区：k × 90 @30/s → $r"
check "结果区：每秒 30 个 k 按回去，光标回到第 1 行，CPU ≤ ${CPU_MAX}%（实测 $(cpu_of "$r")%）" eval '[[ $(pos_of "$r") == "1,1" ]] && cpu_ok "$r"'
g=($(geom 3))
r=$(hold 90 30 "wheel:$((g[0] + 20)):$((g[1] + 5))")
echo "  结果区：滚轮 × 90 @30/s → $r"
check "结果区：每秒 30 格滚轮持续 3 秒，CPU ≤ ${CPU_MAX}%（实测 $(cpu_of "$r")%），光标跟着视图走了很远（行 > 100）" eval 'cpu_ok "$r" && (( $(pos_of "$r" | cut -d, -f1) > 100 )) || { echo "  光标 $(pos_of "$r")"; false; }'
ms=999999
for i in 1 2 3; do key g; key g; sleep 0.3; m=$(burst 200 j); ((m < ms)) && ms=$m; done   # 三次取最小：机器忙的时候单次会被别的进程拖慢
echo "  结果区：一口气 200 个 j（三次取最小）→ ${ms}ms"
check "结果区：一口气 200 个 j，队列排空（光标到第 201 行）≤ ${BURST_MAX}ms（三次取最小，实测 ${ms}ms）" eval '(( ms <= BURST_MAX ))'
key G; sleep 0.3
check "G：最后一行 500，列的位置还是原来的" eval 'pos_is 500,1 && [[ $(seps "$RHY") == "$R0" ]]'
key T; sleep 0.4; key T; sleep 0.4
check "转置再转回：列的位置回到原来的" eval '[[ $(seps "$RHY") == "$R0" ]]'
key g; key g
paged "]" "501–1000+ 行"
check "] 翻到第 2 页：标题 501–1000+ 行，第一行 id 501" eval '[[ $(firstid) == 501 ]]'
e2e_done
