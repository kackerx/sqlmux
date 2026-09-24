#!/usr/bin/env bash
# F0.17 Command palette layout (specs/m0-skeleton/task.md F0.17; tech-design §12 layout / column alignment)
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

D=$(mktemp -d "${TMPDIR:-/tmp}/sqlmux-e2e-cfg.XXXXXX")
trap 'e2e_stop; rm -rf "$D"' EXIT
H() { e2e_flag pane_height; }
. "$(dirname "$0")/palette.sh"
# column where each list row's location starts (the second column after the icon)
loc_cols() {
  local t l s1 s2; t=$(top); l=$(left); read s1 s2 <<<"$(seps)"
  e2e_rows $((l + 4)) $(( $(right) - 1 )) $((s1 + 1)) $((s2 - 1)) $((l + 2)) | python3 -B -c '
import re, sys
sys.path.insert(0, sys.argv[1]); from cells import cells_of
for r in sys.stdin:
    m = re.search(r"\S( {2,})\S", r.split("|")[0])    # end of the name column: the first run of 2+ spaces
    print(sum(w for _, w in cells_of(r[:m.end(1)])) if m else -1)' "$E2E_ROOT/e2e" | sort -u | tr '\n' ' '
}

# ---- size and position: width min(100, W-4), centred, top at (H-1)/6, at most 12 rows
start; pal
check "160×45: width 100, from column 31, top at row 8" eval '[[ $(left) == 31 && $(right) == 130 && $(top) == 8 ]]'
check "at most 12 list rows" eval '(( $(nrows) == 12 ))'
start -x 80 -y 24; pal
check "80×24: width 76, from column 3, top at row 4" eval '[[ $(left) == 3 && $(right) == 78 && $(top) == 4 ]]'

# ---- rows top to bottom: border · scope tabs · input · ─ · list · ─ · footer
start; pal
check "scope tabs sit right under the border, with prefixes" eval '[[ $(e2e_text $(($(left) + 2)) $(($(left) + 40)) $(tabs_y) | tr -s " ") == " 所有 窗口·Pane % 表 @ 命令 >"* ]]'
check "the input row starts with the search icon (info color), the cursor right after it" eval 'y=$(input_y); [[ $(e2e_text $(($(left) + 2)) $(($(left) + 2)) $y) == "$SEARCH_ICON" ]] && style_has $(($(left) + 2)) $y fg=#7dcfff && [[ $(e2e_flag cursor_y) == $((y - 1)) && $(e2e_flag cursor_x) == $(( $(left) + 3 )) ]]'
check "then a separator, the list, a separator and the footer" eval 'read s1 s2 <<<"$(seps)"; [[ $s1 == $(( $(input_y) + 1 )) && $(e2e_text $(($(left) + 1)) $(($(left) + 12)) $(foot_y)) == " ↑/↓ 移动 · "* ]]'

# ---- locations line up in one column, and stay put while scrolling
check "every row's location starts in the same column" eval 'c=$(loc_cols); [[ $c != *" "*" "* ]] || { echo "  location columns: $c"; false; }'
c0=$(loc_cols); for i in $(seq 14); do e2e_keys Down; done; sleep 0.3
check "after scrolling the columns have not moved" eval '[[ $(loc_cols) == "$c0" ]] || { echo "  $(loc_cols) vs $c0"; false; }'
e2e_keys Escape; sleep 0.2

# ---- ascii: ~ in the input row, the icon column is 2 wide for []
printf 'icons = "ascii"\n' > "$D/config.toml"; start -C "$D"; pal
check "ascii: the input row starts with ~" eval '[[ $(e2e_text $(($(left) + 2)) $(($(left) + 2)) $(input_y)) == "~" ]]'
check "ascii: window rows show [] and names still line up" eval '[[ $(e2e_text $(($(left) + 2)) $(($(left) + 3)) $(row_y 1)) == "[]" ]] && c=$(e2e_rows $(($(left) + 2)) $(( $(right) - 1 )) $(row_y 1) $(row_y 6) $(($(left) + 2)) | python3 -c "import re,sys; print(len({re.search(r\"\\S+ +(\\S)\", r).start(1) for r in sys.stdin}))"); [[ $c == 1 ]] || { echo "  name columns differ: $c"; false; }'

e2e_done
