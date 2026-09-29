package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

// consoleFormat lays out what gq covered in console t (§9.5): a selection
// as it is, its line break at the end left out; the statement at the
// cursor for gqq; else the statements the motion touches, the first's
// start to the last's end. It is formatted in a Cmd by sql-formatter or
// formatprg; what comes back after the text changed, or with the editor
// no longer in NORMAL, is dropped.
func (a *App) consoleFormat(t *consoleTab, f editor.FormatSpan) tea.Cmd {
	lines := t.ed.Lines()
	text := strings.Join(lines, "\n")
	from, to := offsetOf(lines, f.From), offsetOf(lines, f.To)
	stmts := sqlkit.Statements(text, sqlkit.PG)
	switch {
	case f.Selected:
		to -= len(text[from:to]) - len(strings.TrimSuffix(text[from:to], "\n"))
	case f.Current:
		i := sqlkit.StmtAt(text, stmts, from)
		if i < 0 {
			return nil
		}
		from, to = stmts[i].Start, stmts[i].End
	default:
		var in []sqlkit.Stmt
		for _, s := range stmts {
			if s.Start < max(to, from+1) && s.End > from {
				in = append(in, s)
			}
		}
		if len(in) == 0 {
			return nil
		}
		from, to = in[0].Start, in[len(in)-1].End
	}
	if from >= to {
		return nil
	}
	sql, ver := text[from:to], t.ver
	prg, opts := a.formatPrg, sqlkit.Options{KeywordCase: a.keywordCase, TabWidth: a.tabWidth}
	start, end := posOf(lines, from), posOf(lines, to)
	return func() tea.Msg {
		out, err := format(sql, prg, opts)
		return formatDone{t, ver, start, end, out, err}
	}
}

// format is sql laid out by sql-formatter, or by the command prg when it
// is set: sql on its stdin, what it writes to stdout, the first line of
// its stderr when it fails (§9.5). Nothing out is a failure too, not the
// SQL gone; past sqlkit.FormatTimeout it is sqlkit.ErrTimeout.
// ponytail: PG's SQL only; the session's dialect when MySQL consoles come (M5).
func format(sql, prg string, o sqlkit.Options) (string, error) {
	out, err := "", error(nil)
	if prg == "" {
		out, err = sqlkit.Format(sql, sqlkit.PG, o)
	} else {
		out, err = runPrg(sql, prg)
	}
	if err == nil && strings.TrimSpace(out) == "" {
		err = errors.New("没有输出")
	}
	return out, err
}

// runPrg is formatprg: what it writes, sql on its stdin, in sh -c.
func runPrg(sql, prg string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sqlkit.FormatTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", prg)
	c.WaitDelay = 100 * time.Millisecond // a child of sh keeps stdout open: it is not waited for
	var out, stderr bytes.Buffer
	c.Stdin, c.Stdout, c.Stderr = strings.NewReader(sql), &out, &stderr
	if err := c.Run(); ctx.Err() != nil {
		return "", sqlkit.ErrTimeout
	} else if err != nil {
		if first, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n"); first != "" {
			return "", errors.New(first)
		}
		return "", err
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}

func (a *App) gotFormat(m formatDone) tea.Cmd {
	switch {
	case m.t.ver != m.ver, m.t.ed.Mode() != editor.Normal: // the text changed meanwhile, or is being typed
		return nil
	case errors.Is(m.err, sqlkit.ErrTimeout):
		return a.showToast(fmt.Sprintf("格式化超时（%v）", sqlkit.FormatTimeout), toastTTL)
	case m.err != nil:
		return a.showToast("格式化失败："+m.err.Error(), toastTTL)
	}
	return a.consoleDid(m.t, m.t.ed.Replace(m.from, m.to, m.text))
}

// offsetOf is where pos is in lines joined by line breaks; posOf the
// other way.
func offsetOf(lines []string, pos editor.Pos) int {
	off := pos.Col
	for _, l := range lines[:pos.Line] {
		off += len(l) + 1
	}
	return off
}

func posOf(lines []string, off int) editor.Pos {
	for n, l := range lines {
		if off <= len(l) {
			return editor.Pos{Line: n, Col: off}
		}
		off -= len(l) + 1
	}
	return editor.Pos{Line: len(lines) - 1, Col: len(lines[len(lines)-1])}
}
