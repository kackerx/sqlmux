#!/usr/bin/env bash
# F20.6 间隙跟 pane_bg、border_focus、默认边框调亮（specs/m20-redis-ui/task.md F20.6；tech-design §7.3 token、§7.8「整体布局」）
# 主题 token gap（缺省 = pane_bg）画 pane 之间、侧栏和 pane 之间的空白列；border_focus（缺省 = focus）画聚焦的 pane 和浮层的边框以及坐在边框线上的标题文字，
# zoom 照旧盖过它；NORMAL 块、执行按钮仍是 focus；tokyonight-storm 的 border 是 #565f89。只读，用共用库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
# conf THEME-TOML: a fresh config dir with theme "x" holding TOML ("" = no theme file, the built-in)
conf() { rm -rf "$D"/*; if [[ -n $1 ]]; then printf 'theme = "x"\n' >"$D/config.toml"; mkdir -p "$D/themes"; printf '%s\n' "$1" >"$D/themes/x.toml"; else : >"$D/config.toml"; fi; }
# gap columns: the one right of the sidebar and the ones between data panes, from the panes' geometry
gaps() { e2e_panes | awk '$1 != "-" && $2 + $4 < 160 { print $2 + $4 }' | sort -n | uniq | tr '\n' ' '; }
gap_is() { local c y; for c in $(gaps); do for y in 2 15 30 42; do [[ $(e2e_style "$c" "$y") == *"bg=$1"* ]] || { echo "  gap column $c row $y: $(e2e_style "$c" "$y"), want bg=$1"; return 1; }; done; done; [[ -n $(gaps) ]]; }
pane_x() { geom "$1" | awk '{ print $1 }'; }
pane_y() { geom "$1" | awk '{ print $2 }'; }

# ---- 缺省主题：间隙是 pane_bg（#24283b），不是 bg（#1f2335）；未聚焦边框是 #565f89
conf ""; start -C "$D"
check "缺省：侧栏和 ① 之间、① 和 ② 之间的间隙列底色是 pane_bg #24283b" gap_is '#24283b'
check "缺省：未聚焦的 ② 边框是 border #565f89；聚焦的 ① 边框是 focus #9ece6a" eval 'style_has $(pane_x 2) $(pane_y 2) fg=#565f89 && style_has $(pane_x 1) $(pane_y 1) fg=#9ece6a'

# ---- 主题里 bg 和 pane_bg 不同：间隙跟 pane_bg；写了 gap 用 gap；tab 栏仍是 bg
conf 'bg = "#101010"
pane_bg = "#202020"'
start -C "$D"
check "bg #101010 / pane_bg #202020：间隙列底色是 pane_bg #202020（不是更深的 bg）" gap_is '#202020'
conf 'bg = "#101010"
pane_bg = "#202020"
gap = "#303030"'
start -C "$D"
check "写了 gap = #303030：间隙列底色是 #303030，pane 内容区还是 pane_bg #202020" eval 'gap_is "#303030" && style_has 130 20 bg=#202020'

# ---- border_focus：聚焦 pane 的边框和边框线上的标题文字；浮层的边框；NORMAL 块还是 focus；zoom 盖过
conf 'border_focus = "#ff00ff"'
start -C "$D"
check "border_focus = #ff00ff：聚焦的 ① 的边框（左上角 ┌）是 #ff00ff，边框线上的标题文字（① 后面的字）也是" eval 'x=$(pane_x 1); y=$(pane_y 1); style_has $x $y fg=#ff00ff && style_has $((x + 1)) $y fg=#ff00ff && style_has $((x + 3)) $y fg=#ff00ff' || { x=$(pane_x 1); y=$(pane_y 1); e2e_style $x $y; e2e_style $((x + 3)) $y; }
check "未聚焦的 ② 边框仍是 border #565f89，不是 border_focus" eval 'style_has $(pane_x 2) $(pane_y 2) fg=#565f89'
check "NORMAL 块还是 focus 色 #9ece6a（border_focus 不管它）" eval 'x=$(e2e_find NORMAL $(( $(H) ))| head -1 | cut -d" " -f1); [[ -n $x ]] && style_has $x $(H) bg=#9ece6a' || { e2e_style 130 "$(H)"; e2e_plain | tail -1; }
e2e_keys C-p; sleep 0.5
check "命令面板（浮层）的边框是 border_focus #ff00ff" eval 'style_has $(left) $(top) fg=#ff00ff'
e2e_keys Escape; sleep 0.3
# 浮层：补全、which-key（照旧 border）、下拉、连接管理；每个浮层的左上角
pbox() { e2e_panes | awk '$1 == "-" { print $2, $3 }' | tail -1; }       # X Y of the last floating box
corner_is() { local b; b=($(pbox)); [[ -n ${b[0]} ]] || { echo "  no floating box"; return 1; }; style_has "${b[0]}" "${b[1]}" "fg=$1" || { echo "  corner at ${b[*]}: $(e2e_style "${b[0]}" "${b[1]}")"; false; }; }
key C-l; key i; e2e_type "sel"; sleep 1
check "补全浮层的边框是 border_focus #ff00ff" corner_is '#ff00ff'
key Escape; key Escape; sleep 0.3
e2e_keys Space; sleep 1
check "which-key 浮层照旧是 border #565f89（不跟 border_focus）" corner_is '#565f89'
e2e_keys Escape; sleep 0.3
pal ">theme"; sleep 0.4; key Enter; sleep 0.6
check "主题下拉（theme.switch）的边框是 border_focus #ff00ff" corner_is '#ff00ff'
key Escape; key Escape; sleep 0.3
pal ">conn"; sleep 0.4; e2e_type ".manage"; sleep 0.4; key Enter; sleep 0.8
check "连接管理浮层的边框是 border_focus #ff00ff" corner_is '#ff00ff'
key Escape; key Escape; sleep 0.3
e2e_keys Space; e2e_type z; sleep 0.5
check "缩放（zoom）：边框是 zoom 色（warn #e0af68），盖过 border_focus" eval 'f=$(focused); style_has $(pane_x $f) $(pane_y $f) fg=#e0af68' || { f=$(focused); e2e_style $(pane_x $f) $(pane_y $f); }
e2e_keys Space; e2e_type z; sleep 0.3

# ---- 没写 border_focus：同 focus；focus 改了，聚焦边框跟着改
conf 'focus = "#ff8800"'
start -C "$D"
check "只写 focus = #ff8800：聚焦 ① 的边框是 #ff8800（border_focus 缺省 = focus），NORMAL 块也是" eval 'style_has $(pane_x 1) $(pane_y 1) fg=#ff8800 && x=$(e2e_find NORMAL $(H) | head -1 | cut -d" " -f1) && style_has $x $(H) bg=#ff8800'
conf 'focus = "#ff8800"
border_focus = "#00aaff"'
start -C "$D"
check "focus = #ff8800、border_focus = #00aaff：边框是 #00aaff，NORMAL 块是 #ff8800（两个分开配）" eval 'style_has $(pane_x 1) $(pane_y 1) fg=#00aaff && x=$(e2e_find NORMAL $(H) | head -1 | cut -d" " -f1) && style_has $x $(H) bg=#ff8800'

e2e_done
