#!/usr/bin/env bash
# F0.11 Trim the default keys (specs/m0-skeleton/task.md F0.11; tech-design §6.8)
# which-key's full list is checked in f0.6.sh; TestDefaultsArePortable covers portability.
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

CFG=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX"); mkdir -p "$CFG/sqlmux"
trap 'e2e_stop; rm -rf "$CFG"' EXIT
start() { e2e_start "$@" "$E2E_BIN"; wait_for 5 flag_is alternate_on 1; sleep 0.3; }
L() { e2e_keys Space; e2e_type "$1"; sleep 0.3; }
focused() { e2e_panes | awk '$6 == 1 { print $1 }'; }
wk_row() { e2e_plain | grep "$1"; }

# ---- sqlmux keys: the removed defaults are gone
out=$(XDG_CONFIG_HOME="$CFG" "$E2E_BIN" keys)
check "keys: no SPC 0-9 / SPC h j k / SPC H J K L / SPC , / SPC & (SPC l is window.last now)" eval '! grep -E "\`SPC ([0-9hjkHJKL,&])\`" <<<"$out"'
check "keys: SPC n / p / l are window.next / prev / last" eval 'grep -qF "| normal | \`SPC n\` | window.next |" <<<"$out" && grep -qF "| normal | \`SPC p\` | window.prev |" <<<"$out" && grep -qF "| normal | \`SPC l\` | window.last |" <<<"$out"'
check "keys: session.new has no default key any more" eval '! grep -q "session.new" <<<"$out"'

# ---- the new window keys do nothing before M5
start
before=$(e2e_panes)
L n; L p; L l
check "SPC n / p / l: nothing changes, no toast" eval '[[ $(e2e_panes) == "$before" ]] && flag_is alternate_on 1 && [[ -z $(e2e_text 100 160 44 | tr -d "─┘└ ") ]]'

# ---- a key the user binds again works and shows up in which-key
printf '[keys.normal]\n"<Leader>h" = "pane.focus.left"\n' >| "$CFG/sqlmux/config.toml"
start -c "$CFG/sqlmux/config.toml"
e2e_keys C-l; sleep 0.2
L h
check 'config "<Leader>h" = "pane.focus.left": SPC h moves focus left' eval '[[ $(focused) == 1 ]]'
e2e_keys Space; sleep 0.6
check "which-key lists h → 焦点移到左边" eval 'e2e_plain | grep -q "h → 焦点移到左边"'
e2e_keys Escape

e2e_done
