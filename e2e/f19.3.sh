#!/usr/bin/env bash
# F19.3 pane 右上角的四个按钮（specs/m19-polish/task.md F19.3；tech-design §7.8「pane 标题」、§7.7）
# 每个 window pane 的标题右端：往右分割、往下分割、缩放（缩放中是还原图标）、关闭；点哪个 pane 的按钮就作用于那个 pane（先聚焦再执行），
# 侧栏不画；窄 pane 整组不画；空 pane 也画；ascii 图标下是 | = [Z] x。只读，用共用库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
SR=$(printf '\xee\xad\x96'); SB=$(printf '\xee\xad\x97'); ZM=$(printf '\xef\x81\xa5'); UZ=$(printf '\xef\x81\xa6'); CL=$(printf '\xef\x80\x8d')   # U+EB56 EB57 F065 F066 F00D
pn() { e2e_panes | awk '$1 != "-" { n++ } END { print n + 0 }'; }
pinfo() { e2e_panes | awk -v p="$1" '$1 == p { print $2, $3, $4, $5 }'; }                   # X Y W H of pane P
# btn GLYPH PANE: "X Y" of GLYPH on the title row of pane PANE (empty when it is not drawn there)
btn() { local g=($(pinfo "$2")) c; [[ -n ${g[0]} ]] || return 0
  for c in $(e2e_find "$1" "${g[1]}"); do (( c >= g[0] && c < g[0] + g[2] )) && { echo "$c ${g[1]}"; return 0; }; done; }
click_btn() { local b; b=$(btn "$1" "$2"); [[ -n $b ]] || { echo "  no $1 on pane $2"; return 1; }; e2e_click $b; sleep 0.7; }
all4() { local p=$1 a b c d; a=$(btn "$SR" $p | cut -d' ' -f1); b=$(btn "$SB" $p | cut -d' ' -f1); c=$(btn "$ZM" $p | cut -d' ' -f1); d=$(btn "$CL" $p | cut -d' ' -f1)
  [[ -n $a && -n $b && -n $c && -n $d ]] && (( a < b && b < c && c < d )) || { echo "  pane $p: $a $b $c $d"; false; }; }
none() { [[ -z $(btn "$SR" $1)$(btn "$SB" $1)$(btn "$ZM" $1)$(btn "$UZ" $1)$(btn "$CL" $1) ]] || { echo "  pane $1 has buttons"; false; }; }

# ---- 默认布局（160 列）：① 的标题右端有四个，从左到右往右分割、往下分割、缩放、关闭；侧栏没有
start; sleep 1
check "（准备）默认布局三个 pane（0 1 2）" eval '[[ $(pn) == 3 ]]'
check "① 的标题右端有四个按钮，从左到右：往右分割 U+EB56、往下分割 U+EB57、缩放 U+F065、关闭 U+F00D" all4 1
check "侧栏（⓪）的标题没有按钮，只有 «" none 0
check "② 的标题已被 query 的工具占满：整组不画（要么四个都在要么都不在，不画半组）" eval 'none 2 || all4 2'

# ---- 命中区和悬停：每个按钮是图标加它后面那 1 列（共 2 列），最后一个按钮后面那列一起亮
SELECT=#364a82
read sx sy <<<"$(btn "$SR" 1)"; read cx cy <<<"$(btn "$CL" 1)"
e2e_move $((sx + 1)) $sy; sleep 0.4
check "鼠标停在往右分割后面那一列：这个按钮的图标和后面那列都是 select 底，前一列不是" eval 'style_has $sx $sy bg=$SELECT && style_has $((sx + 1)) $sy bg=$SELECT && ! style_has $((sx - 1)) $sy bg=$SELECT'
e2e_move $((cx + 1)) $cy; sleep 0.4
check "鼠标停在关闭后面那一列（离右上角的空隙）：关闭的图标和这一列都亮" eval 'style_has $cx $cy bg=$SELECT && style_has $((cx + 1)) $cy bg=$SELECT'
e2e_move 10 30; sleep 0.3

# ---- 点 ① 的往右分割：多一个 pane，原来的 ① 变窄
w0=$(pinfo 1 | cut -d' ' -f3); x0=$(pinfo 1 | cut -d' ' -f1)
e2e_click $((sx + 1)) $sy; sleep 0.7   # 点的是图标后面那一列
check "点 ① 往右分割按钮后面那一列也执行：多出一个 pane（4 个），① 变窄了，焦点在新 pane，新 pane 在 ① 的右边、同一行" eval '[[ $(pn) == 4 ]] && (( $(pinfo 1 | cut -d" " -f3) < w0 )) && f=$(focused) && g=($(pinfo $f)) && (( g[0] > x0 )) && (( g[1] == 1 )) || { e2e_panes; false; }'
# ---- 往下分割
h0=$(pinfo 1 | cut -d' ' -f4); x1=$(pinfo 1 | cut -d' ' -f1)
click_btn "$SB" 1
check "点 ① 的往下分割：又多一个 pane（5 个），① 的高度变矮了，焦点在新 pane，新 pane 在 ① 的下边、同一列" eval '[[ $(pn) == 5 ]] && (( $(pinfo 1 | cut -d" " -f4) < h0 )) && f=$(focused) && g=($(pinfo $f)) && (( g[1] > 1 )) && (( g[0] == x1 )) || { e2e_panes; false; }'
# ---- 关闭
n0=$(pn); click_btn "$CL" 1
check "点 ① 的关闭：少一个 pane" eval '[[ $(pn) == $((n0 - 1)) ]] || { e2e_panes; false; }'
e2e_stop

# ---- 缩放：点缩放，这个 pane 占满、按钮换成还原；再点还原
start -x 220; sleep 1
check "（准备）220 列：① 和 ② 的标题都有四个按钮" eval 'all4 1 && all4 2'
click_btn "$ZM" 1
check "点 ① 的缩放：① 占满（只剩一个 pane 在画，侧栏也不画），标题右端的按钮换成还原 U+F066（编号后面的 zoom 状态图标照旧在左边）" eval 'g=($(pinfo 1)); u=$(btn "$UZ" 1 | cut -d" " -f1); c=$(btn "$CL" 1 | cut -d" " -f1); (( g[2] >= 215 )) && [[ -n $u && -n $c ]] && (( u < c )) && [[ -z $(btn "$ZM" 1) || $(btn "$ZM" 1 | cut -d" " -f1) -lt $u ]] || { e2e_panes; e2e_plain | sed -n 1p | cut -c1-220; false; }'
click_btn "$UZ" 1
check "点还原：三个 pane 都回来，① 标题右端又是缩放" eval '[[ $(pn) == 3 ]] && all4 1 && [[ -z $(btn "$UZ" 1) ]]'

# ---- 点不在焦点的 pane 的按钮：作用于被点的 pane，不是焦点 pane
key C-l; sleep 0.3
check "（准备）焦点在 ②" eval '[[ $(focused) == 2 ]]'
click_btn "$ZM" 1
check "焦点在 ②，点 ① 的缩放：缩放的是 ①（先聚焦再执行），不是 ②" eval '[[ $(focused) == 1 ]] && g=($(pinfo 1)) && (( g[2] >= 215 )) && [[ -z $(pinfo 2) ]] || { e2e_panes; false; }'
click_btn "$UZ" 1
check "（还原）三个 pane" eval '[[ $(pn) == 3 ]]'
key C-l; sleep 0.3
click_btn "$CL" 1
check "焦点在 ②，点 ① 的关闭：关的是 ①（引导页没了），② 和它的 query_1 还在，只剩侧栏和一个 pane" eval '[[ $(pn) == 2 ]] && ! e2e_plain | grep -q "打开表" && e2e_plain | grep -q "query_1" || { e2e_panes; false; }'
e2e_stop

# ---- 空 pane（新 window 的引导页）也画
start; sleep 1
e2e_keys Space; e2e_type c; sleep 0.8
check "SPC c 新 window 的空 pane ①（引导页）：四个按钮也画" eval 'e2e_plain | grep -q "打开表" && all4 1'
e2e_stop

# ---- 窄 pane：整组不画（不画半组）
start -x 70; sleep 1
check "（准备）70 列：① 22 列宽，四个按钮放得下" all4 1
click_btn "$SR" 1; click_btn "$SR" 1
check "把 ① 往右分割两次：最窄的 pane 不到 12 列，它的标题整组不画（不画半组），宽的照画" eval 'ok=1; narrow=0; for p in $(e2e_panes | awk "\$1 != \"-\" && \$1 != 0 { print \$1 }"); do w=$(pinfo $p | cut -d" " -f3); if (( w < 12 )); then narrow=1; none $p || ok=0; else all4 $p || none $p || ok=0; fi; done; (( narrow == 1 && ok == 1 )) || { e2e_panes; e2e_plain | sed -n 1p | cut -c1-70; false; }'
e2e_stop

# ---- ascii 图标：| = [Z] x
mkdir -p "$D/c"; printf 'icons = "ascii"\n' >"$D/c/config.toml"
start -C "$D/c"; sleep 1
check "icons = ascii：① 的标题右端是 | = [Z] x" eval 't=$(e2e_plain | sed -n 1p); [[ $t == *" | = [Z] x "* ]] || { echo "  [$t]"; false; }'
click_btn "[Z]" 1
check "ascii 下缩放中：右端变成 | = [z] x（还原是小写 [z]），编号后面的 [Z] 状态图标照旧在左边" eval 't=$(e2e_plain | sed -n 1p); [[ $t == *"⟨1⟩ [Z] "*" | = [z] x "* ]] || { echo "  [$t]"; false; }'
e2e_done
