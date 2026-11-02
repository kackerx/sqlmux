# The two built-in themes for docs/config.md: the same screen in ristretto, then in tokyonight-storm
# (./record.sh themes writes renders/themes-<name>.png; copy them to assets/theme-<name>.png).
Output renders/themes.mp4
Set Shell zsh
Set FontFamily "Maple Mono NF CN"
Set FontSize 20
Set Width 1200
Set Height 676
Set Padding 12
Hide
Type `cd /tmp/sqlmux-demo && export PATH=$PWD/bin:$PATH XDG_CONFIG_HOME=$PWD/config XDG_DATA_HOME=$PWD/data XDG_STATE_HOME=$PWD/state PS1='$ ' && clear`
Enter
Type `sed -i '' 's/^theme .*/theme = "ristretto"/' config/sqlmux/config.toml && clear && sqlmux sqlite --table t_order --where "status = 'done'"`
Enter
Wait+Screen /done/
Sleep 500ms
Show
Type " b"
Sleep 300ms
Type " b"
Sleep 300ms
Ctrl+l
Sleep 200ms
Type "i"
Type "select id, status, amount"
Enter
Type "from t_order"
Enter
Type "where amount > 100"
Enter
Type "order by amount desc;"
Escape
Sleep 200ms
Enter
Sleep 1500ms
Ctrl+h
Sleep 500ms
Screenshot renders/themes-ristretto.png
Ctrl+c
Sleep 300ms
Ctrl+c
Sleep 1500ms
Type `rm -rf data state && sed -i '' 's/^theme .*/theme = "tokyonight-storm"/' config/sqlmux/config.toml && clear && sqlmux sqlite --table t_order --where "status = 'done'"`
Enter
Wait+Screen /done/
Sleep 500ms
Type " b"
Sleep 300ms
Type " b"
Sleep 300ms
Ctrl+l
Sleep 200ms
Type "i"
Type "select id, status, amount"
Enter
Type "from t_order"
Enter
Type "where amount > 100"
Enter
Type "order by amount desc;"
Escape
Sleep 200ms
Enter
Sleep 1500ms
Ctrl+h
Sleep 500ms
Screenshot renders/themes-tokyonight-storm.png
Sleep 500ms
