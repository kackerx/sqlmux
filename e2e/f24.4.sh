#!/usr/bin/env bash
# M24 node 2, §8.1: libduckdb discovery, download and cancellation in the terminal.
. "$(dirname "$0")/lib.sh"
. "$(dirname "$0")/duckdb-fixture.sh"
. "$(dirname "$0")/palette.sh"
. "$(dirname "$0")/paging.sh"

case $(uname -sm) in
  'Linux x86_64')   platform=linux-amd64; mb=27 ;;
  'Linux aarch64')  platform=linux-arm64; mb=25 ;;
  *) echo 'SKIP this libduckdb download script uses Linux certificate and loader overrides'; exit 0 ;;
esac
lib=libduckdb.so
unset LD_LIBRARY_PATH LD_PRELOAD
system_dirs=(/opt/homebrew/lib /usr/local/lib /home/linuxbrew/.linuxbrew/lib)
for dir in "${system_dirs[@]}"; do
  if [[ -e $dir/$lib ]]; then
    echo "SKIP missing-library cases: sqlmux would find $dir/$lib"
    exit 0
  fi
done
if python3 - "$lib" <<'PY'
import ctypes
import sys

try:
    ctypes.CDLL(sys.argv[1])
except OSError:
    sys.exit(1)
PY
then
  echo "SKIP missing-library cases: the system loader finds $lib"
  exit 0
fi

duckdb_setup || exit 1
mkdir "$D/data" "$D/download" "$D/form" "$D/cancel" "$D/old"
dest="$D/data/sqlmux/duckdb/2.0.1/$lib"
url="https://github.com/duckdb/duckdb/releases/download/v2.0.1/duckdb-shared-libs-$platform.tar.gz"
manual='https://github.com/kackerx/sqlmux#duckdb'
offer() { screen_has 'DuckDB 要用 libduckdb（v2.0.1'; }
empty_data() { [[ -z $(find "$1" ! -type d -print) ]]; }
file_start() { e2e_start -x 260 -D "$1" "${2:-} '$E2E_BIN' '$DUCKDB_FILE'"; }
toast_row() { e2e_cap | tail -2 | head -1; }
declined() {
  local row
  row=$(toast_row)
  [[ $row == *"找不到 libduckdb：见 $manual"* && $row != *找过* ]] && ! offer
}
searched_paths() {
  local log dir
  log=$(e2e_cap | sed -n '/libduckdb.*找过/p')
  [[ $log == *"$1"* && $log == *", $lib"* ]] || return 1
  for dir in "${system_dirs[@]}"; do
    [[ $log == *"$dir/$lib"* ]] || return 1
  done
}
tree_ready() { running && e2e_cap | awk -F '│' '$2 ~ / t_sku( |$)/ { found = 1 } END { exit !found }'; }
download_started() { e2e_recorded '正在下载 libduckdb v2.0.1' 10; }

file_start "$D/data"
check 'Missing library: opening a .duckdb file asks with the release version, size, URL and exact destination' \
  wait_for 5 eval 'offer && screen_has "约 $mb MB" && screen_has "$url" && screen_has "$dest"'
key n
check 'Declining leaves one short error line with the manual link and writes no data files' \
  wait_for 5 eval 'declined && [[ -z $(find "$D/data" -mindepth 1 -print) ]]'

# The first start uses PostgreSQL; the DuckDB connection has never been opened.
rm "$D/conf/config.toml"
(umask 077; cat >"$D/conf/connections.toml" <<TOML
[[connection]]
name = "pg-e2e"
engine = "postgres"
dsn = "$SQLMUX_TEST_PG"
TOML
)
duckdb_connection duck-e2e '' || exit 1
start -x 260 -C "$D/conf" -D "$D/download"
key C-l
check 'Query tab starts on the connected PostgreSQL connection' wait_for 5 screen_has 'pg-e2e ·'
setsql 'select 42 as pg_ready'; key Enter
check 'A PostgreSQL query opens the result area and its log tab' wait_for 5 eval 'rrow 3 | grep -qF 42'
switch_to_duck() { pal '>query.conn'; key Enter; e2e_type duck-e2e; key Enter; }
switch_to_duck
check 'Declining from the query tab also leaves a short error with the manual link' \
  eval 'wait_for 5 offer && key n && wait_for 5 declined'
click_tab 3 '1:'
check 'The query log lists the data path, all three Homebrew paths and the loader name' \
  wait_for 5 searched_paths "$D/download/sqlmux/duckdb/2.0.1/$lib"
key C-k
switch_to_duck
check 'Choosing the unconnected DuckDB connection from the query tab asks to download' wait_for 5 offer
e2e_record "$D/success.screen"
e2e_keys y
check 'Accepting the real download shows a progress toast' download_started
check 'The download installs a nonempty library in the versioned data directory and populates the tree' \
  wait_for 60 eval '[[ -s "$D/download/sqlmux/duckdb/2.0.1/$lib" ]] && tree_ready'
check 'After reconnect the same query tab has the DuckDB connection and schema' \
  wait_for 5 screen_has 'duck-e2e · main'
setsql "select 'DuckDB' as engine, count(*) as total from range(7)"
key Enter
check 'A DuckDB-only statement runs in that query tab and returns DuckDB and 7' \
  wait_for 5 eval 'rrow 3 | grep -qE "DuckDB.*[[:space:]]7([[:space:]]|$)"'

file_start "$D/download"
check 'A fresh process discovers the installed library and opens the seeded tables without asking' \
  wait_for 10 eval 'tree_ready && ! offer'

# The success case above uses the real release. A loopback HTTPS server stalls
# after the first bytes for cancellation. Only these two processes trust it.
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj '/CN=github.com' \
  -addext 'subjectAltName=DNS:github.com' -keyout "$D/key.pem" -out "$D/cert.pem" >"$D/openssl.log" 2>&1 || {
  cat "$D/openssl.log"; exit 1;
}
python3 "$E2E_ROOT/e2e/duckdb-stalled-download.py" "$D/cert.pem" "$D/key.pem" >"$D/proxy.port" &
proxy_pid=$!
trap 'e2e_stop; kill "$proxy_pid" 2>/dev/null; wait "$proxy_pid" 2>/dev/null; rm -rf "$D"' EXIT
wait_for 5 test -s "$D/proxy.port" || exit 1
proxy="env HTTPS_PROXY=http://127.0.0.1:$(cat "$D/proxy.port") NO_PROXY='' no_proxy='' SSL_CERT_FILE='$D/cert.pem'"
partial_received() {
  local file
  for file in "$1"/sqlmux/duckdb/2.0.1/.sqlmux-partial-*; do
    [[ -s $file ]] && return 0
  done
  return 1
}
cancelled() { screen_has '已取消下载 libduckdb' && empty_data "$1"; }

e2e_start -x 200 -C "$D/conf" -D "$D/form" "$proxy '$E2E_BIN'"
wait_for 5 running
pal '>conn.new'; key Enter
e2e_type unsaved-duck
key Tab Tab Right Right Right Right Right Tab
e2e_type "$DUCKDB_FILE"
key C-t
check 'Test in the unsaved DuckDB form offers the download' wait_for 5 offer
e2e_keys y
check 'The form test has received some archive bytes before C-c' wait_for 20 partial_received "$D/form"
e2e_keys C-c
check 'C-c cancels the form download and removes partial files' wait_for 5 cancelled "$D/form"
check 'The unsaved form stays open with the name, engine and file path intact' \
  wait_for 5 eval 'screen_has "[ 测试 ]" && screen_has unsaved-duck && screen_has "$DUCKDB_FILE" && ! grep -q unsaved-duck "$E2E_TMP/config/sqlmux/connections.toml"'
key C-t
check 'Testing the same unsaved form again still offers the DuckDB download' wait_for 5 offer

file_start "$D/cancel" "$proxy"
check 'A separate file connection offers the download with its own empty data directory' wait_for 5 offer
e2e_keys y
check 'The file connection has received some archive bytes before C-c' wait_for 20 partial_received "$D/cancel"
e2e_keys C-c
check 'C-c cancels the file download and leaves no library or partial files' wait_for 5 cancelled "$D/cancel"
key C-h; key g g; key R
check 'Retrying the file connection asks again' wait_for 5 offer

# A relative path must be resolved next to config.toml, away from the pane's cwd.
printf '[duckdb]\nlibrary = "broken-library"\n' >"$D/conf/config.toml"
printf 'not a shared library\n' >"$D/conf/broken-library"
e2e_start -x 200 -C "$D/conf" "'$E2E_BIN' '$DUCKDB_FILE'"
check 'An existing broken relative library is named in the load error and never offers to download' \
  wait_for 5 eval 'screen_has "加载不了 $E2E_TMP/config/sqlmux/broken-library" && ! offer'

old_lib=${SQLMUX_TEST_DUCKDB_OLD:-}
if [[ -n $old_lib && -r $old_lib ]]; then
  # An explicit [duckdb] override would take priority over the downloaded library.
  file_start "$D/old" "env LD_LIBRARY_PATH='$(dirname "$old_lib")'"
  check 'The discovered v1.x library is identified as too old and a download is offered' \
    wait_for 5 eval 'offer && e2e_cap | grep -qE "找到的 libduckdb 是 v1\..*太旧了"'
else
  echo "SKIP too-old library: set SQLMUX_TEST_DUCKDB_OLD to a v1.x libduckdb.so"
fi

e2e_done
