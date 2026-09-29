package app

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/editor"
	"sqlmux/internal/sqlkit"
)

type formatDone struct {
	t        *consoleTab
	ver      int // the console's as gq was pressed
	from, to editor.Pos
	text     string
	err      error
}

// formatTimeout bounds formatprg (§9.5).
const formatTimeout = 5 * time.Second

// consoleFormat is gq (§9.5): what consoleSpan says, a charwise
// selection's line break at its end left out, formatted in a Cmd by
// sql-formatter or formatprg. What comes back after the text changed is
// dropped.
func (a *App) consoleFormat() tea.Cmd {
	t := consoleOf(a.focused())
	if t == nil {
		return nil
	}
	text, from, to := consoleSpan(t, "")
	if from < 0 {
		return nil
	}
	if strings.HasSuffix(text[from:to], "\n") {
		to--
	}
	sql, ver, lines := text[from:to], t.ver, t.ed.Lines()
	prg, opts := a.formatPrg, sqlkit.Options{KeywordCase: a.keywordCase, TabWidth: a.tabWidth}
	start, end := posOf(lines, from), posOf(lines, to)
	return func() tea.Msg {
		out, err := format(sql, prg, opts)
		return formatDone{t, ver, start, end, out, err}
	}
}

// format is sql laid out by sql-formatter, or by the command prg when it
// is set: sql on its stdin, what it writes to stdout, the first line of
// its stderr when it fails (§9.5).
// ponytail: PG's SQL only; the session's dialect when MySQL consoles come (M5).
func format(sql, prg string, o sqlkit.Options) (string, error) {
	if prg == "" {
		return sqlkit.Format(sql, sqlkit.PG, o)
	}
	ctx, cancel := context.WithTimeout(context.Background(), formatTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", prg)
	var out, stderr bytes.Buffer
	c.Stdin, c.Stdout, c.Stderr = strings.NewReader(sql), &out, &stderr
	if err := c.Run(); err != nil {
		if first, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n"); first != "" {
			return "", errors.New(first)
		}
		return "", err
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}

func (a *App) gotFormat(m formatDone) tea.Cmd {
	switch {
	case m.t.ver != m.ver: // the text changed meanwhile
		return nil
	case m.err != nil:
		return a.showToast("格式化失败："+m.err.Error(), toastTTL)
	}
	return a.consoleDid(m.t, m.t.ed.Replace(m.from, m.to, m.text))
}

// posOf is where offset off of lines, joined, is.
func posOf(lines []string, off int) editor.Pos {
	for n, l := range lines {
		if off <= len(l) {
			return editor.Pos{Line: n, Col: off}
		}
		off -= len(l) + 1
	}
	return editor.Pos{Line: len(lines) - 1, Col: len(lines[len(lines)-1])}
}
