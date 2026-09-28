#!/usr/bin/env bash
# F1.6 data pane 的 tab（specs/m1-browse/task.md F1.6；tech-design §7.8「tab 栏」「schema 侧栏」、§12「执行」）
# 布局交给 golden（TestGoldenTabs160x45、TestGoldenTabPick160x45、80x24）；这里测按键、点击和真实 PG 上的打开 / 切换。
# 在自建库里做：「切过去，不重新取数」要锁住表，看有没有等锁的取数语句。
. "$(dirname "$0")/lib.sh"
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
geom() { e2e_panes | awk -v n="$1" '$1 == n { print $2, $3, $4, $5 }'; }  # pane N 的 X Y W H
tab_y() { local g; g=($(geom "$1")); echo $((g[1] + g[3] - 2)); }        # pane N 的 tab 栏（内容区最后一行）
tabbar() { local g; g=($(geom "$1")); e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $((g[1] + g[3] - 2)) | sed 's/ │ +.*//; s/^ //'; }
tabs_are() { [[ $(tabbar "$1") == "$2" ]] || { echo "  ⟨$1⟩ tabs: '$(tabbar "$1")', want '$2'"; false; }; }
tab_x() { local g; g=($(geom "$1")); e2e_find "$2" $((g[1] + g[3] - 2)) | tr ' ' '\n' | awk -v l=${g[0]} -v r=$((g[0] + g[2])) '$1 > l && $1 < r { print; exit }'; }
click_tab() { e2e_click "$(tab_x "$1" "$2")" "$(tab_y "$1")"; sleep 0.4; }   # N TEXT：点 pane N 的 tab 栏上的 TEXT（tab 名或 +）
focus_is() { [[ $(focused) == "$1" ]] || { echo "  focused: '$(focused)', want $1"; false; }; }
tree_in() { e2e_text 2 30 2 | sed 's/^ *//; s/ *$//'; }                   # 树的过滤行
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

# ---- ↵ 打开已经开着的表：只有一个 tab 就切过去，不重新取数；有多个就在面板里选
pal_open t_order Enter
check "面板 ↵ t_order（开着一个）：切过去，不新开" tabs_are 1 "1:t_order* │ 2:t_user-"
e2e_lock t_user
e2e_keys C-p; sleep 0.3; e2e_type "@t_user"; sleep 0.3; key Enter; sleep 1
check "锁住 t_user 时 ↵ 它：切过去，没有发出取数（没有等锁的语句），表格照旧" eval '[[ -z $(e2e_waiting $APP) ]] && tabs_are 1 "1:t_order- │ 2:t_user*" && [[ $(bar) != *busy* ]] && user_kept'
e2e_unlock
key C-h; key /; e2e_type t_order; sleep 0.3; key Enter; key Enter
check "从树 ↵ t_order：同样切过去，焦点回到 ①" eval 'tabs_are 1 "1:t_order* │ 2:t_user-" && focus_is 1'
key C-h; key /; key Escape; key C-l                                       # 清掉树的过滤
pal_open t_order C-t; where "id > 100"; key 2 g t
check "C-t 照样新开：t_order 开在 1、3 两个 tab" tabs_are 1 "1:t_order │ 2:t_user* │ 3:t_order-"
e2e_keys C-p; sleep 0.3; e2e_type "@t_order"; sleep 0.3; key Enter
check "↵ t_order（开着两个）：面板列出两个 tab 供选择，带位置和条件，底栏 ↵ 切过去 · C-t 新 tab" eval 'is_open && [[ $(list | cut -d"|" -f1 | tr "\n" "/") == "t_order  ① · 1/t_order  ① · 3 · id > 100/" && $(footer) == *"↵ 切过去 · C-t 新 tab"* ]] || { list; footer; false; }'
key Down; key Enter
check "选第二项 ↵：切到 tab 3（WHERE id > 100）" eval 'closed && tabs_are 1 "1:t_order │ 2:t_user- │ 3:t_order*" && [[ $(where_in) == "id > 100" ]]'
e2e_keys C-p; sleep 0.3; e2e_type "@t_order"; sleep 0.3; key Enter; key C-t; wait_for 8 settled
check "选择 tab 时按 C-t：新开第 4 个" tabs_are 1 "1:t_order │ 2:t_user │ 3:t_order- │ 4:t_order*"
key x
key C-h; key /; e2e_type t_order; sleep 0.3; key Enter; key Enter
check "从树 ↵ 开着两个的 t_order：也进入选择" eval 'is_open && [[ $(list | cut -d"|" -f1 | tr "\n" "/") == "t_order  ① · 1/t_order  ① · 3 · id > 100/" ]]'
key Escape
key t; wait_for 8 settled
check "树里的 t：始终新开 tab" eval 'tabs_are 1 "1:t_order │ 2:t_user │ 3:t_order- │ 4:t_order*" && focus_is 1'
key x; key C-h; key /; key Escape; key C-l

# ---- 两个 pane：点击 tab 聚焦它的 pane；↵ 切到别的 pane 里的 tab，焦点跟过去
key Space %; pal_open t_sku Enter
check "② 里打开 t_sku" eval 'tabs_are 2 "1:t_sku*" && focus_is 2'
click_tab 1 2:t_user
check "点击 ① 的 tab 2：切过去，并聚焦 ①" eval 'focus_is 1 && [[ $(tabbar 1) == *"2:t_user*"* ]]'
pal_open t_sku Enter
check "焦点在 ① 时 ↵ t_sku：切到 ② 的 tab，焦点跟过去，① 不变" eval 'focus_is 2 && tabs_are 2 "1:t_sku*" && [[ $(tabbar 1) == *"2:t_user*"* ]]'

# ---- 点击 +：先聚焦 + 所在的 pane，再进树的过滤框；接下来打开的表进这个 pane 的新 tab
click_tab 1 +
check "点击 ① 的 +：焦点到树、进入过滤框（INSERT）" eval 'focus_is 0 && mode_is INSERT'
e2e_type t_log; sleep 0.3; key Enter; key Enter; wait_for 8 settled
check "过滤出 t_log、↵ 打开：进 ① 的新 tab，焦点到 ①" eval 'tabs_are 1 "1:t_order │ 2:t_user- │ 3:t_order │ 4:t_log*" && focus_is 1 && tabs_are 2 "1:t_sku*"'
key C-h; key /
check "树里自己按 /：仍保留上次的过滤 t_log" eval 'mode_is INSERT && [[ $(tree_in) == *"t_log "*"1/11" ]] || { echo "  filter row: $(tree_in)"; false; }'
key Enter
click_tab 2 +
check "再点 ② 的 +：先清空上次留下的过滤（t_log），过滤框是空的（§7.8）" eval 'focus_is 0 && mode_is INSERT && [[ $(tree_in) != *t_log* ]] || { echo "  filter row: $(tree_in)"; false; }'
e2e_type zz; sleep 0.3; key Escape
check "点击 ② 的 +、在过滤框按 esc：只清空过滤，焦点还在树上" eval 'focus_is 0 && mode_is NORMAL && [[ $(tree_in) != *zz* ]]'
pal_open t_event Enter
check "esc 之后从面板 ↵ 打开 t_event：仍进 ② 的新 tab" eval 'tabs_are 2 "1:t_sku- │ 2:t_event*" && focus_is 2'
click_tab 2 +; key Escape; key C-l; key C-h                               # 焦点离开树：标记作废
pal_open t_order_item Enter
check "焦点离开过树之后再打开：不进 ② 的新 tab（按 §12 打开到焦点所在的 ① 的当前 tab）" eval 'tabs_are 2 "1:t_sku- │ 2:t_event*" && tabs_are 1 "1:t_order │ 2:t_user- │ 3:t_order │ 4:t_order_item*"'

# 窄侧栏里放不下的名字以 … 结尾（§7.8）：F1.12 起由 80x24 的 golden 覆盖（schema_migrati…）

e2e_done
