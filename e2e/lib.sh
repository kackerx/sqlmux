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

e2e_build() { (cd "$E2E_ROOT" && go build -o "$E2E_BIN" ./cmd/sqlmux); }

# e2e_start [-x W] [-y H] [-k] [-c FILE] CMD — fresh server, session "t" (default 160x45).
# -k: turn on tmux extended-keys before CMD starts, so it can negotiate key enhancements.
# -c: install FILE as $XDG_CONFIG_HOME/sqlmux/config.toml before CMD starts.
# -C: copy DIR's contents into $XDG_CONFIG_HOME/sqlmux/ (config.toml, themes/ …).
# connections.toml (0600) has one connection, doraemon → $SQLMUX_TEST_PG; -C can replace it.
# CMD runs under sh; when it exits the pane prints "[e2e-exit N]" and drops to
# an sh prompt, so terminal restoration can be checked afterwards.
# TERM=xterm-256color + COLORTERM: a detached tmux has no client to report RGB,
# so colorprofile would drop to 256 colors and theme hex values couldn't be checked.
e2e_start() {
  local w=160 h=45 keys=off conf= confdir=
  while [[ $1 == -[xykcC] ]]; do
    case $1 in -x) w=$2; shift ;; -y) h=$2; shift ;; -c) conf=$2; shift ;; -C) confdir=$2; shift ;; -k) keys=on ;; esac; shift
  done
  e2e_stop
  E2E_TMP=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e.XXXXXX")
  mkdir -p "$E2E_TMP"/{config/sqlmux,state,data}
  (umask 077; printf '[[connection]]\nname = "doraemon"\nengine = "postgres"\ndsn = "%s"\n' "$SQLMUX_TEST_PG" >"$E2E_TMP/config/sqlmux/connections.toml")
  [[ -n $conf ]] && cp "$conf" "$E2E_TMP/config/sqlmux/config.toml"
  [[ -n $confdir ]] && cp -Rp "$confdir"/. "$E2E_TMP/config/sqlmux/"
  t -f /dev/null set -s extended-keys "$keys" \; new-session -d -s t -x "$w" -y "$h" \
    -e XDG_CONFIG_HOME="$E2E_TMP/config" -e XDG_STATE_HOME="$E2E_TMP/state" \
    -e XDG_DATA_HOME="$E2E_TMP/data" -e COLORTERM=truecolor \
    -e E2E_CMD="$1" 'sh -c '\''export TERM=xterm-256color; eval "$E2E_CMD"; echo "[e2e-exit $?]"; exec sh'\'''
}

e2e_stop() {
  local sock; sock=$(t display -p '#{socket_path}' 2>/dev/null)
  t kill-server 2>/dev/null; [[ -n $sock ]] && rm -f "$sock"   # tmux leaves the socket file
  [[ -n $E2E_TMP ]] && rm -rf "$E2E_TMP"; E2E_TMP=
}

e2e_keys() { t send-keys -t t "$@"; }          # tmux key names: C-c Escape Enter Space ...
e2e_type() { t send-keys -t t -l "$1"; }       # literal text
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
style_has()  { [[ " $(e2e_style "$1" "$2") " == *" $3 "* ]] || { echo "  ($1,$2): $(e2e_style "$1" "$2"), want $3"; false; }; }
text_is()    { [[ $(e2e_text "$1" "$2" "$3") == "$4" ]] || { echo "  [$1..$2,$3]: '$(e2e_text "$1" "$2" "$3")', want '$4'"; false; }; }
text_has()   { local got; got=$(e2e_text "$1" "$2" "$3"); [[ $got == *"$4"* ]] || { echo "  [$1..$2,$3] '$got' lacks '$4'"; false; }; }
text_ends()  { local got; got=$(e2e_text "$1" "$2" "$3"); [[ $got == *"$4" ]] || { echo "  [$1..$2,$3] '$got' doesn't end with '$4'"; false; }; }

# ---- helpers the f0.*.sh scripts share
start()   { e2e_start "$@" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3; }   # [e2e_start options] — launch sqlmux, wait for its screen
exited()  { screen_has '[e2e-exit'; }
running() { flag_is alternate_on 1 && ! exited; }
# F1.1 starts with one empty data pane; M0's had tabs t_order and t_user. two_tabs opens
# t_user (↵), then t_order in a new tab (C-t), in the focused pane: "1:t_user- │ 2:t_order*".
two_tabs() { local t; for t in "@t_user Enter" "@t_order C-t"; do e2e_keys C-p; sleep 0.3; e2e_type "${t% *}"; sleep 0.3; e2e_keys "${t#* }"; sleep 0.3; done; }
# two_panes: split the focused pane right (SPC %) and move back — "① | ②" side by side with ① focused,
# in place of M0's data | console.
two_panes() { e2e_keys Space; e2e_type %; sleep 0.3; e2e_keys C-h; sleep 0.3; }
focused() { e2e_panes | awk '$6 == 1 && $1 != "-" { print $1 }'; }   # the focused pane's number (a folded strip or the palette box has none)
palette_open() { e2e_plain | grep -q "┌─ 命令面板"; }

e2e_done() { echo "== $E2E_PASS passed, $E2E_FAIL failed"; e2e_stop; ((E2E_FAIL == 0)); }
