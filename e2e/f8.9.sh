#!/usr/bin/env bash
# F8.9 默认布局的 data 和 query 等宽（specs/m8-feedback/task.md F8.9；tech-design §5、§7.1）
# 宽度分配有 golden（侧栏展开 / 折叠各一张）；这里在不同终端宽度下看真实终端里 ① 和 ② 的宽度差不超过 1 列，
# 侧栏折叠后也是，拆分出来的一对同样均分；窗口缩放后仍然均分。只读。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

# widths：① 和 ② 的宽度（e2e_panes 每行：编号 x y w h 焦点）
w1() { e2e_panes | awk '$1 == 1 { print $4 }'; }
w2() { e2e_panes | awk '$1 == 2 { print $4 }'; }
even() { local a b d; a=$(w1); b=$(w2); d=$((a - b)); (( d >= -1 && d <= 1 )) || { echo "  ① $a ② $b"; false; }; }

SOLO= start
check "160 宽：① 63、② 63（原来 70 : 56）" eval '[[ $(w1) == 63 && $(w2) == 63 ]] || { echo "  ① $(w1) ② $(w2)"; false; }'
check "侧栏 32 列" eval '[[ $(e2e_panes | awk "\$1 == 0 { print \$4 }") == 32 ]]'
for w in 200 180 140 120 100 81; do
  e2e_resize $w 45; sleep 0.4
  check "$w 宽：① 和 ② 宽度差不超过 1" even
done
e2e_resize 160 45; sleep 0.4

# ---- 侧栏折叠（SPC b）
e2e_keys Space; e2e_type b; sleep 0.5; e2e_keys Space; e2e_type b; sleep 0.5   # F19.5：第一个聚焦树，第二个折叠
check "（准备）SPC b（两次）折叠了侧栏（只剩 3 列的细栏 »）" eval '[[ $(e2e_panes | awk "\$1 == \"-\" { print \$4 }") == 3 ]]'
check "侧栏折叠后：① 和 ② 仍然等宽（折叠的细栏和分隔线没有算进左边）" even
e2e_keys Space; e2e_type b; sleep 0.5
check "再展开：等宽" even

# ---- 折叠后缩放
e2e_keys Space; e2e_type b; sleep 0.4; e2e_resize 133 45; sleep 0.4
check "折叠、133 宽：等宽" even
e2e_resize 160 45; sleep 0.3; e2e_keys Space; e2e_type b; sleep 0.4

# ---- 拆分出来的一对（SPC w … 的拆分命令）：本来就均分，规则没动
key C-l; e2e_keys Space; e2e_type '%'; sleep 0.6
check "（准备）在 ② 里向右拆分：现在有 4 个 pane" eval '[[ $(e2e_panes | awk "\$1 != \"-\" { n++ } END { print n }") == 4 ]] || { e2e_panes; false; }'
check "拆分出来的这一对（新的聚焦 pane 和它左边的那个）均分，宽度差不超过 1，两个加起来是原来的 63 减一条缝" eval 'set -- $(e2e_panes | awk "\$1 != \"-\" { w[NR] = \$4; if (\$6 == 1) f = NR } END { print w[f - 1], w[f] }"); (( $# == 2 && $1 - $2 <= 1 && $2 - $1 <= 1 && $1 + $2 == 62 )) || { echo "  widths: $*"; e2e_panes; false; }'
e2e_done
