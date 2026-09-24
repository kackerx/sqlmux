# e2e/palette.sh — command palette helpers (F0.13+). Source after lib.sh; expects H().
# Layout (F0.17): border · scope tabs · <search icon> input · ─ · list · ─ · footer.
top()  { e2e_plain | python3 -c 'import sys; print(next((i + 1 for i, l in enumerate(sys.stdin) if "┌─ 命令面板" in l), ""))'; }
left() { local t; t=$(top); [[ -n $t ]] && e2e_find "┌─ 命令面板" "$t" | cut -d' ' -f1; }
right() { local t; t=$(top); e2e_find "┐" "$t" | tr ' ' '\n' | awk -v l="$(left)" '$1 > l { print; exit }'; }   # the box's right border column
tabs_y()  { echo $(( $(top) + 1 )); }
input_y() { echo $(( $(top) + 2 )); }
is_open() { [[ -n $(top) ]]; }
closed()  { ! is_open || { echo "  palette still open"; false; }; }
input_is() { local l; l=$(left); local got; got=$(e2e_text $((l + 4)) $(( $(right) - 2 )) "$(input_y)" | sed 's/ *$//'); [[ $got == "$1" ]] || { echo "  input: '$got', want '$1'"; false; }; }
# the list sits between the first two ─ separators under the input (F0.14 adds a scope-tab row above it)
seps() { local t l; t=$(top); l=$(left); e2e_rows $((l + 1)) $((l + 2)) $((t + 1)) $(H) $((l + 1)) | awk -F'|' -v t=$t '$1 ~ /^─/ { print NR + t }' | head -2 | tr '\n' ' '; }
list() {  # one line per list row: "<text after the icon>|<selected 0/1>" (one capture)
  local t l s1 s2; t=$(top); l=$(left); read s1 s2 <<<"$(seps)"
  ((s2 > s1 + 1)) || return 0
  # columns are padded to line up (F0.17); runs of spaces are squeezed to two so rows read "name  location  …"
  e2e_rows $((l + 4)) $(( $(right) - 1 )) $((s1 + 1)) $((s2 - 1)) $((l + 2)) |
    awk -F'|' '{ sub(/ +$/, "", $1); gsub(/   +/, "  ", $1); print $1 "|" ($2 == "#364a82" ? 1 : 0) }'
}
nrows() { list | grep -c .; }
row_has() { list | cut -d'|' -f1 | grep -qF -- "$1"; }
selected() { list | awk -F'|' '$2 == 1 { print $1 }'; }
row_y() { local s1; read s1 _ <<<"$(seps)"; echo $((s1 + $1)); }   # screen row of list row N (1-based)
foot_y() { local s1 s2; read s1 s2 <<<"$(seps)"; echo $((s2 + 1)); }
footer() { e2e_text $(($(left) + 1)) $(( $(right) - 1 )) "$(foot_y)"; }
clear_input() { local i; for ((i = 0; i < 40; i++)); do e2e_keys BSpace; done; sleep 0.2; }
pal() { e2e_keys C-p; sleep 0.3; [[ -n $1 ]] && { e2e_type "$1"; sleep 0.3; }; true; }

