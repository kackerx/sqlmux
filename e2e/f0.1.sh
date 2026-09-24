#!/usr/bin/env bash
# F0.1 工程初始化、启动与退出（specs/plan.md F0.1）
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

no_toast() { ! screen_has '再按一次 C-c 退出'; }
same_as() { [[ $(e2e_plain) == "$1" ]] || { diff <(echo "$1") <(e2e_plain); false; }; }
colored() { e2e_cap -e | grep -qF -- "$1"; }

e2e_start "$E2E_BIN"
check "进入全屏" wait_for 5 flag_is alternate_on 1
check "开启 all-motion 鼠标 + SGR" eval 'flag_is mouse_all_flag 1 && flag_is mouse_sgr_flag 1'
check "开启 bracketed paste" flag_is bracket_paste_flag 1
check "背景 bg #1f2335" colored '48;2;31;35;53m'
sleep 0.5; big=$(e2e_plain)

e2e_resize 100 30; sleep 0.5; small=$(e2e_plain)
check "100x30 不退出、不 panic" eval 'running && ! screen_has panic'
e2e_resize 160 45; sleep 0.5
check "调回 160x45 无残影（与调整前一致）" same_as "$big"

e2e_keys C-c
# F0.4 起空闲时连按两次 C-c 退出（§6.8）；这里只验证按一次不退出，其余见 f0.4.sh
check "C-c 一次：弹出「再按一次 C-c 退出」" wait_for 2 screen_has '再按一次 C-c 退出'
check "toast 在状态栏上一行右侧，warn 字、#292e42 底、1 列内边距（§7.8）" toast_is '再按一次 C-c 退出'
check "C-c 一次后程序仍在运行" running
check "toast 约 3 秒后消失" wait_for 5 no_toast

e2e_type ':qa'; e2e_keys Enter
check ":qa 退出，退出码 0" wait_for 3 screen_has '[e2e-exit 0]'
check "退出后离开 alt screen" flag_is alternate_on 0
check "退出后光标可见" flag_is cursor_flag 1
check "退出后关闭鼠标上报（all/sgr/any）" eval 'flag_is mouse_all_flag 0 && flag_is mouse_sgr_flag 0 && flag_is mouse_any_flag 0'
check "退出后关闭 bracketed paste" flag_is bracket_paste_flag 0
e2e_type 'echo alive'; e2e_keys Enter
check "shell 正常可用" wait_for 2 eval 'e2e_cap | grep -qx alive'

e2e_start -x 100 -y 30 "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.5
check "100x30 无残影（与同尺寸冷启动一致）" same_as "$small"

# 终端支持键盘增强时（tmux extended-keys on）：请求增强、C-c / :qa 照常、退出后复原
e2e_start -k "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3
check "开启键盘增强协议" eval '[[ $(e2e_flag pane_key_mode) != VT10x ]]'
e2e_keys C-c
check "增强模式下 C-c 仍弹 toast" wait_for 2 screen_has '再按一次 C-c 退出'
e2e_type ':qa'; e2e_keys Enter
check "增强模式下 :qa 退出" wait_for 3 screen_has '[e2e-exit 0]'
check "退出后键盘模式复原" flag_is pane_key_mode VT10x

e2e_done
