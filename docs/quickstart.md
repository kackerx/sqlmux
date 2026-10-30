# Quickstart

From nothing to editing a row in a few minutes. Keys below are the defaults; the full list is in [Keys](keys.md).

## 1. Install

```sh
brew install kackerx/tap/sqlmux          # macOS / Linux, Homebrew
npm install -g sqlmux                    # or npx sqlmux; no Go needed
go install github.com/kackerx/sqlmux/cmd/sqlmux@latest   # Go 1.26+
```

Or download a binary for macOS or Linux (amd64 / arm64) from the [Releases page](https://github.com/kackerx/sqlmux/releases). Check it:

```sh
sqlmux --version
```

sqlmux uses Nerd Font icons by default. Without a Nerd Font, put `icons = "ascii"` in `~/.config/sqlmux/config.toml`.

## 2. Open a database

The fastest way is a SQLite file or a URL on the command line. Nothing is written to your config:

```sh
sqlmux ./app.db
sqlmux "postgres://me@localhost:5432/app?sslmode=disable"
sqlmux "mysql://me@localhost:3306/app"
sqlmux "redis://localhost:6379/0"
```

No database at hand? Make one:

```sh
sqlite3 demo.db "create table todo(id integer primary key, title text not null, done integer not null default 0);
                 insert into todo(title) values ('try sqlmux'), ('star it');"
sqlmux ./demo.db
```

To keep a connection, add it to `~/.config/sqlmux/connections.toml`:

```toml
[[connection]]
name         = "local"
engine       = "postgres"          # postgres | mysql | sqlite | mssql | redis
dsn          = "postgres://me@localhost:5432/app?sslmode=disable"
password_env = "PGPASSWORD"        # or password_cmd = "…", or password = "…"
```

and open it by name:

```sh
sqlmux local
sqlmux local --table orders --where "user_id = 42"   # straight to one table, already filtered
```

Running `sqlmux` with no connections opens a form instead (name, engine, host, port, user, password, database); its password goes to the OS keychain. All the connection fields are in [Configuration](config.md#connectionstoml).

## 3. Your first table

The window has three panes: the schema tree on the left, a data pane, and a query on the right.

1. Press `C-p` for the command palette, type part of a table name (`todo`) and press `↵`. The table opens as a tab in the data pane. (Or move to the tree with `C-h`, go to the table with `j` / `k` and press `↵`.)
2. Press `/` and type a condition, `done = 0`, then `↵`. `]` and `[` page; `go`, `gl`, `gc` open ORDER, LIMIT and the column picker.
3. Move to a cell with `hjkl`, press `i`, change the value and press `↵`. Edited cells are highlighted until you save; `r` undoes one.
4. Press `C-s`. Every change on the table is saved in one transaction, and the bar says `Saved 1 row(s)`.

More on a table: `o` adds a row, `dd` marks one for deletion, `gj` shows the page as JSON, `e` opens a cell in the value editor, `ge` exports CSV or JSON.

## 4. Run a query

1. `C-l` moves the focus right, to the query pane.
2. `i`, type `select done, count(*) from todo group by 1`; completion pops up as you type (`Tab` / `S-Tab` to pick, `↵` to take).
3. `Esc`, then `↵` runs the statement under the cursor. The result opens in the bottom pane.

`;` anywhere opens quick SQL in the palette for a one-off statement. Queries are saved as files in `~/.local/share/sqlmux/queries/`.

## 5. Panes, windows, sessions

`SPC` is the leader (in NORMAL mode). Press it and wait for the hints, or `?` for all keys of the pane you are in.

| Keys | |
|---|---|
| `C-h` `C-j` `C-k` `C-l` | move between panes |
| `SPC %` / `SPC "` | split right / below |
| `SPC z` | zoom the pane, again to restore |
| `SPC c` / `SPC n` / `SPC p` | new window / next / previous |
| `SPC s` | session list; `SPC )` / `SPC (` next / previous session |
| `gt` / `gT` | next / previous tab |
| `C-c` twice | quit |

When you quit, sqlmux saves the layout, and `sqlmux` with no argument brings it back.

## Next

- [Keys](keys.md): every action and its default keys.
- [Configuration](config.md): options, rebinding keys, themes, layouts.
- [Redis](redis.md): the key browser and the console.
