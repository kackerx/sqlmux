#!/usr/bin/env bash
# F0.5 状态栏（specs/m0-skeleton/task.md F0.5；tech-design §7.8「状态栏」「命令行」）
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

CFG=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$CFG"' EXIT
start() { e2e_start "$@" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3; }
H() { e2e_flag pane_height; }
W() { e2e_flag pane_width; }
# 状态栏文字；Nerd Font 图标（私有区）换成 @，便于逐字比较
bar() { e2e_text 1 "$(W)" "$(H)" | python3 -c 'import sys; print("".join("@" if 0xE000 <= ord(c) <= 0xF8FF else c for c in sys.stdin.read().rstrip("\n")))'; }
bar_is()    { local got; got=$(bar); [[ $got == "$1" ]] || { echo "  bar: '$got'"; echo "  want '$1'"; false; }; }
bar_has()   { local got; got=$(bar); [[ $got == *"$1"* ]] || { echo "  bar lacks '$1': '$got'"; false; }; }
bar_lacks() { local got; got=$(bar); [[ $got != *"$1"* ]] || { echo "  bar has '$1': '$got'"; false; }; }
at() { local c; c=$(e2e_find "$1" "$(H)"); [[ -n $c ]] || { echo "  '$1' not on status bar"; return 1; }; style_has "$((${c%% *} + ${3:-0}))" "$(H)" "$2"; }  # TEXT STYLE [DX]
pending_is() { local c; c=$(e2e_find "C-p" "$(H)"); text_is $((c + 7)) $((c + 6 + $(strwidth "$1"))) "$(H)" "$1"; }   # " @ C-p " 之后是 " @ <序列> "
BAR=#292e42 FOCUS=#9ece6a BG=#1f2335 FG=#c0caf5 DIM=#565f89 WARN=#e0af68 INFO=#7dcfff MUTED=#a9b1d6 SEP=#2f3549

# ---- 160 宽 NORMAL：内容与顺序
start
check "160 宽状态栏" bar_is " @ doraemon ▾  0: data*  1: report $(printf '%79s') @ C-p  @ ·  1,1  @ pg@localhost:5432  NORMAL "
check "状态栏底色 #292e42" eval 'style_has 40 45 bg=$BAR && style_has 100 45 bg=$BAR'
check "session 块：focus 底、bg 字、粗体" eval 'at doraemon bg=$FOCUS && at doraemon fg=$BG && at doraemon bold && style_has 1 45 bg=$FOCUS'
check "当前 window：#3b4261 底、fg 字" eval 'at "0: data*" bg=#3b4261 && at "0: data*" fg=$FG && at "0: data*" bg=#3b4261 -1'
check "其余 window：dim 字、无底色" eval 'at "1: report" fg=$DIM && at "1: report" bg=$BAR'
check "C-p 入口：info 色" eval 'at "C-p" fg=$INFO && at "C-p" fg=$INFO -2'
check "待输入序列空闲时为 dim 色的 ·" eval 'pending_is "·" && at "·" fg=$DIM'
check "光标位置 1,1：fg_muted" at "1,1" fg=$MUTED
check "连接地址：info 色、sep 底（含图标与内边距）" eval 'at "pg@localhost:5432" fg=$INFO && at "pg@localhost:5432" bg=$SEP && at "pg@localhost:5432" bg=$SEP -3'
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

# ---- COMMAND 模式（§7.8 命令行）
e2e_type ':'; sleep 0.2
check "按 : 进入 COMMAND：模式块 info 底、bg 字、粗体" eval 'at COMMAND bg=$INFO && at COMMAND fg=$BG && at COMMAND bold'
check "命令行替换 session 块与 window 列表" eval 'bar_lacks doraemon && bar_lacks "0: data" && text_is 1 2 45 " :"'
check "160 宽：右侧显示匹配的命令 :q | :qa（dim）" eval 'bar_has ":q | :qa  @ C-p" && at ":q | :qa" fg=$DIM'
check "命令行至少占一半：右侧各块从第 81 列之后开始" eval '(( $(e2e_find ":q | :qa" 45) > 80 ))'
e2e_type qa; sleep 0.2
check "输入 qa 后只剩 :qa" eval 'bar_has ":qa  @ C-p" && bar_lacks ":q |" && text_is 1 4 45 " :qa"'
e2e_keys Escape; sleep 0.2
check "esc 回到 NORMAL" eval 'at NORMAL bg=$FOCUS && bar_has doraemon'
long() { printf "%$1s" | tr ' ' "$2"; }
e2e_type ":$(long 85 a)"; sleep 0.2
check "输入 85 字：右侧各块都还在" eval 'bar_has "1,1  @ pg@localhost:5432  COMMAND"'
e2e_type "$(long 30 b)"; sleep 0.2
check "输入 115 字：命令行变宽，连接地址先让出" eval 'bar_has " :$(long 85 a)$(long 30 b)" && bar_lacks pg@localhost && bar_has "1,1  COMMAND"'
e2e_type "$(long 20 c)"; sleep 0.2
check "输入 135 字：光标位置也让出，C-p / 待输入 / 模式块还在" eval 'bar_has "$(long 20 c)" && bar_lacks "1,1" && bar_has "@ C-p  @ ·  COMMAND "'
e2e_keys Escape; sleep 0.2

# ---- 80 宽 COMMAND：先去匹配的命令，再去连接地址
start -x 80
e2e_type ':'; sleep 0.2
check "80 宽 COMMAND：不显示匹配的命令和连接地址，模式块可见" eval 'bar_lacks ":q |" && bar_lacks pg@ && bar_has "@ C-p  @ ·  1,1  COMMAND "'
check "80 宽：命令行至少 40 列" eval '(( $(e2e_find "C-p" 45) > 40 ))'
e2e_keys Escape; sleep 0.2

# ---- NORMAL 逐步变窄：连接地址 → 非当前 window → 光标位置 → 截短 session 名
start -x 81; check "81 宽：全部显示" bar_has "1: report  @ C-p  @ ·  1,1  @ pg@localhost:5432  NORMAL "
start -x 80; check "80 宽：先省略连接地址" eval 'bar_lacks pg@ && bar_has "1: report" && bar_has "1,1"'
start -x 57; check "57 宽：去掉非当前 window" eval 'bar_lacks "1: report" && bar_has "1,1"'
start -x 49; check "49 宽：光标位置还在" bar_has "1,1  NORMAL "
start -x 48; check "48 宽：去掉光标位置，session 名完整" eval 'bar_lacks "1,1" && bar_has " doraemon ▾"'
start -x 43; check "43 宽：截短 session 名" bar_has " doraem… ▾"
monotonic() {  # 省略顺序：有连接地址 ⇒ 有 1: report ⇒ 有 1,1 ⇒ session 名完整；且必留的块都在
  local b; b=$(bar)
  local c=0 r=0 p=0 s=0
  [[ $b == *pg@localhost* ]] && c=1; [[ $b == *"1: report"* ]] && r=1; [[ $b == *"1,1"* ]] && p=1; [[ $b == *" doraemon ▾"* ]] && s=1
  ((c <= r && r <= p && p <= s)) || { echo "  width $(W): order broken: '$b'"; return 1; }
  [[ $b == *"▾  0: data*"*"@ C-p  @ ·"*" NORMAL " ]] || { echo "  width $(W): a kept block is missing: '$b'"; return 1; }
}
ok=1; for w in 160 120 100 90 81 80 70 60 58 57 56 50 49 48 46 44 43 42 40 38; do start -x $w -y 12; monotonic || ok=0; done
check "160…38 宽：省略顺序正确，session 块 / 0: data* / C-p / 待输入 / 模式块始终都在" test $ok = 1

# ---- C-p 入口的键位文字取自 keymap
printf '[keys.global]\n"<C-p>" = ""\n"<C-k>" = "palette.open"\n' >| "$CFG/config.toml"; start -c "$CFG/config.toml"
check "改绑 palette.open 为 C-k：状态栏显示 C-k" eval 'bar_has "@ C-k  @ ·" && bar_lacks "C-p"'

e2e_done
