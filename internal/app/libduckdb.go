package app

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/db/duckdb"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/ui"
)

// libFetch is a download of libduckdb going on (F24.3): its bytes so far,
// shown in a toast, its cancel, C-c's, and what it is for, run again once the
// library is there.
type libFetch struct {
	d      duckdb.Download
	got    atomic.Int64
	cancel context.CancelFunc
	wants  []libWant
	shown  string // the toast it last put up
}

// libWant is what a download is for: again once the library is there,
// refused, nil for nothing, when it is not.
type libWant struct{ again, refused func() tea.Cmd }

// libFetched is how the download went.
type libFetched struct{ err error }

// libTick shows how far download f is.
type libTick struct{ f *libFetch }

// libTickEvery is how often its toast says so.
const libTickEvery = 500 * time.Millisecond

// mb is n bytes in MB, as the question and the toast give a size.
func mb(n int64) int64 { return (n + 1<<19) >> 20 }

// offerLibrary asks to download libduckdb when err is that there is none to
// load (§8.1「库从哪来」): yes downloads it and runs again, no runs refused;
// with a download going on, they wait for it. false is no question: err is
// not that, or another question is up. The places looked in go to the log.
// shortcut: a second DuckDB connection failing under the question only says
// so; it connects once it is tried again after the download.
func (a *App) offerLibrary(err error, again, refused func() tea.Cmd) bool {
	d, ok := duckdb.Offer(err)
	if nf := (*duckdb.NotFound)(nil); errors.As(err, &nf) && len(nf.Tried) > 0 {
		w := a.win()
		w.log = append(w.log, ui.LogLine{Head: clock().Format("15:04:05") + "  libduckdb  ", Tail: i18n.T("找过：%s", strings.Join(nf.Tried, ", ")), Err: true})
	}
	if ok && a.libFetch != nil {
		a.libFetch.wants = append(a.libFetch.wants, libWant{again, refused})
		return true
	}
	if !ok || a.confirm != nil {
		return false
	}
	text := i18n.T("DuckDB 要用 libduckdb（v%s，约 %d MB），从 github.com/duckdb/duckdb 的 Release 下载到 %s？", duckdb.PinnedVersion, mb(d.Size), d.Path) + "\n  " + d.URL
	if d.Old != "" {
		text = i18n.T("找到的 libduckdb 是 %s，太旧了。", d.Old) + "\n" + text
	}
	a.confirm = &confirmBox{text: text, yes: i18n.T("下载"), no: refused, then: func() tea.Cmd { return a.fetchLibrary(d, libWant{again, refused}) }}
	return true
}

// fetchLibrary downloads d, the toast saying how far, for w.
func (a *App) fetchLibrary(d duckdb.Download, w libWant) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	f := &libFetch{d: d, cancel: cancel, wants: []libWant{w}}
	a.libFetch = f
	return tea.Batch(a.libProgress(), func() tea.Msg {
		defer cancel()
		return libFetched{duckdb.Fetch(ctx, d, &f.got)}
	})
}

// libProgress is the toast of the download going on, unless another toast is
// up, and the next one's tick.
func (a *App) libProgress() tea.Cmd {
	f := a.libFetch
	if f == nil {
		return nil
	}
	tick := tea.Tick(libTickEvery, func(time.Time) tea.Msg { return libTick{f} })
	if a.toast != "" && a.toast != f.shown { // a save's error, say: it stays its time
		return tick
	}
	s := i18n.T("正在下载 libduckdb v%s：%d / %d MB", duckdb.PinnedVersion, mb(f.got.Load()), mb(f.d.Size))
	if k := a.keys.Hint("cancel", "global"); k != "" {
		s += " · " + i18n.T("%s 取消", k)
	}
	show := a.showToast(s)
	f.shown = a.toast
	return tea.Batch(show, tick)
}

// gotLibFetched says how the download went, and runs again what wanted it
// once the library is there, its refused when it is not (the download's word
// last, the one shown).
func (a *App) gotLibFetched(m libFetched) tea.Cmd {
	f := a.libFetch
	a.libFetch = nil
	var cmds []tea.Cmd
	for _, w := range f.wants {
		run := w.again
		if m.err != nil {
			run = w.refused
		}
		if run != nil {
			cmds = append(cmds, run())
		}
	}
	switch {
	case errors.Is(m.err, context.Canceled):
		cmds = append(cmds, a.showToast(i18n.T("已取消下载 libduckdb")+duckdb.Manual()))
	case m.err != nil:
		cmds = append(cmds, a.showToast(i18n.T("下载 libduckdb 失败：%s", m.err.Error())+duckdb.Manual()))
	default:
		cmds = append(cmds, a.showToast(i18n.T("libduckdb 已下载")))
	}
	return tea.Batch(cmds...)
}
