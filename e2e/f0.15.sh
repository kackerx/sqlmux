#!/usr/bin/env bash
# F0.15 Sidebar: drag to resize, title shows the current schema (specs/m0-skeleton/task.md F0.15; tech-design §7.8, §7.4)
. "$(dirname "$0")/lib.sh"
e2e_build || exit 1

L() { e2e_keys Space; e2e_type "$1"; sleep 0.3; }
side_w() { e2e_panes | awk '$1 == 0 || $1 == "-" { print $4; exit }'; }        # sidebar box width
side_is() { local w; w=$(side_w); [[ $w == "$1" ]] || { echo "  sidebar width $w, want $1"; false; }; }
drag_side() { e2e_down $(( $(side_w) + 1 )) 20; e2e_drag_to "$1" 20; sleep 0.3; }   # the gap column is width + 1

# ---- drag the gap right of the sidebar
start
check "start: sidebar 32 columns, gap at column 33" side_is 32
drag_side 51
check "while dragging the width follows the pointer (pointer at 51 → 50 columns)" side_is 50
drag_side 41; e2e_up 41 20; sleep 0.3
check "release at 41 → 40 columns" side_is 40
e2e_move 70 20; sleep 0.3
check "after release, moving the pointer changes nothing" side_is 40
drag_side 3; e2e_up 3 20; sleep 0.3
check "drag far left: stops at 16" side_is 16
drag_side 150; e2e_up 150 20; sleep 0.3
check "drag far right: stops at half the window (80)" side_is 80
check "the main area still follows (data starts after the gap)" eval '[[ $(e2e_panes | awk "\$1 == 1 { print \$2 }") == 82 ]]'

# ---- the dragged width survives split, zoom, fold/unfold
drag_side 41; e2e_up 41 20; sleep 0.3
L '"'; check "after a split: still 40" side_is 40
L z; L z; check "after zoom and unzoom: still 40" side_is 40
L b; L b; check "after fold and unfold: still 40" side_is 40

# ---- no drag while folded or zoomed
L b; e2e_down 4 20; e2e_drag_to 30 20; e2e_up 30 20; sleep 0.3; L b
check "folded: the strip's gap does not drag" side_is 40
L z; e2e_down 41 20; e2e_drag_to 60 20; e2e_up 60 20; sleep 0.3; L z
check "zoomed: no drag handle" side_is 40

# ---- the limits hold at any window size
start
drag_side 81; e2e_up 81 20; sleep 0.3
check "drag to 80 on a 160-wide window" side_is 80
e2e_resize 100 45; sleep 0.4
check "shrink the window to 100: the sidebar clamps to half (50)" side_is 50
e2e_resize 160 45; sleep 0.4
check "back to 160: the dragged 80 comes back" side_is 80

# ---- the title: ⓪ <icon> <session> (F1.12: no schema dropdown any more)
start
e2e_click 10 1; sleep 0.3
check "clicking the title only focuses the sidebar (F1.12: nothing drops down)" eval '[[ $(focused) == 0 ]] && ! e2e_panes | grep -q "^- "'
start -x 60
check "narrow window: the title shrinks by §7.8, the box stays intact" eval 't=$(e2e_text 1 24 1); [[ $t == "┌─ ⓪ "*"┐" && $(e2e_find ┐ 1 | cut -d" " -f1) == 24 ]]'

e2e_done
