#!/usr/bin/env bash
# F20.7 内置第二个主题 ristretto（specs/m20-redis-ui/task.md F20.7；tech-design §7.3）
# theme = "ristretto" 没有用户文件时用内置的（取 specs/m20-redis-ui/ristretto.toml 的颜色）；用户目录里的 themes/ristretto.toml 优先（它没写的 token 来自 tokyonight-storm，不是内置的 ristretto）；
# 主题下拉（theme.switch）里两个都列，切到 ristretto 立刻生效。只读，用共用库。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
conf() { rm -rf "$D"/*; printf '%s\n' "$1" >"$D/config.toml"; [[ -n $2 ]] && { mkdir -p "$D/themes"; printf '%s\n' "$2" >"$D/themes/ristretto.toml"; }; true; }
pane_x() { geom "$1" | awk '{ print $1 }'; }
pane_y() { geom "$1" | awk '{ print $2 }'; }
gap_x() { geom 0 | awk '{ print $1 + $3 }'; }          # the column right of the sidebar

# ---- theme = "ristretto"，没有用户文件：内置的（pane_bg #393333、border #7a7272、focus #a9dc76、gap 跟 pane_bg）
conf 'theme = "ristretto"'; start -C "$D"
check "内置 ristretto 起得来（没有报错退出）、pane 内容区是 pane_bg #393333" eval 'running && style_has 130 20 bg=#393333'
check "间隙列（侧栏右边）底色是 pane_bg #393333（gap 缺省跟 pane_bg，不是 bg #312b2b）" eval 'style_has $(gap_x) 20 bg=#393333'
check "未聚焦的 ② 边框是 border #7a7272，聚焦的 ① 边框是 focus #a9dc76" eval 'style_has $(pane_x 2) $(pane_y 2) fg=#7a7272 && style_has $(pane_x 1) $(pane_y 1) fg=#a9dc76'
check "没有「找不到主题」之类的 toast" eval '! screen_has "ristretto.toml" && ! screen_has "找不到"'

# ---- 用户目录里有同名文件：用户的优先，没写的 token 来自 tokyonight-storm（不是内置 ristretto）
conf 'theme = "ristretto"' 'pane_bg = "#101010"'; start -C "$D"
check "用户的 themes/ristretto.toml 优先：pane_bg 是用户写的 #101010" style_has 130 20 bg=#101010
check "用户文件没写的 token 沿用 tokyonight-storm：未聚焦边框 #565f89（不是内置 ristretto 的 #7a7272）、聚焦 #9ece6a（不是 #a9dc76）" eval 'style_has $(pane_x 2) $(pane_y 2) fg=#565f89 && style_has $(pane_x 1) $(pane_y 1) fg=#9ece6a'

# ---- icons：ristretto 的 [icon] 里 query 是 \uf489 + #78dce8；icons = "ascii" 时含非 ASCII 的 text 不生效，用 ascii 那套的 >，fg 照用
NF=$(printf '\xef\x92\x89')   # U+F489
conf 'theme = "ristretto"'; start -C "$D"
check "ristretto + nerd：② 标题的 query 图标是 ristretto 的 \uf489，颜色 #78dce8" eval '[[ $(e2e_text 103 103 1) == "$NF" ]] && style_has 103 1 fg=#78dce8'
conf 'theme = "ristretto"
icons = "ascii"'; start -C "$D"
check "ristretto + icons = ascii：query 图标是 ascii 那套的 >（\uf489 不生效），颜色还是 ristretto 的 #78dce8" eval '[[ $(e2e_text 105 105 1) == ">" ]] && style_has 105 1 fg=#78dce8' || { e2e_text 100 110 1; e2e_style 105 1; }

# ---- 主题下拉里两个都列；切换立刻生效
conf '' ; start -C "$D"
check "（准备）缺省主题：pane_bg #24283b" style_has 130 20 bg=#24283b
pal ">theme"; sleep 0.4; key Enter; sleep 0.6
check "主题下拉里两个内置主题都列：tokyonight-storm 和 ristretto" eval 'screen_has tokyonight-storm && screen_has ristretto' || e2e_plain | tail -8 | cut -c1-120
e2e_type ristretto; sleep 0.4; key Enter; sleep 1
check "切到 ristretto：马上生效，pane_bg #393333、聚焦边框 #a9dc76" eval 'style_has 130 20 bg=#393333 && style_has $(pane_x 1) $(pane_y 1) fg=#a9dc76'

e2e_done
