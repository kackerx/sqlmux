#!/usr/bin/env bash
# F0.5 状态栏（specs/m0-skeleton/task.md F0.5；tech-design §7.8「状态栏」「命令行」）
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

CFG=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$CFG"' EXIT
H() { e2e_flag pane_height; }
W() { e2e_flag pane_width; }
# 状态栏文字；Nerd Font 图标（私有区）换成 @，便于逐字比较
bar() { e2e_text 1 "$(W)" "$(H)" | python3 -c 'import sys; print("".join("@" if 0xE000 <= ord(c) <= 0xF8FF else c for c in sys.stdin.read().rstrip("\n")))'; }
bar_is()    { local got; got=$(bar); [[ $got == "$1" ]] || { echo "  bar: '$got'"; echo "  want '$1'"; false; }; }
bar_has()   { local got; got=$(bar); [[ $got == *"$1"* ]] || { echo "  bar lacks '$1': '$got'"; false; }; }
bar_lacks() { local got; got=$(bar); [[ $got != *"$1"* ]] || { echo "  bar has '$1': '$got'"; false; }; }
at() { local c; c=$(e2e_find "$1" "$(H)"); [[ -n $c ]] || { echo "  '$1' not on status bar"; return 1; }; style_has "$((${c%% *} + ${3:-0}))" "$(H)" "$2"; }  # TEXT STYLE [DX]
pending_is() { local c; c=$(pending_col); text_is $c $((c - 1 + $(strwidth "$1"))) "$(H)" "$1"; }   # F0.16：入口只剩搜索图标
BAR=#292e42 FOCUS=#9ece6a BG=#1f2335 FG=#c0caf5 DIM=#565f89 WARN=#e0af68 INFO=#7dcfff MUTED=#a9b1d6 SEP=#2f3549

# ---- 160 宽 NORMAL：内容与顺序
start
check "160 宽状态栏（F0.16：命令面板入口只有图标）" bar_is " @ doraemon ▾  0: data* $(printf '%88s')@  @ ·    1,1  @ sqlmux@localhost:55432  NORMAL "
check "状态栏底色 #292e42" eval 'style_has 40 45 bg=$BAR && style_has 100 45 bg=$BAR'
check "session 块：focus 底、bg 字、粗体" eval 'at doraemon bg=$FOCUS && at doraemon fg=$BG && at doraemon bold && style_has 1 45 bg=$FOCUS'
check "当前 window：#3b4261 底、fg 字" eval 'at "0: data*" bg=#3b4261 && at "0: data*" fg=$FG && at "0: data*" bg=#3b4261 -1'
# 其余 window 的样式与省略：F1.1 起只有一个 window，M5 能新建 window 时补回
check "命令面板入口（搜索图标）：info 色" eval 'style_has $(search_col) 45 fg=$INFO'
check "待输入序列空闲时为 dim 色的 ·" eval 'pending_is "·" && at "·" fg=$DIM'
check "光标位置 1,1：fg_muted" at "1,1" fg=$MUTED
check "连接地址：info 色、sep 底（含图标与内边距）" eval 'at "sqlmux@localhost:55432" fg=$INFO && at "sqlmux@localhost:55432" bg=$SEP && at "sqlmux@localhost:55432" bg=$SEP -3'
check "模式块 NORMAL：focus 底、bg 字、粗体，贴右边" eval 'at NORMAL bg=$FOCUS && at NORMAL fg=$BG && at NORMAL bold && style_has 160 45 bg=$FOCUS'

# ---- 待输入序列（B-03）
e2e_keys Space; sleep 0.2
check "按 SPC：显示 SPC，warn 色粗体" eval 'pending_is SPC && at SPC fg=$WARN && at SPC bold'
e2e_type s; sleep 0.2;  check "SPC s 完成后回到 ·" pending_is "·"
e2e_type g; sleep 0.2;  check "按 g：显示 g" pending_is g
e2e_type t; sleep 0.2;  check "gt 完成后回到 ·" pending_is "·"
e2e_type 5; sleep 0.2;  check "按 5：显示 5，warn 色粗体" eval 'pending_is 5 && at " 5 " fg=$WARN 1 && at " 5 " bold 1'
e2e_type g; sleep 0.2;  check "5 之后按 g：显示 5g" pending_is 5g
e2e_keys Escape; sleep 0.2; check "esc 清空待输入序列" pending_is "·"
e2e_keys Space; sleep 0.1; e2e_keys Escape; sleep 0.2; check "SPC 后 esc：清空" pending_is "·"

# ---- COMMAND 模式：F0.13 起由命令面板进入，状态栏不再有命令行；面板打开时背景（含状态栏）变暗 60%
e2e_type ':'; sleep 0.3
check "按 : 打开面板：模式块显示 COMMAND，状态栏左侧仍是 session 与 window" eval 'bar_has "@ doraemon ▾  0: data*" && [[ $(bar) == *" COMMAND " ]]'
check "COMMAND 模式块的底色是 info 变暗后的颜色（不是 focus）" eval 'at COMMAND bold && ! at COMMAND bg=$FOCUS >/dev/null && ! at COMMAND bg=$INFO >/dev/null'
e2e_keys Escape; sleep 0.2
check "esc 回到 NORMAL" eval 'at NORMAL bg=$FOCUS && bar_has doraemon'
start -x 80
e2e_type ':'; sleep 0.3
check "80 宽：面板打开时模式块 COMMAND 仍然可见" eval '[[ $(bar) == *" COMMAND " ]]'
e2e_keys Escape; sleep 0.2

# ---- NORMAL 逐步变窄：连接地址 →（非当前 window，M5）→ 光标位置 → 截短 session 名
start -x 73; check "73 宽：全部显示" bar_has "0: data*  @  @ ·    1,1  @ sqlmux@localhost:55432  NORMAL "
start -x 72; check "72 宽：先省略连接地址" eval 'bar_lacks sqlmux@ && bar_has "1,1"'
start -x 47; check "47 宽：光标位置还在" bar_has "1,1  NORMAL "
start -x 46; check "46 宽：去掉光标位置，session 名完整" eval 'bar_lacks "1,1" && bar_has " doraemon ▾"'
start -x 41; check "41 宽：截短 session 名" bar_has " doraem… ▾"
monotonic() {  # 省略顺序：有连接地址 ⇒ 有 1,1 ⇒ session 名完整；且必留的块都在
  local b; b=$(bar)
  local c=0 p=0 s=0
  [[ $b == *sqlmux@localhost* ]] && c=1; [[ $b == *"1,1"* ]] && p=1; [[ $b == *" doraemon ▾"* ]] && s=1
  ((c <= p && p <= s)) || { echo "  width $(W): order broken: '$b'"; return 1; }
  [[ $b == *"▾  0: data*"*" @  @ ·   "*" NORMAL " ]] || { echo "  width $(W): a kept block is missing: '$b'"; return 1; }
}
ok=1; for w in 160 120 100 90 80 79 78 70 60 58 57 50 48 47 46 44 42 41 40 38; do start -x $w -y 12; monotonic || ok=0; done
check "160…38 宽：省略顺序正确，session 块 / 0: data* / C-p / 待输入 / 模式块始终都在" test $ok = 1

# ---- 命令面板入口的键位文字取自 keymap（只有 ascii 图标下才显示文字，F0.16）
printf 'icons = "ascii"\n[keys.global]\n"<C-p>" = ""\n"<C-k>" = "palette.open"\n' >| "$CFG/config.toml"; start -c "$CFG/config.toml"
check "ascii 下改绑 palette.open 为 C-k：状态栏显示 ~ C-k" eval 'bar_has " ~ C-k " && bar_lacks "C-p"'

# ---- 待输入块至少 3 列、内容靠左：3 列以内时 C-p 不动（§7.8）
start
cp0=$(search_col)
check "空闲时 · 后补 2 格" bar_has " @  @ ·    1,1"
still() { local c; c=$(search_col); [[ $c == "$cp0" ]] || { echo "  palette entry at $c, idle at $cp0 ($1)"; false; }; }
ok=1
e2e_keys Space; sleep 0.2; still SPC || ok=0; pending_is "SPC" || ok=0; e2e_keys Escape; sleep 0.2
e2e_type g; sleep 0.2; still g || ok=0; pending_is "g  " || ok=0; e2e_keys Escape; sleep 0.2
e2e_type 5; sleep 0.2; still 5 || ok=0; e2e_type 2; sleep 0.2; still 52 || ok=0; pending_is "52 " || ok=0; e2e_keys Escape; sleep 0.2
check "按 SPC / g / 5 / 52：命令面板入口位置不变，序列靠左" test $ok = 1
e2e_type 5; e2e_keys Space; sleep 0.2
check "序列超过 3 列（5 SPC）时才变宽" eval 'pending_is "5 SPC" && (( $(search_col) < cp0 ))'
e2e_keys Escape; sleep 0.2

e2e_done
