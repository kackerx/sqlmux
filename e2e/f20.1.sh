#!/usr/bin/env bash
# F20.1 Redis 键名的模糊补全（specs/m20-redis-ui/task.md F20.1；tech-design §9.8「键名的模糊补全」、§9.7、§14 [redis] index_keys）
# 每个 db 一份本地键名索引：第一次补全键名时在 Meta 上 SCAN COUNT 1000 分批建，index_keys 封顶，截断时补全框底部「已索引 N / 共 M 键」；
# 建好后补全在索引里走 §9.7 的匹配器（match 色高亮），不再每个前缀发 SCAN；控制台跑过写命令丢掉重建。
# 每个脚本自起 redis-server（redis.sh）。用 redis-cli monitor 看线路。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1
mkdir -p "$D/c" "$D/c5"
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$RPORT" >"$D/c/connections.toml"
 cp "$D/c/connections.toml" "$D/c5/connections.toml")
printf '[redis]\nindex_keys = 5000\n' >"$D/c5/config.toml"
MATCH=#ff9e64
sstart() { e2e_start -C "$1" "$E2E_BIN rd"; wait_for 8 flag_is alternate_on 1; sleep 2; }
typed() { key C-l; key Escape; key g g; key d G; key i; e2e_type "$1"; sleep 1.5; }
screen_has() { e2e_plain | grep -qF -- "$1"; }
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }
rows() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do echo "$y $(e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/ *$//')"; done; }
# 补全框里名字是 NAME 的那一行的 y（行里有这个词）
row_of() { rows | awk -v n="$1" 'index($0, n) { print $1; exit }'; }
# NAME 那一行里 WORD 是 match 色：WORD 每个字符的前景都是 match
lit() { local y x i ok=1; y=$(row_of "$1"); [[ -n $y ]] || { echo "  no row with $1"; return 1; }
  x=$(e2e_find "$2" "$y" | cut -d' ' -f1); [[ -n $x ]] || { echo "  no $2 on row $y"; return 1; }
  for ((i = 0; i < ${#2}; i++)); do [[ $(e2e_style $((x + i)) "$y") == *"fg=$MATCH"* ]] || ok=0; done
  ((ok)) || { echo "  $2 at $x,$y: $(e2e_style "$x" "$y")"; return 1; }; }
mon() { : >"$D/mon.txt"; redis-cli -p "$RPORT" monitor >>"$D/mon.txt" 2>&1 & MON=$!; sleep 0.5; }   # redis-cli itself, not the rc function: kill must reach it
unmon() { kill "$MON" 2>/dev/null; wait "$MON" 2>/dev/null; }
scans() { grep -ac '"scan"' "$D/mon.txt"; }

rc flushall >/dev/null
for k in i18n:hello i18n:world i18n_agent:expert:zh i18n_agent:expert:en i18n_agent:basic:zh user:1 user:2 order:100 feature:new:tab:flow:param; do rc set "$k" v >/dev/null; done

sstart "$D/c"; mon                                       # 树已经把 db0 的 9 个键读全了（没被 scan_keys 截断）：补全直接用树的键
typed "SMEMBERS i18n"
check "SMEMBERS i18n：前缀的键都在（i18n:hello、i18n_agent:expert:zh）" eval 'screen_has i18n:hello && screen_has i18n_agent:expert:zh'
typed "SMEMBERS expert"
check "SMEMBERS expert：补出 i18n_agent:expert:zh 和 :en（以前只有前缀 SCAN，中间的词补不出），没有 i18n:hello" eval 'screen_has i18n_agent:expert:zh && screen_has i18n_agent:expert:en && ! screen_has i18n:hello' || rows
check "没截断的 db，补全框底部没有「已索引」" eval '! screen_has "已索引"'
check "expert 在两个候选里都是 match 色（#ff9e64）" eval 'lit i18n_agent:expert:zh expert && lit i18n_agent:expert:en expert'
typed "SMEMBERS i18n_"
check "SMEMBERS i18n_：补出带下划线的 i18n_agent:*，没有 i18n:hello / i18n:world" eval 'screen_has i18n_agent:basic:zh && screen_has i18n_agent:expert:zh && ! screen_has i18n:hello && ! screen_has i18n:world' || rows
check "i18n_ 在 i18n_agent:basic:zh 里是 match 色" lit i18n_agent:basic:zh i18n_
typed "SMEMBERS fntfp"
check "SMEMBERS fntfp：补出 feature:new:tab:flow:param（: 是词首，每段的首字母连起来）" eval 'screen_has feature:new:tab:flow:param' || rows
check "fntfp 的五个字母是 match 色（各段的首字母 f n t f p）" eval 'y=$(row_of feature:new:tab:flow:param); x=$(e2e_find feature:new:tab:flow:param "$y" | cut -d" " -f1); for o in 0 8 12 16 21; do [[ $(e2e_style $((x + o)) "$y") == *"fg=$MATCH"* ]] || { echo "  offset $o: $(e2e_style $((x + o)) "$y")"; false; }; done'
unmon
check "线路：树读全了的 db，第一次补全起到敲完这几个补全，一条 SCAN 都没发（用树的键、不再每个前缀 SCAN）" eval '(( $(scans) == 0 )) || { grep -a scan "$D/mon.txt" | head -3; false; }'

# ---- 写命令丢掉索引，重建
rc set ext:expert:x v >/dev/null
typed "SMEMBERS expert"
check "别处新建的键（不经过控制台）：索引没重建，补全里还没有 ext:expert:x" eval '! screen_has ext:expert:x'
typed "SET zzz 1"; key Escape; key Enter; sleep 1.5
check "（准备）SET zzz 1 在控制台跑了" eval '[[ $(rc get zzz) == 1 ]]'
typed "SMEMBERS expert"; sleep 1.5; typed "SMEMBERS expert"
check "控制台跑过写命令：索引丢掉重建，补全里有 ext:expert:x 了" eval 'screen_has ext:expert:x && screen_has i18n_agent:expert:zh' || rows

# ---- 截断：index_keys = 5000，db 里 12010 个键
rc flushall >/dev/null; redis_big 0; for k in i18n:hello i18n_agent:expert:zh user:1 user:2 order:100; do rc set "$k" v >/dev/null; done
check "（准备）db0 有 12006 个键" eval '[[ $(rc dbsize) == 12006 ]]'
sstart "$D/c5"; mon
typed "SMEMBERS bulk:1"; sleep 2; n1=$(scans); typed "SMEMBERS bulk:1"; n2=$(scans)
check "index_keys = 5000：补全框底部写「已索引 5000 / 共 12006 键」" eval 'screen_has "已索引 5000 / 共 12006 键"' || e2e_plain | sed -n 1,16p | cut -c30-165
check "截断后仍补出 bulk:1* 的候选（在已索引的 5000 个里匹配）" eval 'screen_has bulk:1'
unmon
check "线路：树被 scan_keys 截断了，索引自己 SCAN：每批 SCAN 0… MATCH * COUNT 1000，5000 个封顶 = 5 批；建好后再敲一遍没有新的 SCAN" eval 'n=$(grep -ac "\"match\" \"\*\" \"count\" \"1000\"" "$D/mon.txt"); [[ $n == 5 && $n1 == $n2 ]] || { echo "  index batches: $n, scans $n1 -> $n2"; grep -a scan "$D/mon.txt" | head -9; false; }'

e2e_done
