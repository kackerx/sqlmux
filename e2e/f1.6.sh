#!/usr/bin/env bash
# F1.6 data pane 的 tab（specs/m1-browse/task.md F1.6；tech-design §7.8「tab 栏」「schema 侧栏」、§12「执行」）
# 布局交给 golden（TestGoldenTabs160x45、80x24）；这里测按键、点击和真实 PG 上的打开 / 切换。
# 在自建库里做：「切过去，不重新取数」要锁住表，看有没有等锁的取数语句。
. "$(dirname "$0")/lib.sh"
SOLO=1   # ① alone right of the sidebar, as before M3's console (lib.sh solo)
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
APP=e2e-f16-$$
mkdir -p "$D/own"; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s&application_name=%s"\n' "$E2E_DB" "$APP" >"$D/own/connections.toml"; chmod 600 "$D/own/connections.toml"
. "$(dirname "$0")/palette.sh"
header() { e2e_text 35 159 "$(hy)"; }
where() { key /; clear_in; e2e_type "$1"; sleep 0.2; key Enter; wait_for 8 settled; sleep 0.2; }
transposed() { [[ $(header) =~ │\ 1\ +│\ 2\  ]]; }                      # 转置后表头是记录序号
pal_open() { e2e_keys C-p; sleep 0.3; e2e_type "@$1"; sleep 0.3; key "$2"; wait_for 8 settled; sleep 0.2; }   # NAME KEY：从面板用 ↵ / C-t 打开表

start -C "$D/own"; open_table t_order; wait_for 8 settled
pal_open t_user C-t
check "C-t 新开 t_user：当前 tab 标 *，上一个标 -" tabs_are 1 "1:t_order- │ 2:t_user*"

# ---- 每个 tab 各自保留 WHERE / ORDER / LIMIT / PAGE / COLS / 光标 / 转置（T-01~T-03）
where "id > 10"
key g o; e2e_type name; sleep 0.3; key Enter; wait_for 8 settled
key g c; key j j Space Escape
key 3 j 2 l; key T
user_kept() { [[ $(where_in) == "id > 10" ]] && qb_has "ORDER name $ASC   LIMIT 100   PAGE 1/1   COLS 4/5" && [[ $(cnt) == 40 ]] && pos_is 4,3 && transposed; }
check "t_user：WHERE id > 10、ORDER name、隐藏 email、光标 4,3、转置" user_kept
key g T
check "gT 回到 t_order：还是默认的查询条、光标 1,1、不转置" eval 'tabs_are 1 "1:t_order* │ 2:t_user-" && [[ -z $(where_in) ]] && qb_has "ORDER id $ASC   LIMIT 100   PAGE 1/60   COLS 10/10" && [[ $(cnt) == 6000 ]] && pos_is 1,1 && ! transposed'
key g l; e2e_type 500; sleep 0.3; key Enter; wait_for 8 settled
key ']'; wait_for 8 settled; key 5 j 3 l
order_kept() { qb_has "LIMIT 500   PAGE 2/12" && pos_is 506,4 && ! transposed; }
check "t_order 改成 LIMIT 500、第 2 页、光标 506,4" order_kept
key g t
check "gt 到 t_user：它的七项都还在" eval 'tabs_are 1 "1:t_order- │ 2:t_user*" && user_kept'
key g T
check "再回 t_order：LIMIT、PAGE、光标也都还在" order_kept

# 切 tab 时，正在编辑的 WHERE 退出输入，输入框恢复成生效的条件
key /; e2e_type "id < 5"; sleep 0.2
check "t_order 里正在输入 WHERE" eval 'mode_is INSERT && [[ $(where_in) == "id < 5" ]]'
click_tab 1 2:t_user
check "点击 tab 2：切到 t_user，退出输入（NORMAL）" eval 'tabs_are 1 "1:t_order- │ 2:t_user*" && mode_is NORMAL && [[ $(where_in) == "id > 10" ]]'
click_tab 1 1:t_order
check "点回 t_order：输入框是生效的条件（空），没输完的 id < 5 没有执行" eval 'tabs_are 1 "1:t_order* │ 2:t_user-" && [[ -z $(where_in) ]] && order_kept'

# ---- gt / gT：到头绕回；{N}gt 跳到第 N 个，超出不动；{N}gT 往回 N 个；在树上无效；x 关闭
pal_open t_log C-t
key g t; check "gt 在最后一个：绕回第 1 个" tabs_are 1 "1:t_order* │ 2:t_user │ 3:t_log-"
key g T; check "gT 在第 1 个：绕到最后一个" tabs_are 1 "1:t_order- │ 2:t_user │ 3:t_log*"
key 2 g t; check "2gt：跳到第 2 个" tabs_are 1 "1:t_order │ 2:t_user* │ 3:t_log-"
key 5 g t; check "5gt：超出，不动" tabs_are 1 "1:t_order │ 2:t_user* │ 3:t_log-"
key 2 g T; check "2gT：往回 2 个（绕到第 3 个）" tabs_are 1 "1:t_order │ 2:t_user- │ 3:t_log*"
key C-h; key g t
check "在树上按 gt：① 的 tab 不变，焦点仍在树上" eval 'tabs_are 1 "1:t_order │ 2:t_user- │ 3:t_log*" && focus_is 0'
key C-l; key x
check "x 关闭 t_log：回到上一个 tab t_user" eval 'tabs_are 1 "1:t_order │ 2:t_user*" && [[ $(e2e_plain | head -1) == *" t_user ─"* ]]'

# ---- ↵ 打开这个 pane 里已经开着的表：切过去，不重新取数（F3.37 起 C-t、树里的 t 也一样，「选择 tab」列表去掉了）
pal_open t_order Enter
check "面板 ↵ t_order（开着）：切过去，不新开" tabs_are 1 "1:t_order* │ 2:t_user-"
e2e_lock t_user
e2e_keys C-p; sleep 0.3; e2e_type "@t_user"; sleep 0.3; key Enter; sleep 1
check "锁住 t_user 时 ↵ 它：切过去，没有发出取数（没有等锁的语句），表格照旧" eval '[[ -z $(e2e_waiting $APP) ]] && tabs_are 1 "1:t_order- │ 2:t_user*" && [[ $(bar) != *busy* ]] && user_kept'
e2e_unlock
key C-h; key /; e2e_type t_order; sleep 0.3; key Enter; key Enter
check "从树 ↵ t_order：同样切过去，焦点回到 ①" eval 'tabs_are 1 "1:t_order* │ 2:t_user-" && focus_is 1'
key C-h; key /; key Escape; key C-l                                       # 清掉树的过滤
key g t; pal_open t_order C-t
check "面板里 C-t t_order：也只切过去，不新开第二个（F3.37）" eval 'tabs_are 1 "1:t_order* │ 2:t_user-" && closed'
key g t; key C-h; key /; e2e_type t_order; sleep 0.3; key Enter; key t; wait_for 8 settled
check "树里的 t：同样切过去" eval 'tabs_are 1 "1:t_order* │ 2:t_user-" && focus_is 1'
key C-h; key /; key Escape; key C-l

# ---- 两个 pane：点击 tab 聚焦它的 pane；打开表进最近聚焦的 pane，别的 pane 里开着也不管（F3.37）
key Space %; pal_open t_sku Enter
check "② 里打开 t_sku" eval 'tabs_are 2 "1:t_sku*" && focus_is 2'
click_tab 1 2:t_user
check "点击 ① 的 tab 2：切过去，并聚焦 ①" eval 'focus_is 1 && [[ $(tabbar 1) == *"2:t_user*"* ]]'
pal_open t_sku Enter
check "焦点在 ① 时 ↵ t_sku：① 里没有，就在 ① 新开，② 的 t_sku 不管" eval 'focus_is 1 && tabs_are 1 "1:t_order │ 2:t_user- │ 3:t_sku*" && tabs_are 2 "1:t_sku*"'

# ---- 点击 +（F3.7 起）：在那个 pane 新开一个引导 tab 并切过去，「打开表」选的表开在这个 tab 里；引导页的其余用例在 f3.7
click_tab 1 +
check "点击 ① 的 +：① 新开引导 tab 并切过去，焦点到 ①" eval 'focus_is 1 && tabs_are 1 "1:t_order │ 2:t_user │ 3:t_sku- │ 4:新 tab*"'
key t; e2e_type t_log; sleep 0.3; key Enter; wait_for 8 settled
check "引导页按 t、选 t_log：开在这个 tab 里，② 不变" eval 'tabs_are 1 "1:t_order │ 2:t_user │ 3:t_sku- │ 4:t_log*" && focus_is 1 && tabs_are 2 "1:t_sku*"'

# 窄侧栏里放不下的名字以 … 结尾（§7.8）：F1.12 起由 80x24 的 golden 覆盖（schema_migrati…）

e2e_done
