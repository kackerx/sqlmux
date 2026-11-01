#!/usr/bin/env bash
# F17.2 快速 SQL 在 Redis 连接上跑命令（specs/m17-polish-exec/task.md F17.2；tech-design §12「快速 SQL」、§9.8、§13）
# 当前连接是 Redis 时 `;` 范围照旧，输入一行命令（词法同控制台），↵ 在 Main 上跑，回复摆成表放进面板的结果区；
# 只读 / 危险判定、补全同控制台；C-t 开成结果区的 tab、C-e 开成控制台（query）tab；历史记进 history.jsonl。每个脚本自己起 redis-server。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1
mkdir -p "$D/c"
(umask 077; cat >"$D/c/connections.toml" <<TOML
[[connection]]
name = "rd"
engine = "redis"
dsn = "redis://localhost:$RPORT/0"

[[connection]]
name = "ro"
engine = "redis"
dsn = "redis://localhost:$RPORT/0"
read_only = true
TOML
)
printf 'confirm_dangerous = true\n' >"$D/c/config.toml"
reseed() { rc flushall >/dev/null; redis_seed; }
sstart() { e2e_start -C "$D/c" -D "$D/dt" "$E2E_BIN $1"; wait_for 8 flag_is alternate_on 1; sleep 2; }
quit_app() { e2e_type ":"; sleep 0.4; e2e_type qa; key Enter; wait_for 8 exited; sleep 0.5; }
# qs TEXT: from the sidebar (the query's own `;` is vim's), `;` opens the palette's SQL scope, TEXT is the command; a completion list is closed (↵ would only accept) and ↵ runs it
qs() { key Escape; key Escape; key C-h; sleep 0.3; e2e_type ";$1"; sleep 1.2; [[ -n $(pop) ]] && { key Escape; sleep 0.3; }; key Enter; sleep 1.8; }
pitems() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0   # the completion list's candidates, one per line (the kind icon column skipped)
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 4)) $((g[0] + g[2] - 2)) $y | sed 's/  .*//; s/ *$//'; done; }
inp() { input; }
confirm_open() { e2e_plain | grep -q "y 执行   n 取消"; }
qpane() { e2e_text 35 95 2 | sed 's/^[^0-9]*//; s/ *│.*//'; }   # query pane ①'s first text line after a C-e

# ---- 回复摆成表
reseed; sstart rd
qs "HGETALL hash:user"
check "HGETALL hash:user：输入框是 ;HGETALL hash:user，标题「4 行 · … · 只读」，两列 field / value 四行" eval '[[ $(input) == ";HGETALL hash:user" && $(title) =~ ^4\ 行\ ·\ [0-9.]+(µs|ms|s)\ ·\ 只读\ C-y\ CSV\ ·\ C-t\ 结果区\ ·\ C-e\ query$ && $(grid | tr "\n" /) == "age|30/email|ann@example.com/meta|{\"vip\":true}/name|Ann/" ]] || { input; title; grid; false; }'
qs "GET str:plain"
check "GET str:plain：一列 value、一行 hello" eval '[[ $(grid | tr "\n" /) == "hello/" ]] || { title; grid; false; }'
qs "ZRANGE zset:rank 0 -1 WITHSCORES"
check "ZRANGE … WITHSCORES：成对的数组按列 member / score，五行" eval '[[ $(grid | tr "\n" /) == "alice|1/bob|2.5/carol|3/dave|3/erin|10/" ]] || { grid; false; }'
qs "FOO bar"
check "不存在的命令：服务端的错误 ERR unknown command 原样显示在结果区，没有表" eval '[[ $(line1) == "ERR unknown command"* ]] || { title; line1; false; }'
qs "SET foo 1"
check "SET foo 1（普通连接）：回复 OK，库里真有 foo" eval '[[ $(grid | tr "\n" /) == "OK/" && $(rc get foo) == 1 ]] || { grid; rc get foo; false; }'

# ---- 危险命令走确认（confirm_dangerous），同控制台
qs "FLUSHDB"
check "FLUSHDB：弹确认「y 执行 n 取消」，写着 FLUSHDB，库还没动（30 个键）" eval 'confirm_open && e2e_plain | grep -q "FLUSHDB" && [[ $(rc dbsize) == 30 ]] || { rc dbsize; false; }'
key n; sleep 0.8
check "n：不执行，库还是 30 个键，确认框关了" eval '! confirm_open && [[ $(rc dbsize) == 30 ]] || { rc dbsize; false; }'
qs "FLUSHDB"; key y; sleep 1.5
check "y：执行，库清空（0 个键）" eval '[[ $(rc dbsize) == 0 ]] || { rc dbsize; false; }'

# ---- 补全同控制台：命令名、键名
reseed                                              # 从外部重灌：app 里写命令后键名索引是增量的（F20.8），外部灌的键要树上 R 才进索引
key Escape; key Escape; key C-h; sleep 0.3
y=$(e2e_plain | awk 'NR > 3 && index(substr($0, 1, 34), "db0") { print NR; exit }'); x=$(e2e_find db0 "$y" | cut -d' ' -f1); e2e_click "$x" "$y"; sleep 0.5; key R; sleep 2
key Escape; key Escape; key C-h; sleep 0.3; e2e_type ";HGE"; sleep 1.5
check "命令名：HGE → HGET、HGETALL 在候选里，HGET 排第一" eval '[[ $(pitems | head -1) == HGET && $(pitems | tr "\n" " ") == *" HGETALL "* ]] || { pitems; false; }'
e2e_type "TALL hash:"; sleep 1.5
check "键名：HGETALL hash: → hash:user、hash:binary" eval '[[ $(pitems | tr "\n" " ") == *"hash:user "* && $(pitems | tr "\n" " ") == *"hash:binary "* ]] || { pitems; false; }'
key Escape; key Escape; sleep 0.3

# ---- C-t：结果区里多一个 quick #N tab；C-e：开成控制台（query）tab，里面是这一行
qs "LRANGE list:queue 0 -1"
e2e_keys C-t; sleep 1.5; key Escape; sleep 0.6
check "C-t：结果区里多了 quick #1，五行 first … fifth" eval 'e2e_plain | grep -q "quick #1" && e2e_plain | grep -q "5 行 · " && e2e_plain | grep -q "│ 5 │ fifth" || { e2e_plain | sed -n 22,40p | cut -c30-140; false; }'
# ---- C-t 开出的结果 tab 上 R：只读命令重跑，有副作用的只给 toast
qs "GET str:plain"; e2e_keys C-t; sleep 1.5; key Escape; sleep 0.6
rc set str:plain "changed outside" >/dev/null
e2e_click 60 31; sleep 0.3; key R; sleep 1.5
check "结果 tab 上 R：只读命令（GET）重跑，格子里是新值 changed outside" eval 'e2e_plain | grep -q "changed outside"' || { e2e_plain | tail -12 | cut -c30-160; false; }
qs "SET sk 1"; e2e_keys C-t; sleep 1.5; key Escape; sleep 0.6
rc set sk 2 >/dev/null
e2e_click 60 31; sleep 0.3; key R; sleep 1.5
check "结果 tab 上 R：有副作用的命令（SET）不重跑（库里还是 2），给 toast" eval '[[ $(rc get sk) == 2 ]] && ! e2e_plain | grep -q "│ +1 │ OK.*│ +2 │" ' || { rc get sk; e2e_plain | tail -4 | cut -c1-170; false; }
qs "AUTH hunter2"
qs "HGETALL hash:user"
key C-e; sleep 1.5
check "C-e：开出 query_2（控制台），里面是这一行 HGETALL hash:user，面板关了" eval '! is_open && e2e_plain | grep -q "query_2" && e2e_plain | grep -q "HGETALL hash:user" || { e2e_plain | sed -n 1,3p | cut -c1-170; false; }'
quit_app
check "AUTH hunter2 的行不在 history.jsonl 里（没有 hunter2），GET str:plain 在" eval 'f=$(ls "$D/dt"/sqlmux/history.jsonl) && ! grep -qi "hunter2" "$f" && grep -q "\"sql\":\"GET str:plain\"" "$f"' || { grep -i auth "$D/dt"/sqlmux/history.jsonl; false; }
check "历史：history.jsonl 里有 HGETALL hash:user（db0，不带 db 字段）和 LRANGE list:queue 0 -1" eval 'f=$(ls "$D/dt"/sqlmux/history.jsonl) && grep -q "\"sql\":\"HGETALL hash:user\"" "$f" && grep -q "\"sql\":\"LRANGE list:queue 0 -1\"" "$f" && ! grep -q "\"db\"" "$f" || { tail -4 "$D/dt"/sqlmux/history.jsonl; false; }'
_e2e_kill

# ---- 只读连接：写命令整次不跑，读命令照跑，危险的直接拒、没有确认框
reseed; sstart ro
qs "SET foo 1"
check "ro：SET foo 1 被挡（「ro 是只读连接」），库里没有 foo" eval '[[ $(line1) == *"ro 是只读连接"* && $(rc exists foo) == 0 ]] || { line1; false; }'
qs "FLUSHALL"
check "ro：FLUSHALL 直接拒（没有确认框），库还在（29 个键）" eval '[[ $(line1) == *"ro 是只读连接"* ]] && ! confirm_open && [[ $(rc dbsize) == 29 ]] || { line1; rc dbsize; false; }'
qs "GET str:plain"
check "ro：GET 照跑，hello" eval '[[ $(grid | tr "\n" /) == "hello/" ]] || { grid; false; }'
quit_app
e2e_done
