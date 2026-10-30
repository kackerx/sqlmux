# Keys

Every default key of sqlmux v0.8.0, generated from `sqlmux keys --format toml` by `docs/keys.py`; do not edit by hand.
The leader is `<Space>`. Keys use vim notation (`<C-p>` is Ctrl+P, `<CR>` Enter, `<lt>` the `<` key, `<Leader>c` the leader then `c`).
An action with no key runs from the command palette (`<C-p>`, then `>`), or bind it yourself in `keymaps.toml`:
see [Configuration](config.md#keymapstoml).

## global

Always, in an input too

| Keys | Action | What it does |
|---|---|---|
| `<C-p>` | `palette.open` | Command palette |
| `<C-s>` | `save` | Save |
| `<C-c>` | `cancel` | Cancel / press twice to quit |

## normal

NORMAL mode, the focus in any pane

| Keys | Action | What it does |
|---|---|---|
| `<C-h>` | `pane.focus.left` | Focus left |
| `<C-j>` | `pane.focus.down` | Focus down |
| `<C-k>` | `pane.focus.up` | Focus up |
| `<C-l>` | `pane.focus.right` | Focus right |
| `<Leader>s` | `session.list` | Session list |
| `<Leader>)` | `session.next` | Next session |
| `<Leader>(` | `session.prev` | Previous session |
| `<Leader>L` | `session.last` | Last session |
| `<Leader>c` | `window.new` | New window |
| `<Leader>n` | `window.next` | Next window |
| `<Leader>p` | `window.prev` | Previous window |
| `<Leader>l` | `window.last` | Last window |
| `<Leader>%` | `pane.split.right` | Split right |
| `<Leader>"` | `pane.split.below` | Split below |
| `<Leader>z` | `pane.zoom` | Zoom / restore |
| `<Leader>x` | `pane.close` | Close pane |
| `<Leader>q` | `pane.number` | Jump by number |
| `<Leader>b` | `tree.toggle` | Open / focus / fold the sidebar |
| `<Leader>r` | `result.toggle` | Show / hide the results |
| `?`, `<Leader>?` | `keyhelp.open` | Key help |
| `<Esc>` | `pane.error.close` | Close error bar |
| `gt` | `tab.next` | Next tab |
| `gT` | `tab.prev` | Previous tab |
| `<Leader>wn` | `tab.move.next` | Move tab to the next pane |
| `<Leader>wp` | `tab.move.prev` | Move tab to the previous pane |
| — | `cell.default` | Set DEFAULT |
| — | `cell.null` | Set NULL |
| — | `conn.manage` | Manage connections… |
| — | `conn.new` | New connection… |
| — | `key.delete` | Delete the key |
| — | `key.rename` | Rename the key |
| — | `key.ttl` | Set the key's TTL |
| — | `layout.open` | Open layout |
| — | `layout.save` | Save layout as… |
| — | `palette.command` | Palette: command |
| — | `palette.session` | Command palette: sessions |
| — | `palette.tab` | Palette: Tab |
| — | `palette.table` | Palette: table |
| — | `palette.window` | Palette: window |
| — | `pane.focus.next` | Next pane |
| — | `pane.resize.down` | Resize down |
| — | `pane.resize.left` | Resize left |
| — | `pane.resize.right` | Resize right |
| — | `pane.resize.up` | Resize up |
| — | `quit` | Quit |
| — | `session.close` | Close session |
| — | `session.new` | New session |
| — | `session.rename` | Rename session… |
| — | `tab.new` | New tab |
| — | `tab.quit` | Close tab, and the pane / window with it when empty |
| — | `tab.reopen` | Reopen the closed tab |
| — | `tab.save.close` | Save and close tab |
| — | `table.alter.add_column` | Add column… |
| — | `table.alter.add_index` | Add index… |
| — | `table.alter.drop_column` | Drop column… |
| — | `table.alter.drop_index` | Drop index… |
| — | `table.alter.rename` | Rename table… |
| — | `theme.switch` | Switch theme… |
| — | `tips.next` | Next tip |
| — | `tips.show` | All tips… |
| — | `tx.commit` | Commit |
| — | `tx.mode` | Manual commit |
| — | `tx.rollback` | Roll back |
| — | `window.close` | Close window |
| — | `window.rename` | Rename window |

## landing

The landing page: a new tab, a pane with no tab

| Keys | Action | What it does |
|---|---|---|
| `t` | `tab.table` | Open a table |
| `c` | `query.new` | New query |
| `x` | `tab.close` | Close tab |
| `:` | `palette.open` | Command palette |
| `;` | `palette.sql` | Quick SQL |

## query

A query's NORMAL / VISUAL

| Keys | Action | What it does |
|---|---|---|
| `<CR>` | `query.run` | Run |
| `s` | `jump.start` | Jump to a character on the screen |
| `gs` | `query.schema` | Switch schema |
| — | `query.conn` | Switch connection… |
| — | `query.external` | Edit in $EDITOR |
| — | `query.format` | Format the whole query |
| — | `query.rename` | Rename query… |

## queryinsert

A query in INSERT

| Keys | Action | What it does |
|---|---|---|
| `<C-r>` | `query.history` | Query history… |

## jsonview

A grid's JSON view: the rest is its read only editor's

| Keys | Action | What it does |
|---|---|---|
| `<Esc>` | `json.leave` | Back to the grid |
| `q` | `json.leave q` | Back to the grid |
| `gj` | `grid.view.json` | View as JSON |
| `Y` | `json.yank` | Copy the whole JSON |
| `s` | `jump.start` | Jump to a character on the screen |

## tree

The focus on the schema tree (NORMAL)

| Keys | Action | What it does |
|---|---|---|
| `j`, `<Down>` | `tree.down` | Down |
| `k`, `<Up>` | `tree.up` | Up |
| `gg` | `tree.top` | First item |
| `G` | `tree.bottom` | Last item |
| `l`, `<Right>` | `tree.expand` | Expand |
| `h`, `<Left>` | `tree.collapse` | Collapse / up a level |
| `<CR>` | `tree.open` | Open |
| `t` | `tree.open.tab` | Open |
| `/` | `tree.filter` | Filter |
| `R` | `tree.refresh` | Refresh the tables |
| `dd` | `tree.delete` | Delete query file, table or view |
| `s` | `jump.start` | Jump to a character on the screen |
| `:` | `palette.open` | Command palette |
| `;` | `palette.sql` | Quick SQL |
| — | `tree.fold` | Fold the sidebar |
| — | `tree.pick` | Pick databases / schemas… |

## grid

The focus on the grid (NORMAL)

| Keys | Action | What it does |
|---|---|---|
| `h`, `<Left>`, `<S-Tab>` | `grid.left` | Left |
| `j`, `<Down>` | `grid.down` | Down |
| `k`, `<Up>` | `grid.up` | Up |
| `l`, `<Right>`, `<Tab>` | `grid.right` | Right |
| `gg` | `grid.top` | First row |
| `G` | `grid.bottom` | Last row |
| `0`, `^` | `grid.first` | First column |
| `<C-r>` | `where.history` | History / favorites |
| `$` | `grid.last` | Last column |
| `<CR>`, `i` | `grid.edit` | Edit cell |
| `r` | `grid.revert` | Undo this cell's change |
| `o` | `grid.row.add` | Add a row |
| `dd` | `grid.row.delete` | Mark / unmark this row for deletion |
| `R` | `grid.refresh` | Refresh |
| `T` | `grid.transpose` | Transpose |
| `x` | `tab.close` | Close tab |
| `/` | `grid.where` | WHERE condition |
| `go` | `grid.order` | ORDER |
| `gl` | `grid.limit` | LIMIT |
| `gp` | `grid.page` | PAGE |
| `gc` | `grid.cols` | COLS |
| `ge`, `fe` | `grid.export` | Export… |
| `gs` | `grid.order.cycle` | Sort by this column |
| `gS` | `grid.order.add` | Add to sorting |
| `gj` | `grid.view.json` | View as JSON |
| `*`, `fv` | `grid.filter.cell` | Filter by this value |
| `e`, `O` | `grid.value` | Open in the value editor |
| `fc` | `grid.col.hide` | Hide this column |
| `>` | `grid.col.wider` | Widen this column |
| `<lt>` | `grid.col.narrower` | Narrow this column |
| `=` | `grid.col.fit` | Column widths back to automatic |
| `]` | `grid.page.next` | Next page |
| `[` | `grid.page.prev` | Previous page |
| `s` | `jump.start` | Jump to a character on the screen |
| `m` | `grid.select.toggle` | Mark / unmark this record |
| `v` | `grid.select.visual` | VISUAL: extend the selection |
| `V` | `grid.select.unset` | Unselect a range |
| `yy` | `grid.yank.row` | Copy row |
| `yl` | `grid.yank` | Copy cell |
| `p` | `grid.paste` | Paste as a new row |
| `yi` | `grid.yank.insert` | Copy as INSERT |
| `Y` | `grid.yank.json` | Copy as JSON |
| `:` | `palette.open` | Command palette |
| `;` | `palette.sql` | Quick SQL |
| — | `grid.ddl.column` | Column definition |
| — | `grid.ddl.table` | Table DDL |
| — | `grid.order.toggle` | Reverse the sort |
| — | `grid.refresh.auto` | Auto refresh |
| — | `grid.stop` | Stop |

## result

The result area, over the grid

| Keys | Action | What it does |
|---|---|---|
| `P` | `result.pin` | Pin result |
| `q`, `x` | `result.close` | Close result |
| `R` | `result.rerun` | Run again |
| `]` | `result.page.next` | Next page of the result |
| `[` | `result.page.prev` | Previous page of the result |
| — | `result.export` | Export… |

## select

Records marked, or VISUAL on a table: over grid / result; its keys are complete, no timeoutlen to wait

| Keys | Action | What it does |
|---|---|---|
| `d` | `grid.select.delete` | Mark the selected records for deletion |
| `y` | `grid.select.yank` | Copy the selected records as JSON |
| `<Esc>` | `grid.select.clear` | Clear the selection / end VISUAL |

## wherenormal

The WHERE input in NORMAL

| Keys | Action | What it does |
|---|---|---|
| `<CR>` | `where.run` | Run |
| `/` | `where.history` | History / favorites |
| `<Esc>` | `where.leave` | Back to the grid |

## input

In any input

| Keys | Action | What it does |
|---|---|---|
| `<C-r>` | `where.history` | History / favorites |
| `<C-f>` | `where.star` | Favorite / unfavorite |

## cell

While a cell is edited

| Keys | Action | What it does |
|---|---|---|
| `<CR>` | `cell.accept` | Accept this cell |
| `<Esc>` | `cell.done` | End editing |

## keyhelp

While the key help (?) is open

| Keys | Action | What it does |
|---|---|---|
| `<Esc>` | `keyhelp.close` | Close key help |
| `<BS>` | `keyhelp.back` | Up a level |
| `<C-d>` | `keyhelp.scroll.down` | Scroll down |
| `<C-u>` | `keyhelp.scroll.up` | Scroll up |

## segments

A time cell, its segments overlay open

| Keys | Action | What it does |
|---|---|---|
| `<Tab>` | `cell.segment.next` | Next segment |
| `<S-Tab>` | `cell.segment.prev` | Previous segment |
| `<Up>` | `cell.up` | Segment up |
| `<Down>` | `cell.down` | Segment down |
| `<C-n>` | `cell.option.next` | Next option |
| `<C-p>` | `cell.option.prev` | Previous option |

## options

A cell that is not a time, its options overlay open and with options

| Keys | Action | What it does |
|---|---|---|
| `<Tab>`, `<C-n>`, `<Down>` | `cell.option.next` | Next option |
| `<S-Tab>`, `<C-p>`, `<Up>` | `cell.option.prev` | Previous option |

## newrow

While a cell of a row added and not saved is edited, over the options / segments overlay

| Keys | Action | What it does |
|---|---|---|
| `<Tab>` | `cell.field.next` | Next field |
| `<S-Tab>` | `cell.field.prev` | Previous field |

## complete

While the completion list is up (↵ and esc are the input's)

| Keys | Action | What it does |
|---|---|---|
| `<Tab>`, `<C-n>`, `<Down>` | `complete.down` | Next candidate |
| `<S-Tab>`, `<C-p>`, `<Up>` | `complete.up` | Previous candidate |

## sessions

While the session list is open

| Keys | Action | What it does |
|---|---|---|
| `j` | `sessions.down` | Down |
| `k` | `sessions.up` | Up |
| `l` | `sessions.expand` | Expand |
| `h` | `sessions.collapse` | Collapse |
| `<CR>` | `sessions.attach` | attach |
| `n` | `sessions.new` | New session |
| `x` | `sessions.kill` | Close session |
| `$` | `sessions.rename` | Rename session |
| `<Esc>` | `sessions.close` | Close list |

## dropdown

While the LIMIT or another dropdown is open

| Keys | Action | What it does |
|---|---|---|
| `<Up>`, `<C-p>` | `dropdown.up` | Up |
| `<Down>`, `<C-n>` | `dropdown.down` | Down |
| `<CR>` | `dropdown.select` | Select |
| `<Esc>` | `dropdown.close` | Close dropdown |

## cols

While the COLS list is open

| Keys | Action | What it does |
|---|---|---|
| `j` | `cols.down` | Down |
| `k` | `cols.up` | Up |
| `J` | `cols.move.down` | Move the column down |
| `K` | `cols.move.up` | Move the column up |
| `<Space>` | `cols.toggle` | Show / hide this column |
| `a` | `cols.all` | Show all |
| `A` | `cols.none` | Hide all |
| `/` | `cols.filter` | Filter columns |
| `<Esc>` | `cols.close` | Close COLS |

## order

While the ORDER list is open

| Keys | Action | What it does |
|---|---|---|
| `j` | `order.down` | Down |
| `k` | `order.up` | Up |
| `<Space>` | `order.toggle` | Add to the sort, or reverse it |
| `a` | `order.remove` | Remove from the sort |
| `A` | `order.clear` | Clear the sort |
| `<CR>` | `order.only` | Sort by this column alone |
| `/` | `order.filter` | Filter columns |
| `<Esc>` | `order.close` | Close ORDER |

## where

While the WHERE history / favorites dropdown is open

| Keys | Action | What it does |
|---|---|---|
| `<C-n>`, `<Tab>`, `<Down>` | `where.down` | Down |
| `<C-p>`, `<S-Tab>`, `<Up>` | `where.up` | Up |
| `<CR>` | `where.apply` | Run the WHERE |
| `<C-e>` | `where.edit` | Put in the input |
| `<C-f>` | `where.star` | Favorite / unfavorite |
| `<Esc>` | `where.close` | Close dropdown |

## jump

A flash-style jump going on: other characters are the pattern, a label jumps

| Keys | Action | What it does |
|---|---|---|
| `<Esc>` | `jump.cancel` | Leave the jump |
| `<BS>` | `jump.back` | Jump: delete a character |
| `<CR>` | `jump.confirm` | Jump to the nearest match |

## form

While the connection form is open

| Keys | Action | What it does |
|---|---|---|
| `<Tab>` | `form.next` | Next item |
| `<S-Tab>` | `form.prev` | Previous item |
| `<CR>`, `<C-s>` | `form.save` | Save |
| `<C-t>` | `form.test` | Test connection |
| `<Esc>` | `form.cancel` | Cancel |

## groups

The connection form's group field with its list of groups; the form's keys come after

| Keys | Action | What it does |
|---|---|---|
| `<Tab>` | `form.group.tab` | Take the group / next field |
| `<CR>` | `form.group.enter` | Take the group / save |
| `<Down>`, `<C-n>` | `form.group.next` | Next group |
| `<Up>`, `<C-p>` | `form.group.prev` | Previous group |
| `<Esc>` | `form.group.close` | Close the group list |

## conns

While the connection manager's list has the keys

| Keys | Action | What it does |
|---|---|---|
| `j`, `<Down>` | `conns.down` | Down |
| `k`, `<Up>` | `conns.up` | Up |
| `<Tab>`, `l`, `<Right>`, `<CR>` | `conns.form` | Edit |
| `x` | `conns.delete` | Delete connection |
| `<C-s>` | `form.save` | Save |
| `<C-t>` | `form.test` | Test connection |
| `<Esc>` | `conns.close` | Close |

## palettepreview

While the palette's preview has the focus: the rest is its read only editor's

| Keys | Action | What it does |
|---|---|---|
| `<Esc>` | `palette.preview.leave` | Back to the list |

## palette

While the palette is open

| Keys | Action | What it does |
|---|---|---|
| `<Up>`, `<C-p>` | `palette.up` | Up |
| `<Down>`, `<C-n>` | `palette.down` | Down |
| `<CR>` | `palette.run` | Run |
| `<Esc>` | `palette.close` | Close the palette |
| `<Tab>` | `palette.scope.next` | Next scope |
| `<S-Tab>` | `palette.scope.prev` | Previous scope |
| `<C-t>` | `palette.open.tab` | Open |
| `<C-y>` | `quicksql.copy` | Copy result |
| `<C-f>` | `palette.preview.down` | Scroll the preview down |
| `<C-b>` | `palette.preview.up` | Scroll the preview up |
| `<C-o>` | `palette.preview.focus` | Focus the preview |
| `<C-e>` | `quicksql.edit` | Edit in a query |

## ddl

While the table's DDL is shown

| Keys | Action | What it does |
|---|---|---|
| `j`, `<Down>` | `ddl.down` | Down |
| `k`, `<Up>` | `ddl.up` | Up |
| `<C-d>` | `ddl.page.down` | Scroll down |
| `<C-u>` | `ddl.page.up` | Scroll up |
| `gg` | `ddl.top` | To the top |
| `G` | `ddl.bottom` | To the bottom |
| `<Esc>` | `ddl.close` | Close |

## menu

While a right-click menu is open

| Keys | Action | What it does |
|---|---|---|
| `j`, `<Down>` | `menu.down` | Down |
| `k`, `<Up>` | `menu.up` | Up |
| `<CR>` | `menu.run` | Run |
| `<Esc>` | `menu.close` | Close menu |

## value

While the value editor's box is open: C-s writes back and closes, the rest is its vim

| Keys | Action | What it does |
|---|---|---|
| `<C-s>` | `value.save` | Write back and close |
| `<Esc>` | `value.leave` | Value editor: leave |

## confirm

While a confirm box is open

| Keys | Action | What it does |
|---|---|---|
| `y`, `<CR>` | `confirm.yes` | OK |
| `n`, `<Esc>` | `confirm.no` | Cancel |

