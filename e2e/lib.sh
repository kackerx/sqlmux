# e2e/lib.sh — tmux black-box helpers. Usage: `. e2e/lib.sh` from a test script.
# Private socket only (never the user's tmux server); XDG_* go to a temp dir.
# Coordinates for mouse helpers are 1-based (column X, row Y), like SGR mouse.

# One socket per run: worker, reviewer and tester run these scripts concurrently,
# and a shared socket lets one run's kill-server take down another's session.
E2E_SOCK=sqlmux-e2e-$$
trap 'e2e_stop' EXIT
E2E_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
E2E_BIN=${E2E_BIN:-$E2E_ROOT/bin/sqlmux}
E2E_PASS=0 E2E_FAIL=0
# sqlmux won't start without a connection (§14): every run needs the seeded PG from
# docker-compose.yml (AGENTS.md「集成测试环境」) and SQLMUX_TEST_PG pointing at it.
[[ -n $SQLMUX_TEST_PG ]] || { echo "e2e: SQLMUX_TEST_PG is not set (see docker-compose.yml)" >&2; trap - EXIT; exit 1; }
# The user's PG* settings and ~/.pgpass stay out of every run.
unset $(env | sed -n 's/^\(PG[A-Z_]*\)=.*/\1/p'); export PGPASSFILE=/nonexistent/pgpass

t() { tmux -L "$E2E_SOCK" "$@"; }

# e2e_build: go build into E2E_BIN — skipped when E2E_NOBUILD is set, so a full run builds once and every
# script shares that binary (AGENTS.md「tester 的规则」: one build per round, not one per script).
e2e_build() { [[ -n $E2E_NOBUILD ]] && { [[ -x $E2E_BIN ]] || { echo "e2e: E2E_NOBUILD but no $E2E_BIN" >&2; return 1; }; return 0; }
  (cd "$E2E_ROOT" && go build -o "$E2E_BIN" ./cmd/sqlmux); }

# e2e_start [-x W] [-y H] [-k] [-c FILE] CMD — fresh server, session "t" (default 160x45).
# -k: turn on tmux extended-keys before CMD starts, so it can negotiate key enhancements.
# -c: install FILE as $XDG_CONFIG_HOME/sqlmux/config.toml before CMD starts.
# -C: copy DIR's contents into $XDG_CONFIG_HOME/sqlmux/ (config.toml, themes/ …).
# -S: use DIR (the caller's, kept across starts) as $XDG_STATE_HOME instead of a fresh one.
# -D: the same for $XDG_DATA_HOME (consoles' files, §11).
# connections.toml (0600) has one connection, doraemon → $SQLMUX_TEST_PG; -C can replace it.
# CMD runs under sh in the temp dir, so what sqlmux writes to its working directory (a result's
# CSV, §11) stays out of the worktree; when it exits the pane prints "[e2e-exit N]" and drops to
# an sh prompt there, so terminal restoration can be checked afterwards. That shell has no history
# file: if CMD dies early, the keys a script goes on sending land there, not in the user's ~/.bash_history.
# TERM=xterm-256color + COLORTERM: a detached tmux has no client to report RGB,
# so colorprofile would drop to 256 colors and theme hex values couldn't be checked.
e2e_start() {
  local w=160 h=45 keys=off conf= confdir= state= data=
  while [[ $1 == -[xykcCSD] ]]; do
    case $1 in -x) w=$2; shift ;; -y) h=$2; shift ;; -c) conf=$2; shift ;; -C) confdir=$2; shift ;; -S) state=$2; shift ;; -D) data=$2; shift ;; -k) keys=on ;; esac; shift
  done
  _e2e_kill
  E2E_TMP=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e.XXXXXX")
  mkdir -p "$E2E_TMP"/{config/sqlmux,state,data}
  (umask 077; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$SQLMUX_TEST_PG" >"$E2E_TMP/config/sqlmux/connections.toml")
  [[ -n $conf ]] && cp "$conf" "$E2E_TMP/config/sqlmux/config.toml"
  [[ -n $confdir ]] && cp -Rp "$confdir"/. "$E2E_TMP/config/sqlmux/"
  t -f /dev/null set -s extended-keys "$keys" \; new-session -d -s t -x "$w" -y "$h" \
    -e XDG_CONFIG_HOME="$E2E_TMP/config" -e XDG_STATE_HOME="${state:-$E2E_TMP/state}" \
    -e XDG_DATA_HOME="${data:-$E2E_TMP/data}" -e COLORTERM=truecolor -e HISTFILE=/dev/null -e E2E_DIR="$E2E_TMP" \
    -e E2E_CMD="$1" 'sh -c '\''export TERM=xterm-256color; cd "$E2E_DIR"; eval "$E2E_CMD"; echo "[e2e-exit $?]"; exec sh'\'''
}

_e2e_kill() {
  local sock; sock=$(t display -p '#{socket_path}' 2>/dev/null)
  t kill-server 2>/dev/null; [[ -n $sock ]] && rm -f "$sock"   # tmux leaves the socket file
  [[ -n $E2E_TMP ]] && rm -rf "$E2E_TMP"; E2E_TMP=
}
# e2e_stop: what every script's EXIT trap runs — the tmux server, and e2e_own_db's database.
e2e_stop() {
  _e2e_kill
  [[ -n $E2E_DB ]] && PGOPTIONS="-c client_min_messages=warning" psql "$SQLMUX_TEST_PG" -qAt -c "drop database if exists sqlmux_e2e_$$ with (force)"; E2E_DB=
}

# e2e_own_db: a private copy of the seed, sqlmux_e2e_<pid>, for tests that lock tables or write
# (AGENTS.md「集成测试环境」: the shared sqlmux database stays read-only). E2E_DB is its DSN.
e2e_own_db() {
  E2E_DB=$(python3 -c 'import sys, urllib.parse as u; print(u.urlsplit(sys.argv[1])._replace(path="/" + sys.argv[2]).geturl())' "$SQLMUX_TEST_PG" "sqlmux_e2e_$$")
  PGOPTIONS="-c client_min_messages=warning" psql "$SQLMUX_TEST_PG" -qAt -c "drop database if exists sqlmux_e2e_$$ with (force)" -c "create database sqlmux_e2e_$$" &&
    psql "$E2E_DB" -q -v ON_ERROR_STOP=1 -f "$E2E_ROOT/testdata/seed/pg.sql" >/dev/null
}

e2e_keys() { t send-keys -t t "$@"; }          # tmux key names: C-c Escape Enter Space ...
e2e_type() { t send-keys -t t -l -- "$1"; }    # literal text (-- so "-0x1.8" isn't read as a flag)
e2e_cap()  { t capture-pane -p -t t "$@"; }    # add -e for SGR colors
e2e_flag() { t display -p -t t "#{$1}"; }      # e.g. alternate_on cursor_flag mouse_all_flag
e2e_record() { t pipe-pane -t t -o "cat >> '$1'"; }   # FILE — append everything the program writes to the pane
e2e_resize() { t resize-window -t t -x "$1" -y "$2"; }

# Raw SGR mouse reports (DECSET 1006), injected as literal bytes.
_sgr() { t send-keys -t t -l $'\e['"<$1;$2;$3$4"; }
e2e_click()  { _sgr 0 "$1" "$2" M; _sgr 0 "$1" "$2" m; }
e2e_dclick() { e2e_click "$1" "$2"; e2e_click "$1" "$2"; }
e2e_move()   { _sgr 35 "$1" "$2" M; }                          # motion, no button
e2e_drag()   { _sgr 0 "$1" "$2" M; _sgr 32 "$3" "$4" M; _sgr 0 "$3" "$4" m; }
e2e_down()   { _sgr 0 "$1" "$2" M; }                           # press left button
e2e_drag_to() { _sgr 32 "$1" "$2" M; }                         # motion with left button held
e2e_up()     { _sgr 0 "$1" "$2" m; }                           # release
e2e_wheel()  { _sgr $([[ $3 == up ]] && echo 64 || echo 65) "$1" "$2" M; }  # X Y up|down

# Cell queries on the current screen, 1-based (see e2e/cells.py).
_cells() { e2e_cap -e -N | E2E_W=$(e2e_flag pane_width) python3 "$E2E_ROOT/e2e/cells.py" "$@"; }
e2e_style() { _cells style "$1" "$2"; }           # X Y -> "fg=#rrggbb bg=#rrggbb bold"
e2e_text()  { _cells text "$1" "$2" "$3"; }       # X1 X2 Y -> text of those columns
e2e_widths() { _cells width; }                     # display width of each row
e2e_plain() { _cells plain; }                      # screen text with tabs expanded
e2e_rows()  { _cells rows "$@"; }                  # X1 X2 Y1 Y2 BX -> "text|bg" per row
e2e_panes() { _cells panes; }                      # "N X Y W H focused" per pane box
e2e_find()  { _cells find "$1" "$2"; }            # TEXT Y -> start columns

# wait_for SECS CMD... — poll CMD every 0.1s until it succeeds or SECS pass.
wait_for() {
  local i n=$(($1 * 10)); shift
  for ((i = 0; i < n; i++)); do "$@" && return 0; sleep 0.1; done
  return 1
}

# check DESC CMD... — run CMD as an assertion; on failure dump the screen.
check() {
  local _desc=$1; shift
  if "$@"; then E2E_PASS=$((E2E_PASS + 1)); echo "PASS $_desc"
  else E2E_FAIL=$((E2E_FAIL + 1)); echo "FAIL $_desc"; e2e_cap | sed 's/^/  | /'; fi
}
ASC=$(printf '\xef\x85\xa0') DESC=$(printf '\xef\x85\xa1')   # ORDER's sort_asc U+F160 / sort_desc U+F161 (F1.8)
SEARCH_ICON=$(printf '\xef\x80\x82')   # U+F002: the status bar's palette entry (icon only under nerd icons, F0.16)
search_col() { e2e_find "$SEARCH_ICON" "$(e2e_flag pane_height)" | cut -d' ' -f1; }
# column where the pending key sequence starts: " <search> " then " <keyboard> <seq>"
pending_col() { echo $(( $(search_col) + 5 )); }
strwidth()   { python3 "$E2E_ROOT/e2e/cells.py" strwidth "$1"; }
# toast_is TEXT — §7.8：状态栏上一行、靠右，warn 字、#292e42 底、左右各 1 列内边距
toast_is() {
  local y=$(($(e2e_flag pane_height) - 1)) c; c=$(e2e_find "$1" "$y")
  [[ -n $c ]] && ((c > $(e2e_flag pane_width) / 2)) || { echo "  no toast '$1' on row $y: $(e2e_text 1 "$(e2e_flag pane_width)" "$y")"; return 1; }
  style_has "$c" "$y" fg=#e0af68 && style_has $((c - 1)) "$y" bg=#292e42 && style_has $((c + $(strwidth "$1"))) "$y" bg=#292e42
}
screen_has() { e2e_cap | grep -qF -- "$1"; }
flag_is()    { [[ $(e2e_flag "$1") == "$2" ]]; }
# style_has X Y STYLE: X / Y must be real columns / rows (1-based) — an empty find turns into 0 and would read some other cell
style_has()  { [[ $1 =~ ^[1-9][0-9]*$ && $2 =~ ^[1-9][0-9]*$ ]] || { echo "  style_has: no cell at ($1,$2)"; return 1; }
  [[ " $(e2e_style "$1" "$2") " == *" $3 "* ]] || { echo "  ($1,$2): $(e2e_style "$1" "$2"), want $3"; false; }; }
text_is()    { [[ $(e2e_text "$1" "$2" "$3") == "$4" ]] || { echo "  [$1..$2,$3]: '$(e2e_text "$1" "$2" "$3")', want '$4'"; false; }; }
text_has()   { local got; got=$(e2e_text "$1" "$2" "$3"); [[ $got == *"$4"* ]] || { echo "  [$1..$2,$3] '$got' lacks '$4'"; false; }; }
text_ends()  { local got; got=$(e2e_text "$1" "$2" "$3"); [[ $got == *"$4" ]] || { echo "  [$1..$2,$3] '$got' doesn't end with '$4'"; false; }; }

# ---- helpers the f0.*.sh scripts share
# start [e2e_start options] — launch sqlmux, wait for its screen; with SOLO set, then solo.
start() {
  e2e_start "$@" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1 || echo "e2e: sqlmux did not start: $(e2e_plain | grep -m1 'sqlmux:')"; sleep 0.3
  if [[ -n $SOLO ]]; then solo; fi
}
# solo: close ② console_1 of M3's default layout (§5), so ① spans the width right of the sidebar
# with the focus, as scripts written for M1's one-pane layout assume (m3-console/task.md「M1 / M2 的 e2e」).
solo() { e2e_keys C-l; sleep 0.3; e2e_keys Space; e2e_type x; wait_for 3 eval '[[ -z $(e2e_panes | awk "\$1 == 2") ]]'; sleep 0.2; }
exited()  { screen_has '[e2e-exit'; }
running() { flag_is alternate_on 1 && ! exited; }
# F1.1 starts with one empty data pane; M0's had tabs t_order and t_user. two_tabs opens
# t_user (↵), then t_order in a new tab (C-t), in the focused pane: "1:t_user- │ 2:t_order*".
two_tabs() { local t; for t in "@t_user Enter" "@t_order C-t"; do e2e_keys C-p; sleep 0.3; e2e_type "${t% *}"; sleep 0.3; e2e_keys "${t#* }"; sleep 0.3; done; }
# two_panes: split the focused pane right (SPC %) and move back — "① | ②" side by side with ① focused,
# in place of M0's data | console.
two_panes() { e2e_keys Space; e2e_type %; sleep 0.3; e2e_keys C-h; sleep 0.3; }
# e2e_lock TABLE: another session holds an ACCESS EXCLUSIVE lock on TABLE in E2E_DB until e2e_unlock,
# so the next query on it waits and pg_stat_activity shows its text (e2e_waiting APP).
e2e_lock() {
  psql "$E2E_DB" -q -c "begin" -c "lock table $1 in access exclusive mode" -c "select pg_sleep(60)" -c "commit" >/dev/null 2>&1 & E2E_LOCKER=$!
  wait_for 5 eval "[[ \$(psql \"\$E2E_DB\" -At -c \"select count(*) from pg_locks l join pg_class c on c.oid = l.relation where c.relname = '$1' and l.mode = 'AccessExclusiveLock' and l.granted\") == 1 ]]"
}
e2e_unlock() {
  psql "$E2E_DB" -qAt -c "select pg_terminate_backend(pid) from pg_stat_activity where datname = current_database() and query like '%pg_sleep(60)%' and pid <> pg_backend_pid()" >/dev/null
  wait "$E2E_LOCKER" 2>/dev/null
}
e2e_waiting() { psql "$E2E_DB" -At -c "select query from pg_stat_activity where application_name = '$1' and wait_event_type = 'Lock'" | tr '\n' ' ' | sed 's/ *$//'; }

# grid_y: the row of the first table's header rule (┼); its header is one above, its first data row one below.
grid_y() { e2e_plain | awk '/┼/ { print NR; exit }'; }
# open_table NAME: open a table from the palette into the focused data pane (↵) and wait for its grid.
open_table() { e2e_keys C-p; sleep 0.3; e2e_type "@$1"; sleep 0.3; e2e_keys Enter; wait_for 5 eval '[[ -n $(grid_y) && $(e2e_plain | head -1) == *" $1 ─"* ]]'; }
# noicon: drop the nerd-font icons (U+Fxxx) and the space after each: " 1:<table> t_order* " → " 1:t_order* " (F3.7 tab bars).
noicon() { LC_ALL=C sed $'s/\xef[\x80-\xbf][\x80-\xbf] //g'; }
focused() { e2e_panes | awk '$6 == 1 && $1 != "-" { print $1 }'; }   # the focused pane's number (a folded strip or the palette box has none)
# Panes and their tab bars (any layout). geom N: pane N's "X Y W H"; the tab bar is its content's last row.
geom() { e2e_panes | awk -v n="$1" '$1 == n { print $2, $3, $4, $5 }'; }
geom_is() { local g; g=$(geom "$1"); [[ $g == "$2" ]] || { echo "  ⟨$1⟩ at [$g], want [$2]"; false; }; }
focus_is() { local f; f=$(focused); [[ $f == "$1" ]] || { echo "  focused ⟨${f}⟩, want ⟨$1⟩"; false; }; }
tab_y() { local g; g=($(geom "$1")); echo $((g[1] + g[3] - 2)); }
tabbar() { local g; g=($(geom "$1")); e2e_text $((g[0] + 1)) $((g[0] + g[2] - 2)) $((g[1] + g[3] - 2)) | noicon | sed 's/ │ +.*//; s/^ //'; }   # "1:t_order- │ 2:t_user*"
tabs_are() { [[ $(tabbar "$1") == "$2" ]] || { echo "  ⟨$1⟩ tabs: '$(tabbar "$1")', want '$2'"; false; }; }
# tab_x N TEXT: the column of TEXT on pane N's tab bar; "2:t_user" looks for "2:", as an icon sits between the number and the name (F3.7)
tab_x() { local g t=$2; [[ $t == *:* ]] && t=${t%%:*}:; g=($(geom "$1")); e2e_find "$t" $((g[1] + g[3] - 2)) | tr ' ' '\n' | awk -v l=${g[0]} -v r=$((g[0] + g[2])) '$1 > l && $1 < r { print; exit }'; }
click_tab() { e2e_click "$(tab_x "$1" "$2")" "$(tab_y "$1")"; sleep 0.4; }   # N TEXT: click a tab (or +) on pane N's tab bar
palette_open() { e2e_plain | grep -q "┌─ 命令面板"; }
# errbar N: pane N's error bar (§7.8「错误栏」, F3.20) — the error_bg rows right above its tab bar, one line each, trailing
# blanks cut (the first ends in " ×", the gap before it squeezed); nothing when it has none. errbar_y N: its first row.
ERROR_BG=#3b2230
errbar_y() { local g y; g=($(geom "$1")); for ((y = g[1] + g[3] - 3; y > g[1]; y--)); do [[ $(e2e_style $((g[0] + 1)) $y) == *bg=$ERROR_BG* ]] || break; done; echo $((y + 1)); }
errbar() { local g y; g=($(geom "$1")); for ((y = $(errbar_y "$1"); y <= g[1] + g[3] - 3; y++)); do e2e_text $((g[0] + 2)) $((g[0] + g[2] - 2)) $y | sed 's/ *$//; s/  *×$/ ×/'; done; }
errbar_is() { local got; got=$(errbar "$1" | head -1); [[ $got == "$2" ]] || { echo "  ⟨$1⟩ error bar: '$got', want '$2'"; false; }; }   # N FIRST-ROW (with its " ×")

# ---- the f1.*.sh scripts' data pane: 160x45, ① alone right of the sidebar (x 35..159)
H() { e2e_flag pane_height; }
key() { e2e_keys "$@"; sleep 0.3; }
bar() { e2e_text 1 "$(e2e_flag pane_width)" "$(H)"; }                    # the status bar
pos() { bar | grep -oE ' [0-9]+,[0-9]+ ' | tr -d ' '; }                 # its 行,列
pos_is() { [[ $(pos) == "$1" ]] || { echo "  行,列 '$(pos)', want '$1'"; false; }; }
mode_is() { [[ $(bar) == *" $1 " ]] || { echo "  mode: $(e2e_text 120 160 "$(H)")"; false; }; }
where_in() { e2e_text 42 157 2 | sed 's/ *$//'; }                       # the WHERE input (▾ at its right end)
qb() { e2e_text 35 159 3; }                                             # the query bar's second row
qb_has() { [[ $(qb) == *"$1"* ]] || { echo "  query bar: '$(qb)', want '$1'"; false; }; }
cnt() { qb | grep -oE 'auto · [^ ]+ 行' | awk '{ print $3 }'; }
settled() { [[ $(bar) != *busy* && $(cnt) != "…" ]]; }
clear_in() { local i; for ((i = 0; i < 80; i++)); do e2e_keys BSpace; done; sleep 0.2; }
hy() { echo $(( $(grid_y) - 1 )); }                                     # the grid's header row

e2e_done() { echo "== $E2E_PASS passed, $E2E_FAIL failed"; e2e_stop; ((E2E_FAIL == 0)); }
