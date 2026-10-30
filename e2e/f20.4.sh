#!/usr/bin/env bash
# F20.4 打开树时光标不跳：`[tree] follow`（specs/m20-redis-ui/task.md F20.4；tech-design §7.8「tree.toggle」、§6 SPC b、§14 [tree] follow）
# 默认：SPC b 进树、点折叠栏的 » 展开，树的光标原地不动（也不展开折着的节点）；`follow = true`：落到焦点 pane 当前 tab 的节点（F3.29，f3.29.sh 测这一条）。
# 只读，用共用库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
printf '[tree]\nfollow = true\n' >"$D/follow.toml"
printf '[tree]\nfollow = false\n' >"$D/nofollow.toml"

SELECT=#364a82
rows() { e2e_rows 2 31 4 $(( $(H) - 4 )) 3; }
cur() { rows | awk -F'|' -v s=$SELECT '$2 == s { print $1 }' | python3 -c 'import sys; print(" ".join("".join(c for c in sys.stdin.read() if not (0xE000 <= ord(c) <= 0xF8FF) and c not in "▸▾").split()))'; }   # 光标行（select 底）的文字
toggle() { key Space; e2e_type b; sleep 0.4; }
tree_shown() { [[ -n $(geom 0) ]]; }
visible() { rows | cut -d'|' -f1 | grep -qE "$1"; }                  # 树上看得到匹配 RE 的行
cur_is() { local c; c=$(cur); [[ $c == "$1" ]] || { echo "  cursor on \"$c\", want \"$1\""; false; }; }

# 准备：焦点在树上，public 折起来、光标停在它上面（t_order 不在可见节点里），再回 ① 聚焦 t_order 的 tab
prep() {
  open_table t_order; wait_for 8 settled
  key C-h; key g g; key 3 j; key h
  check "准备：光标在折起来的 public 上，树上看不到 t_order 表节点" eval '[[ $(cur) == public* ]] && ! visible "t_order .*6.0k"'
  P0=$(cur); key C-l
}

# ---- 默认（没写 [tree]）
start; prep
toggle
check "默认：焦点在 t_order 的 tab 上，SPC b 聚焦树：焦点到树，光标原地（public），t_order 没有被展开出来" eval 'focus_is 0 && cur_is "$P0" && ! visible "t_order .*6.0k"'
toggle; toggle
check "默认：SPC b 收起、再 SPC b 展开：光标还在 public，public 还是折着的" eval 'tree_shown && focus_is 0 && cur_is "$P0" && ! visible "t_order .*6.0k"'
key C-l; toggle; toggle; e2e_click 2 10; sleep 0.4       # 收起（先聚焦树再收起），点折叠栏的 »
check "默认：点折叠栏展开：焦点到树，光标还是 public" eval 'tree_shown && focus_is 0 && cur_is "$P0"'

# ---- 光标停在别的表上：聚焦一个别的 tab 再进树，光标还在原来那一行
key j; key j; key l; sleep 0.3; C0=$(cur)
check "准备：光标往下走了几行（不在 public 上）" eval '[[ $C0 != "$P0" && -n $C0 ]]'
key C-l; toggle
check "默认：聚焦 t_order 的 tab 再 SPC b：光标还在原来那一行" eval 'focus_is 0 && cur_is "$C0"'
key j; C1=$(cur)
check "（对照）进树后 j 照常动光标" eval '[[ $C1 != "$C0" ]]'

# ---- 显式写 follow = false：同默认
start -c "$D/nofollow.toml"; prep
toggle
check "follow = false：SPC b 聚焦树，光标原地" eval 'focus_is 0 && cur_is "$P0" && ! visible "t_order .*6.0k"'

# ---- follow = true：落到当前 tab 的节点，折着的 public、Tables 展开
start -c "$D/follow.toml"; prep
toggle
check "follow = true：SPC b 聚焦树：光标落在 public 下的 t_order，public 展开了" eval 'focus_is 0 && cur_is "t_order 6.0k" && visible "t_order .*6.0k"'
key C-l; toggle; toggle; toggle
check "follow = true：收起再展开：光标仍在 t_order" eval 'tree_shown && focus_is 0 && cur_is "t_order 6.0k"'
key j; key C-l; toggle; toggle; e2e_click 2 10; sleep 0.4
check "follow = true：点折叠栏展开：光标回到 t_order（点 » 同 SPC b）" eval 'tree_shown && focus_is 0 && cur_is "t_order 6.0k"'

e2e_done
