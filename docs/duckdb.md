# DuckDB

sqlmux opens DuckDB databases next to your other connections: the tree, table tabs with paging, sorting, filtering and editing, queries and cancelling work as they do on PostgreSQL. A `:memory:` connection queries Parquet and CSV files straight from disk.

## Which install has DuckDB

| | DuckDB |
|---|---|
| macOS, Windows: every install method | yes |
| Linux: Homebrew, npm, `sqlmux_<version>_linux_<arch>.tar.gz`, `go install` | no |
| Linux: `sqlmux_<version>_linux_<arch>_duckdb.tar.gz` from the [Releases page](https://github.com/kackerx/sqlmux/releases) | yes, needs glibc |
| Linux: `go install -tags duckdb github.com/kackerx/sqlmux/cmd/sqlmux@latest` | yes, needs glibc |

Why Linux is split: to load DuckDB's library, sqlmux's binary has to be linked to glibc. The plain Linux release is static, so it runs anywhere, Alpine and NixOS included; the `_duckdb` build does not run on Alpine or other musl systems. A sqlmux without DuckDB still lists the engine; connecting says which package to take.

## Where libduckdb comes from

sqlmux does not bundle DuckDB. It loads DuckDB's shared library, `libduckdb` (`libduckdb.so`, `libduckdb.dylib` on macOS, `duckdb.dll` on Windows), v2.0.0 or newer, when it connects. It looks, in order:

1. `library` under `[duckdb]` in `config.toml`, if set (then nowhere else);
2. the copy sqlmux downloaded, in its data directory (`~/.local/share/sqlmux/duckdb/`, on Windows `%LOCALAPPDATA%\sqlmux\duckdb\`);
3. on macOS and Linux, Homebrew's `lib` (`/opt/homebrew/lib`, `/home/linuxbrew/.linuxbrew/lib`), `/usr/local/lib`, then the system's library path; on Windows, the folder `sqlmux.exe` is in.

With none found, the first DuckDB connection asks:

```
DuckDB needs libduckdb (v2.0.1, about 27 MB). Download it from the github.com/duckdb/duckdb Release into /home/me/.local/share/sqlmux/duckdb/2.0.1/libduckdb.so?
  https://github.com/duckdb/duckdb/releases/download/v2.0.1/duckdb-shared-libs-linux-amd64.tar.gz
```

`y` downloads DuckDB's own archive for your platform (18 to 27 MB; the library inside is about 90 MB on disk), checks it against a sha256 built into sqlmux, and connects; `C-c` cancels a download. `n` cancels the connection and links to [the ways to install it yourself](../README.md#duckdb). Nothing is downloaded without asking.

The other ways:

- `brew install duckdb` (macOS, Linuxbrew).
- Download `libduckdb` from [DuckDB's releases](https://github.com/duckdb/duckdb/releases) and set its path:

  ```toml
  [duckdb]
  library = "/opt/duckdb/libduckdb.so"
  ```

  A relative path is from the directory `config.toml` is in.

The downloaded version, v2.0.1, moves with sqlmux's releases. A file written by a newer DuckDB may not open with it (DuckDB's own error says so): point `library` at a newer libduckdb.

On Windows, `duckdb.dll` needs the [Microsoft Visual C++ Redistributable](https://learn.microsoft.com/cpp/windows/latest-supported-vc-redist); without it the library does not load, and the error says so.

## Connect

From the command line, a file ending in `.duckdb` or `.ddb` opens as DuckDB, without saving anything:

```sh
sqlmux ./data.duckdb
```

To keep a connection, add it to `~/.config/sqlmux/connections.toml` (on Windows `%APPDATA%\sqlmux\connections.toml`), or use the connection form:

```toml
[[connection]]
name   = "analytics"
engine = "duckdb"
dsn    = "~/data/analytics.duckdb"     # ?mode=ro opens it read only

[[connection]]
name   = "scratch"
engine = "duckdb"
dsn    = ":memory:"                    # select * from 'orders.parquet'
```

- The DSN is a file path; `~` is expanded and a relative path is from where you start sqlmux.
- A missing file is not created: add `?mode=rwc` to create it.
- `?mode=ro` opens the file read only; `rw` (the default) and `rwc` are the other modes.
- Other query parameters are DuckDB settings: `?memory_limit=4GB&threads=4`.
- `:memory:` is a database in memory, one for the whole sqlmux process.

## In sqlmux

- The tree: connection → schema → Tables, Views, Functions (your macros) and Sequences. Databases you `attach` are not in the tree; query them with three-part names (`other.main.t`).
- `use other_db` is refused: the connection stays in its own file. `use <schema>` is fine.
- A table needs a primary key or a unique constraint to be edited; without one it is read only, and the tab says why.
- `read_only = true` runs every transaction read only. The file is still opened for writing, so other programs cannot open it meanwhile; for a file that others can read at the same time, use `?mode=ro`.

## Things to know

- **One writer per file.** DuckDB lets one process write a file at a time. While sqlmux is connected to a file, your own Python or DuckDB CLI cannot open it (DuckDB's error names sqlmux's PID), and the other way round. Disconnect the connection (right-click it in the tree, *Disconnect*) to release the file.
- **DuckDB runs inside sqlmux.** A query that crashes DuckDB or runs out of memory takes sqlmux down with it. DuckDB uses up to 80% of RAM by default and spills to disk beyond that; set `?memory_limit=` in the DSN to lower it.
- **No savepoints.** In a manual transaction (`Tx: Manual`), a failed save or a WHERE that errors aborts the whole transaction, and what you had not committed is lost. In the default auto mode this does not matter.
