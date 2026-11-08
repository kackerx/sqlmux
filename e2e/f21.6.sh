#!/usr/bin/env bash
# F21.6 越过 sqlmux 的边界切到外层 tmux 的 pane（specs/m21-cli-outer/task.md F21.6；tech-design §6「pane.focus 越界」）
# sqlmux 开在 tmux 窗口右边的 pane，左边是 shell：[pane] outer_nav = "auto"，sqlmux 里焦点在侧栏按 C-h，焦点交给左边的 pane
# （tmux display -p '#{pane_active}'）；sqlmux 里还有相邻 pane 时 C-h 在里面切；off（缺省）时不动。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
mkdir -p "$D/c"
(umask 077; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$SQLMUX_TEST_PG" >"$D/c/connections.toml")
# sqlmux in the right pane of the window, a shell in the left one; PS is sqlmux's pane, PL the shell's
setup() {
  e2e_start -x 200 -C "$D/c" "$E2E_BIN"; wait_for 6 flag_is alternate_on 1; wait_for 15 settled; sleep 1
  PS=$(t display -p -t t '#{pane_id}')
  PL=$(t split-window -hb -P -F '#{pane_id}' -t "$PS"); sleep 0.5
  t select-pane -t "$PS"; sleep 0.5
}
active() { t display -p -t "$1" '#{pane_active}'; }
press() { t send-keys -t "$PS" "$1"; sleep 0.5; }

printf '[pane]\nouter_nav = "auto"\n' >"$D/c/config.toml"
setup
check "（准备）sqlmux 在右边的 pane，它是当前 pane，左边是 shell" eval '[[ $(active $PS) == 1 && $(active $PL) == 0 ]]'
press C-h
check "auto：焦点在 ① 按 C-h，左边还有侧栏：在 sqlmux 里切到侧栏，tmux 的当前 pane 还是 sqlmux 的" eval '[[ $(active $PS) == 1 && $(active $PL) == 0 ]]'
press C-h
check "auto：焦点已在最左的侧栏再按 C-h：交给外层 tmux，左边的 pane 成了当前 pane" eval '[[ $(active $PL) == 1 && $(active $PS) == 0 ]] || { echo "  sqlmux pane $(active $PS), shell pane $(active $PL)"; false; }'
t select-pane -t "$PS"; sleep 0.3
press C-l; press C-l
check "auto：往右按 C-l 两次（侧栏 → ① → ②），sqlmux 里右边还有 pane：在里面切，不出去" eval '[[ $(active $PS) == 1 && $(active $PL) == 0 ]]'
press C-l
check "auto：焦点在最右的 ② 再按 C-l：交给外层 tmux，它右边没有 pane，tmux 照 select-pane -R 绕到最左的 pane（vim-tmux-navigator 也是这样）" eval '[[ $(active $PL) == 1 && $(active $PS) == 0 ]]'

printf '' >"$D/c/config.toml"
setup
press C-h; press C-h; press C-h
check "off（缺省）：在边上按几下 C-h 也不出去，sqlmux 的 pane 还是当前 pane" eval '[[ $(active $PS) == 1 && $(active $PL) == 0 ]]'
e2e_done
