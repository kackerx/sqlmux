package app

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

type formatDone struct {
	t        *queryTab
	ver      int // the query's as gq was pressed
	from, to editor.Pos
	text     string
	err      error
	whole    bool // query.format's: the cursor stays where it is (F8.3)
}

// queryFormat lays out what gq covered in query t (§9.5): a selection
// as it is, its line break at the end left out; the statement at the
// cursor for gqq; else the statements the motion touches, the first's
// start to the last's end. It is formatted in a Cmd by sql-formatter or
// formatprg; what comes back after the text changed, or with the editor
// no longer in NORMAL, is dropped.
func (a *App) queryFormat(t *queryTab, f editor.FormatSpan, whole bool) tea.Cmd {
	if !t.conn.sql() {
		return a.showToast(i18n.T("这个连接的命令没有格式化"))
	}
	lines := t.ed.Lines()
	text := strings.Join(lines, "\n")
	from, to := offsetOf(lines, f.From), offsetOf(lines, f.To)
	d := t.conn.eng.Dialect()
	stmts := sqlkit.Statements(text, d)
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
		out, err := format(sql, d, prg, opts)
		return formatDone{t, ver, start, end, out, err, whole}
	}
}

// format is sql laid out by sql-formatter as d's, or by the command prg
// when it is set: sql on its stdin, what it writes to stdout, the first
// line of its stderr when it fails (§9.5). Nothing out is a failure too,
// not the SQL gone; past sqlkit.FormatTimeout it is sqlkit.ErrTimeout.
func format(sql string, d sqlkit.Dialect, prg string, o sqlkit.Options) (string, error) {
	var out string
	var err error
	if prg == "" {
		out, err = sqlkit.FormatAll(sql, d, o)
	} else {
		out, err = runPrg(sql, prg)
	}
	if err == nil && strings.TrimSpace(out) == "" {
		err = errors.New(i18n.T("没有输出"))
	}
	return out, err
}

// runPrg is formatprg: what it writes, sql on its stdin, in sh -c.
func runPrg(sql, prg string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sqlkit.FormatTimeout)
	defer cancel()
	c := config.ShellCommand(ctx, prg)
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
	case !slices.Contains(a.queries(), m.t): // closed meanwhile, or closed and opened again on its file
		return nil
	case m.t.ver != m.ver, m.t.ed.Mode() != editor.Normal && !m.t.ed.Plain(): // the text changed meanwhile, or is being typed
		return nil
	case errors.Is(m.err, sqlkit.ErrTimeout):
		return a.showToast(i18n.T("格式化超时（%v）", sqlkit.FormatTimeout))
	case m.err != nil:
		return a.showToast(i18n.T("格式化失败：%s", m.err.Error()))
	}
	cur := m.t.ed.Cursor()
	eff := m.t.ed.Replace(m.from, m.to, m.text)
	if m.whole {
		m.t.ed.Goto(cur) // as it is, or the nearest character when the text got shorter
	}
	return a.queryDid(m.t, eff)
}

// formatAll is query.format: all the statements of query t laid out as
// gq does (§9.5, F5.16).
func (a *App) formatAll(t *queryTab) tea.Cmd {
	if t.ed.Mode() != editor.Normal && !t.ed.Plain() {
		return nil
	}
	if from, to, ok := t.ed.SelectionSpan(); ok && t.ed.Plain() { // a selection: as it is, as gq in VISUAL does
		return a.queryFormat(t, editor.FormatSpan{From: from, To: to, Selected: true}, false)
	}
	lines := t.ed.Lines()
	return a.queryFormat(t, editor.FormatSpan{To: editor.Pos{Line: len(lines) - 1, Col: len(lines[len(lines)-1])}}, true)
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
