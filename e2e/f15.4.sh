#!/usr/bin/env bash
# F15.4 状态栏的特性提示（specs/m15-pg-databases/task.md F15.4；tech-design §7.8「特性提示」）
# 命令面板入口左边一段 dim 色的「Tip: …」；单击换下一条、不重复到轮完；`?` 作用域列出全部、↵ 执行那个 Action（不是面板的一个 tab）；
# `tips.next`「下一条提示」、`tips.show`「所有提示…」；键位从 keymap 读（解绑的跳过、改键跟着变）；窄屏最先让位；`[ui] tips = false` 关掉。
# 5 分钟自动换一条的那一下在 f15.4b.sh（要等 5 分多钟）。lib.sh 默认把 tips 关了，这里每份 config.toml 自己写 [ui]。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
mkdir -p "$D/on" "$D/off" "$D/en" "$D/km"
printf '[ui]\n' >"$D/on/config.toml"                       # 空的 [ui]：tips 默认开
printf '[ui]\ntips = false\n' >"$D/off/config.toml"
printf 'language = "en"\n\n[ui]\n' >"$D/en/config.toml"
printf '[ui]\n' >"$D/km/config.toml"
printf '[keys.grid]\n"yl" = ""\n"yi" = ""\n"<C-y>" = "grid.yank"\n' >"$D/km/keymaps.toml"

# 状态栏里的提示：「Tip: 」之后到两个以上空格之前
tip() { bar | python3 -c "
import re, sys
m = re.search(r'Tip: (.*?)(?:  |\$)', sys.stdin.read())
print(m.group(1) if m else '')"; }
tip_x() { e2e_find "Tip:" "$(H)" | cut -d' ' -f1; }
tip_click() { e2e_click $(( $(tip_x) + 3 )) "$(H)"; sleep 0.4; }
# 面板 ? 作用域里的全部提示：滚一遍，每行只留提示的文字（截断的 … 去掉）
all_rows() { local i; for ((i = 0; i < ${1:-45}; i++)); do list | cut -d'|' -f1 | sed 's/  .*//; s/…$//'; key Down; done | sort -u; }

start -C "$D/on"
check "tips 默认开（[ui] 里没写 tips）：状态栏有「Tip: …」" eval '[[ -n $(tip) ]]' || bar
check "提示是 dim 色（#565f89）" eval 'style_has "$(( $(tip_x) + 1 ))" "$(H)" fg=#565f89'
check "提示在命令面板入口（搜索图标）的左边，在左边几段的右边" eval '(( $(tip_x) > 30 && $(tip_x) < $(search_col) ))' || { tip_x; search_col; }
check "提示里的键位是 keymap 里的（没有留着 {key} 占位符）" eval '[[ $(tip) != *"{key}"* ]]'

# ---- 单击换下一条；一轮里不重复；轮完再来
pal '?'; sleep 0.6
ROWS=$(all_rows 40); N=$(echo "$ROWS" | grep -c .)
key Escape; sleep 0.3
check "? 作用域：列出全部提示（二十多条，每条是「提示」类）" eval '(( N >= 20 ))' || echo "  N=$N"
SEQ=(); for ((i = 0; i < N; i++)); do SEQ+=("$(tip)"); tip_click; done
check "单击换下一条：一轮 $N 次，每次都不一样（一轮里不重复）" eval '[[ $(printf "%s\n" "${SEQ[@]}" | sort -u | grep -c .) == "$N" ]]' || printf '  %s\n' "${SEQ[@]}"
check "轮完重来：第 $((N + 1)) 条不是上一轮最后一条" eval '[[ $(tip) != "${SEQ[$((N - 1))]}" ]]'
check "一轮里出现的提示正好是 ? 里列的这些（前缀对得上）" eval 'python3 - "$ROWS" "${SEQ[@]}" <<PY
import sys
rows = [r for r in sys.argv[1].split("\n") if r]
seq = sys.argv[2:]
ok = all(any(s.startswith(r) for s in seq) for r in rows) and all(any(s.startswith(r) for r in rows) for s in seq)
sys.exit(0 if ok else 1)
PY' || { echo "$ROWS"; }

# ---- ? 作用域：不是面板的一个 tab；↵ 执行那一条的 Action
pal; sleep 0.4
check "面板的 tab 行里没有「提示 ?」" eval '! e2e_text 1 160 "$(tabs_y)" | grep -qF "提示"' || e2e_text 1 160 "$(tabs_y)"
ok=1; for i in 1 2 3 4 5 6 7 8 9; do key Tab; input_is "?" >/dev/null 2>&1 && ok=0; done
check "Tab 转一圈不会停在 ? 作用域上" eval '(( ok == 1 ))'
key Escape; sleep 0.3
pal '?'; sleep 0.4; e2e_type "侧栏"; sleep 0.5
check "? 后再打「侧栏」：过滤到「{key} 打开 / 聚焦 / 收起侧栏」（F19.5 改了文字）" eval 'list | head -3 | grep -q "聚焦 / 收起侧栏"' || list
key Enter; sleep 0.6
check "↵：执行 tree.toggle：侧栏开着、焦点到树上，不收起（F19.5）" eval 'screen_has "Tables (" && [[ $(focused) == 0 ]]' || e2e_cap | head -8
e2e_keys Space; e2e_type b; sleep 0.6
check "再 SPC b：侧栏收起来了（树里的 Tables 没有了）" eval '! screen_has "Tables (" && closed' || e2e_cap | head -8
pal; sleep 0.5
check "执行过的提示不进「最近」：面板刚打开时的列表里没有「提示」类的行" eval '! list | grep -qE "  提示\|"' || list
key Escape; sleep 0.3
key Space; sleep 0.2; key e; sleep 0.5

# ---- tips.next / tips.show
before=$(tip)
pal ">下一条提示"; sleep 0.4
check "命令面板里有「下一条提示」（tips.next）" eval 'row_has "下一条提示"' || list
key Enter; sleep 0.5
check "↵：换了一条" eval '[[ -n $(tip) && $(tip) != "$before" ]]'
pal ">所有提示"; sleep 0.4
check "命令面板里有「所有提示…」（tips.show）" eval 'row_has "所有提示"' || list
key Enter; sleep 0.5
check "↵：面板以 ? 作用域重新打开，列的是提示" eval 'is_open && row_has "提示"' || list
key Escape; sleep 0.3

# ---- 窄屏：提示最先让位，别的段还在
# 模式附加信息（-- editing WHERE --）比提示重要：宽的时候两个都在，窄一点提示先让位、信息一个字不少
open_table t_user; wait_for 8 settled; key /; sleep 0.5
check "表格里 / 写 WHERE：状态栏有「-- editing WHERE --」，提示也在" eval '[[ $(bar) == *"-- editing WHERE --"* && -n $(tip) ]]' || bar
# 提示是随机抽的，长短不一：一点一点收窄，每个宽度上信息都是整条的，提示在某个宽度让位
ok=1 gone=0; for w in 135 125 115 105 95; do
  e2e_resize $w 45; sleep 0.8
  [[ $(bar) == *"-- editing WHERE --"* ]] || ok=0
  [[ -z $(tip) ]] && { gone=$w; break; }
done
check "收窄（135 … 95 列）：「-- editing WHERE --」每个宽度上一个字不少，提示在某个宽度（$gone 列）让位" eval '(( ok && gone > 0 ))' || bar
e2e_resize 160 45; sleep 0.8; key Escape; sleep 0.3; key Escape; sleep 0.3
# 一点一点收窄：提示（长短不一）在某个宽度让位，那时左边的 0: data 和右边的连接地址还在；让位之前它是整条的（不被截断）
gone=0; for w in 140 130 120 110 100 95 90 85; do
  e2e_resize $w 45; sleep 0.8
  t=$(tip); [[ -n $t ]] && { [[ $(bar) == *"Tip: $t  "* ]] || { echo "  $w 列：提示被截断了：$(bar)"; break; }; }
  if [[ -z $t ]]; then gone=$w; break; fi
done
check "窗口收窄：提示在某个宽度（$gone 列）整条让位，此时 0: data 和连接地址还在；让位之前是整条的" eval '(( gone > 0 )) && [[ $(bar) == *"0: data"* && $(bar) == *"sqlmux@"* ]]' || bar
e2e_resize 160 45; sleep 1
check "回到 160 列：提示又出来了" eval '[[ -n $(tip) ]]' || bar

# ---- 英文
e2e_stop
start -C "$D/en"
check "language = en：提示是英文（没有汉字），仍以「Tip: 」开头" eval 't=$(tip); [[ -n $t && ! $t =~ [一-龥] ]]' || tip
e2e_stop

# ---- 关掉
start -C "$D/off"
sleep 1
check "[ui] tips = false：状态栏没有提示" eval '[[ $(bar) != *"Tip:"* ]]'
e2e_stop

# ---- 键位跟着 keymap：解绑的跳过，改键跟着变
start -C "$D/km"
pal '?'; sleep 0.6
ROWS2=$(all_rows 40)
key Escape; sleep 0.3
check "改键：yl 解绑、<C-y> 绑到 grid.yank 之后，复制单元格那一条写的是 C-y" eval 'echo "$ROWS2" | grep -q "table pane 里 C-y 复制当前单元格" && ! echo "$ROWS2" | grep -q "table pane 里 yl "' || echo "$ROWS2"
check "yi 解绑（grid.yank.insert 没有别的键）：复制成 INSERT 那一条不在了" eval '! echo "$ROWS2" | grep -q "复制成 INSERT"'
check "其余提示的数量比默认少一条（N - 1）" eval '[[ $(echo "$ROWS2" | grep -c .) == $((N - 1)) ]]' || echo "  $(echo "$ROWS2" | grep -c .) vs $N"
SEQ2=(); for ((i = 0; i < N; i++)); do SEQ2+=("$(tip)"); tip_click; done
check "一轮里从不出现解绑的那一条" eval '! printf "%s\n" "${SEQ2[@]}" | grep -q "复制成 INSERT"'

e2e_done
