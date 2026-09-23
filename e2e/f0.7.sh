#!/usr/bin/env bash
# F0.7 布局树与 pane 操作（键盘）（specs/m0-skeleton/task.md F0.7；tech-design §5「布局树」、§7.8）
# 鼠标操作属于 F0.8。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

start() { e2e_start "$@" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3; }
L() { e2e_keys Space; e2e_type "$1"; sleep 0.3; }         # SPC <key>
key() { e2e_keys "$1"; sleep 0.25; }
focused() { e2e_panes | awk '$6 == 1 { print $1 }'; }     # 聚焦 pane 的编号
geom() { e2e_panes | awk -v n="$1" '$1 == n { print $2, $3, $4, $5 }'; }   # ⟨n⟩ 的 X Y W H
nums() { e2e_panes | awk '{ printf "%s ", $1 }'; }        # 按出现顺序（行优先）列出编号
focus_is() { local f; f=$(focused); [[ $f == "$1" ]] || { echo "  focused ⟨$f⟩, want ⟨$1⟩"; false; }; }
geom_is()  { local g; g=$(geom "$1"); [[ $g == "$2" ]] || { echo "  ⟨$1⟩ at [$g], want [$2]"; false; }; }
title_of() { local g; g=($(geom "$1")); e2e_text "${g[0]}" $((g[0] + g[2] - 1)) "${g[1]}"; }
empty_pane() {  # §7.8 空 pane：内容区为空、tab 栏只有 +
  local g y; g=($(geom "$1")); local x1=$((g[0] + 1)) x2=$((g[0] + g[2] - 2)) yb=$((g[1] + g[3] - 2))
  for ((y = g[1] + 1; y < yb; y++)); do [[ -z $(e2e_text $x1 $x2 $y | tr -d ' ') ]] || { echo "  ⟨$1⟩ row $y: $(e2e_text $x1 $x2 $y)"; return 1; }; done
  [[ $(e2e_text $x1 $x2 $yb | sed 's/ *$//') == " +" ]] || { echo "  ⟨$1⟩ tab bar: '$(e2e_text $x1 $x2 $yb)'"; false; }
}
width_of() { local g; g=($(geom "$1")); echo "${g[2]}"; }
NF_DATA=$(printf '\xef\x87\x80')   # U+F1C0

# ---- 分割（§5）：同类型的空 pane，获得焦点，按树的遍历顺序编号
start
L '"'
check 'SPC "：data 上下分割，⟨1⟩ data / ⟨2⟩ data（新）/ ⟨3⟩ console' eval 'geom_is 1 "34 1 70 22" && geom_is 2 "34 23 70 22" && geom_is 3 "105 1 56 44"'
check "新 pane 是同类型的空 pane，标题只有 ⟨2⟩ <图标> data" eval '[[ $(title_of 2) == "┌─ ⟨2⟩ $NF_DATA data ─"*"─┐" ]] && empty_pane 2'
check "新 pane 获得焦点" focus_is 2
start
L %
check "SPC %：左右分割，⟨1⟩ data / ⟨2⟩ data（新）/ ⟨3⟩ console，新 pane 聚焦" eval 'geom_is 1 "34 1 35 44" && geom_is 2 "70 1 34 44" && geom_is 3 "105 1 56 44" && focus_is 2 && empty_pane 2'

# ---- 按方向切焦点：与几何位置一致（§5：相邻且重叠最长）
start
L '"'                                   # ⟨1⟩ 左上、⟨2⟩ 左下、⟨3⟩ 右
key C-l; check "左下 C-l → 右边的 console ⟨3⟩" focus_is 3
key C-h; check "console C-h → 左边的 data（⟨1⟩ 或 ⟨2⟩）" eval '[[ $(focused) == 1 || $(focused) == 2 ]]'
key C-h; check "再 C-h → 侧栏 ⟨0⟩" focus_is 0
key C-l; key C-k; check "C-k → 上面的 ⟨1⟩" focus_is 1
key C-j; check "C-j → 下面的 ⟨2⟩" focus_is 2
L k; check "SPC k 与 C-k 相同" focus_is 1
L j; check "SPC j 与 C-j 相同" focus_is 2
L l; check "SPC l 与 C-l 相同" focus_is 3
L h; check "SPC h 与 C-h 相同（离开 console）" eval '[[ $(focused) == 1 || $(focused) == 2 ]]'
# 右边再上下分割，把分界线挪开，让重叠长度有差别
key C-l; L '"'                          # console 分成 ⟨3⟩ 上、⟨4⟩ 下
L K; L K; L K                           # console 的分界线上移 15%
key C-h; key C-k; check "准备：焦点在左上 ⟨1⟩" focus_is 1
key C-l; check "左上 C-l → 与它重叠更长的右上 ⟨3⟩" focus_is 3
key C-h; key C-j; key C-l; check "左下 C-l → 与它重叠更长的右下 ⟨4⟩" focus_is 4
key C-l; check "最右边再 C-l：焦点不动" focus_is 4

# ---- 关闭（§5）：兄弟 pane 占满；侧栏和唯一的 pane 关不掉
start
L '"'; L x
check "SPC x：关掉新 pane，⟨1⟩ data 恢复原来的大小" eval 'geom_is 1 "34 1 70 44" && [[ $(nums) == "0 1 2 " ]]'
L x
check "关掉 data：console 占满主区域并获得焦点" eval 'geom_is 1 "34 1 127 44" && focus_is 1 && [[ $(title_of 1) == *console* ]]'
L x
check "唯一的 pane：SPC x 不关闭" eval 'geom_is 1 "34 1 127 44" && flag_is alternate_on 1'
key C-h; L x
check "侧栏：SPC x 不关闭" eval 'geom_is 0 "1 1 32 44" && focus_is 0'

# ---- 缩放（§5 / P-03）：占满状态栏以上的整个区域，侧栏也被盖住；再按还原
start
L z
check "SPC z：⟨1⟩ 占满 160×44，侧栏不画" eval 'geom_is 1 "1 1 160 44" && [[ $(nums) == "1 " ]]'
L z
check "再 SPC z：还原" eval '[[ $(nums) == "0 1 2 " ]] && geom_is 1 "34 1 70 44" && focus_is 1'
L z; L '"'; check "缩放中分割：退出缩放，照常分割" eval '[[ $(nums) == "0 1 3 2 " ]] && focus_is 2'
L z; L x; check "缩放中关闭：退出缩放，照常关闭" eval '[[ $(nums) == "0 1 2 " ]] && geom_is 1 "34 1 70 44"'
L z; L q; e2e_type 2; sleep 0.3; check "缩放中按编号跳转：退出缩放，跳到 ⟨2⟩" eval '[[ $(nums) == "0 1 2 " ]] && focus_is 2'
start
L z; e2e_type ':q'; e2e_keys Enter; sleep 0.3; e2e_type ':q'; e2e_keys Enter; sleep 0.3
check "回归：缩放中用 :q 关掉 data 的最后一个 tab，画面正常、console 占满" eval '[[ $(nums) == "0 1 " ]] && geom_is 1 "34 1 127 44" && [[ $(title_of 1) == *console* ]]'

# ---- 调整大小（§5）：每次 5%，次数前缀，10%–90%；方向与 tmux resize-pane 相同
start
check "准备：data 70 列（主区域 127 列）" eval '[[ $(width_of 1) == 70 ]]'
L H; check "SPC H：分割线左移约 5%（6 列）" eval '[[ $(width_of 1) == 64 ]]'
L L; check "SPC L：移回" eval '[[ $(width_of 1) == 70 ]]'
e2e_type 3; L H; check "3 SPC H：一次左移约 15%" eval '[[ $(width_of 1) == 51 ]]'
for i in $(seq 20); do L H; done
check "一直 SPC H：停在 10%" eval '(( $(width_of 1) * 100 / 127 == 10 ))'
for i in $(seq 25); do L L; done
check "一直 SPC L：停在 90%" eval 'w=$(width_of 1); (( (w + 1) * 100 / 127 >= 89 && w * 100 / 127 <= 90 ))'
start
key C-l; L H
check "焦点在右边的 console 时 SPC H 也是分割线左移（与 tmux 相同）" eval '[[ $(width_of 1) == 64 ]]'
L J; check "没有上下分割时 SPC J 不起作用" eval '[[ $(e2e_panes | awk "{print \$5}" | sort -u) == 44 ]]'
key C-h; L '"'; L J
check "上下分割后 SPC J：分割线下移" eval 'geom_is 2 "34 25 64 20"'
L K; L K; check "SPC K ×2：分割线上移" eval 'geom_is 2 "34 21 64 24"'

# ---- 按编号跳转（§5）：编号一直显示；数字跳转；其他键只关闭编号
start
L q
check "SPC q：每个 pane 中央显示编号（侧栏为 0）" eval '[[ $(e2e_text 16 16 23) == 0 && $(e2e_text 68 68 23) == 1 && $(e2e_text 132 132 23) == 2 ]]'
sleep 2; check "编号不会自动消失" eval '[[ $(e2e_text 132 132 23) == 2 ]]'
e2e_type 2; sleep 0.3; check "按 2：跳到 ⟨2⟩，编号消失" eval 'focus_is 2 && [[ $(e2e_text 132 132 23) != 2 ]]'
L q; e2e_type 0; sleep 0.3; check "SPC q 0：跳到侧栏" focus_is 0
L q; e2e_type 7; sleep 0.3; check "不存在的编号：焦点不动" focus_is 0
L q; e2e_type ':'; sleep 0.3
check "按其他键（:）：只关闭编号，不打开命令行" eval 'focus_is 0 && [[ $(e2e_text 16 16 23) != 0 ]] && [[ $(e2e_text 1 160 45) != *COMMAND* ]]'

# ---- 折叠侧栏（§7.8）：3 列细栏，» 加竖排 schema · SPC b；再按恢复
start
L b
check "SPC b：侧栏折叠成 3 列宽的细栏" eval '[[ $(e2e_text 1 3 1) == "┌─┐" && $(e2e_text 1 3 44) == "└─┘" ]] && geom_is 1 "5 1 86 44"'
check "细栏：顶部 »，下面竖排 schema · SPC b" eval 's=""; for y in $(seq 2 16); do s+=$(e2e_text 2 2 $y); done; [[ $s == "»schema · SPC b" ]] || { echo "  got: $s"; false; }'
key C-h; check "折叠后 C-h 进不去侧栏" focus_is 1
L q; e2e_type 0; sleep 0.3; check "折叠后 SPC q 0 也跳不过去" focus_is 1
L b; check "再 SPC b：恢复 32 列" eval 'geom_is 0 "1 1 32 44" && geom_is 1 "34 1 70 44"'
key C-h; L b; check "焦点在侧栏时折叠：焦点移到主区域" eval '[[ $(focused) == 1 ]]'

e2e_done
