#!/usr/bin/env bash
# F21.5 聚焦和未聚焦的边框用不同线型 border_focus_style（specs/m21-cli-outer/task.md F21.5；tech-design §7.3、§14）
# border = "rounded" + border_focus_style = "bold"：聚焦的 pane 是粗直角（┏ ┓ ┗ ┛），其余是圆角细线（╭ ╮ ╰ ╯）；C-l 切焦点后两个 pane 的角对应换掉；
# 命令面板用聚焦的线型；两个都没写时所有框是 ┌ ┐ └ ┘。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
declare -a G
mkdir -p "$D/c"
(umask 077; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$SQLMUX_TEST_PG" >"$D/c/connections.toml")
run() { e2e_start -C "$D/c" "$E2E_BIN"; wait_for 6 flag_is alternate_on 1; wait_for 15 settled; sleep 1; }
# the panes' boxes are found by their single-line corners (e2e_panes): the geometry is taken from a start with the default border, which no border changes
cell() { e2e_text "$1" "$1" "$2"; }
tl() { local g=(${G[$1]}); cell "${g[0]}" "${g[1]}"; }
br() { local g=(${G[$1]}); cell $((g[0] + g[2] - 1)) $((g[1] + g[3] - 1)); }

: >"$D/c/config.toml"
run
G[1]=$(geom 1); G[2]=$(geom 2)
check "两个都没写：┌ 单线，没有圆角也没有粗线" eval '[[ $(tl 1) == "┌" && $(tl 2) == "┌" ]] && ! e2e_plain | grep -q "[╭┏]"'

printf 'border = "rounded"\nborder_focus_style = "bold"\n' >"$D/c/config.toml"
run
f=1; o=2; [[ $(tl 2) == "┏" ]] && f=2 o=1
check "rounded + bold：聚焦的 pane（⟨$f⟩）四角是粗直角，另一个（⟨$o⟩）是圆角" eval '[[ $(tl $f) == "┏" && $(br $f) == "┛" && $(tl $o) == "╭" && $(br $o) == "╯" ]] || { echo "  focused $(tl $f) $(br $f), other $(tl $o) $(br $o)"; false; }'

[[ $f == 1 ]] && key C-l || key C-h
check "切焦点（C-l / C-h）：聚焦的 pane 变了，新聚焦的 ┏ … ┛，原来聚焦的变成 ╭ … ╯" eval '[[ $(tl $o) == "┏" && $(br $o) == "┛" && $(tl $f) == "╭" && $(br $f) == "╯" ]] || { echo "  now: ⟨$f⟩ $(tl $f) $(br $f), ⟨$o⟩ $(tl $o) $(br $o)"; false; }'
key C-p; sleep 0.4
check "命令面板用聚焦的线型（┏━ 命令面板）" eval 'e2e_plain | grep -q "┏━ .*命令面板\|┏━ .*Command" || { e2e_plain | head -12 | cut -c1-120; false; }'
key Escape

printf 'border = "rounded"\n' >"$D/c/config.toml"
run
check "没写 border_focus_style：聚焦的 pane 也是圆角（跟 border）" eval '[[ $(tl 1) == "╭" && $(tl 2) == "╭" ]] && ! e2e_plain | grep -q "┏"'
printf 'border_focus_style = "wavy"\n' >"$D/c/config.toml"
e2e_start -C "$D/c" "$E2E_BIN"; wait_for 6 screen_has '[e2e-exit 1]'
check "border_focus_style 写错：启动报错退出，错误里有键名" eval 'e2e_plain | grep -q "border_focus_style"'
e2e_done
