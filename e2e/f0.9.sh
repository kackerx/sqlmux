#!/usr/bin/env bash
# F0.9 表格网格样式（specs/m0-skeleton/task.md F0.9；tech-design §7.6「网格样式」「列宽」，颜色见 §7.3）
# F1.3 起表格是真实数据：打开 seed 的 t_order（按 id 排序）。表头下面一行是横线，再往下是数据行；最后一行内容是 tab 栏。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

CFG=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$CFG"' EXIT
SEP=#2f3549 FUNC=#7aa2f7 NUMBER=#ff9e64 PANE_BG=#24283b ROW_ALT=#1f2335 ROW=#292e42 CURSOR=#3d59a1 CURSOR_BLUR=#2f3549
data() { e2e_panes | awk '$1 == 1 { print $2, $3, $4, $5 }'; }   # ⟨1⟩ data 的 X Y W H
# 表格区的列范围：data pane 内部（不含边框）
inner() { local g; g=($(data)); echo $((g[0] + 1)) $((g[0] + g[2] - 2)); }
seps_in() { local a b; read a b <<<"$(inner)"; e2e_find "$1" "$2" | tr ' ' '\n' | awk -v a=$a -v b=$b '$1 >= a && $1 <= b' | tr '\n' ' '; }
sepy() { local y; for y in 2 3 4 5 6; do [[ -n $(seps_in ┼ $y) ]] && { echo $y; return; }; done; }   # 表头下的横线
last() { local g; g=($(data)); echo $((g[1] + g[3] - 3)); }   # 最后一行数据（tab 栏上一行）
# 竖线、横线上的 ┼、表头：列位置逐行一致（§7.6）
aligned() {
  local s cross y a b; s=$(sepy); [[ -n $s ]] || { echo "  no row with ┼"; return 1; }
  cross=$(seps_in ┼ $s)
  for y in $((s - 1)) $(seq $((s + 1)) $(last)); do [[ $(seps_in │ $y) == "$cross" ]] || { echo "  row $y │ at [$(seps_in │ $y)], ┼ at [$cross]"; return 1; }; done
  read a b <<<"$(inner)"
  [[ $(e2e_text $a $b $s | tr -d '─┼') == "" ]] || { echo "  row $s: $(e2e_text $a $b $s)"; return 1; }
}
padded() {  # 每个单元格左右各留 1 列空白：│ 两边都是空格
  local x y s; s=$(sepy)
  for y in $((s - 1)) $((s + 1)) $((s + 2)) $(last); do for x in $(seps_in │ $y); do
    [[ $(e2e_text $((x - 1)) $((x - 1)) $y) == " " && $(e2e_text $((x + 1)) $((x + 1)) $y) == " " ]] || { echo "  row $y col $x: '$(e2e_text $((x - 1)) $((x + 1)) $y)'"; return 1; }
  done; done
}
sep_color() { local x y s; s=$(sepy); for y in $((s - 1)) $s $((s + 1)) $((s + 2)); do for x in $(seps_in "$([[ $y == $s ]] && echo ┼ || echo │)" $y); do style_has $x $y fg=$SEP || return 1; done; done; }
rowno() { local g c; g=($(data)); c=$(seps_in ┼ $(sepy)); e2e_text $((g[0] + 1)) $((${c%% *} - 1)) $1 | tr -d ' '; }   # Y 行的行号
zebra() {  # X [CUR] — 行号为偶数：row_alt；奇数：pane_bg；当前行（光标所在，默认第 1 行）：row
  local y n want x=$1 cur=${2:-1}
  for y in $(seq $(( $(sepy) + 1 )) $(last)); do
    n=$(rowno $y)
    if [[ $n == "$cur" ]]; then want=$ROW; elif ((n % 2 == 0)); then want=$ROW_ALT; else want=$PANE_BG; fi
    style_has $x $y bg=$want >/dev/null || { echo "  row $y (#$n): $(e2e_style $x $y), want bg=$want"; return 1; }
  done
}
NF_KEY=$(printf '\xef\x82\x84')   # U+F084

# ---- 160×45：t_order 的列从第 34 列的边框起：行号 [35,39]、id [41,46]、user_id …
start; open_table t_order
HY=$(( $(sepy) - 1 )) DY=$(( $(sepy) + 1 ))   # 表头行、第一行数据
check "表头下画横线，与竖线交叉处为 ┼（行号列之后第一处在第 40 列）" eval '[[ $(seps_in ┼ $(sepy)) == "40 47 "* ]] || { echo "  ┼ at [$(seps_in ┼ $(sepy))]"; false; }'
check "竖线、┼、表头的列位置逐行一致" aligned
check "每个单元格左右各留 1 列空白" padded
check "竖线和横线为 sep 色" sep_color
heads_func() { local n c; for n in id user_id status created_at; do c=$(e2e_find "$n" $HY | tr ' ' '\n' | awk '$1 > 34 { print; exit }'); [[ -n $c ]] || { echo "  $n not in the header"; return 1; }; style_has $c $HY fg=$FUNC || return 1; done; }   # ① only: the sidebar has names too
check "表头为 func 色：id / user_id / status / created_at" eval 'heads_func && text_is 40 47 $HY "│ $NF_KEY id │"'
check "主键列 id 的表头带钥匙图标 U+F084" text_is 42 42 $HY "$NF_KEY"
check "斑马纹：偶数行 row_alt、奇数行 pane_bg，当前行（第 1 行）row" zebra 60
check "当前单元格（第 1 行 id，含左右留白）为 cursor 色" eval 'style_has 41 $DY bg=$CURSOR && style_has 46 $DY bg=$CURSOR && style_has 40 $DY bg=$ROW && style_has 48 $DY bg=$ROW'
check "数值列 id 右对齐、number 色" eval 'text_is 41 46 $DY "    1 " && style_has 45 $DY fg=$NUMBER && style_has 45 $((DY + 1)) fg=$NUMBER'
e2e_keys C-h; sleep 0.3
check "data 失焦：当前单元格改为 cursor_blur" eval 'style_has 41 $DY bg=$CURSOR_BLUR && style_has 46 $DY bg=$CURSOR_BLUR'
e2e_keys C-l; sleep 0.3
e2e_wheel 60 20 down; sleep 0.3
check "滚动后表头固定、斑马纹按行号（光标被夹回视图，当前行是第 4 行）" eval 'text_is 44 45 $HY id && [[ $(rowno $DY) == 4 ]] && zebra 60 4'
check "滚动后竖线仍对齐" aligned

# ---- 80×24：按比例压缩但不小于表头；放不下的列从右边裁掉；不越界
start -x 80 -y 24; open_table t_order
HY=$(( $(sepy) - 1 ))
check "80 宽：竖线与 ┼ 逐行对齐" aligned
check "80 宽：每行都是 80 列，data 右边框在原位" eval '[[ $(e2e_widths | sed "/^0$/d" | sort -u) == 80 ]] && [[ $(e2e_find ┐ 1) == "24 80" ]]'
check "80 宽：放不下的 meta 从右边裁掉，只露出一截，后面的列不画" eval 'text_has 26 80 $HY "│ meta" && ! text_has 26 80 $HY "raw" >/dev/null'
check "80 宽：列宽不小于表头（user_id / status / amount / paid 完整）" text_has 26 80 $HY "│ user_id │ status │ amount │ paid │"

# ---- ascii 图标：钥匙为 *
printf 'icons = "ascii"\n' >| "$CFG/config.toml"; start -c "$CFG/config.toml"; open_table t_order
check "icons = ascii：主键表头为 * id" text_is 41 46 $(( $(sepy) - 1 )) " * id "

e2e_done
