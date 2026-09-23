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

t() { tmux -L "$E2E_SOCK" "$@"; }

e2e_build() { (cd "$E2E_ROOT" && go build -o "$E2E_BIN" ./cmd/sqlmux); }

# e2e_start [-x W] [-y H] [-k] CMD — fresh server, session "t" (default 160x45).
# -k: turn on tmux extended-keys before CMD starts, so it can negotiate key enhancements.
# CMD runs under sh; when it exits the pane prints "[e2e-exit N]" and drops to
# an sh prompt, so terminal restoration can be checked afterwards.
# TERM=xterm-256color + COLORTERM: a detached tmux has no client to report RGB,
# so colorprofile would drop to 256 colors and theme hex values couldn't be checked.
e2e_start() {
  local w=160 h=45 keys=off
  while [[ $1 == -[xyk] ]]; do
    case $1 in -x) w=$2; shift ;; -y) h=$2; shift ;; -k) keys=on ;; esac; shift
  done
  e2e_stop
  E2E_TMP=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e.XXXXXX")
  mkdir -p "$E2E_TMP"/{config,state,data}
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
e2e_resize() { t resize-window -t t -x "$1" -y "$2"; }

# Raw SGR mouse reports (DECSET 1006), injected as literal bytes.
_sgr() { t send-keys -t t -l $'\e['"<$1;$2;$3$4"; }
e2e_click()  { _sgr 0 "$1" "$2" M; _sgr 0 "$1" "$2" m; }
e2e_dclick() { e2e_click "$1" "$2"; e2e_click "$1" "$2"; }
e2e_move()   { _sgr 35 "$1" "$2" M; }                          # motion, no button
e2e_drag()   { _sgr 0 "$1" "$2" M; _sgr 32 "$3" "$4" M; _sgr 0 "$3" "$4" m; }
e2e_wheel()  { _sgr $([[ $3 == up ]] && echo 64 || echo 65) "$1" "$2" M; }  # X Y up|down

# Cell queries on the current screen, 1-based (see e2e/cells.py).
_cells() { e2e_cap -e -N | E2E_W=$(e2e_flag pane_width) python3 "$E2E_ROOT/e2e/cells.py" "$@"; }
e2e_style() { _cells style "$1" "$2"; }           # X Y -> "fg=#rrggbb bg=#rrggbb bold"
e2e_text()  { _cells text "$1" "$2" "$3"; }       # X1 X2 Y -> text of those columns
e2e_widths() { _cells width; }                     # display width of each row
e2e_plain() { _cells plain; }                      # screen text with tabs expanded
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
screen_has() { e2e_cap | grep -qF -- "$1"; }
flag_is()    { [[ $(e2e_flag "$1") == "$2" ]]; }
style_has()  { [[ " $(e2e_style "$1" "$2") " == *" $3 "* ]] || { echo "  ($1,$2): $(e2e_style "$1" "$2"), want $3"; false; }; }
text_is()    { [[ $(e2e_text "$1" "$2" "$3") == "$4" ]] || { echo "  [$1..$2,$3]: '$(e2e_text "$1" "$2" "$3")', want '$4'"; false; }; }

e2e_done() { echo "== $E2E_PASS passed, $E2E_FAIL failed"; e2e_stop; ((E2E_FAIL == 0)); }
