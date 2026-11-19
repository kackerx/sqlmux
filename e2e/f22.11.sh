#!/usr/bin/env bash
# F22.11 树上 > / < / = 调侧栏宽度（specs/m22-menus-keys/task.md F22.11；tech-design §7.8「键盘调宽」）
# > / < 每次 4 列、带次数 × 4，夹在 16 到窗口宽度的一半；= 按当前画得出来的最长一行适配（展开长名字的 schema 后最长一行完整、没有 …，收起后变窄）；
# 命令面板里有这三个命令；:qa 重启后宽度还在（和拖动同一份 window 记录）。自建库（长名字的 schema 和表），state 放临时目录。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
mkdir -p "$D/c"; (umask 077; printf '[[connection]]\nname = "rw"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/c/connections.toml")
LONG=s22_11
psql "$E2E_DB" -q -c "create schema $LONG" -c "create table $LONG.a_table_with_a_rather_long_name_yyyyyyyyyyyyyyyyyyyyyyyyyyy (id int)"
tw() { geom 0 | awk '{ print $3 }'; }                          # the sidebar's width
tw_is() { local w; w=$(tw); [[ $w == "$1" ]] || { echo "  sidebar width $w, want $1"; false; }; }
trow() { e2e_plain | grep -m1 "$1" | cut -c1-$(( $(tw) + 1 )); }
srow() { local y; for y in $(seq 3 43); do [[ $(e2e_text 1 80 "$y") == *"$1"* ]] && { echo "$y"; return; }; done; }
quit_app() { e2e_type ':qa'; e2e_keys Enter; wait_for 5 screen_has '[e2e-exit 0]'; }

SOLO=1 start -x 200 -C "$D/c" -S "$D/st"; sleep 1
key C-h
W0=$(tw)
check "（准备）焦点在树上（⓪），侧栏宽 $W0" eval '[[ $(focused) == 0 && -n $W0 ]]' 
e2e_keys '>'; sleep 0.4; e2e_keys '>'; sleep 0.5
check "树上 > 两下：宽 8 列（$W0 → $((W0 + 8))）" tw_is $((W0 + 8))
e2e_keys '<'; sleep 0.4; e2e_keys '<'; sleep 0.5
check "< 两下回去（$W0）" tw_is "$W0"
e2e_type 2; e2e_keys '>'; sleep 0.5
check "2> 加 8 列（带次数 × 4）" tw_is $((W0 + 8))
e2e_type 3; e2e_keys '<'; sleep 0.5
check "3< 减 12 列" tw_is $((W0 - 4))
for i in $(seq 12); do e2e_keys '<'; sleep 0.2; done; sleep 0.4
check "一路 < 夹在 16" tw_is 16
for i in $(seq 40); do e2e_keys '>'; sleep 0.15; done; sleep 0.4
check "一路 > 夹在窗口宽度的一半（200 列 → 100）" tw_is 100
e2e_type 30; e2e_keys '<'; sleep 0.5
check "（准备）30< 回到 16" tw_is 16

# = ：按画得出来的最长一行（树里别的行都比它短，S22_11 展开前后它才是最宽的）
LONGT=a_table_with_a_rather_long_name_yyyyyyyyyyyyyyyyyyyyyyyyyyy
treetext() { local w; w=$(tw); e2e_plain | cut -c1-$((w + 1)); }
no_ellipsis() { ! treetext | sed -n 4,40p | grep -q "…" || { treetext | sed -n 3,30p; false; }; }
key =; sleep 0.4
W1=$(tw)
check "= 在 s22_11 收起时：宽度按当前最长的一行，比 16 宽（$W1）、行都完整" eval '((W1 > 16)) && no_ellipsis'
y=$(srow "$LONG"); e2e_click 10 "$y"; sleep 0.3; key l; wait_for 5 eval '[[ -n $(srow "Tables (1)") ]]'
y=$(srow "Tables (1)"); e2e_click 10 "$y"; sleep 0.3; key l; wait_for 5 eval 'treetext | grep -q a_table_with'; sleep 0.5
check "（准备）s22_11 → Tables 展开：长名字的表在树里（被侧栏截成 …）" eval 'treetext | grep a_table_with | grep -q "…"' 
key =; sleep 0.5
W2=$(tw)
check "= 展开后：宽度变宽（$W1 → $W2），表名完整、整棵树没有 …" eval '((W2 > W1)) && treetext | grep -q "$LONGT" && no_ellipsis'
e2e_keys '<'; sleep 0.5
check "（紧贴）再 < 一下：表名被截成 …，说明 = 取的是刚好放下的宽度" eval 'tw_is $((W2 - 4)) && treetext | grep -q "…"'
key =; sleep 0.4
check "又 =：回到 $W2（和上一次一样）" tw_is "$W2"
y=$(srow "$LONG"); e2e_click 10 "$y"; sleep 0.3; key h; sleep 0.5
key =; sleep 0.4
check "收起 s22_11 再 =：宽度回到 $W1（展开和收起前后不同）" tw_is "$W1"

# 命令面板里有这三个命令
pal "侧栏"
check "命令面板：「侧栏」搜到 tree.wider / tree.narrower / tree.fit" eval 'list | grep -qE "tree.wider" && list | grep -q "tree.narrower" && list | grep -q "tree.fit" || { list; false; }'
key Escape; sleep 0.3
pal "tree.wider"
check "命令面板：按名字搜 tree.wider 也有" eval '[[ $(nrows) -ge 1 ]]'
key Escape; sleep 0.3

# which-key：树上按 ? 列着三个键
key '?'; sleep 0.6
check "树上 ?（which-key）：有 > 侧栏加宽、< 侧栏变窄、= 侧栏按最长的名字自适应" eval 'e2e_plain | grep -q ">   → 侧栏加宽" && e2e_plain | grep -q "<   → 侧栏变窄" && e2e_plain | grep -q "=   → 侧栏按最长的名字自适应"'
key Escape; sleep 0.3

# 和拖动同一份记录：:qa 重启、同一份 state，宽度还在
key =; key 2; e2e_keys '>'; sleep 0.5
WQ=$(tw)
quit_app
check "（准备）:qa 退出了" screen_has '[e2e-exit 0]'
SOLO= start -x 200 -C "$D/c" -S "$D/st"; sleep 1
check "重启后宽度还是 $WQ" tw_is "$WQ"

# 折叠时无事：SPC b 收起，从命令面板执行三条，展开后宽度不变
key Space; key b; sleep 0.4
check "（准备）SPC b：侧栏收起" folded
for c in 侧栏加宽 侧栏变窄 侧栏按最长的名字自适应; do pal ">$c"; key Enter; sleep 0.5; done
check "折叠时从面板执行加宽 / 变窄 / 自适应：还是收起的、程序没崩" eval 'folded && running'
key Space; key b; sleep 0.4
check "SPC b 展开：宽度还是 $WQ（折叠时三条都没动它）" tw_is "$WQ"
e2e_done
