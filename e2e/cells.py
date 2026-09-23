"""Query a `tmux capture-pane -p -e` dump by screen cell (1-based X, Y).

  cells.py style X Y      -> "fg=#rrggbb bg=#rrggbb bold"  (colors: hex, idx:N or -)
  cells.py text X1 X2 Y   -> text of columns X1..X2 on row Y
  cells.py width          -> display width of every row, one per line
  cells.py plain          -> the screen as plain text, tabs expanded
  cells.py find TEXT Y    -> start columns of TEXT on row Y, space-separated

Widths follow tmux: East Asian W/F = 2 columns, everything else (incl. the
Nerd Font private-use area) = 1. Tabs expand to 8-column stops.
"""
import os
import re
import sys
import unicodedata

SGR = re.compile(r"\x1b\[([0-9;:]*)m|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b\[[0-9;?]*[A-Za-z]")
WIDTH = int(os.environ.get("E2E_W", 1 << 30))  # pane width, for the last tab stop
BASIC = ["000000", "cd0000", "00cd00", "cdcd00", "0000ee", "cd00cd", "00cdcd", "e5e5e5"]


def width(ch):
    return 2 if unicodedata.east_asian_width(ch) in "WF" else 1


def color(ps, i):
    """Parse 38/48 extended color at ps[i]; return (value, next index)."""
    if ps[i + 1] == "2":
        return "#%02x%02x%02x" % tuple(int(v) for v in ps[i + 2 : i + 5]), i + 5
    return "idx:" + ps[i + 2], i + 3


def apply(st, params):
    ps = [p.split(":")[0] for p in (params.split(";") if params else ["0"])]
    i = 0
    while i < len(ps):
        p = ps[i] or "0"
        n = int(p)
        if n == 0:
            st.update(fg="-", bg="-", attrs=set())
        elif n in (38, 48):
            v, i = color(ps, i)
            st["fg" if n == 38 else "bg"] = v
            continue
        elif n == 39:
            st["fg"] = "-"
        elif n == 49:
            st["bg"] = "-"
        elif 30 <= n <= 37:
            st["fg"] = "#" + BASIC[n - 30]
        elif 40 <= n <= 47:
            st["bg"] = "#" + BASIC[n - 40]
        elif n in (1, 2, 3, 4, 5, 7, 9):
            st["attrs"].add({1: "bold", 2: "dim", 3: "italic", 4: "underline", 5: "blink", 7: "reverse", 9: "strike"}[n])
        elif n == 22:
            st["attrs"] -= {"bold", "dim"}
        elif 23 <= n <= 29:
            st["attrs"] -= {{23: "italic", 24: "underline", 25: "blink", 27: "reverse", 29: "strike"}.get(n)}
        i += 1


def grid(dump):
    rows = []
    st = {"fg": "-", "bg": "-", "attrs": set()}  # tmux carries SGR state across lines
    for line in dump.split("\n"):
        row, pos = [], 0
        for m in [*SGR.finditer(line), None]:
            for ch in line[pos : m.start() if m else len(line)]:
                cell = (ch, st["fg"], st["bg"], frozenset(st["attrs"]))
                if ch == "\t":  # tmux keeps HT the renderer used to skip blanks; stops every 8, last column caps
                    row += [(" ", *cell[1:])] * (min(len(row) // 8 * 8 + 8, WIDTH - 1) - len(row))
                else:
                    row += [cell, ("", *cell[1:])] if width(ch) == 2 else [cell]
            if m:
                if m.group(1) is not None:
                    apply(st, m.group(1))
                pos = m.end()
        rows.append(row)
    return rows


def main():
    g = grid(sys.stdin.read())
    cmd, args = sys.argv[1], [int(a) for a in sys.argv[2:] if a.isdigit()]
    if cmd == "style":
        x, y = args
        _, fg, bg, attrs = g[y - 1][x - 1] if x <= len(g[y - 1]) else (" ", "-", "-", ())
        print(" ".join([f"fg={fg}", f"bg={bg}", *sorted(attrs)]))
    elif cmd == "text":
        x1, x2, y = args
        print("".join(c[0] for c in g[y - 1][x1 - 1 : x2]))
    elif cmd == "find":
        # Wide chars own two cells; pad them with \0 so string index == column.
        pad = lambda cs: "".join(c + "\0" if width(c) == 2 else c for c in cs)
        text, row = pad(sys.argv[2]), "".join(c[0] or "" for c in g[int(sys.argv[3]) - 1])
        row = pad(row)
        print(" ".join(str(m.start() + 1) for m in re.finditer("(?=" + re.escape(text) + ")", row)))
    elif cmd == "plain":
        for row in g:
            print("".join(c[0] for c in row))
    elif cmd == "width":
        for row in g:
            print(len(row))


if __name__ == "__main__":
    main()
