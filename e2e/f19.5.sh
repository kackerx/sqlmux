#!/usr/bin/env bash
# F19.5 tree.toggle 照 DataGrip 带焦点（specs/m19-polish/task.md F19.5；tech-design §7.8「schema 侧栏」）
# SPC b：侧栏开着、焦点在树上 → 收起并回到进树之前的 pane；开着、焦点不在树上 → 只聚焦树、不收起；收起时 → 展开并聚焦树（j 动的是树的光标）。
# 回到的是进树之前的那个 pane（① 进的回 ①，② 进的回 ②），不是第一个；缩放中先退出缩放；点标题的 « 收起、点折叠栏的 » 展开并聚焦树。只读，用共用库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
leader() { e2e_keys Space; e2e_type "$1"; sleep 0.6; }
open_sb() { e2e_plain | sed -n 1p | grep -q "«"; }                 # 侧栏开着：标题上有 «
folded() { [[ $(e2e_text 1 3 1) == "┌─┐" ]]; }                    # 折叠栏：┌─┐
tsel() { local y; for y in $(seq 3 43); do [[ $(e2e_style 4 "$y") == *bg=#364a82* ]] && { e2e_text 1 32 "$y" | noicon | tr -s ' ' | sed 's/^[│ ]*[▾▸]* *//; s/[ │]*$//'; return; }; done; }
at() { local c; c=$(e2e_find "$1" "$2"); echo "${c%% *} $2"; }

start -S "$D/st"; sleep 1
check "（准备）默认布局：侧栏开着，三个 pane（0 1 2）" eval 'open_sb && [[ $(e2e_panes | awk "\$1 != \"-\" { printf \"%s \", \$1 }") == "0 1 2 " ]]'

# ---- 焦点在 ②
key C-l; key C-l; sleep 0.3
check "（准备）焦点在 ②" eval '[[ $(focused) == 2 ]] || { echo "  focused [$(focused)]"; false; }'
leader b
check "焦点在 ②、侧栏开着：SPC b 只把焦点移到树，侧栏还在" eval '[[ $(focused) == 0 ]] && open_sb && ! folded'
t0=$(tsel); key j; t1=$(tsel)
check "（对照）焦点真在树上：j 动的是树的光标" eval '[[ -n $t0 && $t0 != $t1 ]] || { echo "  [$t0] -> [$t1]"; false; }'
leader b
check "焦点在树上：SPC b 收起侧栏，焦点回到进树之前的 ②（不是第一个 pane）" eval 'folded && [[ $(focused) == 2 ]] || { echo "  focused [$(focused)]"; e2e_text 1 3 1; false; }'
leader b
check "侧栏收起时：SPC b 展开并聚焦树" eval 'open_sb && [[ $(focused) == 0 ]]'
t0=$(tsel); key j; t1=$(tsel)
check "展开后 j 动的是树的光标" eval '[[ $t0 != $t1 ]]'

# ---- 从 ① 进树，再回 ①
key C-l; sleep 0.3
check "（准备）树里按 C-l 到了 ①" eval '[[ $(focused) == 1 ]]'
leader b; leader b
check "从 ① 进树再 SPC b 收起：焦点回到 ①" eval 'folded && [[ $(focused) == 1 ]] || { echo "  focused [$(focused)]"; false; }'
leader b; key C-l; sleep 0.3
check "（准备）展开聚焦树后 C-l 又回到 ①，侧栏开着" eval 'open_sb && [[ $(focused) == 1 ]]'

# ---- 缩放中
key C-l; sleep 0.3; leader z
check "（准备）② 缩放中：侧栏被遮住" eval '! open_sb && [[ $(focused) == 2 ]]'
leader b
check "缩放中 SPC b：先退出缩放（三个 pane 和侧栏都回来），焦点在树" eval 'open_sb && [[ $(focused) == 0 ]] && [[ $(e2e_panes | awk "\$1 != \"-\" { printf \"%s \", \$1 }") == "0 1 2 " ]]'
key C-l; key C-l; sleep 0.3

# ---- 点 « 和 »
read x y <<<"$(at "«" 1)"; e2e_click $x $y; sleep 0.5
check "焦点在 ②（不在树上），点标题的 «：收起，焦点还在 ②" eval 'folded && [[ $(focused) == 2 ]] || { echo "  focused [$(focused)]"; false; }'
e2e_click 2 1; sleep 0.5
check "点折叠栏的 »：展开并聚焦树" eval 'open_sb && [[ $(focused) == 0 ]]'
t0=$(tsel); key j; t1=$(tsel)
check "点 » 之后 j 动的是树的光标" eval '[[ $t0 != $t1 ]]'
read x y <<<"$(at "«" 1)"; e2e_click $x $y; sleep 0.5
check "焦点在树上点 «：收起，焦点回到进树之前的 ②" eval 'folded && [[ $(focused) == 2 ]] || { echo "  focused [$(focused)]"; false; }'
check "程序没崩" running
e2e_done
