# Private DuckDB file and libduckdb config shared by f24.1 and f24.2.
duckdb_setup() {
  local engine_dir=/home/kacker/proj/sqlmux-aud/engines/duckdb/dl
  local library=${SQLMUX_TEST_DUCKDB:-$engine_dir/lib/libduckdb.so}
  local cli=${SQLMUX_TEST_DUCKDB_CLI:-$engine_dir/cli/duckdb}
  [[ -r $library && -x $cli ]] || {
    echo 'e2e: set SQLMUX_TEST_DUCKDB to libduckdb and SQLMUX_TEST_DUCKDB_CLI to the DuckDB CLI' >&2
    return 1
  }
  e2e_build || return 1
  D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-duckdb.XXXXXX") || return 1
  trap 'e2e_stop; rm -rf "$D"' EXIT
  DUCKDB_FILE=$D/x.duckdb
  DUCKDB_CLI=("$cli" -batch -bail -noheader -list -init /dev/null)
  [[ ! -v AI_AGENT ]] || DUCKDB_CLI+=(-no-agent)
  unset DATABASE_URL
  mkdir "$D/conf" || return 1
  printf '[duckdb]\nlibrary = "%s"\n' "$library" >"$D/conf/config.toml" || return 1
  duckdb_cli <"$E2E_ROOT/testdata/seed/duckdb.sql"
}

# Call only before opening the file in sqlmux or after sqlmux has released it.
duckdb_cli() {
  XDG_CONFIG_HOME="$D" XDG_STATE_HOME="$D" XDG_DATA_HOME="$D" "${DUCKDB_CLI[@]}" "$DUCKDB_FILE" "$@"
}

duckdb_connection() { # NAME [additional TOML field]
  (umask 077; printf '[[connection]]\nname = "%s"\nengine = "duckdb"\ndsn = "%s"\n%s\n' \
    "$1" "$DUCKDB_FILE" "$2" >>"$D/conf/connections.toml")
}

duckdb_quit() { # pass discard when a failed save leaves a staged edit
  e2e_type ':qa'
  e2e_keys Enter
  if [[ $1 == discard ]]; then
    wait_for 5 screen_has '修改未保存，退出会丢弃' || return 1
    e2e_keys y
  fi
  wait_for 5 screen_has '[e2e-exit 0]'
}
