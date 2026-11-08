# Redis

sqlmux opens Redis 6.0 or newer next to your SQL databases: a key browser in the tree, each key as a table you can edit, and a console that knows the server's commands.

## Connect

`sqlmux redis://localhost:6379/0` opens a server straight from the command line, without saving anything. To keep a connection, add it to `~/.config/sqlmux/connections.toml` (or use the connection form):

```toml
[[connection]]
name     = "cache"
engine   = "redis"
dsn      = "redis://localhost:6379/0"   # rediss:// for TLS; user and password can go in the URL (ACL)
password_cmd = "pass show redis/cache"  # optional, as for any connection
delimiter = ":"                          # optional, overrides [redis] delimiter for this connection
```

`password_cmd`, `password_env`, the keychain, `read_only` and `ssh` work as for SQL connections ([Configuration](config.md#connectionstoml)).

## The tree

Connection → databases → namespaces → keys. Only non-empty databases (from `INFO keyspace`) and the one in the DSN are listed, each with its `DBSIZE`. Key names are split into namespaces by `:` (`[redis] delimiter`), and each key has an icon for its type.

- sqlmux reads keys with `SCAN`, never `KEYS`. A scan stops after `[redis] scan_keys` keys (10000); the end of the list then says how many were scanned.
- `/` in the tree filters on the server: `SCAN MATCH` with your pattern (`*` is added on both sides when you type none). The text you typed is highlighted in each name. While you browse the results, `esc` goes back to the filter input; a second `esc` clears it.
- `R` scans the database again.
- Right-click (or the palette) on a key: open, delete, rename, set TTL. `dd` deletes a key after a confirmation. On a namespace, *Delete N keys…* removes everything under it with `UNLINK`, after you confirm the count; `C-c` stops it.

## Key tabs

`↵` on a key opens it as a tab. A line above the table shows its type, length, TTL, encoding and memory, with buttons to change the TTL, rename and delete.

| Type | Columns | Editing |
|---|---|---|
| string | `value` | edit; `e` opens the value editor, which formats JSON |
| hash | `field`, `value` | edit, add (`o`), delete (`dd`) |
| list | `index`, `value` | edit values, add at the end, delete |
| set | `member` | edit, add, delete |
| zset | `member`, `score` | edit, add, delete |
| stream | `id`, then one column per field | delete entries |
| RedisJSON | the document in the value editor | edit (`JSON.SET`) |

Edits work as on a SQL table: change cells, then `C-s` writes them all in one `MULTI` / `EXEC`. The key is `WATCH`ed and the cells you changed are read back first, so if someone else changed it since you loaded the page, nothing is written and the tab reloads.

Hashes, sets and sorted sets up to `[redis] load_all` entries (5000) are read whole, so you can sort, filter and page them locally. Longer ones are read with `HSCAN` / `SSCAN` (forward paging only, `MATCH` filter) or `ZRANGE` by offset. `gj` shows the key as JSON (a hash as an object, a list or set as an array), and `ge` exports it.

## Console

A query tab on a Redis connection is a console: one command per line, quoted like `redis-cli` (`"…"` with `\n` and `\xHH` escapes, `'…'`). Lines starting with `#` are comments.

- `↵` in NORMAL mode runs the line under the cursor; a VISUAL selection runs every selected line, one after another, stopping at the first error.
- Command names, subcommands, key names, hash fields and tokens such as `EX` / `NX` complete as you type. Key names match fuzzily, anywhere in the name, as table names do: `expert` finds `i18n_agent:expert:zh`. For that, the console's first completion in a database indexes its key names in the background (up to `[redis] index_keys`, 100000). Until the index is ready, the list says "Building the index" at its foot and matches prefixes; if it cannot be built, that line says why, and `R` in the tree tries again. The index is saved to disk (`[redis] index_cache`), so on the next start completion uses it at once while a fresh one is built ("Updating the index"). Writes from sqlmux keep the index current; keys other clients write appear after `R` in the tree. Above the cursor, a signature line from `COMMAND DOCS` (Redis 7+) shows the arguments and highlights the one you are on.
- Replies become tables: a scalar in one `value` cell, an array one row per item, a map as `field` / `value`, `ZRANGE … WITHSCORES` as `member` / `score`, nil as NULL. Errors go to the error bar.
- The database dropdown in the console's title switches databases; typing `SELECT n` in the console only changes that one connection.
- Commands run on a read-only connection are refused when the server's `COMMAND INFO` marks them as writes; commands the server marks `@dangerous` ask first (`confirm_dangerous`).

## Quick commands

`;` on a Redis connection runs one command in the palette, with the console's completion and its read-only and dangerous-command checks; the reply shows as a table under it. `C-t` opens the reply as a result tab, `C-e` moves the command into a console.

## The palette

The palette (`C-p`, and its `@` table scope) lists Redis keys next to tables and queries: the keys of the databases already indexed by the console, plus the keys the tree has loaded. `↵` opens the key's tab. The palette never scans a database on its own.

## Not yet

Cluster, Sentinel, Pub/Sub and `MONITOR` are not supported.
