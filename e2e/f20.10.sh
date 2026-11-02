#!/usr/bin/env bash
# F20.10 query 里敲 Redis 命令时补全框的 panic（specs/m20-redis-ui/task.md F20.10；tech-design §9.8）
# 用户 2026-11-02：Redis 连接的 query 里输入 hget key xx 时 panic（slice bounds out of range [44:6]，view.go 画补全框：补全框的起点越过了光标）。
# 复现：键多（超过 2000 个名字，匹配走 Cmd）、往返慢（慢代理），在 query 里敲 hget key xx，结果没回来时继续打字、换行、删字，进程不退出。每个脚本自起 redis-server。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1
rc flushall >/dev/null; redis_big 0; for k in key:1 key:2 hash:h i18n_agent:expert:zh; do rc set "$k" v >/dev/null; done
rc hset hash:key f v >/dev/null
redis_slow 200 || exit 1
mkdir -p "$D/c"; (umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$SPORT" >"$D/c/connections.toml")
sstart() { e2e_start -C "$D/c" "$E2E_BIN rd"; wait_for 8 flag_is alternate_on 1; sleep 3; }
alive() { running && ! screen_has "panic" && ! screen_has "slice bounds"; }
to_console() { key C-l; key Escape; key g g; key d G; key i; }

sstart
quiet() { local a b=-1 n=0; while ((n < 4)); do a=$(wc -l <"$D/mon.txt"); [[ $a == "$b" ]] && n=$((n + 1)) || n=0; b=$a; sleep 1; done; }
: >"$D/mon.txt"; redis-cli -p "$RPORT" monitor >>"$D/mon.txt" 2>&1 & MON=$!; sleep 0.5; quiet
to_console; e2e_type "get b"; sleep 1; quiet; kill $MON 2>/dev/null; wait $MON 2>/dev/null          # 第一次补全建索引（12006 个名字，匹配走 Cmd），等它建完
# 多行：第一行很长，第二行的补全起点在整段文字里是偏移 44 以后、在这一行里是第 6 列（复现 view.go 的 [44:6]）
to_console
e2e_type "get key:1 get key:2 get key:3 get key:4"; key Escape; key Escape; key o
e2e_type "hget key"; sleep 0.05; key BSpace; key BSpace; key BSpace; key BSpace; e2e_type "set"; sleep 3
check "多行 query（第一行很长），第二行敲 hget key 再退格：补全框起点不越过光标，进程还在、没有 panic" alive
to_console
e2e_type "hget key xx"; sleep 0.1; e2e_type "y"; sleep 0.1; key Enter; e2e_type "hget k"; sleep 0.2; key BSpace; key BSpace; key BSpace; e2e_type "set a b"; sleep 3
check "用户的输入 hget key xx（结果没回来继续打字、换行、删字）：进程还在、没有 panic" alive
to_console
for w in "hget key xx" "hset key f v" "hdel key ff" "get key:1"; do e2e_type "$w"; key Enter; done
sleep 4
check "连着几行 Redis 命令，每行打完马上换行（补全结果在换行后才回来）：进程还在" alive
to_console
for i in 1 2 3 4 5 6; do e2e_type "hget key xx"; key C-n; key BSpace; key BSpace; key BSpace; key Enter; done
sleep 4
check "补全框开着时 C-n、退格、换行混着打：进程还在" alive
key Escape; key Escape; sleep 1
for i in 1 2 3; do key o; e2e_type "hget key xx"; key Escape; key d d; done
sleep 4
check "NORMAL / INSERT 切换、o 开新行、dd 删行混着：进程还在" alive
check "最后还能正常用：控制台还响应输入（INSERT 下打字能回显）" eval 'key i; e2e_type "zz"; sleep 0.8; screen_has zz'

# ---- 随机按键序列（固定种子）：打字、退格、换行、C-n、esc、o、dd、长词混着，每隔几步看一眼进程还在
python3 - "$D/seq.txt" "${SEED:-20}" "${STEPS:-220}" <<'PY'
import random, sys
r = random.Random(int(sys.argv[2]))
words = ["hget key xx", "hset key f v", "get key:1", "hdel key ff", "set a b", "k", "i18n", "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "hget aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa k"]
keys = ["Enter", "BSpace", "BSpace", "C-n", "Escape", "Escape", "i", "o", "d d", "g g", "G", "A", "0", "$", "I", "k", "j", "h", "l", "Home", "End", "C-a", "C-e", "u"]
out = []
for _ in range(int(sys.argv[3])):
    out.append("W " + r.choice(words) if r.random() < 0.45 else "K " + r.choice(keys))
open(sys.argv[1], "w").write("\n".join(out) + "\n")
PY
to_console
n=0; dead=0
while read -r kind arg; do
  if [[ $kind == W ]]; then e2e_type "$arg"; else key $arg; fi
  sleep ${DELAY:-0.08}; n=$((n + 1))
  if (( n % 20 == 0 )); then alive || { dead=$n; break; }; fi
done <"$D/seq.txt"
sleep 3
check "固定种子的随机按键序列（220 步，打字 / 退格 / 换行 / C-n / esc / o / dd / 长词混着，补全结果的到达顺序随慢代理而定）：进程还在、没有 panic" eval '(( dead == 0 )) && alive' || { echo "  dead at step $dead"; e2e_plain | head -3 | cut -c1-160; }

e2e_done
