#!/usr/bin/env bash
# M24 node 1, §7.8 / §8.1 / §10.1 / §10.3 / §14: DuckDB files, catalog, paging and grid saves.
E2E_NOCONN=1
. "$(dirname "$0")/lib.sh"
. "$(dirname "$0")/duckdb-fixture.sh"
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/ordering.sh"
. "$(dirname "$0")/paging.sh"
duckdb_setup || exit 1

# Find actual tree rows so clicks do not depend on the number of visible nodes.
tree_row() { e2e_plain | awk -F '│' -v name="$1" '$2 ~ (" " name "( |$)") { print NR; exit }'; }
tree_has() { [[ -n $(tree_row "$1") ]]; }
tree_click() {
  local y x
  y=$(tree_row "$1"); [[ -n $y ]] || return 1
  x=$(e2e_find "$2" "$y"); [[ -n $x ]] || return 1
  e2e_click "${x%% *}" "$y"
}
table_on_screen() { [[ $(e2e_cap | head -1) == *" $1 ─"* ]]; }
sku_title() { # 1-based visible row, default first
  local x y width
  read -r x y width _ <<<"$(geom 1)"
  [[ -n $width ]] || return 1
  e2e_text "$((x + 1))" "$((x + width - 2))" "$(( $(grid_y) + ${1:-1} ))" |
    awk -F '│' '{ gsub(/^ +| +$/, "", $4); print $4 }'
}
edit_title() {
  key 0 l l Enter
  e2e_type "$1"
  key Enter
}
stored_titles_are() {
  screen_has '[e2e-exit 0]' || return 1
  local titles
  titles=$(duckdb_cli -c "select title from t_sku where code in ('sku_0', 'sku_1') order by code") || return 1
  [[ $titles == "$1"$'\n'"${2:-SKU 1}" ]] || { printf 'CLI returned:\n%s\n' "$titles"; return 1; }
}

start -C "$D/conf"
pal '>conn.new'; key Enter
e2e_type file
key Tab Tab Right Right Right Right Right Tab
e2e_type "$DUCKDB_FILE"
key C-t
check 'DuckDB connection form opens the file and reports the engine version' wait_for 5 screen_has '已连接 · DuckDB v'
key Enter
check 'Open a DuckDB file: saving the connection loads its catalog' wait_for 5 tree_has 'Tables [(]6[)]'

check 'Tree shows the main and agentable DuckDB schemas' eval 'tree_has main && tree_has agentable'
tree_click agentable '▸'
wait_for 5 tree_has 'Tables [(]3[)]'
tree_click 'Tables [(]3[)]' '▸'
check 'Tree expands agentable → Tables → agent, agent_version and goal' wait_for 5 eval 'tree_has agent && tree_has agent_version && tree_has goal'
tree_click agentable '▾'
wait_for 5 tree_has t_order
tree_click t_order t_order
check 'Open main.t_order from the tree: page 1 starts at id 1' wait_for 5 eval 'table_on_screen t_order && screen_has "PAGE 1/60" && ids_are "1,2,"'
key ']'
check 'Page forward: page 2 starts at id 101' wait_for 5 eval 'screen_has "PAGE 2/60" && ids_are "101,102,"'
key '['
check 'Page back: page 1 starts at id 1 again' wait_for 5 eval 'screen_has "PAGE 1/60" && ids_are "1,2,"'

open_table t_log
check 'Keyless table is readable' wait_for 5 eval 'table_on_screen t_log && screen_has plain'
key l Enter
check 'Keyless table blocks editing and explains the missing row identity' wait_for 3 eval 'screen_has "t_log 没有主键，也没有全部列都非空的唯一索引，只读" && mode_is NORMAL'

# t_sku has a usable UNIQUE constraint and no primary key.
open_table t_sku
check 'Save setup: sku_0 starts with its seeded title' wait_for 5 eval '[[ $(sku_title) == "SKU 0" ]]'
edit_title grid-saved
key C-s
check 'Save a cell edit: grid reports one saved row' wait_for 5 screen_has '已保存 1 行'
cp "$E2E_TMP/config/sqlmux/connections.toml" "$D/conf/connections.toml" || exit 1
check 'sqlmux exits and releases the DuckDB file before CLI readback' duckdb_quit
check 'Save a cell edit: CLI sees the new value only on the intended row' stored_titles_are grid-saved

# A separate Main connection within the same process can commit an intervening edit.
duckdb_connection peer '' || exit 1
start -C "$D/conf"
open_table t_sku
check 'Conflict setup: the grid has already read grid-saved' wait_for 5 eval '[[ $(sku_title) == grid-saved ]]'
key C-l
pal '>query.conn'; key Enter
e2e_type peer; key Enter
check 'Query pane is on the separate peer connection' wait_for 5 eval 'e2e_cap | head -1 | grep -qF "peer ·"'
setsql "update t_sku set title = 'peer-wrote' where code = 'sku_0' returning title"
key Enter
check 'Peer write completed while the grid still shows its old value' wait_for 5 eval '[[ $(sku_title) == grid-saved ]] && rrow 3 | grep -qF -- peer-wrote'
key C-h
edit_title stale-grid
key C-s
check 'Save against a stale row: UI identifies sku_0 and reports the rollback' wait_for 5 eval 'screen_has "code = sku_0" && screen_has "行数据已变化或行不存在，已回滚"'
check 'sqlmux discards the unsaved grid edit and exits before CLI readback' duckdb_quit discard
check 'Stale save preserves the peer write and leaves the other row alone' stored_titles_are peer-wrote

start -x 200 -C "$D/conf"
open_table t_sku
key C-l
setsql "attach ':memory:' as other; use other"
key V Enter
check 'Query pane refuses the batch that tries to switch to another database' wait_for 5 screen_has '不能切换到别的库'
key C-h
edit_title file-stayed
key C-s
check 'Grid can still save to the original DuckDB table' wait_for 5 eval 'screen_has "已保存 1 行" && [[ $(sku_title) == file-stayed ]]'

# Make the second visible row stale before the grid's manual transaction starts.
key C-l
pal '>query.conn'; key Enter
e2e_type peer; key Enter
setsql "update t_sku set title = 'peer-second' where code = 'sku_1' returning title"
key Enter
check 'Peer updates sku_1 while the grid retains the old second row' wait_for 5 eval '[[ $(sku_title 2) == "SKU 1" ]] && rrow 3 | grep -qF -- peer-second'
pal '>query.conn'; key Enter
e2e_type file; key Enter
pal '>tx.mode'; key Enter; key Escape
check 'The file connection is in manual-commit mode' wait_for 5 screen_has 'Tx: 手动'
key C-h
edit_title manual-first
key j
edit_title manual-stale
check 'Both grid rows are staged before the manual save' eval '[[ $(sku_title) == manual-first && $(sku_title 2) == manual-stale ]]'
key C-s
check 'Two-row save reports stale sku_1 and marks the manual transaction failed' wait_for 5 eval 'screen_has "code = sku_1" && [[ $(bar) == *事务出错* ]]'
key C-l
setsql 'COMMIT'
key Enter
check 'COMMIT of the failed grid save reports ROLLBACK' wait_for 5 eval 'screen_has ROLLBACK && [[ $(bar) != *事务出错* ]]'
check 'sqlmux exits before checking the file after COMMIT' duckdb_quit discard
check 'Neither staged row was committed, and the preceding grid save landed in the file' stored_titles_are file-stayed peer-second

# Launch from the fixture directory so the positional argument is literally x.duckdb.
e2e_start -c "$D/conf/config.toml" "cd '$D' && '$E2E_BIN' x.duckdb"
check 'sqlmux x.duckdb opens a temporary DuckDB connection with the main schema' wait_for 5 eval 'running && tree_has x && tree_has main && tree_has t_sku'
open_table t_sku
check 'Temporary DuckDB connection reads the same file' wait_for 5 eval '[[ $(sku_title) == file-stayed ]]'
check 'Temporary DuckDB connection exits without saving a connection or layout' eval 'duckdb_quit && [[ ! -e $E2E_TMP/config/sqlmux/connections.toml && -z $(find "$E2E_TMP" -name last.toml) ]]'
e2e_done
