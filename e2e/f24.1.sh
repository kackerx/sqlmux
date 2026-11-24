#!/usr/bin/env bash
# M24 node 1, §7.6 / §8.1 / §8.3 / §13: DuckDB queries, cancellation, and file connections.
E2E_NOCONN=1
. "$(dirname "$0")/lib.sh"
. "$(dirname "$0")/duckdb-fixture.sh"
. "$(dirname "$0")/paging.sh"
. "$(dirname "$0")/exporting.sh"
duckdb_setup || exit 1
duckdb_connection file '' || exit 1
# LIMIT makes browsing this view cheap, while exporting all rows takes long enough to interrupt.
duckdb_cli -c "create view t_export as select i as id, 'export-row-' || md5(i::varchar) as payload from range(10000000) r(i)" || exit 1

start -C "$D/conf"
printf 'sample,quantity\nfrom_csv,37\n' >"$E2E_TMP/x.csv"
key C-l
setsql "from 'x.csv'"
key Enter
csv_result() { [[ $(rrow 1) == *sample*quantity* && $(rrow 3) == *from_csv*37* ]]; }
check "CSV query: from 'x.csv' returns its column names and row" wait_for 5 csv_result

# A new Main connection would lose this marker, even if it could run SELECT again.
setsql "set variable e2e_session = 'duck-session'"
key Enter
check 'Cancellation setup: DuckDB accepts the session variable' wait_for 5 screen_has 'SET'
setsql 'select count(*) from range(100000000000) a, range(1000) b'
e2e_keys Enter
check 'Long DuckDB query is executing' wait_for 5 screen_has 'busy'
e2e_keys C-c
check 'Cancel a long DuckDB query: screen shows 查询已取消 and clears busy' wait_for 3 eval 'screen_has 查询已取消 && ! screen_has "busy"'
setsql "select getvariable('e2e_session') as marker"
key Enter
check 'After cancellation the same query connection returns its session variable' wait_for 5 eval 'rrow 3 | grep -qF -- duck-session'

key C-h
open_table t_export
check 'Export setup: the large DuckDB view returns rows' wait_for 5 screen_has 'export-row-'
wait_for 5 settled
export_pick 1
check 'Export all: DuckDB is busy and has opened the CSV' wait_for 5 eval 'screen_has busy && [[ -f $E2E_TMP/t_export.csv ]]'
e2e_keys C-c
check 'Cancelled DuckDB export reports cancellation, with no success toast or partial CSV' wait_for 5 eval 'screen_has 导出已取消 && ! screen_has 已导出 && [[ ! -e $E2E_TMP/t_export.csv ]]'
check 'Read-write sqlmux exits before the read-only connection opens the file' duckdb_quit

rm "$D/conf/connections.toml"
duckdb_connection ro 'read_only = true' || exit 1
start -C "$D/conf"
open_table t_sku
check 'Read-only connection can browse a table' wait_for 5 screen_has 'SKU 0'
key l l Enter
check 'Read-only connection blocks grid editing and says why' wait_for 3 eval 'screen_has "ro 是只读连接" && mode_is NORMAL'
key C-l
setsql "update t_sku set title = 'unwanted' where code = 'sku_0'"
key Enter
check 'Read-only connection blocks SQL and shows the reason' wait_for 5 screen_has 'ro 是只读连接，整次都没有执行'
setsql "select title from t_sku where code = 'sku_0'"
key Enter
check 'Read-only query still returns the original value' wait_for 5 eval 'rrow 3 | grep -qF "SKU 0"'
check 'Read-only sqlmux exits cleanly' duckdb_quit
original_title() {
  screen_has '[e2e-exit 0]' && [[ $(duckdb_cli -c "select title from t_sku where code = 'sku_0'") == 'SKU 0' ]]
}
check 'DuckDB CLI confirms the rejected write left the file intact' original_title

if [[ $(uname -s) == Linux ]]; then
  static_bin=$D/sqlmux-static
  check 'Build the untagged Linux binary with CGO disabled' env CGO_ENABLED=0 go -C "$E2E_ROOT" build -o "$static_bin" ./cmd/sqlmux
  e2e_start -c "$D/conf/config.toml" "'$static_bin' '$DUCKDB_FILE'"
  check 'Opening a DuckDB file with the untagged binary explains that DuckDB is unavailable' wait_for 5 screen_has '这个 sqlmux 不带 DuckDB'
  alpine_version() {
    local version
    version=$(docker run --rm -v "$static_bin:/sqlmux:ro" --entrypoint /sqlmux alpine:3 --version) || return 1
    printf '%s\n' "$version"
    [[ $version == 'sqlmux '* ]]
  }
  check 'The same untagged binary runs --version in alpine:3' alpine_version
fi
e2e_done
