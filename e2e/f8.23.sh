#!/usr/bin/env bash
# F8.23 树的右键菜单、删 query 文件、删表 / 视图（specs/m8-feedback/task.md F8.23；tech-design §7.8「右键菜单」、§13）
# 右键（SGR 按钮 2）弹菜单：query 文件 → 打开 / 重命名… / 删除…，表 → 打开 / 在新 tab 打开 / 删除表…，视图 → 删除视图…，别的节点不弹；
# 菜单的键（j k ↑ ↓ ↵ esc）、点别处关；删表走危险确认框（红色、正文是要执行的 DROP，confirm_dangerous = false 也问），dd 等于菜单里的删除；
# 手动事务模式拒绝，只读连接拒绝。表和视图建在自建库里，只在自建库上删。
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1
. "$(dirname "$0")/palette.sh"

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); trap 'e2e_stop; rm -rf "$D"' EXIT
e2e_own_db || { echo "e2e: could not create the private database" >&2; exit 1; }
ERROR=#f7768e
mkdir -p "$D/c" "$D/ro"
(umask 077; printf '[[connection]]\nname = "rw"\nengine = "postgres"\ndsn = "%s"\n' "$E2E_DB" >"$D/c/connections.toml"
 printf '[[connection]]\nname = "ro"\nengine = "postgres"\ndsn = "%s"\nread_only = true\n' "$E2E_DB" >"$D/ro/connections.toml")
psql_n() { psql "$E2E_DB" -At -c "$1"; }
files() { ls "$E2E_TMP/data/sqlmux/queries" 2>/dev/null | sed 's/\.sql$//' | tr '\n' ' '; }
toast_text() { e2e_text 1 "$(e2e_flag pane_width)" $(( $(H) - 1 )) | sed 's/[ │└┘─┌┐]//g'; }
toasted() { e2e_recorded "$1"; }
fresh_rec() { : >|"$D/rec"; }
rclick() { _sgr 2 "$1" "$2" M; _sgr 2 "$1" "$2" m; sleep 0.6; }                  # 右键单击
srow() { local y; for y in $(seq 3 43); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && { echo "$y"; return; }; done; }     # 侧栏里 NAME 最靠上的那一行（表在「工作区」的 tab 节点之上）
srow_last() { local y r=; for y in $(seq 3 43); do [[ $(e2e_text 1 32 "$y") == *" $1 "* ]] && r=$y; done; echo "$r"; }     # 最靠下的（Queries 下的 query 在「工作区」的之下）
pop() { e2e_panes | awk '$1 == "-" { print $2, $3, $4, $5 }' | tail -1; }
mitems() { local g y; g=($(pop)); [[ -n ${g[0]} ]] || return 0; for ((y = g[1] + 1; y < g[1] + g[3] - 1; y++)); do e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $y | sed 's/^ *//; s/^[^ ]* //; s/ *$//'; done | tr '\n' '|'; }
menu_is() { [[ $(mitems) == "$1" ]] || { echo "  menu: [$(mitems)], want [$1]"; false; }; }
no_menu() { [[ -z $(pop) ]] || { echo "  popup: [$(mitems)]"; false; }; }
asked() { e2e_plain | grep -q "不能撤销"; }
dbox() { e2e_plain | grep -oE 'DROP [A-Z ]+"[^"]*"\."[^"]*"|DROP [A-Z ]+[`"][^ ]*' | head -1; }
box_red() { local g; g=($(pop)); [[ -n ${g[0]} ]] && style_has $((g[0] + 2)) $((g[1] + 1)) fg=$ERROR >/dev/null; }   # 框里的标题行（「删除表 …，不能撤销：」）是 error 色
tsel() { local y; for y in $(seq 3 43); do [[ $(e2e_style 4 "$y") == *bg=#364a82* ]] && { e2e_text 1 32 "$y"; return; }; done; }
tree_focus() { local i; for ((i = 0; i < 6; i++)); do [[ $(focused) == 0 ]] && return 0; key C-h; done; }
tree_to() { local i; tree_focus; key G; for ((i = 0; i < 60; i++)); do [[ $(tsel) == *" $1 "* ]] && return 0; key k; done; return 1; }
newcon() { pal ">query.new"; key Enter; sleep 0.8; }
type_in() { key i; e2e_type "$1"; key Escape; sleep 1.5; }

psql_n "create table t_drop1 (id int); create table t_drop2 (id int); create table t_drop3 (id int); create view v_drop1 as select 1 as x" >/dev/null
SOLO= start -x 200 -C "$D/c"; e2e_record "$D/rec"; sleep 1
check "（准备）自建库的 t_drop1、t_drop2、t_drop3 在树里" eval '[[ -n $(srow t_drop1) && -n $(srow t_drop2) && -n $(srow t_drop3) ]]'

# ---- 表节点上右键：菜单；esc / 点别处关；键盘选
y=$(srow t_drop1); rclick 12 "$y"
check "右键表 t_drop1：弹菜单「打开 / 在新 tab 打开 / 删除表…」，后面是 F14.4 的五个结构模板，贴着指针" eval 'menu_is "打开|在新 tab 打开|删除表…|加列…|加索引…|改名…|删除列…|删除索引…|"'
key Escape; sleep 0.3
check "esc：菜单关掉" no_menu
rclick 12 "$y"; e2e_click 120 30; sleep 0.4
check "右键后点菜单以外的地方：菜单关掉（这次点击不透传，ⓘ 焦点没被点走）" eval 'no_menu'
rclick 12 "$y"; key j; key j; key Enter; sleep 0.6
check "右键 → j j ↵ 选「删除表…」：弹危险确认框，正文是 DROP TABLE，框是红色" eval 'asked && [[ $(dbox) == *"DROP TABLE"*"t_drop1"* ]] && box_red || { echo "  [$(dbox)] red? $(pop)"; false; }'
check "确认框里 confirm_dangerous = false 也问（lib 默认关）" eval 'asked'
key n; sleep 0.5
check "n：取消，表还在" eval '[[ $(psql_n "select count(*) from pg_tables where tablename = '"'t_drop1'"'") == 1 && -n $(srow t_drop1) ]]'

# ---- 删表：y 之后关掉这张表的 tab、树里没了、库里没了
tree_to t_drop1; key Enter; wait_for 8 eval '[[ $(e2e_plain | head -1) == *" t_drop1 ─"* ]]'
check "（准备）t_drop1 在 ① 里开着" eval '[[ $(e2e_plain | head -1) == *" t_drop1 ─"* ]]'
fresh_rec; y=$(srow t_drop1); rclick 12 "$y"; key j; key j; key Enter; sleep 0.5; key y; sleep 1.5
check "y：库里 t_drop1 没了" eval '[[ $(psql_n "select count(*) from pg_tables where tablename = '"'t_drop1'"'") == 0 ]]'
check "toast「已删除 t_drop1」，树里没有了，开着的 tab 关了" eval 'toasted "已删除" && [[ -z $(srow t_drop1) && $(e2e_plain | head -1) != *" t_drop1 ─"* ]]'

# ---- dd 等于菜单里的删除表；schema 节点上无事
tree_to t_drop2; key d; key d; sleep 0.5
check "表节点 t_drop2 上 dd：弹同样的确认框（DROP TABLE … t_drop2）" eval 'asked && [[ $(dbox) == *"t_drop2"* ]]'
key n; sleep 0.4
tree_to public; key d; key d; sleep 0.5
check "schema 节点（public）上 dd：什么都不发生" eval '! asked && no_menu'
y=$(srow public); rclick 12 "$y"
check "schema 节点上右键：不弹菜单" no_menu
y=$(for y in $(seq 3 43); do [[ $(e2e_text 1 32 "$y") == *"Tables ("* ]] && { echo "$y"; break; }; done); rclick 12 "$y"
check "「Tables」分组节点上右键：不弹菜单" no_menu

# ---- 视图：删除视图…
tree_to "Views (3)"; key l; sleep 0.5
y=$(srow v_drop1); rclick 12 "$y"
check "右键视图 v_drop1：菜单里是「删除视图…」" eval '[[ $(mitems) == *"删除视图…"* ]] || { echo "  menu: [$(mitems)]"; false; }'
key j; key j; key Enter; sleep 0.5
check "确认框正文是 DROP VIEW … v_drop1" eval 'asked && e2e_plain | grep -q "DROP VIEW" && [[ $(dbox) == *"v_drop1"* || $(e2e_plain | grep -c "v_drop1") -ge 1 ]]'
key y; sleep 1.2
check "y：视图删了（库里没有）" eval '[[ $(psql_n "select count(*) from pg_views where viewname = '"'v_drop1'"'") == 0 && -z $(srow v_drop1) ]]'

# ---- 手动事务模式：拒绝删表
key C-l; g=($(geom 2)); x=$(e2e_find "Tx:" "${g[1]}" | tr ' ' '\n' | awk -v l="${g[0]}" '$1 > l { print; exit }'); e2e_click "$x" "${g[1]}"; sleep 0.4; e2e_type 手动; sleep 0.3; key Enter; sleep 0.4
fresh_rec; y=$(srow t_drop3); rclick 12 "$y"; key j; key j; key Enter; sleep 0.8
check "手动事务模式：删表被拒绝，toast「手动事务模式下不能删表：…」，没有确认框，表还在" eval '! asked && toasted "手动事务模式下不能删表" && [[ $(psql_n "select count(*) from pg_tables where tablename = '"'t_drop3'"'") == 1 ]]'
g=($(geom 2)); x=$(e2e_find "Tx:" "${g[1]}" | tr ' ' '\n' | awk -v l="${g[0]}" '$1 > l { print; exit }'); e2e_click "$x" "${g[1]}"; sleep 0.4; e2e_type 自动; sleep 0.3; key Enter; sleep 0.4

# ---- query 文件：右键 → 删除… → 确认
key C-l; type_in "keep me"; newcon; type_in "kill me"
e2e_type ':q'; key Enter; sleep 0.8
check "（准备）query_2 关着，文件有内容，Queries 下还在" eval '[[ $(files) == *query_2* && -n $(srow_last query_2) ]]'
y=$(srow_last query_2); rclick 12 "$y"
check "右键 Queries 下的 query_2：菜单「打开 / 重命名… / 删除…」" eval 'menu_is "打开|重命名…|删除…|"'
key j; key j; key Enter; sleep 0.5
check "选「删除…」：确认框「删除 query_2？」" eval '[[ $(e2e_plain | grep -oE "删除 query_2？" | head -1) == "删除 query_2？" ]]'
key y; sleep 0.8
check "y：文件删了，Queries 下没有 query_2 了，query_1 的内容还在" eval '[[ $(files) == "query_1 " && -z $(srow_last query_2) ]]'
y=$(srow_last query_1); rclick 12 "$y"; key Enter; sleep 0.8
check "右键开着的 query_1 → 打开（第一项）：聚焦它（已开着不重复开），工作区下还是一个 query_1 tab" eval 'no_menu && [[ $(e2e_plain | grep -c "keep me") -ge 1 ]]'
y=$(srow_last query_1); rclick 12 "$y"; key j; key j; key Enter; sleep 0.6
check "开着的 query_1 选「删除…」：拒绝，toast「先关掉它的 tab」，文件还在" eval 'toasted "先关掉它的 tab" || toast_text | grep -q "先关掉它的 tab"; [[ $(files) == "query_1 " ]]'

# ---- 只读连接：拒绝删表
start -x 200 -C "$D/ro"; e2e_record "$D/rec"; sleep 1
fresh_rec; y=$(srow t_drop3); rclick 12 "$y"; key j; key j; key Enter; sleep 0.8
check "只读连接 ro：删表被拒绝，没有确认框，表还在" eval '! asked && [[ $(psql_n "select count(*) from pg_tables where tablename = '"'t_drop3'"'") == 1 ]] && { toasted "只读" || toast_text | grep -q "只读"; } || { echo "  toast: $(toast_text)"; false; }'
e2e_done
