#!/usr/bin/env bash
# F0.2 Frame、Block、主题与静态布局（specs/m0-skeleton/task.md F0.2；tech-design §7.2 §7.3 §7.8）
# icons = "ascii" 要到 F0.3 才接入配置文件，这里只检查 golden；黑盒部分见 f0.3.sh。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

FOCUS=#9ece6a BORDER=#3b4261 DIM=#565f89 BG=#1f2335 PANE_BG=#24283b SEP=#2f3549
NF_FILTER=$(printf '\xef\x82\xb0') NF_CONSOLE=$(printf '\xef\x84\xa0')  # U+F0B0 U+F120（bash 3.2 没有 \u）

cols_are() { local got; got=$(e2e_find "$1" "$2"); [[ $got == "$3" ]] || { echo "  row $2 '$1' at [$got], want [$3]"; false; }; }
widths_are() { local got; got=$(e2e_widths | sed '/^0$/d' | sort -u | tr '\n' ' '); [[ $got == "$1 " ]] || { echo "  row widths: $got"; false; }; }
# 最后一行是状态栏（F0.5 起有内容）：没有 pane 的边框，底色 #292e42
status_row() { ! e2e_text 1 "$1" "$2" | python3 -c 'import sys; sys.exit(0 if set(sys.stdin.read()) & set("│┌┐└┘─") else 1)' && style_has $(($1 / 2)) "$2" bg=#292e42; }
running() { flag_is alternate_on 1 && ! screen_has '[e2e-exit'; }
at() { local c; c=$(e2e_find "$1" "$2"); style_has "$((${c%% *} + ${4:-0}))" "$2" "$3"; }  # TEXT Y STYLE [DX]
start() { e2e_start "$@" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3; }

# ---- 160x45 nerd：布局（§7.8）
start
check "每行都是 160 列" widths_are 160
check "上边框角：侧栏 [1,32]、data [34,103]、console [105,160]" eval 'cols_are ┌ 1 "1 34 105" && cols_are ┐ 1 "32 103 160"'
check "下边框在第 44 行，pane 占满状态栏以上" eval 'cols_are └ 44 "1 34 105" && cols_are ┘ 44 "32 103 160"'
check "最后一行留给状态栏" status_row 160 45
check "横向间隔 1 列，bg 底色" eval 'style_has 33 1 bg=$BG && style_has 33 20 bg=$BG && style_has 104 44 bg=$BG'
check "data:console = 70:56 = 5:4" eval '(( (103 - 34 + 1) * 4 == (160 - 105 + 1) * 5 ))'
check "pane 内容区为 pane_bg" eval 'style_has 130 20 bg=$PANE_BG && style_has 150 30 bg=$PANE_BG'   # data 的表格有斑马纹（F0.9）

# ---- 聚焦色（§7.2）
check "⟨1⟩ data 聚焦：边框 focus" eval 'style_has 34 1 fg=$FOCUS && style_has 34 20 fg=$FOCUS && style_has 103 44 fg=$FOCUS && style_has 60 44 fg=$FOCUS'
check "⟨1⟩ data 聚焦：标题 focus" eval 'at "⟨1⟩" 1 fg=$FOCUS && at "t_order" 1 fg=$FOCUS'
check "⟨0⟩ 侧栏未聚焦：边框 border、标题 dim" eval 'style_has 1 1 fg=$BORDER && style_has 32 20 fg=$BORDER && at "⟨0⟩" 1 fg=$DIM && at "schema" 1 fg=$DIM'
check "⟨2⟩ console 未聚焦：边框 border、标题 dim" eval 'style_has 105 1 fg=$BORDER && style_has 160 44 fg=$BORDER && at "⟨2⟩" 1 fg=$DIM && at "console" 1 fg=$DIM'

# ---- 标题与提示（§7.2 §7.8）
check "标题从左上角右 1 列起、前留 1 空格" eval 'text_is 1 4 1 "┌─ ⟨" && text_is 34 37 1 "┌─ ⟨" && text_is 105 108 1 "┌─ ⟨"'
check "标题文字：schema / data · t_order / console" eval 'text_has 1 32 1 schema && text_has 34 103 1 "data · t_order" && text_has 105 160 1 "⟨2⟩ $NF_CONSOLE console"'
check "侧栏提示 SPC b，离右上角 1 列" text_ends 1 32 1 " SPC b ─┐"
check "160 宽 console 先截对象名，下拉框与 ▶ run ↵ 完整（§7.8 退让）" text_ends 105 160 1 "console · cons…  doraemon.public ▾  ▶ run  ↵ ─┐"
check "▶ run：focus 底、bg 字、粗体" eval 'at "▶ run" 1 "fg=$BG" && at "▶ run" 1 "bg=$FOCUS" && at "▶ run" 1 bold && at "▶ run" 1 bold 3'
check "↵：dim 色" at "↵ ─┐" 1 fg=$DIM

# ---- tab 栏（§7.8）
check "tab 栏在内容区最后一行（第 43 行）" eval 'text_has 34 103 43 "1:t_order*" && text_has 34 103 43 "2:t_user-" && text_has 105 160 43 "1:console_1*"'
check "当前 tab：pane_bg 底、focus 字" eval 'at "1:t_order*" 43 bg=$PANE_BG && at "1:t_order*" 43 fg=$FOCUS'
check "其他 tab：dim 字、bg 底；分隔符 #2f3549" eval 'at "2:t_user-" 43 fg=$DIM && at "2:t_user-" 43 bg=$BG && at "│ 2:t_user" 43 fg=$SEP'
check "tab 后有 +，右端 dim 键位提示" eval 'text_has 34 103 43 "│ +" && text_ends 34 103 43 "gt/gT │" && at "gt/gT │" 43 fg=$DIM'

# ---- 侧栏内部（§7.8）
check "过滤行：图标 U+F0B0 info 色、/ fg、表数 dim" eval 'text_is 3 3 2 "$NF_FILTER" && style_has 3 2 fg=#7dcfff && at "/ " 2 fg=#c0caf5 && at "14 tables" 2 fg=$DIM'
check "分隔线 #2f3549" eval 'style_has 2 3 fg=$SEP && style_has 31 3 fg=$SEP'
check "表项：图标 func、行数 border 色" eval 'style_has 3 4 fg=#7aa2f7 && at "124" 4 fg=$BORDER'
check "当前表 t_order：图标 focus、整行 select 底" eval 'y=$(for y in $(seq 4 20); do [[ $(e2e_text 1 32 $y) == *" t_order "* ]] && echo $y && break; done); style_has 3 $y fg=$FOCUS && style_has 2 $y bg=#364a82 && style_has 31 $y bg=#364a82'
check "提示行：键名 focus 粗体、说明 dim" eval 'at "j/k" 43 fg=$FOCUS && at "j/k" 43 bold && at "move" 43 fg=$DIM'

# ---- 标题栏退让（§7.8 pane 标题）：先截对象名，再按 ▶ run > 下拉框 > ↵ 从低往高丢，最后只留 ⟨n⟩
console_title() { local c; c=$(e2e_find ┌ 1); c=${c##* }; e2e_text "$c" "$(e2e_flag pane_width)" 1; }
title_ends() { local got; got=$(console_title); [[ $got == *"$1" ]] || { echo "  console title '$got', want suffix '$1'"; false; }; }
# §7.8 第 2 步：提示放不下才跳过——被跳过的提示，宽度一定大于「对象名 + 填充的 ─」所占的列数；
# 键位文字 ↵ 依附于 ▶ run，只在 run 显示时才要求（也才允许）出现。
# 提示宽度含前导空格：" doraemon.public ▾" 18，"  ▶ run " 8，" ↵" 2。
no_wasted_room() {
  T=$(console_title) python3 - <<'PY'
import os, re, sys
t = os.environ["T"]
m = re.match(r"^┌─ ⟨2⟩ \S console((?: · \S+)?) (─*)(.*)─┐$", t)
if not m:
    sys.exit(0 if re.match(r"^┌─ ⟨2⟩ ─*┐$", t) else f"  unparsed console title: {t!r}")
free = len(m[1]) + len(m[2])
run = "▶ run" in m[3]
if "↵" in m[3] and not run:
    sys.exit(f"  {t!r}: ↵ shown without ▶ run")
want = {"▶ run": 8, "doraemon.public ▾": 18, **({"↵": 2} if run else {})}
skipped = [h for h, w in want.items() if h not in m[3] and free >= w]
if skipped:
    sys.exit(f"  {t!r}: skipped {skipped} with {free} free columns")
PY
}
start -x 200
check "200 宽：对象名完整，doraemon.public ▾ 在 ▶ run ↵ 左边" title_ends "console · console_1 ────────────── doraemon.public ▾  ▶ run  ↵ ─┐"
check "200 宽：data:console 仍约为 5:4" eval 'set -- $(e2e_find ┌ 1) $(e2e_find ┐ 1); dw=$(($5 - $2 + 1)) cw=$(($6 - $3 + 1)); ((dw * 4 - cw * 5 <= 9 && cw * 5 - dw * 4 <= 9))'
start -x 140; check "140 宽：对象名已截完，先丢 ↵" title_ends "⟨2⟩ $NF_CONSOLE console ─ doraemon.public ▾  ▶ run  ─┐"
start -x 130; check "130 宽：跳过下拉框，▶ run ↵ 完整，对象名完整" title_ends "⟨2⟩ $NF_CONSOLE console · console_1 ─  ▶ run  ↵ ─┐"
start -x 100; check "100 宽：只剩 ▶ run" title_ends "⟨2⟩ $NF_CONSOLE console ─  ▶ run  ─┐"
start -x 60;  check "60 宽：只保留 ⟨2⟩" title_ends "─ ⟨2⟩ ───────┐"
ok=1; for w in 110 115 120 125 130; do start -x $w -y 12; title_ends "▶ run  ↵ ─┐" || ok=0; done
check "110–130 宽：下拉框放不下时仍显示 ▶ run ↵" test $ok = 1
ok=1; for w in 65 70 75 80 85; do
  start -x $w -y 12; t=$(console_title); [[ $t == *"↵"* && $t != *"▶ run"* ]] && { echo "  $w: '$t'"; ok=0; }
done
check "65–85 宽：没有脱离 ▶ run 单独出现的 ↵" test $ok = 1
ok=1; for w in 220 200 180 170 165 160 150 140 135 130 125 120 115 110 105 100 95 90 85 80 75 70 65 60; do
  start -x $w -y 12; no_wasted_room || ok=0
done
check "220…60 宽：没有「放得下却没显示」的按钮" test $ok = 1

# ---- 80x24：侧栏 24 列、截断、不越界
start -x 80 -y 24
check "每行都是 80 列" widths_are 80
check "80 宽：24 / 30 / 24" eval 'cols_are ┌ 1 "1 26 57" && cols_are ┐ 1 "24 55 80" && cols_are ┘ 23 "24 55 80"'
check "console 标题截断为 console · c…" text_has 57 80 1 "console · c… ─┐"
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
check "resize 60x15 → 5x5 → 160x45 后画面完整" eval 'running && widths_are 160 && cols_are ┐ 1 "32 103 160"'

e2e_done
