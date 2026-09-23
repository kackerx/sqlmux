#!/usr/bin/env bash
# F0.9 表格网格样式（specs/m0-skeleton/task.md F0.9；tech-design §7.6「网格样式」「列宽」，颜色见 §7.3）
# data pane：第 2 行 WHERE，第 3 行表头，第 4 行表头下的横线，第 5 行起是数据行；最后一行内容是 tab 栏。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

CFG=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$CFG"' EXIT
start() { e2e_start "$@" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3; }
SEP=#2f3549 FUNC=#7aa2f7 NUMBER=#ff9e64 PANE_BG=#24283b ROW_ALT=#1f2335 ROW=#292e42 CURSOR=#3d59a1 CURSOR_BLUR=#2f3549
data() { e2e_panes | awk '$1 == 1 { print $2, $3, $4, $5 }'; }   # ⟨1⟩ data 的 X Y W H
# 表格区的列范围：data pane 内部（不含边框）
inner() { local g; g=($(data)); echo $((g[0] + 1)) $((g[0] + g[2] - 2)); }
seps_in() { local a b; read a b <<<"$(inner)"; e2e_find "$1" "$2" | tr ' ' '\n' | awk -v a=$a -v b=$b '$1 >= a && $1 <= b' | tr '\n' ' '; }
# 竖线、横线上的 ┼、表头：列位置逐行一致（§7.6）
aligned() {
  local cross; cross=$(seps_in ┼ 4); [[ -n $cross ]] || { echo "  row 4 has no ┼"; return 1; }
  local g last y; g=($(data)); last=$((g[1] + g[3] - 3))   # 最后一行数据（tab 栏上一行）
  for y in 3 $(seq 5 $last); do [[ $(seps_in │ $y) == "$cross" ]] || { echo "  row $y │ at [$(seps_in │ $y)], ┼ at [$cross]"; return 1; }; done
  local a b; read a b <<<"$(inner)"
  [[ $(e2e_text $a $b 4 | tr -d '─┼') == "" ]] || { echo "  row 4: $(e2e_text $a $b 4)"; return 1; }
}
padded() {  # 每个单元格左右各留 1 列空白：│ 两边都是空格
  local x y g last; g=($(data)); last=$((g[1] + g[3] - 3))
  for y in 3 5 6 $last; do for x in $(seps_in │ $y); do
    [[ $(e2e_text $((x - 1)) $((x - 1)) $y) == " " && $(e2e_text $((x + 1)) $((x + 1)) $y) == " " ]] || { echo "  row $y col $x: '$(e2e_text $((x - 1)) $((x + 1)) $y)'"; return 1; }
  done; done
}
sep_color() { local x y; for y in 3 4 5 6; do for x in $(seps_in "$([[ $y == 4 ]] && echo ┼ || echo │)" $y); do style_has $x $y fg=$SEP || return 1; done; done; }
zebra() {  # 行号为偶数：row_alt；奇数：pane_bg；当前行（第 1 行）：row
  local y n want x=$1 g last; g=($(data)); last=$((g[1] + g[3] - 3))
  for y in $(seq 5 $last); do
    n=$(e2e_text $(( g[0] + 1 )) $(( g[0] + 3 )) $y | tr -d ' ')
    if [[ $n == 1 ]]; then want=$ROW; elif ((n % 2 == 0)); then want=$ROW_ALT; else want=$PANE_BG; fi
    style_has $x $y bg=$want >/dev/null || { echo "  row $y (#$n): $(e2e_style $x $y), want bg=$want"; return 1; }
  done
}
NF_KEY=$(printf '\xef\x82\x84')   # U+F084

# ---- 160×45
start
check "表头下画横线，与竖线交叉处为 ┼" eval '[[ $(seps_in ┼ 4) == "39 46 57 67 " ]]'
check "竖线、┼、表头的列位置逐行一致" aligned
check "每个单元格左右各留 1 列空白" padded
check "竖线和横线为 sep 色" sep_color
check "表头为 func 色：id / biz_type / status / created_at" eval 'style_has 43 3 fg=$FUNC && style_has 48 3 fg=$FUNC && style_has 59 3 fg=$FUNC && style_has 69 3 fg=$FUNC && text_is 40 67 3 " $NF_KEY id │ biz_type │ status  │"'
check "主键列 id 的表头带钥匙图标 U+F084" text_is 41 41 3 "$NF_KEY"
check "斑马纹：偶数行 row_alt、奇数行 pane_bg，当前行（第 1 行）row" zebra 60
check "当前单元格（第 1 行 id，含左右留白）为 cursor 色" eval 'style_has 40 5 bg=$CURSOR && style_has 45 5 bg=$CURSOR && style_has 39 5 bg=$ROW && style_has 47 5 bg=$ROW'
check "数值列 id 右对齐、number 色" eval 'text_is 40 45 5 "  689 " && style_has 42 5 fg=$NUMBER && style_has 44 6 fg=$NUMBER'
e2e_click 130 20; sleep 0.3
check "data 失焦：当前单元格改为 cursor_blur" eval 'style_has 40 5 bg=$CURSOR_BLUR && style_has 45 5 bg=$CURSOR_BLUR'
e2e_click 60 20; sleep 0.3
e2e_wheel 60 20 down; sleep 0.3
check "滚动后表头固定、斑马纹按行号" eval 'text_is 43 44 3 id && [[ $(e2e_text 35 38 5 | tr -d " ") == 4 ]] && zebra 60'
check "滚动后竖线仍对齐" aligned

# ---- 80×24：按比例压缩但不小于表头；放不下的列从右边裁掉；不越界
start -x 80 -y 24
check "80 宽：竖线与 ┼ 逐行对齐" aligned
check "80 宽：每行都是 80 列，data 右边框在原位" eval '[[ $(e2e_widths | sed "/^0$/d" | sort -u) == 80 ]] && [[ $(e2e_find ┐ 1) == "24 55 80" ]]'
check "80 宽：放不下的 status 从右边裁掉，只露出一截" text_ends 27 54 3 "│ stat"
check "80 宽：列宽不小于表头（biz_type 仍完整）" text_has 27 54 3 "biz_type"

# ---- ascii 图标：钥匙为 *
printf 'icons = "ascii"\n' >| "$CFG/config.toml"; start -c "$CFG/config.toml"
check "icons = ascii：主键表头为 * id" text_is 40 45 3 " * id "

e2e_done
