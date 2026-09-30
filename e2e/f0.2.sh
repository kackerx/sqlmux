#!/usr/bin/env bash
# F0.2 Frame、Block、主题与静态布局（specs/m0-skeleton/task.md F0.2；tech-design §7.2 §7.3 §7.8）
# icons = "ascii" 要到 F0.3 才接入配置文件，这里只检查 golden；黑盒部分见 f0.3.sh。
# 前面的用例用 SOLO（① 独占右侧）；最后一节是 M3 的默认布局 ⓪ | ① | ② console_1（F3.6 补回 console，F3.11 补回 schema 下拉框）。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

FOCUS=#9ece6a BORDER=#3b4261 DIM=#565f89 BG=#1f2335 PANE_BG=#24283b SEP=#2f3549
NF_FILTER=$(printf '\xef\x82\xb0') NF_TABLE=$(printf '\xef\x83\x8e') NF_CONSOLE=$(printf '\xef\x92\x89')  # U+F0B0 U+F0CE U+F489（bash 3.2 没有 \u）

cols_are() { local got; got=$(e2e_find "$1" "$2"); [[ $got == "$3" ]] || { echo "  row $2 '$1' at [$got], want [$3]"; false; }; }
widths_are() { local got; got=$(e2e_widths | sed '/^0$/d' | sort -u | tr '\n' ' '); [[ $got == "$1 " ]] || { echo "  row widths: $got"; false; }; }
# 最后一行是状态栏（F0.5 起有内容）：没有 pane 的边框，底色 #292e42
status_row() { ! e2e_text 1 "$1" "$2" | python3 -c 'import sys; sys.exit(0 if set(sys.stdin.read()) & set("│┌┐└┘─") else 1)' && style_has $(($1 / 2)) "$2" bg=#292e42; }
at() { local c; c=$(e2e_find "$1" "$2"); [[ -n $c ]] || { echo "  '$1' not on row $2"; return 1; }; style_has "$((${c%% *} + ${4:-0}))" "$2" "$3"; }  # TEXT Y STYLE [DX]

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
check "tab 栏在内容区最后一行（第 43 行），F3.7 起编号后面是类型图标" eval '[[ $(e2e_text 34 160 43 | noicon) == *"│ 1:t_user- │ 2:t_order* │"* ]]'
check "当前 tab：pane_bg 底、focus 字" eval 'at "2:" 43 bg=$PANE_BG && at "2:" 43 fg=$FOCUS'
check "其他 tab：dim 字、bg 底；分隔符 #2f3549" eval 'at "1:" 43 fg=$DIM && at "1:" 43 bg=$BG && at "│ 2:" 43 fg=$SEP'
check "tab 后有 +，右端 dim 键位提示" eval 'text_has 34 160 43 "│ +" && text_ends 34 160 43 "gt/gT │" && at "gt/gT │" 43 fg=$DIM'

# ---- 侧栏内部（§7.8）
check "过滤行：图标 U+F0B0 info 色、表数 dim（F0.16：nerd 下没有 /）" eval 'text_is 3 3 2 "$NF_FILTER" && style_has 3 2 fg=#7dcfff && text_is 4 14 2 " 11 tables " && at "11 tables" 2 fg=$DIM'
check "分隔线 #2f3549" eval 'style_has 2 3 fg=$SEP && style_has 31 3 fg=$SEP'
tree_y() { local y; for y in $(seq 4 30); do [[ $(e2e_text 1 32 $y) == *" $1 "* ]] && echo $y && return; done; }   # 树里 NAME 所在的行（F1.12 起是层级树）
check "表项：图标 func、行数 border 色" eval 'y=$(tree_y t_order_item); at "$NF_TABLE" $y fg=#7aa2f7 && at "400" $y fg=$BORDER'
check "当前表 t_order（F1.2：与光标行分开画）：图标和表名 focus 色，行不是 select 底" eval 'y=$(tree_y t_order); at "$NF_TABLE" $y fg=$FOCUS && at t_order $y fg=$FOCUS && style_has 2 $y bg=$PANE_BG && style_has 31 $y bg=$PANE_BG'
check "提示行（F1.2：hintRow）：全部 dim 色，· 分隔" eval 'text_is 1 32 43 "│ j/k move · ↵ open            │" && at "j/k" 43 fg=$DIM && at "move" 43 fg=$DIM && at "↵ open" 43 fg=$DIM'

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

# ---- M3 默认布局 ⓪ | ① | ② console_1（§5、§7.8；F3.6 补回 F1.1 删掉的 console 用例）
SOLO= start
check "上边框角：侧栏 [1,32]、① [34,103]、② [105,160]；①:② = 70:56 = 5:4" eval 'cols_are ┌ 1 "1 34 105" && cols_are ┐ 1 "32 103 160"'
check "② console 未聚焦：边框 border、标题 dim" eval 'style_has 105 1 fg=$BORDER && style_has 160 44 fg=$BORDER && at "②" 1 fg=$DIM && at "console_1" 1 fg=$DIM'
check "160 宽 ② 标题：② <图标> console_1，右边 sqlmux.public ▾ 和 ▶ run ↵（F3.11）" eval '[[ $(e2e_text 105 160 1) =~ ^┌─\ ②\ $NF_CONSOLE\ console_1\ ─+\ sqlmux\.public\ ▾\ \ ▶\ run\ \ ↵\ ─┐$ ]] || { echo "  $(e2e_text 105 160 1)"; false; }'
check "▶ run：focus 底、bg 字、粗体；↵ dim" eval 'at "▶ run" 1 "fg=$BG" && at "▶ run" 1 "bg=$FOCUS" && at "▶ run" 1 bold && at "▶ run" 1 bold 3 && at "↵ ─┐" 1 fg=$DIM'
# 标题栏退让（§7.8 pane 标题）：先截对象名（截完就不显示），再按 ▶ run > 下拉框 > ↵ 从低往高丢，最后只留 ⟨n⟩
console_title() { local c; c=$(e2e_find ┌ 1); c=${c##* }; e2e_text "$c" "$(e2e_flag pane_width)" 1; }
title_ends() { local got; got=$(console_title); [[ $got == *"$1" ]] || { echo "  console title '$got', want suffix '$1'"; false; }; }
# §7.8 第 2 步：提示放不下才跳过——被跳过的提示，宽度一定大于「对象名 + 填充的 ─」所占的列数；
# 键位文字 ↵ 依附于 ▶ run，只在 run 显示时才要求（也才允许）出现。提示宽度含前导空格："  ▶ run " 8，" sqlmux.public ▾" 16，" ↵" 2。
no_wasted_room() {
  T=$(console_title) python3 - <<'PY'
import os, re, sys
t = os.environ["T"]
m = re.match(r"^┌─ ② \S((?: \S+)?) (─*)(.*)─┐$", t)   # ② <icon> [object]
if not m:
    sys.exit(0 if re.match(r"^┌─ ② ─*┐$", t) else f"  unparsed console title: {t!r}")
free = len(m[1]) + len(m[2])
run = "▶ run" in m[3]
if "↵" in m[3] and not run:
    sys.exit(f"  {t!r}: ↵ shown without ▶ run")
want = {"▶ run": 8, "sqlmux.public ▾": 16, **({"↵": 2} if run else {})}
extra = 0 if m[3].strip() else 1   # with no hint at all the title ends "─┐"; the first hint also brings the space in " ─┐"
skipped = [h for h, w in want.items() if h not in m[3] and free >= w + extra]
if skipped:
    sys.exit(f"  {t!r}: skipped {skipped} with {free} free columns")
PY
}
SOLO= start -x 200
check "200 宽：对象名完整，sqlmux.public ▾ 在 ▶ run ↵ 左边，①:② 仍约为 5:4" eval 't=$(console_title); [[ $t == "┌─ ② $NF_CONSOLE console_1 ─"*"─ sqlmux.public ▾  ▶ run  ↵ ─┐" ]] && set -- $(e2e_find ┌ 1) $(e2e_find ┐ 1) && dw=$(($5 - $2 + 1)) cw=$(($6 - $3 + 1)) && ((dw * 4 - cw * 5 <= 9 && cw * 5 - dw * 4 <= 9)) || { echo "  $t"; false; }'
SOLO= start -x 140; check "140 宽：提示都在，对象名先截短" title_ends "② $NF_CONSOLE console_1 ─ sqlmux.public ▾  ▶ run  ↵ ─┐"
SOLO= start -x 110; check "110 宽：↵ 最先让位，下拉框和 ▶ run 还在" title_ends "② $NF_CONSOLE  sqlmux.public ▾  ▶ run  ─┐"
SOLO= start -x 100; check "100 宽：下拉框放不下被跳过，▶ run ↵ 在" title_ends "② $NF_CONSOLE console…   ▶ run  ↵ ─┐"
SOLO= start -x 70;  check "70 宽：对象名放不下就不显示，▶ run ↵ 仍在" title_ends "② $NF_CONSOLE   ▶ run  ↵ ─┐"
SOLO= start -x 60;  check "60 宽：提示都放不下，只剩 ② <图标> 和截短的对象名" title_ends "─ ② $NF_CONSOLE cons… ─┐"
ok=1; for w in 65 70 75 80 85; do
  SOLO= start -x $w -y 12; t=$(console_title); [[ $t == *"↵"* && $t != *"▶ run"* ]] && { echo "  $w: '$t'"; ok=0; }
done
check "65–85 宽：没有脱离 ▶ run 单独出现的 ↵" test $ok = 1
ok=1; for w in 220 200 180 170 165 160 150 140 135 130 125 120 115 110 105 100 95 90 85 80 75 70 65 60; do
  SOLO= start -x $w -y 12; no_wasted_room || ok=0
done
check "220…60 宽：没有「放得下却没显示」的按钮" test $ok = 1
SOLO= start -x 80 -y 24
check "80 宽：侧栏 24、① 30、② 24" eval 'cols_are ┌ 1 "1 26 57" && cols_are ┐ 1 "24 55 80" && cols_are ┘ 23 "24 55 80"'
check "80 宽 ② 标题：② <图标> co… 加 ▶ run ↵" text_ends 57 80 1 "② $NF_CONSOLE co…   ▶ run  ↵ ─┐"

e2e_done
