#!/usr/bin/env bash
# F16.5 Redis 的补全和签名提示（specs/m16-redis/task.md F16.5；tech-design §9.8「补全」、F15.3 的提示浮层）
# 连接时在 Meta 上取一次 COMMAND DOCS：命令名 → 子命令 → 键名（SCAN MATCH <前缀>* COUNT 100，去抖，不循环到底）→ hash 的字段名（HKEYS，长度 ≤ load_all）；
# 签名提示用 F15.3 的行：当前参数亮（#9ece6a 粗体），别的暗（#565f89）。没有 DOCS 的服务端（Redis 6）退化成只补命令名，那条用单测盖（假连接），这里不测。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1
mkdir -p "$D/c"
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$RPORT" >"$D/c/connections.toml")
reseed() { rc flushall >/dev/null; redis_seed; }
quit_app() { e2e_type ":"; sleep 0.4; e2e_type qa; key Enter; wait_for 8 exited; sleep 0.5; }
sstart() { e2e_start -C "$D/c" "$E2E_BIN $1"; wait_for 8 flag_is alternate_on 1; sleep 2; }
DIM=#565f89 LIT=#9ece6a
# typed TEXT: replace the console's text with TEXT (one line), insert mode, wait for the completion and the hint
typed() { key C-l; key Escape; key g g; key d G; key i; e2e_type "$1"; sleep 2; }
screen_has() { e2e_plain | grep -qF -- "$1"; }
# the hint row (the row under the cursor's line, from the query's left edge): its text and what of it is lit
qx() { e2e_find "▶" 2 | cut -d' ' -f1; }   # the column of the ▶ of query_1's first line: the query's left edge + 1
sig_text() { local x; x=$(qx); e2e_text $((x + 1)) $((x + 60)) 3 | sed 's/ *│.*$//; s/^ *//; s/ *$//'; }
sig_lit() { local x i out=; x=$(qx); for ((i = x + 1; i < x + 60; i++)); do [[ $(e2e_style $i 3) == *"fg=$LIT"* ]] && out+=$(e2e_text $i $i 3); done; echo "$out"; }

# ---- 签名提示：当前参数亮，别的暗
reseed; sstart rd
typed "HSET key f"
check "HSET key f：提示行（光标行下面）是「HSET key field value …」，亮的是可变的那一组「field value …」" eval '[[ $(sig_text) == "HSET key field value …" && $(sig_lit) == "field value …" ]]' || { sig_text; sig_lit; }
check "提示行的底是 row 色 #292e42、字是暗色 #565f89（HSET 的 H）" eval 'x=$(qx); [[ $(e2e_style $((x + 6)) 3) == *"fg=$DIM"*"bg=#292e42"* ]]' || { x=$(qx); e2e_style $((x + 6)) 3; }
typed "HSET "
check "HSET ␣：亮的是 key" eval '[[ $(sig_lit) == key ]]' || { sig_text; sig_lit; }
typed "HSET key field "
check "HSET key field ␣：还在可变的那一组「field value …」上" eval '[[ $(sig_lit) == "field value …" ]]' || { sig_text; sig_lit; }
typed "HSET key field value "
check "HSET key field value ␣：写满一组之后还是那一组（可变参数）" eval '[[ $(sig_lit) == "field value …" ]]' || { sig_text; sig_lit; }
typed "GET "
check "GET ␣：提示「GET key」，亮的是 key" eval '[[ $(sig_text) == "GET key" && $(sig_lit) == key ]]' || { sig_text; sig_lit; }
typed "ZADD zset:rank 1 "
check "ZADD zset:rank 1 ␣：提示行里有 ZADD key、score、member，亮的是「score member …」那一组" eval '[[ $(sig_text) == *"[NX | XX] [GT | LT] [CH] [INCR] score member …" && $(sig_lit) == "score member …" ]]' || { sig_text; sig_lit; }
typed "SET k v "
check "SET k v ␣：提示行从 COMMAND DOCS 展开可选参数（Redis 8.10 起 SET 多了 IFEQ 等，所以提示行长了会从左边截成 …，只看里面有「[NX | XX」），亮的是 [NX | XX …" eval '[[ $(sig_text) == *"[NX | XX"* && $(sig_lit) == "[NX | XX"* ]] || { echo "TEXT[$(sig_text)] LIT[$(sig_lit)]"; false; }'
typed 'GET "unclosed'
check "引号里面：不乱，提示行还是「GET key」" eval '[[ $(sig_text) == "GET key" ]]' || sig_text

# ---- 补全：命令名 → 子命令 → 键名 → 字段名
typed "ZRA"
check "ZRA：候选里有 ZRANGE、ZRANK（命令名，带一句说明）" eval 'screen_has ZRANGE && screen_has ZRANK' || e2e_plain | sed -n 1,14p | cut -c34-165
typed "CONFIG "; key C-n; sleep 1
check "CONFIG ␣ C-n：子命令 GET、SET、RESETSTAT、REWRITE、HELP" eval 'screen_has "GET" && screen_has RESETSTAT && screen_has REWRITE && screen_has "Returns the effective values"' || e2e_plain | sed -n 1,14p | cut -c34-165
typed "CLIENT K"
check "CLIENT K：只剩子命令 KILL（说明是 Terminates open connections.）" eval 'screen_has "KILL" && screen_has "Terminates open connections."' || e2e_plain | sed -n 1,10p | cut -c34-165
rc monitor >"$D/mon.txt" 2>&1 & MON=$!; sleep 0.5
typed "GET str:p"
check "GET str:p：候选是键 str:plain，右边写「键」" eval 'screen_has "str:plain" && e2e_plain | grep -q "str:plain.*键"' || e2e_plain | sed -n 1,10p | cut -c34-165
key Tab; key Enter; sleep 0.4
check "Tab 选第一项、↵ 接受：那一行成了 GET str:plain" eval 'e2e_plain | grep -q "▶  1 GET str:plain$\\|▶  1 GET str:plain *│"' || e2e_plain | sed -n 2p | cut -c98-165
typed "HGET hash:user n"
check "HGET hash:user n：候选是字段 name，右边写「字段」（键已知、长度 ≤ load_all，用 HKEYS）" eval 'screen_has "name" && e2e_plain | grep -q "name.*字段"' || e2e_plain | sed -n 1,10p | cut -c34-165
typed "HDEL hash:user "; key C-n; sleep 1
check "HDEL hash:user ␣ C-n：四个字段 age email meta name" eval 'screen_has age && screen_has email && screen_has meta && screen_has name' || e2e_plain | sed -n 1,12p | cut -c34-165
kill $MON 2>/dev/null; wait $MON 2>/dev/null
check "服务端看到的（F20.1）：键名补全不再每个前缀发 SCAN MATCH str:p*、没有 KEYS（树读全了的 db 直接用树的键）；字段补全是 HKEYS hash:user" eval '! grep -qiE "\"scan\".*\"match\" \"str:p" "$D/mon.txt" && ! grep -qi "\"keys\"" "$D/mon.txt" && grep -qi "\"hkeys\" \"hash:user\"" "$D/mon.txt"' || { grep -ai "scan\|keys" "$D/mon.txt" | head -5; }
# 写命令之后键名补全看得到新键（缓存在写命令后清掉）
reseed; sstart rd
typed "GET zznew"
check "（准备）新键 zznew:1 还没建：GET zznew 没有候选" eval '! screen_has "zznew:1"' || true
key Escape; key g g; key d G; key i; e2e_type "SET zznew:1 v"; key Escape; key Enter; sleep 1.5
typed "GET zznew"
check "SET zznew:1 v 之后 GET zznew：候选里有 zznew:1（键名补全的缓存被写命令清掉了）" eval 'screen_has "zznew:1"' || e2e_plain | sed -n 1,10p | cut -c34-165
quit_app; _e2e_kill

e2e_done
