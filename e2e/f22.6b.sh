#!/usr/bin/env bash
# F22.6 固定、关闭一批的边角（specs/m22-menus-keys/task.md F22.6 a–c；tech-design §7.8「tab 栏」）
# f22.6.sh 测了右键固定、关闭其他、拆分、重启；这里补：SPC wP 用键固定、名字后面是 pin 图标（U+F435，结果 tab 的那个）；{N}gt 按显示顺序；
# 关闭右侧时有未保存修改的 tab 不关、toast 说几个；关闭左侧留下固定的；关掉的 tab.reopen 能重开；只有一个 pane 时「移到下一个 pane」dim，
# 引导页的 tab 菜单只有 关闭 / 向右拆分 / 向下拆分；两个 pane 时把固定的 tab 移过去仍固定；固定的 tab 用 x 能关。SQLite 文件。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/cellops.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
DB=$D/shop.db; sqlite3 "$DB" <"$E2E_ROOT/testdata/seed/sqlite.sql"
mkdir -p "$D/c"; (umask 077; printf '[[connection]]\nname = "shop"\nengine = "sqlite"\ndsn = "%s"\n' "$DB" >"$D/c/connections.toml")
printf '[keys.global]\n"<F4>" = "tab.reopen"\n' >"$D/k.toml"
PIN=$(printf '\xef\x90\xb5')   # nf-oct-pin, the result tab's pin (ui/icons.go)
tabraw() { local g; g=($(geom "${1:-1}")); e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $((g[1] + g[3] - 2)); }
tabbar() { tabraw "$1" | noicon | sed 's/  */ /g'; }
tabxy() { local g y; g=($(geom "${2:-1}")); y=$((g[1] + g[3] - 2)); echo "$(e2e_find "$1" "$y" | tr ' ' '\n' | awk -v a="${g[0]}" -v b="$((g[0] + g[2]))" '$1 > a && $1 < b { print; exit }') $y"; }   # within pane N
rmenu() { local x y; read -r x y <<<"$(tabxy "$1" "${2:-1}")"; rclick "$x" "$y"; sleep 0.6; }
title() { local g; g=($(geom "${1:-1}")); e2e_text $((g[0])) $((g[0] + g[2] - 1)) "${g[1]}" | noicon; }
toast_has() { e2e_cap | grep -qF -- "$1"; }
box() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }
mitems() { local g y; g=($(box)); [[ -n ${g[0]} ]] || return 0; for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/^ *//; s/^[^ ]* //; s/ *$//; s/   */ /g'; done | tr '\n' '|'; }
fg_of() { local xy; xy=$(find_xy "$1"); e2e_style "${xy% *}" "${xy#* }" | sed 's/ bg=.*//'; }

e2e_start -C "$D/c" -K "$D/k.toml" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 1; solo
for t in t_user t_order t_sku t_log t_kv; do open_table "$t"; wait_for 8 settled; done
open_table t_log; wait_for 8 settled; key l; key Enter; e2e_type X; sleep 0.2; key Enter; sleep 0.4
check "（准备）五个 tab，t_log 里改了一格没保存" eval '[[ $(tabbar) == *"1:t_user"*"2:t_order"*"3:t_sku"*"4:t_log"*"5:t_kv"* ]] && e2e_plain | grep -q "X"' || tabbar

# ---- SPC wP、pin 图标、{N}gt
open_table t_kv; wait_for 8 settled
key Space; key w; key P; sleep 0.6
check "t_kv 上 SPC wP：到最前（1:t_kv），名字后面是 pin 图标 U+F435，别的 tab 没有" eval '[[ $(tabbar) == *"1:t_kv"*"2:t_user"*"3:t_order"*"4:t_sku"*"5:t_log"* && $(tabraw | grep -o "$PIN" | wc -l) == 1 && $(tabraw) == *"t_kv $PIN"* ]]' || tabraw
key 4; key g; key t; sleep 0.6
check "4gt：按显示顺序到第 4 个（t_sku），不是原来的第 4 个 t_log" eval '[[ $(title) == *t_sku* ]]' || title

# ---- 关闭右侧：有未保存修改的不关
rmenu t_sku
check "表 tab 的菜单：第一项「关闭」右侧画它的键 x（表格里 x = tab.close）" eval '[[ $(mitems) == "关闭 x|"* ]] || { echo "  [$(mitems)]"; false; }'
menu_click "关闭右侧"; sleep 0.5
check "右键 t_sku → 关闭右侧：t_log 有未保存的修改没关，toast「1 个 tab 没关：有未保存的修改、未提交的事务或正在执行」" eval '[[ $(tabbar) == *"1:t_kv"*"2:t_user"*"3:t_order"*"4:t_sku"*"5:t_log"* ]] && toast_has "1 个 tab 没关：有未保存的修改、未提交的事务或正在执行"' || tabbar
rmenu t_sku; menu_click "关闭左侧"; sleep 0.5
check "右键 t_sku → 关闭左侧：t_user、t_order 关了，固定的 t_kv 留着" eval '[[ $(tabbar) == *"1:t_kv"*"2:t_sku"*"3:t_log"* && $(tabbar) != *t_user* && $(tabbar) != *t_order* ]]' || tabbar
key F4; sleep 0.8; wait_for 8 settled
check "tab.reopen：重开一个刚关掉的（t_user 或 t_order）" eval '[[ $(tabbar) == *t_order* || $(tabbar) == *t_user* ]]' || tabbar

# ---- 移到下一个 pane
rmenu t_kv
check "只有一个 pane：「移到下一个 pane」是 dim（和「固定」字色不同）" eval '[[ $(fg_of "移到下一个 pane") != "$(fg_of 取消固定)" ]]' || mitems
key Escape; sleep 0.3
key Space; key '%'; sleep 0.8
read -r x y <<<"$(tabxy + 2)"; e2e_click $((x + 1)) "$y"; sleep 0.6   # ② 是空的（没有 tab），点它的 + 开一个引导页 tab
check "（准备）SPC % 拆出 ②，点 + 开了引导页的 tab（1:）" eval '[[ -n $(geom 2) && $(tabbar 2) == *"1:"* && $(tabbar 2) != *t_* ]]' || tabbar 2
read -r x y <<<"$(tabxy "1:" 2)"; rclick $((x + 1)) "$y"; sleep 0.6
check "引导页的 tab 右键：只有 关闭 / 向右拆分 / 向下拆分" eval '[[ $(mitems) =~ ^关闭[^|]*\|向右拆分[^|]*\|向下拆分[^|]*\|$ ]] || { echo "  [$(mitems)]"; false; }'
check "引导页菜单的键（§7.6：带 Action 的项右侧画键）：关闭 x（引导页的 x = tab.close）、向右拆分 SPC %、向下拆分 SPC \"" eval '[[ $(mitems) == "关闭 x|向右拆分 SPC %|向下拆分 SPC \"|" ]] || { echo "  [$(mitems)]"; false; }'
key Escape; sleep 0.3
rmenu t_kv 1; menu_click "移到下一个 pane"; sleep 0.6
check "两个 pane：t_kv → 移到下一个 pane，在 ② 里、还带 pin 图标；① 里没有它" eval '[[ $(tabraw 2) == *"t_kv $PIN"* && $(tabbar 1) != *t_kv* ]]' || { tabraw 1; tabraw 2; }

# ---- 固定的 tab 用 x 能关
key x; sleep 0.6
check "焦点在固定的 t_kv 上按 x：关了（固定不挡 tab.close），哪个 pane 里都没有它" eval '! e2e_plain | grep -qE "[0-9]: ?[^ ]* ?t_kv"' || e2e_plain | grep -E "[0-9]: ?[^ ]* ?t_kv"

e2e_done
