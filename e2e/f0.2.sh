#!/usr/bin/env bash
# F0.2 Frame、Block、主题与静态布局（specs/m0-skeleton/task.md F0.2；tech-design §7.2 §7.3 §7.8）
# icons = "ascii" 要到 F0.3 才接入配置文件，这里只检查 golden；黑盒部分见 f0.3.sh。
# F1.1 起默认只有侧栏和一个 data pane：console 的标题、退让与 data:console 比例的用例到 M3 补回。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

FOCUS=#9ece6a BORDER=#3b4261 DIM=#565f89 BG=#1f2335 PANE_BG=#24283b SEP=#2f3549
NF_DATA=$(printf '\xef\x87\x80') NF_FILTER=$(printf '\xef\x82\xb0') NF_TABLE=$(printf '\xef\x83\x8e')  # U+F1C0 U+F0B0 U+F0CE（bash 3.2 没有 \u）

cols_are() { local got; got=$(e2e_find "$1" "$2"); [[ $got == "$3" ]] || { echo "  row $2 '$1' at [$got], want [$3]"; false; }; }
widths_are() { local got; got=$(e2e_widths | sed '/^0$/d' | sort -u | tr '\n' ' '); [[ $got == "$1 " ]] || { echo "  row widths: $got"; false; }; }
# 最后一行是状态栏（F0.5 起有内容）：没有 pane 的边框，底色 #292e42
status_row() { ! e2e_text 1 "$1" "$2" | python3 -c 'import sys; sys.exit(0 if set(sys.stdin.read()) & set("│┌┐└┘─") else 1)' && style_has $(($1 / 2)) "$2" bg=#292e42; }
at() { local c; c=$(e2e_find "$1" "$2"); style_has "$((${c%% *} + ${4:-0}))" "$2" "$3"; }  # TEXT Y STYLE [DX]

# ---- 160x45 nerd：布局（§7.8；F1.1 起只有侧栏和一个 data pane）
start
check "每行都是 160 列" widths_are 160
check "上边框角：侧栏 [1,32]、data [34,160]" eval 'cols_are ┌ 1 "1 34" && cols_are ┐ 1 "32 160"'
check "下边框在第 44 行，pane 占满状态栏以上" eval 'cols_are └ 44 "1 34" && cols_are ┘ 44 "32 160"'
check "最后一行留给状态栏" status_row 160 45
check "横向间隔 1 列，bg 底色" eval 'style_has 33 1 bg=$BG && style_has 33 20 bg=$BG && style_has 33 44 bg=$BG'
check "pane 内容区为 pane_bg" eval 'style_has 130 20 bg=$PANE_BG && style_has 150 30 bg=$PANE_BG'
two_tabs

# ---- 聚焦色（§7.2）
check "⟨1⟩ data 聚焦：边框 focus" eval 'style_has 34 1 fg=$FOCUS && style_has 34 20 fg=$FOCUS && style_has 160 44 fg=$FOCUS && style_has 60 44 fg=$FOCUS'
check "① data 聚焦：标题 focus" eval 'at "①" 1 fg=$FOCUS && at "t_order" 1 fg=$FOCUS'
check "⓪ 侧栏未聚焦：边框 border、标题 dim" eval 'style_has 1 1 fg=$BORDER && style_has 32 20 fg=$BORDER && at "⓪" 1 fg=$DIM && at "doraemon" 1 fg=$DIM'

# ---- 标题与提示（§7.2 §7.8）
check "标题从左上角右 1 列起、前留 1 空格（F0.16：nerd 下编号为 ⓪①②）" eval 'text_is 1 4 1 "┌─ ⓪" && text_is 34 37 1 "┌─ ①"'
check "侧栏提示 SPC b，离右上角 1 列" text_ends 1 32 1 " SPC b ─┐"

# ---- tab 栏（§7.8）
check "tab 栏在内容区最后一行（第 43 行）" eval 'text_has 34 160 43 "1:t_user- │ 2:t_order*"'
check "当前 tab：pane_bg 底、focus 字" eval 'at "2:t_order*" 43 bg=$PANE_BG && at "2:t_order*" 43 fg=$FOCUS'
check "其他 tab：dim 字、bg 底；分隔符 #2f3549" eval 'at "1:t_user-" 43 fg=$DIM && at "1:t_user-" 43 bg=$BG && at "│ 2:t_order" 43 fg=$SEP'
check "tab 后有 +，右端 dim 键位提示" eval 'text_has 34 160 43 "│ +" && text_ends 34 160 43 "gt/gT │" && at "gt/gT │" 43 fg=$DIM'

# ---- 侧栏内部（§7.8）
check "过滤行：图标 U+F0B0 info 色、表数 dim（F0.16：nerd 下没有 /）" eval 'text_is 3 3 2 "$NF_FILTER" && style_has 3 2 fg=#7dcfff && text_is 4 14 2 " 11 tables " && at "11 tables" 2 fg=$DIM'
check "分隔线 #2f3549" eval 'style_has 2 3 fg=$SEP && style_has 31 3 fg=$SEP'
tree_y() { local y; for y in $(seq 4 30); do [[ $(e2e_text 1 32 $y) == *" $1 "* ]] && echo $y && return; done; }   # 树里 NAME 所在的行（F1.12 起是层级树）
check "表项：图标 func、行数 border 色" eval 'y=$(tree_y t_order_item); at "$NF_TABLE" $y fg=#7aa2f7 && at "400" $y fg=$BORDER'
check "当前表 t_order（F1.2：与光标行分开画）：图标和表名 focus 色，行不是 select 底" eval 'y=$(tree_y t_order); at "$NF_TABLE" $y fg=$FOCUS && at t_order $y fg=$FOCUS && style_has 2 $y bg=$PANE_BG && style_has 31 $y bg=$PANE_BG'
check "提示行（F1.2：hintRow）：全部 dim 色，· 分隔" eval 'text_is 1 32 43 "│ j/k move · ↵ open · t tab    │" && at "j/k" 43 fg=$DIM && at "move" 43 fg=$DIM && at "↵ open" 43 fg=$DIM'

# ---- 80x24：侧栏 24 列、截断、不越界
start -x 80 -y 24
check "每行都是 80 列" widths_are 80
check "80 宽：侧栏 24、data 占满其余" eval 'cols_are ┌ 1 "1 26" && cols_are ┐ 1 "24 80" && cols_are ┘ 23 "24 80"'
check "最后一行留给状态栏" status_row 80 24

# ---- 侧栏宽度切换点：<100 列为 24
start -x 99;  check "99 宽侧栏 24 列" eval '[[ $(e2e_find ┐ 1) == 24\ * ]]'
start -x 100; check "100 宽侧栏 32 列" eval '[[ $(e2e_find ┐ 1) == 32\ * ]]'

# ---- 极小尺寸不 panic、不越界
for s in "40 10" "20 5" "10 3" "3 2" "1 1"; do
  set -- $s; start -x $1 -y $2
  check "${1}x$2 不 panic、每行 $1 列" eval "running && widths_are $1"
done

# ---- 运行中 resize 到小尺寸再回来
start
for s in "60 15" "5 5" "160 45"; do e2e_resize $s; sleep 0.3; done
check "resize 60x15 → 5x5 → 160x45 后画面完整" eval 'running && widths_are 160 && cols_are ┐ 1 "32 160"'

e2e_done
