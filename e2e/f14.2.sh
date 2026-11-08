#!/usr/bin/env bash
# F14.2 查询历史（specs/m14-sqlite/task.md F14.2；tech-design §11「查询历史」、§6.4 queryinsert、§14 history_max）（PG）
# query 里跑的语句（成功、失败、取消）一行 JSON 追加到 $XDG_DATA_HOME/sqlmux/history.jsonl，表格取数不记；INSERT 下 C-r（也在命令面板里）开下拉，
# 只列当前连接的，新的在前，fzf 模糊匹配，↵ 把原文（带换行）放到光标处；NORMAL 下 C-r 仍是 redo；vim = false 时 C-r 同样是历史；
# [query] history_max 启动时截掉最旧的；超过 64 KB 的语句截断记 truncated。自建库 t_h；"other" 连接只用来证明别的连接的不列。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/paging.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
psql "$E2E_DB" -q -c "create table t_h (id int primary key, v text)" -c "insert into t_h values (1,'one'),(2,'two')" >/dev/null
mkdir -p "$D/own" "$D/data/sqlmux"
printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n\n[[connection]]\nname = "other"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" "$E2E_DB" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
H=$D/data/sqlmux/history.jsonl
hjson() { python3 -c 'import sys, json; print(*[json.dumps(json.loads(l), ensure_ascii=False) for l in open(sys.argv[1])], sep="\n")' "$H"; }
hfield() { python3 -c 'import sys, json; ls = [json.loads(l) for l in open(sys.argv[1])]; i = int(sys.argv[2]); print(ls[i].get(sys.argv[3], "-"))' "$H" "$1" "$2"; }
qhas() { e2e_plain | python3 -c 'import sys; print(*[l.rstrip("\n")[62:] for l in sys.stdin], sep="\n")' | grep -qF -- "$1"; }
hn() { grep -c . "$H" 2>/dev/null || echo 0; }
y_of() { e2e_plain | grep -nF -- "$1" | head -1 | cut -d: -f1; }
paste() { t set-buffer -b e2e "$1"; t paste-buffer -r -p -b e2e -t t; sleep 0.4; }
run1() { setsql "$1"; key Enter; sleep 1.2; }

# ---- 1. 记录：成功、失败、受影响行数、取消；表格取数不记
start -C "$D/own" -D "$D/data"; key C-l
check "（准备）启动时没有 history.jsonl" eval '[[ ! -e $H ]]'
run1 "select 1 as one"
run1 "select * from nope_t"
run1 "update t_h set v = v where id = 1"
check "三条语句三行，没有别的" eval '[[ $(hn) == 3 ]]' || cat "$H"
check "第 1 行：conn doraemon、schema public、sql、tag SELECT 1、没有 error" eval '[[ $(hfield 0 conn) == doraemon && $(hfield 0 schema) == public && $(hfield 0 sql) == "select 1 as one" && $(hfield 0 tag) == "SELECT 1" && $(hfield 0 error) == - ]]'
check "第 2 行（失败）：error 是消息第一行，没有 tag" eval '[[ $(hfield 1 sql) == "select * from nope_t" && $(hfield 1 error) == *nope_t* && $(hfield 1 error) != *$'"'"'\n'"'"'* && $(hfield 1 tag) == - ]]' || hjson
check "第 3 行：tag UPDATE 1，took_ms 是数字，at 是时间" eval '[[ $(hfield 2 tag) == "UPDATE 1" && $(hfield 2 took_ms) =~ ^[0-9]+$ && $(hfield 2 at) =~ ^20[0-9-]+T ]]'
check "文件权限 0600" eval '[[ $(fmode "$H") == 600 ]]'
setsql "select pg_sleep(30)"; key Enter; sleep 1; key C-c; sleep 1.5
check "取消的那条也记：第 4 行 error 是 canceled" eval '[[ $(hn) == 4 && $(hfield 3 sql) == "select pg_sleep(30)" && $(hfield 3 error) == canceled ]]' || hjson
key C-h; open_table t_h; wait_for 8 settled; key r; wait_for 8 settled; sleep 0.5
check "打开表、刷新：表格取数的 SQL 不记（还是 4 行）" eval '[[ $(hn) == 4 ]]'
key C-l; key g g; key d G

# ---- 2. C-r：只列当前连接的，新的在前，注释，模糊匹配，↵ 放到光标处（带换行）
printf '%s\n' '{"at":"2026-10-12T09:01:00+08:00","conn":"other","sql":"select '"'"'only_other'"'"'","tag":"SELECT 1","took_ms":1}' \
  '{"at":"2026-10-12T09:02:00+08:00","conn":"doraemon","schema":"public","sql":"select id,\n  v\nfrom t_h\nwhere id < 5","tag":"SELECT 2","took_ms":3}' >>"$H"
printf '%s\n' '{"at":"2026-10-12T09:03:00+08:00","conn":"doraemon","sql":"fine","tag":"UPDATE 1","took_ms":12}' \
  '{"at":"2026-10-12T09:04:00+08:00","conn":"doraemon","sql":"bad","took_ms":0,"error":"boom"}' \
  '{"at":"2026-10-12T09:05:00+08:00","conn":"doraemon","sql":"slow","took_ms":900,"error":"canceled"}' >>"$H"
key i; e2e_type "xx"; key C-r; sleep 0.6
check "INSERT 下 C-r：开下拉，列出当前连接的语句，不列 other 的" eval 'qhas "select pg_sleep(30)" && qhas "update t_h set v = v where id = 1" && qhas "select * from nope_t" && qhas "select 1 as one" && ! qhas "only_other"'
check "多行语句一行化显示：select id, v from t_h where id < 5（空白折成一个空格）" eval 'qhas "select id, v from t_h where id < 5"'
check "新的在前：slow、bad、fine、多行那条、pg_sleep、update、nope_t，select 1 最后" eval 'o=(slow bad fine "select id, v from t_h" "select pg_sleep(30)" "update t_h set" "select * from nope_t" "select 1 as one"); p=0; for w in "${o[@]}"; do y=$(y_of "$w"); [[ -n $y ]] && ((y > p)) || exit 1; p=$y; done' || e2e_plain | sed -n 1,16p
check "注释（dim，放不下就丢）：fine 那行「UPDATE 1 · 12ms」，bad 是「失败: boom」，slow 是「失败: canceled」，都带月-日 时:分" eval 'e2e_plain | grep -F fine | grep -qE "[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2} · UPDATE 1 · 12ms" && e2e_plain | grep -F bad | grep -qF "失败: boom" && e2e_plain | grep -F slow | grep -qF "失败: canceled"' || e2e_plain | sed -n 1,12p
e2e_type "nope"; sleep 0.4
check "打 nope：只剩 nope_t 那一条" eval 'qhas "select * from nope_t" && ! qhas "select 1 as one" && ! qhas "pg_sleep"'
key Enter; sleep 0.5
check "↵：下拉关了，原文放到光标处（xx 后面），还在 INSERT" eval '! qhas "select 1 as one" && qhas "xxselect * from nope_t" && mode_is INSERT'
key Escape; key g g; key d G; key i; key C-r; sleep 0.5; e2e_type "< 5"; sleep 0.4; key Enter; sleep 0.5
check "多行那条原文放进来：四行，缩进和换行都在" eval 'qhas "1 select id," && qhas "2   v" && qhas "3 from t_h" && qhas "4 where id < 5"'
check "没有别的连接的语句出现在文件之外：history.jsonl 还是 9 行（C-r 只读，不写）" eval '[[ $(hn) == 9 ]]'
key Escape; key g g; key d G; key i; e2e_type "select * from t_"; sleep 1.2
check "（准备）补全列表开着，列出 t_ 开头的表" eval 'qhas "t_order"'
key C-r; sleep 0.6
check "补全开着按 C-r：开的是历史（列出语句），补全列表收起了" eval 'qhas "select pg_sleep(30)" && ! qhas "t_order"'
e2e_type "nope"; sleep 0.3; key Enter; sleep 0.6
check "↵：原文放到光标处，补全没有再冒出来" eval 'qhas "t_select * from nope_t" && ! qhas "t_order" && mode_is INSERT' || e2e_plain | sed -n 1,10p

# ---- 3. NORMAL 下 C-r 仍是 redo（不开历史）；命令面板里有「查询历史…」
key Escape; key g g; key d G; key i; e2e_type "hello"; key Escape; key u; key C-r; sleep 0.5
check "NORMAL：改 hello、u 撤销、C-r 重做：hello 回来，没有开历史" eval 'qhas "1 hello" && ! qhas "select 1 as one"'
key g g; key d G
pal "查询历史"
check "命令面板里有「查询历史…」" eval 'row_has "查询历史"'
key Enter; sleep 0.6
check "↵ 执行它：开历史下拉（query 里）" eval 'qhas "select 1 as one" && qhas "update t_h set"'
key Escape; sleep 0.3
_e2e_kill

# ---- 4. vim = false：C-r 同样是历史
mkdir -p "$D/novim"; cp "$D/own/connections.toml" "$D/novim/"; printf 'vim = false\n' >"$D/novim/config.toml"
start -C "$D/novim" -D "$D/data"; key C-l; sleep 0.3
e2e_type "yy"; key C-r; sleep 0.6
check "vim = false：C-r 开历史，列出当前连接的语句" eval 'qhas "select pg_sleep(30)" && ! qhas "only_other"'
e2e_type "nope"; sleep 0.3; key Enter; sleep 0.5
check "↵：放到光标处（yy 后面）" eval 'qhas "yyselect * from nope_t"'
_e2e_kill

# ---- 5. history_max：启动时截掉最旧的，留最新的几条
mkdir -p "$D/hm" "$D/data3/sqlmux"; cp "$D/own/connections.toml" "$D/hm/"; printf '[query]\nhistory_max = 3\n' >"$D/hm/config.toml"
for i in 1 2 3 4 5 6; do printf '{"at":"2026-10-12T09:0%d:00+08:00","conn":"doraemon","sql":"select %d","tag":"SELECT 1","took_ms":1}\n' $i $i; done >"$D/data3/sqlmux/history.jsonl"
start -C "$D/hm" -D "$D/data3"; sleep 1.5
H3=$D/data3/sqlmux/history.jsonl
check "history_max = 3：启动后文件只剩最新的 3 行（select 4 5 6）" eval '[[ $(grep -c . "$H3") == 3 && $(python3 -c "import sys, json; print(*[json.loads(l)[\"sql\"] for l in open(sys.argv[1])])" "$H3") == "select 4 select 5 select 6" ]]'
_e2e_kill
printf '[query]\nhistory_max = 0\n' >"$D/hm/config.toml"
start -C "$D/hm" -D "$D/data3"; sleep 1.5
check "history_max = 0：启动报配置错误（必须大于 0），文件不动" eval '[[ $(grep -c . "$H3") == 3 ]] && { e2e_plain | grep -qF "history_max" || ! running; }' || e2e_plain | sed -n 1,6p
_e2e_kill

# ---- 6. 超过 64 KB 的语句：截断并记 truncated
mkdir -p "$D/data2"; start -C "$D/own" -D "$D/data2"; key C-l
python3 -c "print(\"select '\" + \"a\" * 70000 + \"' as big\", end=\"\")" >"$D/big.sql"
t load-buffer -b e2e "$D/big.sql"
key Escape; key g g; key d G; key i; t paste-buffer -r -p -b e2e -t t; sleep 1.5; key Escape; key Enter; sleep 4
H2=$D/data2/sqlmux/history.jsonl
check "70 KB 的语句：记了一行，sql 是前 65536 字节，truncated 为 true，有 tag" eval '[[ $(grep -c . "$H2") == 1 ]] && python3 -c "
import sys, json
h = json.loads(open(sys.argv[1]).read())
sys.exit(0 if h.get(\"truncated\") is True and len(h[\"sql\"].encode()) == 65536 and h.get(\"tag\") == \"SELECT 1\" else 1)" "$H2"' || { wc -c "$H2"; head -c 300 "$H2"; }
# ---- 7. 两个 sqlmux 进程同时跑语句：两边的行都在，没有半行（history.jsonl 共用，queries 各用各的）
mkdir -p "$D/data4/sqlmux" "$D/data5/sqlmux" "$D/cfg5/sqlmux" "$D/st5"
H4=$D/data4/sqlmux/history.jsonl; ln -s "$H4" "$D/data5/sqlmux/history.jsonl"
cp "$D/own/connections.toml" "$D/cfg5/sqlmux/"; printf 'language = "zh"\nconfirm_dangerous = false\n' >"$D/cfg5/sqlmux/config.toml"
SOCK5=$E2E_SOCK-5; t5() { tmux -L "$SOCK5" "$@"; }
start -C "$D/own" -D "$D/data4"; key C-l
t5 -f /dev/null new-session -d -s t -x 160 -y 45 -e XDG_CONFIG_HOME="$D/cfg5" -e XDG_STATE_HOME="$D/st5" -e XDG_DATA_HOME="$D/data5" -e COLORTERM=truecolor \
  "env TERM=xterm-256color $E2E_BIN"; sleep 2; t5 send-keys -t t C-l
stmts() { python3 -c "import sys; print('; '.join('select %s%02d' % (sys.argv[1], i) for i in range(1, 21)))" "$1"; }
setsql "$(stmts 1)"; key g g; key V G
t5 send-keys -t t Escape; sleep 0.3; t5 send-keys -t t g g d G i; sleep 0.3; t5 load-buffer -b e2e5 <(stmts 2); t5 paste-buffer -r -p -b e2e5 -t t; sleep 0.5
t5 send-keys -t t Escape; sleep 0.3; t5 send-keys -t t g g; sleep 0.3; t5 send-keys -t t V G; sleep 0.3
t5 send-keys -t t Enter; key Enter; sleep 8
check "两个进程各跑 20 条（选区执行，同时按 ↵）：文件里 40 行，每行都是完整的 JSON" eval '[[ $(grep -c . "$H4") == 40 ]] && python3 -c "
import sys, json
[json.loads(l) for l in open(sys.argv[1])]" "$H4"' || wc -l "$H4"
check "两边的语句都在（1xx 20 条、2xx 20 条，各一条，没有丢也没有重）" eval '[[ $(python3 -c "
import sys, json
ss = sorted(json.loads(l)[\"sql\"] for l in open(sys.argv[1]))
print(len(ss), len(set(ss)), sum(s.startswith(\"select 1\") for s in ss), sum(s.startswith(\"select 2\") for s in ss))" "$H4") == "40 40 20 20" ]]' || python3 -c "
import sys, json
print(*sorted(json.loads(l)['sql'] for l in open(sys.argv[1])))" "$H4"
t5 kill-server 2>/dev/null; rm -f "$(tmux -L "$SOCK5" display -p '#{socket_path}' 2>/dev/null)"
_e2e_kill
e2e_done
