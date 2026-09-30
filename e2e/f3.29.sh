#!/usr/bin/env bash
# F3.29 SPC b 展开树时定位到当前 tab（specs/m3-console/task.md F3.29；tech-design §7.8「schema 侧栏」）
# 默认布局：⓪ 树 | ① | ② console_1。树收起后 SPC b 展开，焦点到树上，光标落在焦点 pane 当前 tab 对应的节点。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

SELECT=#364a82
cur() { e2e_rows 2 31 4 $(( $(H) - 4 )) 3 | awk -F'|' -v s=$SELECT '$2 == s { print $1 }' | python3 -c 'import sys; print(" ".join("".join(c for c in sys.stdin.read() if not (0xE000 <= ord(c) <= 0xF8FF) and c not in "▸▾").split()))'; }   # 光标行（select 底）的文字
sub_of() { e2e_rows 2 31 4 $(( $(H) - 4 )) 3 | cut -d'|' -f1 | sed -n "/$1/,\$p"; }   # 从 NAME 那一行往下
toggle() { key Space; e2e_type b; sleep 0.4; }
tree_shown() { [[ -n $(geom 0) ]]; }

start; open_table t_order; wait_for 8 settled
key C-h; key /; key Escape; key g g; key 2 j; key h                          # 把 public 折起来：t_order 不在可见节点里
check "准备：public 折起来了，树上看不到 t_order 表节点" eval '! e2e_rows 2 31 4 $(( $(H) - 4 )) 3 | grep -q "t_order .*6.0k"'
key C-l; toggle
check "SPC b：树收起" eval '! tree_shown && focus_is 1'
toggle
check "焦点在 t_order 的 tab 上时 SPC b：树展开，焦点到树上，光标在 public 下的 t_order 节点，折着的 public、Tables 也展开了" eval 'tree_shown && focus_is 0 && [[ $(cur) == "t_order 6.0k" ]] || { echo "  cursor on \"$(cur)\""; false; }'
key C-l; key C-l; toggle; toggle
check "在 ② 的 console 上按：光标在工作区的 console_1 节点" eval 'focus_is 0 && [[ $(cur) == console_1 ]] && [[ -n $(sub_of 工作区 | grep console_1) ]] || { echo "  cursor on \"$(cur)\""; false; }'
toggle
check "树展开时 SPC b：照旧收起" eval '! tree_shown'

# ---- 树正在过滤、目标被滤掉：什么都不改；清掉过滤后，原来手动折叠的节点仍是折叠的（5514975）
start; open_table t_order; wait_for 8 settled
key C-h; key g g; key 2 j; key h                                             # 手动把 public 折起来
key /; e2e_type sku; sleep 0.3; key Enter
check "准备：过滤 sku，光标在 t_sku" eval '[[ $(cur) == "t_sku 20" ]] || { echo "  cursor on \"$(cur)\""; false; }'
key C-l; toggle; toggle
check "焦点在 t_order 上 SPC b 收起再展开：t_order 被滤掉了，光标不动（还在 t_sku）" eval '[[ $(cur) == "t_sku 20" ]] || { echo "  cursor on \"$(cur)\""; false; }'
key C-h; key /; key Escape
check "清掉过滤：public 还是折叠的（没有被定位展开）" eval '! e2e_rows 2 31 4 $(( $(H) - 4 )) 3 | grep -qE "t_order .*6.0k|t_sku .*20"'

e2e_done
