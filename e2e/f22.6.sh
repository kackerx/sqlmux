#!/usr/bin/env bash
# F22.6 tab 的右键菜单、固定、关闭一批（specs/m22-menus-keys/task.md F22.6；tech-design §7.8「tab 栏」、§14 pinned）
# 开五张表：右键第三个 →「固定」：它到最前并带标记；右键 →「关闭其他」只剩固定的和当前的；右键 →「向右拆分」多一个 pane、这个 tab 在新 pane 里；
# :qa 重启后固定还在（last.toml）；SPC w 的 which-key 里有固定和关闭其他。SQLite 文件，只读。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
DB=$D/shop.db; sqlite3 "$DB" <"$E2E_ROOT/testdata/seed/sqlite.sql"
mkdir -p "$D/c"; (umask 077; printf '[[connection]]\nname = "shop"\nengine = "sqlite"\ndsn = "%s"\n' "$DB" >"$D/c/connections.toml")
rclick() { _sgr 2 "$1" "$2" M; _sgr 2 "$1" "$2" m; sleep 0.6; }
# pane N's tab bar, with the icons off, one line
tabbar() { local g; g=($(geom "${1:-1}")); e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $((g[1] + g[3] - 2)) | noicon | sed 's/  */ /g'; }
# the column and row of the tab named $1 in pane ${2:-1}
tabxy() { local g y; g=($(geom "${2:-1}")); y=$((g[1] + g[3] - 2)); echo "$(e2e_find "$1" "$y" | cut -d' ' -f1) $y"; }
menu_has() { e2e_plain | grep -qF "$1"; }
menu_click() { local y; y=$(e2e_plain | grep -nF "$1" | head -1 | cut -d: -f1); local x; x=$(e2e_find "$1" "$y" | cut -d' ' -f1); e2e_click $((x + 1)) "$y"; sleep 0.8; }

e2e_start -C "$D/c" -S "$D/st" -D "$D/dt" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 1; solo
for t in t_user t_order t_sku t_log t_kv; do open_table "$t"; wait_for 8 settled; done
check "（准备）五个 tab：t_user t_order t_sku t_log t_kv" eval '[[ $(tabbar) == *"1:t_user"*"2:t_order"*"3:t_sku"*"4:t_log"*"5:t_kv"* ]]' || tabbar

read -r x y <<<"$(tabxy t_sku)"
rclick "$x" "$y"
check "右键第三个 tab（t_sku）：菜单有 关闭 / 关闭其他 / 关闭左侧 / 关闭右侧 / 向右拆分 / 向下拆分 / 固定，后面有键" eval 'menu_has 关闭其他 && menu_has 关闭左侧 && menu_has 关闭右侧 && menu_has 向右拆分 && menu_has 向下拆分 && menu_has 固定 && e2e_plain | grep -qE "关闭其他 +SPC wo" && e2e_plain | grep -qE "固定 +SPC wP"' || e2e_plain | sed -n 3,20p | cut -c30-120
menu_click "固定"
check "点「固定」：t_sku 到最前（1:t_sku），后面是固定标记，其余顺序不变" eval '[[ $(tabbar) == *"1:t_sku"*"2:t_user"*"3:t_order"*"4:t_log"*"5:t_kv"* ]] && ! tabbar | grep -q "t_user "[^a-z]*"\*"' || tabbar
read -r x y <<<"$(tabxy t_sku)"; rclick "$x" "$y"
check "再右键 t_sku：这一项写「取消固定」" eval 'menu_has 取消固定'
key Escape; sleep 0.3

read -r x y <<<"$(tabxy t_order)"; rclick "$x" "$y"; menu_click "关闭其他"
check "右键 t_order →「关闭其他」：只剩固定的 t_sku 和当前的 t_order" eval '[[ $(tabbar) == *"1:t_sku"*"2:t_order"* && $(tabbar) != *t_user* && $(tabbar) != *t_log* && $(tabbar) != *t_kv* ]]' || tabbar
key Space; key w; sleep 1
check "SPC w 的 which-key 里有 固定 和 关闭其他" eval 'e2e_plain | grep -q "固定 / 取消固定 tab" && e2e_plain | grep -q "关闭其他 tab"' || e2e_plain | tail -8
key Escape; sleep 0.3

read -r x y <<<"$(tabxy t_order)"; rclick "$x" "$y"; menu_click "向右拆分"
check "右键 t_order →「向右拆分」：多一个 pane，t_order 在新 pane 里（旧 pane 只剩 t_sku）" eval '[[ -n $(geom 2) && $(tabbar 1) == *t_sku* && $(tabbar 1) != *t_order* && $(tabbar 2) == *t_order* ]]' || { tabbar 1; tabbar 2; }
read -r x y <<<"$(tabxy t_sku 1)"; rclick "$x" "$y"; menu_click "向下拆分"
check "右键唯一的 tab t_sku →「向下拆分」：只分割，t_sku 还在，新 pane 是空的" eval '[[ $(tabbar 1) == *t_sku* ]] && [[ -n $(geom 3) ]]'

e2e_type ":"; sleep 0.3; e2e_type qa; key Enter; sleep 1.2
e2e_start -C "$D/c" -S "$D/st" -D "$D/dt" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 2; wait_for 8 settled
check "重启（last.toml）：t_sku 还是固定的、在最前" eval '[[ $(tabbar 1) == *"1:t_sku"* ]] && grep -q "pinned = true" "$(find "$D/st" -name last.toml | head -1)"' || { tabbar 1; find "$D/st" -name last.toml | head -1 | xargs grep -n pinned; }
e2e_done
