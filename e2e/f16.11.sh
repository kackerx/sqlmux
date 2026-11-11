#!/usr/bin/env bash
# F16.11 补全框和右键菜单的图标（specs/m16-redis/task.md F16.11；tech-design §7.6「右键菜单」、§7.7、§9.7「候选行的布局」）
# 补全候选左边一列种类图标和颜色（照 nvim-cmp 的 lspkind）：关键字 / 表 / 列 / 片段，右边的 dim 说明照旧；右键菜单每项左边一个图标（表格的格、表头、结果 tab 的格、树的表和 query），
# 菜单宽度算上图标列；选中行的图标和文字同色、其余 dim；icons = "ascii" 下补全是字母、菜单没有图标；主题的 [icon] 覆盖对菜单和补全生效。
# 用 SQLite 文件（t_user 三行，22 张表）：不占 PG / MySQL。值、函数的候选图标（PG）在 f16.11b.sh，Redis 的在 f16.11c.sh。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/cellops.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
DB=$D/shop.db; sqlite3 "$DB" <"$E2E_ROOT/testdata/seed/sqlite.sql"
mkdir -p "$D/ascii" "$D/th/themes"; printf 'icons = "ascii"\n' >"$D/ascii/config.toml"
printf 'theme = "x"\n' >"$D/th/config.toml"
mkdir -p "$D/th2/themes"; printf 'theme = "y"\n' >"$D/th2/config.toml"
printf '[icon]\njsonview = { text = "V" }\nfunction = { text = "F" }\n' >"$D/th2/themes/y.toml"
printf '[icon]\nopen = { text = "O", fg = "#ff0000" }\nkeyword = { text = "K" }\njson = { text = "J" }\n' >"$D/th/themes/x.toml"
BOX() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }                # the last floating box: x y w h
# the rows of the open box, one line each: "U+f040 label" (the icon's code point; "-" when the row has none), the key after two spaces dropped (F22.3)
rows() { local g y; g=($(BOX)); [[ -n ${g[0]} ]] || return 0
  for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y; done | python3 -c '
import sys
import re
for l in sys.stdin.read().split("\n"):
    l = l.strip()
    if not l: continue
    if "\ue000" <= l[0] <= "\uf8ff" or "\U000f0000" <= l[0] <= "\U000ffffd": print("U+%04x %s" % (ord(l[0]), re.sub(r"\s{2,}\S+$", "", l[1:].strip())))
    else: print("- " + re.sub(r"\s{2,}\S+$", "", l))'; }
icon_fg() { local g; g=($(BOX)); e2e_style $((g[0] + 2)) $((g[1] + $1)); }                # N: the style of the icon cell of the Nth row (1-based)
rows_are() { local got; got=$(rows); [[ $got == "$1" ]] || { echo "  rows:"; echo "$got" | sed 's/^/    /'; false; }; }
has_row() { rows | grep -qE "$1" || { echo "  no row /$1/ in:"; rows | sed 's/^/    /'; false; }; }
newq() { key Escape; key Escape; e2e_type ggdG; sleep 0.3; key i; e2e_type "$1"; sleep 1.2; }
tree_row() { e2e_plain | python3 -c '
import re, sys
n = int(sys.argv[2]); hit = 0
for i, l in enumerate(sys.stdin.read().split("\n"), 1):
    if re.search(sys.argv[1], re.sub("[-] ?", "", l[:34])):
        hit += 1
        if hit == n: print(i); break' "$1" "${2:-1}"; }

e2e_start "$E2E_BIN $DB"; wait_for 5 flag_is alternate_on 1; sleep 1
open_table t_user; wait_for 8 settled

# ---- 表格的右键菜单：格、表头
rclick 50 8; sleep 0.6
check "格上右键：十三项（最后三项是行操作 row_add、row_delete、复制 clipboard），每项左边一个图标（值编辑器 pencil、过滤 filter、排序 ↑ ↓、加入排序、隐藏 eye_slash、复制 clipboard ×2、以 JSON 查看 json、导出 download）" rows_are "U+f040 在值编辑器中打开
U+f0b0 按这个值过滤
U+f062 按这一列排序 ↑
U+f063 按这一列排序 ↓
U+f062 加入排序
U+f070 隐藏这一列
U+f0ea 复制这一格
U+f0ea 复制行为 JSON
U+f1c9 以 JSON 查看
U+f019 导出…
U+f067 添加行
U+f068 删除行
U+f0ea 克隆行"
check "选中行（第 1 行）的图标和文字同色（fg）、其余行的图标是 dim" eval '[[ $(icon_fg 1) == "fg=#c0caf5 bg=#364a82" && $(icon_fg 2) == "fg=#565f89 bg=#24283b" ]]' || { icon_fg 1; icon_fg 2; }
g=($(BOX)); w0=${g[2]}
check "菜单宽度算上图标列：最长的一项「在值编辑器中打开」后面还有空格和右边框，没有被裁（rows_are 里项都是完整的）" eval '(( w0 == 25 ))' || echo "  w=$w0"
key j; key j; sleep 0.3
check "j j：选中移到第 3 行，图标颜色跟着（第 3 行 fg，第 1 行变 dim）" eval '[[ $(icon_fg 3) == "fg=#c0caf5 bg=#364a82" && $(icon_fg 1) == "fg=#565f89 bg=#24283b" ]]' || { icon_fg 1; icon_fg 3; }
key Escape; sleep 0.3
rclick 50 8; sleep 0.6; menu_click "复制这一格"; sleep 0.4
check "点菜单项照常（图标列不改命中）：「复制这一格」复制了那一格" eval '[[ -n $(clip) ]]'
hx=$(e2e_find name "$(hy)" | tr ' ' '\n' | awk '$1 > 34 { print; exit }')
rclick "$((hx + 1))" "$(hy)"; sleep 0.6
check "表头上右键：四项（排序 ↑ ↓、加入排序、隐藏这一列），图标同格菜单" rows_are "U+f062 按这一列排序 ↑
U+f063 按这一列排序 ↓
U+f062 加入排序
U+f070 隐藏这一列"
key Escape; sleep 0.3

# ---- 树的右键菜单：表、query
y=$(tree_row '[▸▾] +t_user( |$)'); rclick 10 "$y"; sleep 0.6
check "树里的表右键：打开 / 在新 tab 打开 open、删除表 trash、加列 column、加索引 index、改名 rename、删除列 hide、删除索引 trash" rows_are "U+f115 打开
U+f115 在新 tab 打开
U+f1f8 删除表…
U+eb5f 加列…
U+f0dc 加索引…
U+f246 改名…
U+f070 删除列…
U+f1f8 删除索引…"
key Escape; sleep 0.3
y=$(tree_row 'query_1' 2); rclick 10 "$y"; sleep 0.6
check "树里 Queries 下的 query 右键：打开 open、重命名 rename、删除 trash" rows_are "U+f115 打开
U+f246 重命名…
U+f1f8 删除…"
key Escape; sleep 0.3

# ---- 补全：种类图标和颜色
key C-l; key C-l; key i
newq "sel"
check "sel：select 是关键字：图标 keyword（U+f030b），色 keyword（#bb9af7）" eval 'has_row "^U\+f030b select" && [[ $(icon_fg 1) == "fg=#bb9af7 "* ]]' || icon_fg 1
newq "select * from t_u"
check "表：t_user 前面是 table 图标（U+f0ce），色 #7aa2f7（树里表的图标色）" eval 'has_row "^U\+f0ce t_user" && [[ $(icon_fg 1) == "fg=#7aa2f7 "* ]]' || icon_fg 1
newq "select * from t_user where na"
check "列：name 是 column 图标（U+eb5f）、natural join 是关键字图标（U+f030b）" eval 'has_row "^U\+eb5f name" && has_row "^U\+f030b natural join"'
newq "insert into t_user "
check "INSERT 的列清单片段：snippet 图标（U+eb66），色 info（#7dcfff）" eval 'has_row "^U\+eb66 \(id, name" && [[ $(icon_fg 1) == "fg=#7dcfff "* ]]' || rows
newq "sel"
key Tab; key Enter; sleep 0.4
check "Tab ↵ 接受：query 里是 select，图标没有写进去" eval '[[ $(e2e_plain | grep -m1 "▶  1 \|   1 select" | sed "s/.*1 //; s/ *│.*//") == "select" ]]' || e2e_plain | sed -n 2,3p | cut -c98-165
newq "select * from t_user"; key Escape; key Enter; wait_for 8 eval '[[ $(bar) != *busy* && -n $(geom 3) ]]'; sleep 0.5
g=($(geom 3)); rclick $((g[0] + 16)) $((g[1] + 5)); sleep 0.6
check "结果 tab 的格右键：值编辑器 pencil、复制这一格 clipboard、以 JSON 查看 json、导出 download" rows_are "U+f040 在值编辑器中打开
U+f0ea 复制这一格
U+f1c9 以 JSON 查看
U+f019 导出…"
key Escape; sleep 0.3
quit() { e2e_type ":"; sleep 0.4; e2e_type qa; key Enter; sleep 0.5; e2e_stop; }
quit

# ---- icons = "ascii"：补全是字母、菜单没有图标
e2e_start -C "$D/ascii" "$E2E_BIN $DB"; wait_for 5 flag_is alternate_on 1; sleep 1
open_table t_user; wait_for 8 settled
rclick 50 8; sleep 0.6
check "ascii：格的右键菜单十三项，没有图标列（行首就是文字）" rows_are "- 在值编辑器中打开
- 按这个值过滤
- 按这一列排序 ↑
- 按这一列排序 ↓
- 加入排序
- 隐藏这一列
- 复制这一格
- 复制行为 JSON
- 以 JSON 查看
- 导出…
- 添加行
- 删除行
- 克隆行"
g=($(BOX))
check "ascii：菜单比 nerd 的窄两列（23，没有图标和空格）" eval '(( ${g[2]} == 23 ))' || echo "  w=${g[2]}"
key Escape; sleep 0.3
y=$(tree_row 't_user( |$)'); rclick 10 "$y"; sleep 0.6
check "ascii：树里表的菜单同样没有图标" eval '! rows | grep -qv "^- "' || rows
key Escape; sleep 0.3
key C-l; key C-l; key i
newq "select * from t_u"
check "ascii：表候选前面是一个字母（+，ascii 的 table 图标），色同 nerd 的" eval 'has_row "^- \+ t_user" && [[ $(icon_fg 1) == "fg=#7aa2f7 "* ]]' || rows
newq "sel"
check "ascii：关键字候选前面是 k" eval 'has_row "^- k select"' || rows
quit

# ---- 主题的 [icon] 覆盖：open 换成红色的 O、keyword 换成 K、json 换成 J
e2e_start -C "$D/th" "$E2E_BIN $DB"; wait_for 5 flag_is alternate_on 1; sleep 1
open_table t_user; wait_for 8 settled
check "主题里写 json（v0.5.0 的键类型名，现在也是菜单的图标名）照常加载：启动没有「不认识的键」" eval '! screen_has "不认识的键"' || e2e_plain | head -3
rclick 50 8; sleep 0.6
check "主题覆盖 json = J：格菜单里「以 JSON 查看」前面是 J（不是 nf 的图标）" eval 'rows | grep -q "^- J 以 JSON 查看"'
key Escape; sleep 0.3
y=$(tree_row '[▸▾] +t_user( |$)'); rclick 10 "$y"; sleep 0.6
check "主题覆盖 open = O 红色：树里表菜单第一项前面是 O，图标是红色（fg=#ff0000）" eval 'rows | head -1 | grep -q "^- O 打开" && [[ $(icon_fg 1) == "fg=#ff0000 "* ]]' || { rows | head -2; icon_fg 1; }
check "同一个主题里 open 的覆盖对第二项（在新 tab 打开）也生效，不覆盖的 trash 还是默认的" eval 'rows | sed -n 2p | grep -q "^- O " && rows | sed -n 3p | grep -q "^U+f1f8 "'
key Escape; sleep 0.3
key C-l; key C-l; key i
newq "sel"
check "主题覆盖 keyword = K：关键字候选前面是 K" eval 'has_row "^- K select"' || rows
quit

# ---- 主题里写了不存在的图标名（jsonview 是 F16.11 早先的改名、function 是没有候选的种类）：启动时报「不认识的键」
e2e_start -C "$D/th2" "$E2E_BIN $DB"; wait_for 5 flag_is alternate_on 1; sleep 1.5
check "主题里写 jsonview / function：启动提示「不认识的键」，两个名字都在提示里" eval 'screen_has "不认识的键" && e2e_plain | tr -d "\n" | grep -q "jsonview" && e2e_plain | tr -d "\n" | grep -q "function"' || e2e_plain | head -3
e2e_done
