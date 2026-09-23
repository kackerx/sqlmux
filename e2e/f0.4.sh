#!/usr/bin/env bash
# F0.4 Action 注册表与命令行（specs/m0-skeleton/task.md F0.4；tech-design §6.1、§6.8 C-c、§7.8 命令行 / toast）
# 「所有键位都经由 Action」由单测 TestKeysRunActions 覆盖。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

CFG=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$CFG"' EXIT
start() { e2e_start "$@" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3; }
start_with() { printf "$1" >| "$CFG/config.toml"; start -c "$CFG/config.toml"; }
exited()  { screen_has '[e2e-exit'; }
running() { flag_is alternate_on 1 && ! exited; }
cmd() { e2e_type ":$1"; e2e_keys Enter; sleep 0.3; }
# 命令行占状态栏左侧（F0.5 起前面有 1 列内边距、右侧还有别的块）；'' 表示命令行没打开
cmdline() { local row; row=$(e2e_text 1 "$(e2e_flag pane_width)" "$(e2e_flag pane_height)"); [[ $row == *" COMMAND " ]] || return 0; row=${row#" "}; echo "${row%%"  "*}"; }
cmdline_is() { local got; got=$(cmdline); [[ $got == "$1" ]] || { echo "  cmdline: '$got', want '$1'"; false; }; }
quit_clean() {  # 退出码 0，终端标志位按 F0.1 复原
  wait_for 3 screen_has '[e2e-exit 0]' && flag_is alternate_on 0 && flag_is mouse_all_flag 0 &&
    flag_is mouse_sgr_flag 0 && flag_is bracket_paste_flag 0 && flag_is cursor_flag 1
}
empty_body() { local y; for y in $(seq 2 42); do [[ -z $(e2e_text 35 159 $y | tr -d " ") ]] || { echo "  row $y: $(e2e_text 35 159 $y)"; return 1; }; done; }
NF_CONSOLE=$(printf '\xef\x92\x89')     # U+F489（F0.12 起）
EMPTY_TABS="│ +$(printf '%123s')│"      # 34..160 列：只有 +
codepoints() { python3 -c 'import sys; print(" ".join("%x" % ord(c) for c in sys.argv[1]))' "$1"; }

# ---- :q 关 tab → 关 pane → 空 pane（默认布局：data 两个 tab，console 一个）
start
cmd q
check ":q 关掉当前 tab，回到上一个 tab（t_user）" eval 'text_has 34 103 1 "data · t_user" && text_has 34 103 43 "1:t_user*" && ! text_has 34 103 43 t_order >/dev/null'
cmd q
check "关掉 data 的最后一个 tab：pane 关闭，console 占满并成为 ⟨1⟩" eval '[[ $(e2e_find ┌ 1) == "1 34" && $(e2e_find ┐ 1) == "32 160" ]] && text_has 34 160 1 "⟨1⟩" && text_has 34 160 1 "console · console_1"'
check "console 获得焦点（focus 色边框）" style_has 34 1 fg=#9ece6a
cmd q
check "唯一的 pane 关掉最后一个 tab：保留为空 pane，标题只有 ⟨1⟩ <图标> console" text_is 34 51 1 "┌─ ⟨1⟩ $NF_CONSOLE console ─"
check "空 pane：内容区为空" empty_body
check "空 pane：tab 栏只有 +，前面没有 │，右侧没有提示" text_is 34 160 43 "$EMPTY_TABS"
check "空 pane：标题栏右侧没有任何提示（§7.8 空 pane）" eval '[[ $(e2e_text 34 160 1) == "┌─ ⟨1⟩ $NF_CONSOLE console "*"─┐" && $(e2e_text 52 160 1) =~ ^─+┐$ ]] || { echo "  title: $(e2e_text 34 160 1)"; false; }'
check "空 pane：程序不退出" running
cmd q
check "空 pane 上再 :q：不退出、画面不变" eval 'running && text_is 34 160 43 "$EMPTY_TABS"'

# ---- 命令行（§7.8：占用状态栏左侧）
start
e2e_type ':ab'; sleep 0.2
check "命令行显示在状态栏左侧" cmdline_is ':ab'
e2e_keys Escape; sleep 0.2
check "esc 关闭命令行" cmdline_is ''
e2e_type ':ab'; e2e_keys BSpace BSpace; sleep 0.2
check "退格删到只剩 : 时命令行仍打开" cmdline_is ':'
e2e_keys BSpace; sleep 0.2
check "再退格（删空）关闭命令行" cmdline_is ''
cmd foo
check "未知命令：toast「未知命令: foo」" toast_is '未知命令: foo'
check "未知命令后命令行关闭、程序不退出" eval 'cmdline_is "" && running'
cmd qa
check ":qa 退出，终端复原" quit_clean

# ---- 字素簇整体输入、整体删除
start
E_ACUTE=$(printf 'e\xcc\x81') THUMB=$(printf '\xf0\x9f\x91\x8d\xf0\x9f\x8f\xbd')   # e+U+0301、👍🏽（bash 3.2 没有 \u）
e2e_type ":x${E_ACUTE}${THUMB}中"; sleep 0.3
check "命令行完整显示 é、👍🏽、中" eval '[[ $(codepoints "$(cmdline)") == "3a 78 65 301 1f44d 1f3fd 4e2d" ]]'
e2e_keys Enter; sleep 0.3
check "未知命令 toast 里的字素完整" toast_is "未知命令: x${E_ACUTE}${THUMB}中"
e2e_type ":x${E_ACUTE}"; e2e_keys BSpace; sleep 0.2
check "退格删掉整个 é（e+U+0301），不留下 e" cmdline_is ':x'
e2e_keys Escape; sleep 0.2; e2e_type ":x${THUMB}"; e2e_keys BSpace; sleep 0.2
check "退格删掉整个 👍🏽，不留下 👍" cmdline_is ':x'
e2e_keys Escape; sleep 0.2; e2e_type ":x$(printf '\xf0\x9f\x87\xa8\xf0\x9f\x87\xb3')"; e2e_keys BSpace; sleep 0.2
check "退格删掉整个国旗 🇨🇳（两个区域指示符）" cmdline_is ':x'
e2e_keys Escape; sleep 0.2

# ---- 连按两次 C-c 退出（§6.8）
start
e2e_keys C-c; sleep 0.3
check "C-c 一次：toast「再按一次 C-c 退出」，不退出" eval 'toast_is "再按一次 C-c 退出" && running'
e2e_keys C-c
check "2 秒内再按：退出，终端复原" quit_clean
start
e2e_keys C-c; sleep 2.3; e2e_keys C-c; sleep 0.3
check "两次间隔超过 2 秒：不退出，第二次重新算第一次" eval 'running && toast_is "再按一次 C-c 退出"'
e2e_keys C-c
check "紧接着再按：退出" quit_clean

# ---- 「再按一次 C-c 退出」只显示 2 秒；窗口就是它在屏幕上的这段时间（§6.8）
start
e2e_keys C-c; sleep 1.3
check "1.3 秒时提示还在" screen_has "再按一次 C-c 退出"
e2e_keys C-c
check "提示还在时再按：退出" quit_clean
start
e2e_keys C-c; sleep 2.2
check "2.2 秒时提示已消失（只显示 2 秒）" eval '! screen_has "再按一次"'
e2e_keys C-c; sleep 0.3
check "消失后再按：不退出，提示重新出现" eval 'running && toast_is "再按一次 C-c 退出"'
e2e_keys Escape; sleep 2.2
start
e2e_keys C-c; sleep 0.3; cmd foo
check "被「未知命令」toast 顶掉" eval 'toast_is "未知命令: foo" && ! screen_has "再按一次"'
e2e_keys C-c; sleep 0.3
check "顶掉后再按 C-c：算第一次，不退出" eval 'running && toast_is "再按一次 C-c 退出"'
start
cmd foo; sleep 2.4
check "其他 toast 仍显示 3 秒：2.7 秒时还在" screen_has "未知命令: foo"
sleep 0.8
check "约 3 秒后消失" eval '! screen_has "未知命令: foo"'

# ---- 命令行里的 C-c 等同 esc，不计入连按
start
e2e_type ':ab'; sleep 0.2; e2e_keys C-c; sleep 0.3
check "命令行里 C-c：只关闭命令行，不弹提示" eval 'cmdline_is "" && ! screen_has "再按一次" && running'
e2e_keys C-c; sleep 0.3
check "随后的 C-c 算第一次：不退出，出现提示" eval 'running && toast_is "再按一次 C-c 退出"'
e2e_keys C-c
check "再按一次退出" quit_clean

# ---- toast 里的键位文字取自 keymap
start_with '[keys.global]\n"<C-c>" = ""\n"<C-q>" = "cancel"\n'
e2e_keys C-c; sleep 0.3
check "C-c 解绑后按 C-c 无反应" eval 'running && ! screen_has "再按一次"'
e2e_keys C-q; sleep 0.3
check "改绑 C-q：toast「再按一次 C-q 退出」" toast_is '再按一次 C-q 退出'
e2e_keys C-q
check "连按两次 C-q 退出" quit_clean

# ---- timeoutlen（§6.5）：歧义节点等 timeoutlen 后执行
start_with '[keys.normal]\ng = "cmdline.open"\n'
e2e_type g; sleep 0.4
check "g 是 gt 的前缀：0.4 秒时还没执行" cmdline_is ''
sleep 1
check "约 1 秒后执行 g（打开命令行）" cmdline_is ':'
e2e_keys Escape; sleep 0.2
e2e_type g; sleep 0.2; e2e_type t; sleep 1.3
check "1 秒内按成 gt：不执行 g" cmdline_is ''

e2e_done
