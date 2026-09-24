#!/usr/bin/env bash
# F0.4 Action 注册表与命令行（specs/m0-skeleton/task.md F0.4；tech-design §6.1、§6.8 C-c、§7.8 toast）
# 「所有键位都经由 Action」由单测 TestKeysRunActions 覆盖。
# F0.13 起状态栏命令行被命令面板取代：: 打开面板的命令范围，:q↵ / :qa↵ 不变；面板本身见 f0.13.sh。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

CFG=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$CFG"' EXIT
start_with() { printf "$1" >| "$CFG/config.toml"; start -c "$CFG/config.toml"; }
cmd() { e2e_type ":$1"; e2e_keys Enter; sleep 0.3; }
quit_clean() {  # 退出码 0，终端标志位按 F0.1 复原
  wait_for 3 screen_has '[e2e-exit 0]' && flag_is alternate_on 0 && flag_is mouse_all_flag 0 &&
    flag_is mouse_sgr_flag 0 && flag_is bracket_paste_flag 0 && flag_is cursor_flag 1
}
empty_body() { local y; for y in $(seq 2 42); do [[ -z $(e2e_text 35 159 $y | tr -d " ") ]] || { echo "  row $y: $(e2e_text 35 159 $y)"; return 1; }; done; }
NF_DATA=$(printf '\xef\x87\x80')     # U+F1C0（F0.12 起）
EMPTY_TABS="│ +$(printf '%123s')│"      # 34..160 列：只有 +

# ---- :q 关 tab → 关 pane → 空 pane（① 开 t_user、t_order 两个 tab；右边分出 ② 开 t_sku）
start; two_tabs
cmd q
check ":q 关掉当前 tab，回到上一个 tab（t_user）" eval 'text_has 34 160 1 "① $NF_DATA t_user" && text_has 34 160 43 "1:t_user*" && ! text_has 34 160 43 t_order >/dev/null'
e2e_keys Space %; sleep 0.3; e2e_keys C-p; sleep 0.3; e2e_type "@t_sku"; sleep 0.3; e2e_keys Enter; sleep 0.3; e2e_keys C-h; sleep 0.3
cmd q
check "关掉 ① 的最后一个 tab：pane 关闭，② 占满并成为 ⟨1⟩" eval '[[ $(e2e_find ┌ 1) == "1 34" && $(e2e_find ┐ 1) == "32 160" ]] && text_has 34 160 1 "① $NF_DATA t_sku"'
check "剩下的 pane 获得焦点（focus 色边框）" style_has 34 1 fg=#9ece6a
cmd q
check "唯一的 pane 关掉最后一个 tab：保留为空 pane，标题只有 ① <图标>（F0.16）" text_is 34 41 1 "┌─ ① $NF_DATA ─"
check "空 pane：内容区为空" empty_body
check "空 pane：tab 栏只有 +，前面没有 │，右侧没有提示" text_is 34 160 43 "$EMPTY_TABS"
check "空 pane：标题栏右侧没有任何提示（§7.8 空 pane）" eval '[[ $(e2e_text 34 160 1) == "┌─ ① $NF_DATA "*"─┐" && $(e2e_text 41 160 1) =~ ^─+┐$ ]] || { echo "  title: $(e2e_text 34 160 1)"; false; }'
check "空 pane：程序不退出" running
cmd q
check "空 pane 上再 :q：不退出、画面不变" eval 'running && text_is 34 160 43 "$EMPTY_TABS"'

# ---- : 打开命令面板（F0.13），esc 关闭；:qa↵ 退出
start
e2e_type ':'; sleep 0.3
check ": 打开命令面板" palette_open
e2e_keys Escape; sleep 0.3
check "esc 关闭面板，回到 NORMAL" eval '! palette_open && [[ $(e2e_text 150 160 45) == *" NORMAL " ]]'
cmd qa
check ":qa↵ 退出，终端复原" quit_clean

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
# 「被别的 toast 顶掉」「其他 toast 显示 3 秒」：F0.13 去掉了「未知命令」toast，M0 里已没有别的 toast，这两项暂不测

# ---- 面板里的 C-c 等同 esc，不计入连按
start
e2e_type ':ab'; sleep 0.2; e2e_keys C-c; sleep 0.3
check "面板里 C-c：只关闭面板，不弹提示" eval '! palette_open && ! screen_has "再按一次" && running'
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
start_with '[keys.normal]\ng = "palette.open"\n'
e2e_type g; sleep 0.4
check "g 是 gt 的前缀：0.4 秒时还没执行" eval '! palette_open'
sleep 1
check "约 1 秒后执行 g（打开命令面板）" palette_open
e2e_keys Escape; sleep 0.2
e2e_type g; sleep 0.2; e2e_type t; sleep 1.3
check "1 秒内按成 gt：不执行 g" eval '! palette_open'

e2e_done
