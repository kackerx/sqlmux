#!/usr/bin/env bash
# F6.1 键位配置收尾（specs/m6-config/task.md F6.1；tech-design §6.7「冲突」）
# 冲突浮层的内容和「键位文字都从 keymap 读取」的源码审计有单测；这里在真实终端里测：keymaps.toml 有冲突时启动出浮层、一行一条、
# 三个键都能关、关掉后界面正常；没有冲突时没有浮层；改了键之后 pane 标题、tab 栏、命令面板、which-key、toast 上显示的都是新键。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
printf '[keys.grid]\n"<C-b>" = "grid.refresh"\n"<c-b>" = "grid.transpose"\n"g" = "grid.refresh"\n[keys.nosuch]\n"x" = "tab.close"\n' >"$D/conflict.toml"
printf '[keys.normal]\n"<Leader>b" = ""\n"<C-g>" = "tree.toggle"\n"gt" = ""\n"<Leader>t" = "tab.next"\n"gT" = ""\n"<Leader>T" = "tab.prev"\n[keys.global]\n"<C-p>" = ""\n"<C-o>" = "palette.open"\n"<C-c>" = ""\n"<C-x>" = "cancel"\n' >"$D/rebind.toml"
overlay() { e2e_plain | grep -q "keymaps.toml 有 .* 处问题"; }
ovl_lines() { e2e_plain | grep -E '^ *│? *│ +\[keys\.' | sed 's/^[^[]*//; s/ *│.*//'; }

# ---- 冲突：启动时浮层，一行一条
start -K "$D/conflict.toml"; sleep 1
check "启动：浮层标题「keymaps.toml 有 2 处问题：」" eval 'e2e_plain | grep -q "keymaps.toml 有 2 处问题："'
check "一行一条：<c-b> 与 <C-b> 同键、nosuch 没有这个表" eval 'e2e_plain | grep -q "\[keys.grid\] <c-b>: 与 \"<C-b>\" 是同一个键" && e2e_plain | grep -q "\[keys.nosuch\] x: 没有这个表"'
check "按钮显示 confirm.yes 的键（默认 y）：「y 知道了」" eval 'e2e_plain | grep -q "y 知道了"'
key Enter
check "↵ 关掉浮层，界面正常（树、①、②、状态栏都在）" eval '! overlay && running' 2>/dev/null || { overlay && echo "  overlay still open"; false; }
check "关掉后键位正常：SPC b 还能收起树（没有被冲突的行影响；F19.5 起第一个聚焦、第二个收起）" eval 'e2e_keys Space; e2e_type b; sleep 0.4; e2e_keys Space; e2e_type b; sleep 0.4; [[ -z $(geom 0) ]]'
e2e_stop; start -K "$D/conflict.toml"; sleep 1; key y
check "y 也能关" eval '! overlay'
e2e_stop; start -K "$D/conflict.toml"; sleep 1; key Escape
check "esc 也能关" eval '! overlay'

# ---- 没有冲突：没有浮层
e2e_stop; start; sleep 0.8
check "没有 keymaps.toml 冲突：启动没有浮层" eval '! overlay'
e2e_stop; start -K "$D/rebind.toml"; sleep 0.8
check "改键本身不是冲突：重新绑定默认键不出浮层" eval '! overlay'

# ---- 改了键：显示的都是新键
check "树的 pane 标题：没有键位文字（F10.9），只有 «" eval 't=$(e2e_plain | sed -n 1p); [[ $t == *"«"* && $t != *"C-g"* && $t != *"SPC b"* ]]'
g=($(geom 2)); tb=$(e2e_text $((g[0])) $((g[0] + g[2] - 1)) $((g[1] + g[3] - 2)))
check "query 的 tab 栏提示：用新键（不再是 gt/gT）" eval '[[ $tb == *"SPC t"* && $tb != *"gt/gT"* ]] || { echo "  $tb"; false; }'
key Space; sleep 0.8
check "which-key（SPC 之后）：有 t → 下一个 tab、T → 上一个 tab，没有 b → 折叠 / 展开 schema 树" eval 'w=$(e2e_plain); grep -q "t → 下一个 tab" <<<"$w" && grep -q "T → 上一个 tab" <<<"$w" && ! grep -q "b → 折叠" <<<"$w"'
key Escape
e2e_keys C-o; sleep 0.6
check "命令面板：C-o 打开，C-p 解绑了不再打开" eval 'is_open && { key Escape; e2e_keys C-p; sleep 0.4; ! is_open; }'
key Escape
e2e_keys C-g; sleep 0.4; e2e_keys C-g; sleep 0.4   # F19.5：第一个聚焦树，第二个收起
check "C-g 收起树（新键生效，两次）" eval '[[ -z $(geom 0) ]]'
e2e_keys C-g; sleep 0.4
e2e_keys C-c; sleep 0.4
check "C-c 解绑后没有「再按一次」提示" eval '! screen_has "再按一次"'
e2e_keys C-x; sleep 0.4
check "toast 里的退出键是新键：再按一次 C-x 退出" eval 'screen_has "再按一次 C-x 退出"'

e2e_done
