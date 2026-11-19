#!/usr/bin/env bash
# F22.5 「键 :」范围的边角（specs/m22-menus-keys/task.md F22.5 b、c；tech-design §12「范围与前缀」「Redis 键」）
# 表格里按 : 打开面板再敲 q↵ 照旧关 tab、qa↵ 照旧退出；palette.key 绑了键后打开面板、输入是 :；
# 2 万键的 db（连接经过每个往返 40ms 的代理）冷启动进 :：空输入几秒内列出键、最后没有「索引建立中」那一行（那一行本身单测和 f22.5 管）。每个脚本自起 redis-server。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/redis.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; redis_stop; rm -rf "$D"' EXIT
redis_start R || exit 1
rc flushall >/dev/null
python3 -c 'print("\n".join("SET k22:%d v" % i for i in range(20000)))' | rc >/dev/null
redis_slow 40 || exit 1   # 每个往返 40ms：索引要建一会儿，「索引建立中」那一行看得见
mkdir -p "$D/rd"
(umask 077; printf '[[connection]]\nname = "rd"\nengine = "redis"\ndsn = "redis://localhost:%s/0"\n' "$SPORT" >"$D/rd/connections.toml")
printf '[keys.global]\n"<F6>" = "palette.key"\n' >"$D/k.toml"
names() { list | sed 's/|.*//' | tr '\n' ';'; }

# ---- 表格里 : 之后的 q / qa
SOLO=1 start; open_table t_user; wait_for 8 settled; open_table t_order; wait_for 8 settled
key ":"; sleep 0.4; e2e_type q; key Enter; sleep 0.8
check "表格里 : 打开面板、敲 q↵：关掉当前 tab（t_order），不是进「键」范围找 q" eval 'closed && ! e2e_plain | head -1 | grep -q t_order && e2e_plain | head -1 | grep -q t_user' || e2e_plain | head -2
key ":"; sleep 0.4; e2e_type qa; key Enter
check "再 : 敲 qa↵：退出" eval 'wait_for 8 exited'
_e2e_kill

# ---- palette.key、冷启动建索引
e2e_start -C "$D/rd" -K "$D/k.toml" "$E2E_BIN"; wait_for 8 flag_is alternate_on 1; sleep 2
key F6; sleep 0.3
check "绑到 F6 的 palette.key：打开面板，输入是 :、高亮「键」" eval 'is_open && input_is ":"'
t0=$SECONDS
wait_for 10 eval 'names | grep -q "k22:.*Key;"'
dt=$((SECONDS - t0))
check "空输入列出 k22:* 的键（${dt}s 内，后台匹配那条路），最后没有「索引建立中」那一行" eval 'names | grep -q "k22:.*Key;" && ! e2e_plain | grep -q "索引建立中"' || { names | head -c 300; e2e_plain | tail -10; }
e2e_type 19999; sleep 1.5
check ":19999：k22:19999 在列表里" eval 'names | grep -q "k22:19999  rd/db0  Key"' || names | head -c 300
e2e_done
