# Changelog

All notable changes to sqlmux are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/) (while in 0.x, a minor version may break things).

## [Unreleased]

## [0.8.0] - 2026-10-31

### Added

- Open one table straight from the command line: `sqlmux [connection | file | URL] --table [schema.]table [--where "user_id = 42"]`. The flags go before or after the connection; without one, the first connection in `connections.toml` is used. The WHERE runs as if you had typed it and goes into the table's history, and quitting does not overwrite the layout `sqlmux` restores. On a Redis connection `--table` opens a key. ([#6](https://github.com/kackerx/sqlmux/pull/6), idea by MrLiuX)
- Buttons at the right of every pane's title: split right, split below, zoom / unzoom and close. A click acts on that pane, focused or not. Their icons can be themed as `split_right`, `split_below`, `unzoom` (and the existing `zoom`, `close`).
- A running request shows moving dots (`●••`, or `o..` with ASCII icons): in the status bar's busy block, in a running query's result tab, in a table's row count while it is being counted, and on a connection that is connecting. Nothing redraws while sqlmux is idle.

### Changed

- `tree.toggle` (`SPC b`) works like DataGrip's tool windows: from a pane it focuses the sidebar, opening it if folded; from the sidebar it folds it and goes back to the pane you came from. The `«` in the sidebar's title folds it, the `»` of the folded sidebar opens and focuses it. `tree.fold` folds without the rest, for your own key.
- A table whose first page holds fewer rows than LIMIT knows its row count at once: no `COUNT` query is sent, PAGE says `1/1`, and in a narrow pane the count stays visible as `N rows` while the other parts give way.
- The Homebrew and npm package descriptions name SQL Server.

## [0.7.0] - 2026-10-26

### Added

- **Windows**: native builds for Windows (amd64 and arm64), as zip files on the Releases page and through npm. sqlmux runs in Windows Terminal with PowerShell or cmd. Without `XDG_*` variables the configuration lives in `%APPDATA%\sqlmux` and history, state and layouts in `%LOCALAPPDATA%\sqlmux`. `password_cmd`, the external editor (`notepad` by default) and `formatprg` run through `cmd /c`; the clipboard works both ways through PowerShell; SQLite paths with a drive letter (`sqlmux C:\data\app.db`) open as files. The old console window (conhost) is not supported, and there is no Windows keychain support yet.
- **SQL Server**: `engine = "mssql"` with a `sqlserver://user@host:1433?database=app` DSN (`mssql://` works too; add `&TrustServerCertificate=true` for a self-signed server). The tree lists databases, schemas, tables, views, functions and sequences; tables page with `OFFSET … FETCH`, edit and save like the other engines; queries split into batches at `GO` lines; formatting uses the T-SQL dialect. The connection form has SQL Server too. Read-only connections are checked by sqlmux only, as SQL Server has no read-only session.
- Delete a connection from the tree: `dd` on a connection node, or *Delete…* in its right-click menu, which also has *Edit…* to open it in the connection manager.

### Changed

- Deleting a connection that has open tabs no longer refuses: the confirmation says how many tabs (and unsaved edits) go with it, and then closes them in every session.
- `C-r` in the WHERE input opens even when the table has no history yet, and says "No history".
- Filtering a Redis tree with `/` scans in bigger batches, shows "Searching…" on the database while it runs, and a new pattern cancels the scan still running.
- Exported file names replace the characters Windows does not allow (`< > : " / \ | ? *`) with `_`, on every platform: the key `hash:big` exports as `hash_big.csv`.

### Fixed

- The command palette crashed when it previewed a query while the current connection was Redis.
- The MATCH input of a Redis key tab was drawn over the chips row below it.
- The connection form's error line wraps instead of being cut off.

## [0.6.1] - 2026-10-21

### Added

- Column widths in the transposed view (`T`): `>` / `<` widen or narrow the record under the cursor (with a count), dragging the separator to the right of a record's header does the same, and `=` puts every width back to automatic, in both views. Widths are kept per tab and in layouts (`transpose_widths`, by the record's number on the page); the widths of the normal view are left as they are.

## [0.6.0] - 2026-10-20

### Added

- **Production connections**: `production = true` on a connection draws it in a warning color (theme key `production`), asks before every write (table saves, write statements, Redis write commands, deletes, renames and TTL changes in the tree) even with `confirm_dangerous = false`, and makes it read-only unless `read_only = false` is written too. The connection form has a switch for it.
- **Preview before save**: `C-s` / `:w` on a table first shows the statements it will run, values inlined (up to 20, then "… N more"), and how many rows change, are added and are deleted; `y` runs them, `n` goes back with your edits kept. Redis keys list their commands. On by default; `[grid] preview_save = false` turns it off, except on production connections.
- **External commands**: `engine = "exec"` with `dialect = "postgres"` or `"redis"` runs each statement through a command you name (`command = [..., "{sql}"]`) that prints JSON, for databases you can only reach through a CLI tool. Always read-only. See the configuration docs.
- Quick SQL (`;`) on a Redis connection runs one Redis command, with the console's completion and checks.
- `sqlmux redis://…` (and `rediss://`) opens a Redis server from the command line, like `postgres://` and `mysql://`.
- The tab bar scrolls when the tabs don't fit: the current tab stays in view, and `‹N` / `N›` at the ends show (and scroll to) the hidden ones.
- **Column widths**: `>` / `<` widen or narrow the column under the cursor (with a count, `3>`), `=` puts every column back to automatic; or drag the separator between two headers. Set widths are kept per tab and in layouts, on table and result tabs.
- **Pick databases and schemas**: right-click a connection (*Pick databases…*, MySQL *Pick schemas…*) or a PostgreSQL database (*Pick schemas…*), or *Pick databases / schemas…* in the palette, and tick which ones the tree shows and loads; the node then shows `2/7`. Saved as `databases` / `schemas` in `connections.toml`.
- External-command connections page through output their command cut short (`truncated: true`), and a PostgreSQL one shows its database as a level in the tree, like any PostgreSQL connection.
- The command palette's preview scrolls with the mouse wheel, and `C-o` moves the focus into it as a read-only text with vim movement, search, selection and copy; `esc` goes back to the list.

### Fixed

- The "Connecting to …" toast of a new session no longer stays after the connection is up.
- The palette says "1 window", not "1 windows".
- `sqlmux keys --format toml` no longer prints internal spec references in its comments.
- `sqlmux --version` from `go install` shows the module version instead of `(none, unknown)`.
- The Homebrew formula and the npm package describe all four engines; the npm packages include the MIT license; the Homebrew build is stripped, so its binary is smaller.

### Changed

- Saving a table now shows the preview first. To save straight away as before, set `[grid] preview_save = false` in `config.toml`.

## [0.5.1] - 2026-10-17

### Added

- Icons in the completion list: each candidate shows its kind (keyword, table, view, column, value, snippet; for Redis: command, subcommand, key, field, token), in the style of nvim-cmp.
- Icons in the right-click menus of the tree and the table.
- New icon names a theme can override: `keyword`, `value`, `snippet`, `open`, `edit`, `copy`, `trash`, `rename`, `hide`. They follow `icons`: with `icons = "ascii"`, completion shows a letter per kind and the menus have no icons.

## [0.5.0] - 2026-10-15

### Added

- **Redis** (6.0 or newer) as a fourth engine: `engine = "redis"` with a `redis://` or `rediss://` DSN, ACL user and password, SSH tunnel, and the connection form's Test button (sends `PING`).
- Redis tree: databases, then key namespaces split by a delimiter (`[redis] delimiter`, overridable per connection), with a type icon on each key. The tree reads keys with `SCAN` up to `[redis] scan_keys` and never runs `KEYS`; the sidebar filter `/` becomes a server-side `SCAN MATCH`. Delete, rename and set TTL from the tree; deleting a whole namespace asks first and can be stopped with `C-c`.
- Redis key tabs for string, hash, list, set, sorted set, stream and RedisJSON values. They work like table tabs: edit cells, mark rows, save with `C-s` (in a `WATCH`/`MULTI` transaction), filter with a `MATCH` pattern, export, JSON view. An info row shows type, length, TTL, encoding and memory, and lets you change the TTL, rename the key or delete it.
- Redis console: the query editor on a Redis connection runs one command per line. Replies are laid out as tables and paged in memory. Command names, subcommands, key names and hash fields complete, and a signature hint (from `COMMAND DOCS`) highlights the argument you are typing. Read-only connections refuse write commands and dangerous commands ask first, both judged by the server's own `COMMAND INFO`.
- New config keys: `[redis] delimiter`, `scan_keys`, `load_all`; per-connection `delimiter`; icon names `redis`, `str`, `hash`, `list`, `set`, `zset`, `stream`, `json`.
- JSON view: `s` jumps (flash-style) within the text, and `Y` copies the whole JSON to the clipboard.

### Changed

- Arrow keys now move the cursor in tables and the tree in vim mode too, not only with `vim = false`.

## [0.4.0] - 2026-10-14

### Added

- PostgreSQL connections list every database you can connect to: the tree is now connection → database → schema. Other databases connect when you expand them. Queries pick a database from the schema dropdown, and layouts remember it.
- SSH tunnels: `ssh = "user@host"` on a connection (also in the form's advanced fields) reaches PostgreSQL or MySQL through `ssh -W` with your `~/.ssh/config`. sqlmux cannot answer a password prompt, so load your key with `ssh-add` first.
- Signature hint in queries: inside an `INSERT … VALUES` row, an `UPDATE … SET`, a condition or a function call, a line above the cursor shows which column the current value belongs to and its type. Turn it off with `[query] hints = false`.
- Value completion: enum values, `true`/`false`, `NULL`, `DEFAULT` and `now()` where a value of that column goes. `INSERT INTO t ` completes to a column list with `VALUES ()`. Identifiers that need quoting are completed with quotes.
- Snippet-style placeholders: structure templates and the INSERT skeleton put you on the first placeholder in Select mode, so typing replaces it and `Tab`/`S-Tab` move between placeholders.
- Search in the editors (query, value editor, JSON view) highlights every match, with a `[n/total]` count. New theme keys: `search`, `search_current`.
- A rotating feature tip on the status bar (click for the next one; the palette lists them all). Turn it off with `[ui] tips = false`.

### Changed

- The value editor, the ORDER list and the COLS list size themselves to their content instead of a fixed width.
- The flash jump ends at once when the typed pattern matches nothing on screen.
- The JSON view pages and refreshes with the same keys as the table or result it shows, including any you rebound.

### Fixed

- `sqlmux --version` from Homebrew now shows the commit and date instead of `(none, unknown)`.

### Breaking

- The JSON view moved from `gJ` to `gj`.
  **Migration:** to keep `gJ`, add `"gJ" = "grid.view.json"` under both `[keys.grid]` and `[keys.jsonview]` in `keymaps.toml`.
- The actions `json.page.next`, `json.page.prev` and `json.refresh` were removed (the JSON view now uses the table's paging and refresh keys).
  **Migration:** delete any lines that bind them in `[keys.jsonview]`. To page the JSON view with other keys, rebind `grid.page.next` / `grid.page.prev` / `grid.refresh` (and `result.page.*`) instead.

## [0.3.0] - 2026-10-13

### Added

- **SQLite** without cgo: `engine = "sqlite"` with a file path as the DSN (`~` expands; query parameters such as `?mode=ro` pass through). sqlmux never creates a missing file. Tables without a primary key are edited by `rowid`.
- Open a database straight from the command line: `sqlmux ./app.db`, `sqlmux postgres://…`, `sqlmux mysql://…`. This makes a temporary connection that is not written to `connections.toml` or the keychain. `DATABASE_URL` is used when no connections are configured and no argument or `--layout` is given. `sqlmux <connection name>` works as before.
- Query history: every statement run in a query (success or failure) is appended to `history.jsonl` in the data directory. `C-r` in INSERT mode (and the palette) opens a fuzzy-searchable list for the current connection; `↵` inserts the statement. The limit is `[query] history_max` (default 1000).
- Tree objects: Indexes and Keys under each table, plus Functions and Sequences under PostgreSQL and MySQL schemas (where the engine has them). A foreign key jumps to its target table, and a function opens its definition in a new query.
- Structure templates from a table's right-click menu or the palette: add column, add index, rename, drop column, drop index. Each opens a ready-to-edit statement for your engine in a new query.
- JSON view of a table page or a result (`gJ`): read-only, highlighted, with vim movement, search and copy. Paging and filtering keep working.

### Changed

- Unknown keys in a theme file (colors or `[icon]` names) are now a warning toast instead of a startup error, so a theme written for a newer version still loads.

### Fixed

- Cancelling a MySQL statement no longer holds the connection when the server does not answer the `KILL`: sqlmux drops the connection after a few seconds and reconnects.
- After `gqq` in a query, the view no longer stays scrolled two columns to the right.

## [0.2.2] - 2026-10-12

This tag has no GitHub Release, Homebrew or npm build (it was pushed together with other tags, so no workflow ran). Install it with `go install github.com/kackerx/sqlmux/cmd/sqlmux@v0.2.2` or build from source. Everything below is also in 0.3.0.

### Added

- Multi-column sort: ORDER takes several columns, each with its own direction. Sorted columns show an arrow (and a position number) in the header. Click a header to sort by it; `gs` / `gS` do the same from the keyboard.
- Value editor (`e` or `O` on a cell): long text and JSON open in a vim editor next to the cell. JSON is formatted and highlighted; `gqq` reformats, `C-s` writes back and closes, `:w` writes back. Read-only cells and results open read-only.
- Filter by cell value (`*` or `fv`) adds `column = value` to the WHERE. `fc` hides the column. A table right-click menu offers these and more.
- Export (`ge` or `fe`, or the toolbar button): CSV or JSON, the current page or every row (streamed; `C-c` cancels). Files are written to the current directory.
- The connection form has a Test button (`C-t`) and shows an example DSN for the chosen engine.
- `SPC wn` / `SPC wp` move the current tab to the next / previous pane.
- The palette's table scope also lists your saved queries.
- Theme key `selected` for confirmed row selections (falls back to `visual`).
- npm packaging (`npx sqlmux`, `npm i -g sqlmux`). The first version actually published to npm is 0.3.0.

### Changed

- A connection that cannot be reached at startup no longer exits sqlmux. The UI opens, the connection is marked as failed, and its tabs show the error until `R` or a reconnect succeeds.
- The ORDER popup is now a list like COLS, with its own `[keys.order]` scope: `j`/`k` move, `space` adds a column or flips its direction, `a` removes it, `A` clears the sort, `↵` sorts by that column alone, and `/` filters (typing no longer filters right away).
- Completion ranks by match quality first, so typing `sele` suggests `select` ahead of a table whose name contains it.
- Unknown keys in `config.toml` and `connections.toml` (for example a misspelled `readonly`) are now reported in a toast at startup instead of being silently ignored.
- Formatting a query with many statements formats them one at a time and no longer times out on large files.

### Fixed

- Scrolling a large result (500 rows, wide or CJK/JSON values) no longer lags.
- A connection lost after `COMMIT` was sent is reported as "commit result unknown" and the table reloads, instead of being reported as rolled back.
- Two sqlmux processes no longer overwrite each other's query file: writing over a file changed on disk is refused once, like vim. History, favorites and other saved state from several processes are merged instead of the last writer winning.
- Keys and pastes no longer leak into the input under a confirmation dialog. The mouse wheel no longer scrolls the table under an open popup. Key help is no longer drawn under the palette.
- `C-h` deletes a character in every single-line input.
- Long names in dropdowns and completion lists are cut with `…` instead of overflowing.
- Layout files treat bad split values consistently instead of silently dropping a pane.

### Breaking

- `V` on a table now removes a range from the selection (UNSET mode) instead of starting a selection like `v`.
  **Migration:** to keep the old behaviour, add `"V" = "grid.select.visual"` under `[keys.grid]` in `keymaps.toml`.

## [0.2.1] - 2026-10-11

### Added

- `sqlmux --version` prints the version, commit and build date.
- Prebuilt binaries for macOS and Linux (amd64 and arm64) on each GitHub Release, with `checksums.txt`.
- Homebrew: `brew install kackerx/tap/sqlmux` (builds from source).

## [0.2.0] - 2026-10-11

### Added

- `go install github.com/kackerx/sqlmux/cmd/sqlmux@latest` now works (the module path is `github.com/kackerx/sqlmux`).
- Record selection in tables: `m` marks a record, `v` selects a range. With records selected, `d` marks them for deletion and `y` copies them as a JSON array.
- Flash-style jump: `s` in a table, result, tree or query, then type; labels appear on the matches and pressing one jumps there.
- Connection groups: `group = "prod"` on a connection (also in the form, with completion) divides the tree and the connection manager with labelled separators.
- Query results are paged: 500 rows per page, with `]` / `[` for the next and previous page, when sqlmux added the LIMIT itself.
- `J` / `K` in COLS reorder columns. The order is saved per tab and used by copy, export and the transposed view.
- A new session asks for its name first.

### Changed

- "Console" is now called "query" everywhere: the tree's Queries node, tab titles, commands and messages. Query files live in `queries/` and are named `query_<n>.sql`.
- Default rows kept per result: 500 (was 1000).
- The workspace tree lists tabs directly under each window, labelled with their pane number, without a pane level.
- The sidebar shows `«` / `»` icons to collapse and expand it instead of the `SPC b` text.
- Deleting a query file that is open closes its tabs instead of refusing.
- The WHERE history and favorites dropdown uses the full height of the pane.

### Breaking

- The console → query rename changed config keys, action ids and scope names, with **no aliases for the old names**. Old `keymaps.toml` lines that use them are skipped with a warning, an old `[console]` table in `config.toml` is ignored, and a theme with `[icon] console` fails to load.
  **Migration:**
  - `config.toml`: rename `[console]` to `[query]` (the key inside, `max_rows`, is unchanged).
  - `keymaps.toml`: rename `[keys.console]` to `[keys.query]` and `[map.console.*]` to `[map.query.*]`. Rename the actions `console.new`, `console.run`, `console.format`, `console.schema`, `console.conn`, `console.rename`, `console.tx` and `console.external` to `query.new`, `query.run` and so on.
  - Theme files: rename the `[icon]` entry `console` to `query`.
  - Nothing to do for data files: on first start sqlmux renames `consoles/` to `queries/` in the data directory (`$XDG_DATA_HOME/sqlmux`, default `~/.local/share/sqlmux`) and `console_<n>.sql` to `query_<n>.sql` (files you renamed yourself are left alone). It also updates the keys in `state.json`. Layout files that say `kind = "console"` still load. If `queries/` already exists, nothing is moved.
- `s` in a query's NORMAL mode now starts the flash jump instead of vim's `s` (substitute character). `cl` does the same as the old `s`, and VISUAL `s` is unchanged.
  **Migration:** to get vim's `s` back, add `"s" = ""` under `[keys.query]` in `keymaps.toml`.

## [0.1.0] - 2026-10-10

First release. PostgreSQL and MySQL.

Note: `go install github.com/kackerx/sqlmux/...` does not work for this version (its module path was `sqlmux`). Build from source, or use 0.2.0 or later.

### Added

- A tmux-style workspace: sessions, windows, panes and tabs, with splits, zoom, a session list and a command palette (`:`) for commands, tables, panes, windows and sessions. Save and restore workspaces as layout files; on start sqlmux restores the layout you quit from.
- Several connections per session, defined in `connections.toml` or in a form. The form stores passwords in the OS keychain; `password_cmd`, `password_env`, `${VAR}` in the DSN or a plain password also work. Edit and delete connections from a connection manager. Read-only connections, a manual transaction mode, and automatic reconnect after a dropped connection.
- Schema tree: schemas → tables and views → columns, plus the open workspace and your query files.
- Table tabs: WHERE with completion, history and favorites, ORDER, LIMIT and paging, a column picker (COLS) and a transposed view. Copy a cell or a row, or copy a row as an INSERT statement or as JSON. Look at a column's definition or the table's DDL.
- Inline editing: edit cells, add and delete rows, and save everything in one transaction. Enum, boolean and time columns get pickers, and changes can be reverted per cell.
- SQL console: a vim editor diff-tested against nvim (operators, text objects, VISUAL and block selection, registers, system clipboard, search), with completion, syntax highlighting, formatting, auto-pairs and a schema picker. Results show in a bottom pane, can be pinned and exported to CSV, and dangerous statements ask for confirmation first.
- Quick SQL (`;`) from anywhere, with a DDL preview in the palette.
- Every key can be rebound per pane type in `keymaps.toml` (`SPC` is the leader). `sqlmux keys` lists every action and `sqlmux keys --check` reports conflicts. Key help (`?`), which-key hints and full mouse support are built in, and `vim = false` gives plain editors and arrow-key navigation.
- Themes, border styles, Nerd Font or ASCII icons, and an English or Chinese UI (`language = "zh"`).

[Unreleased]: https://github.com/kackerx/sqlmux/compare/v0.8.0...HEAD
[0.8.0]: https://github.com/kackerx/sqlmux/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/kackerx/sqlmux/compare/v0.6.1...v0.7.0
[0.6.1]: https://github.com/kackerx/sqlmux/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/kackerx/sqlmux/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/kackerx/sqlmux/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/kackerx/sqlmux/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/kackerx/sqlmux/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/kackerx/sqlmux/compare/v0.2.2...v0.3.0
[0.2.2]: https://github.com/kackerx/sqlmux/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/kackerx/sqlmux/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/kackerx/sqlmux/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/kackerx/sqlmux/releases/tag/v0.1.0
