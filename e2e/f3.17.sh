#!/usr/bin/env bash
# F3.17 ? 键位帮助（specs/m3-console/task.md F3.17；tech-design §6.5「键位帮助」、§6.8）
# 浮层的画法由 golden（TestGoldenKeyHelp160x45）覆盖；这里测真实终端里的按键、用户配置的合并与分组、console 里的 <leader>?。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
mkdir -p "$D/c"; printf '[map.grid.normal]\nL = "5l"\n[keys.grid]\n"gz" = "grid.top"\n' >"$D/c/config.toml"
box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }       # 帮助浮层的 X Y W H
help() { local g y; g=($(box)); [[ -n ${g[0]} ]] || return 0
  for ((y = g[1]; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/^ *//; s/ *$//'; done; }   # 含上边框那一行（显示当前前缀）
has() { help | grep -qE -- "$1" || { echo "  help lacks /$1/:"; help | sed 's/^/    /'; false; }; }
under() { help | awk -v g="$1" -v k="$2" 'index($0, "[") == 1 { cur = $0 } cur == g && index($0, k) { found = 1 } END { exit !found }' || { echo "  '$2' not under $1"; false; }; }   # GROUP TEXT：TEXT 出现在组标题 GROUP 之下
mode() { bar | awk '{ print $NF }'; }

SOLO=1 start -C "$D/c"; open_table t_order
key '?'
check "表格里按 ?：打开键位帮助，上边框是 ?，模式 COMMAND" eval '[[ -n $(box) && $(help | head -1) == *"─ ? ─"* && $(mode) == COMMAND ]]'
check "列出 hjkl、g / y 前缀和它们的名字" eval 'has "h +→ 左移" && has "l +→ 右移" && has "g +→ …" && has "R +→ 刷新"'
check "用户映射 L → 5l 在组 [map.grid.normal] 下，排在 [keys.grid] 之前" eval 'under "[map.grid.normal]" "L   → 5l" && [[ $(help | grep -n "^\[" | head -2 | cut -d: -f2 | tr "\n" " ") == "[map.grid.normal] [keys.grid] " ]]'
check "normal、global 的键各在自己的组里（[keys.normal] 的 C-h，[keys.global] 的 C-p）" eval 'under "[keys.normal]" "C-h → " && under "[keys.global]" "C-p → 命令面板"'
key g
check "按 g：进入 g 这一层，列出 go / gl 和用户加的 gz" eval '[[ $(help | head -1) == *"─ g ─"* ]] && has "o → ORDER" && has "l → LIMIT" && under "[keys.grid]" "z → 第一行"'
key BSpace
check "<BS>：回到上一层" eval '[[ $(help | head -1) == *"─ ? ─"* ]]'
key g; key l
check "g 层里按 l：关掉帮助，照 grid 执行 gl（打开 LIMIT 下拉框）" eval '[[ $(help | head -1) != *"─ "[?g]" ─"* && $(mode) == COMMAND ]] && screen_has 1000'
key Escape; key Escape
key '?'; key Escape
check "esc 关闭帮助" eval '[[ -z $(box) && $(mode) == NORMAL ]]'
key '?'; key q
check "列表里没有的键（q）不做事，帮助还开着" eval '[[ -n $(box) ]]'
key Escape

# ---- 点框里不关、点框外才关；根层标题是 keyhelp.open 绑的键
key '?'; g=($(box)); e2e_click $((g[0] + 3)) $((g[1] + 1)); sleep 0.3
check "点帮助框里的组标题：不关" eval '[[ -n $(box) ]]'
e2e_click 60 5; sleep 0.3
check "点框外面：关掉" eval '[[ -z $(box) ]]'
printf '[keys.normal]\n"?" = ""\n[keys.grid]\n"g?" = "keyhelp.open"\n' >"$D/c2.toml"; mkdir -p "$D/c2"; mv "$D/c2.toml" "$D/c2/config.toml"   # F3.25 起 ? 在 [keys.normal]
SOLO=1 start -C "$D/c2"; open_table t_order; key g; key '?'
check "keyhelp.open 改绑成 g?：按 g? 打开，根层标题显示 g?" eval '[[ $(help | head -1) == *"─ g? ─"* ]] || { help | head -1; false; }'
key Escape

# ---- leader 是 Ctrl 键（<C-a>）时：帮助里按 <C-a>? 回到根层；按 <C-a> 弹出的 which-key 画在帮助上面
mkdir -p "$D/c3"; printf '[keys]\nleader = "<C-a>"\n' >"$D/c3/config.toml"
SOLO=1 start -C "$D/c3"; open_table t_order; key '?'; key g
key C-a; sleep 0.8
check "帮助里按 <C-a>：which-key 画在帮助上面，看得见（上边框 C-a）" eval 'e2e_plain | grep -q "^┌─ C-a "'
e2e_type '?'; sleep 0.4
check "接着按 ?：回到根层，列的还是表格的键" eval '[[ $(help | head -1) == *"─ C-a ? ─"* || $(help | head -1) == *"─ ? ─"* ]] && has "R +→ 刷新" || { help | head -3; false; }'
key Escape

# ---- F3.25：? 绑在 [keys.normal]，引导页、树、console 的 NORMAL 下都打开帮助
start; key '?'
check "① 的引导页上 ?：打开键位帮助" eval '[[ -n $(box) && $(help | head -1) == *"─ ? ─"* ]]'
key Escape; key C-h; key '?'
check "树上 ?：打开键位帮助" eval '[[ -n $(box) && $(help | head -1) == *"─ ? ─"* ]]'
key Escape; key C-l; key C-l
# console：NORMAL 下 ? 打开帮助；INSERT 下照常输入；VISUAL 下、操作符后面的 ? 仍是 vim 的反向搜索
key '?'
check "console 的 NORMAL 下 ?：打开键位帮助，不是反向搜索" eval '[[ -n $(box) && $(help | head -1) == *"─ ? ─"* && $(mode) == COMMAND && $(e2e_text 105 160 42) != "│?"* ]]'
key Escape
check "esc 关掉帮助，回到 NORMAL" eval '[[ -z $(box) && $(mode) == NORMAL ]]'
key i; e2e_type 'select 1 ?'; sleep 0.3; key Escape
check "INSERT 下 ?：照常输入到 console 里" eval '[[ -z $(box) && $(e2e_text 105 160 2) == *"select 1 ?"* ]]'
key v; key '?'
check "VISUAL 下 ?：交给 vim，是反向搜索的命令行" eval '[[ -z $(box) && $(mode) == COMMAND && $(e2e_text 105 160 42) == "│?"* ]]'
key Escape; key Escape; key d; key '?'
check "d 后面的 ?：交给 vim（d? 的搜索命令行）" eval '[[ -z $(box) && $(e2e_text 105 160 42) == "│?"* ]]'
key Escape
key Space; e2e_type '?'; sleep 0.4
check "console 里 <leader>?：打开帮助（根层标题是 keyhelp.open 绑的键，F3.25 起是 ?），列出 ↵ 执行和 gs，不列 vim 自己的键" eval '[[ $(help | head -1) == *"─ ? ─"* ]] && has "↵ +→ 执行" && has "gs +→ |g +→ …" && ! help | grep -qE "^dd|w +→ 下一个词"'
key Escape

e2e_done
