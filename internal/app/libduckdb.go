package app

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/db/duckdb"
	"github.com/kackerx/sqlmux/internal/i18n"
)

// libFetch is a download of libduckdb going on (F24.3): its bytes so far,
// shown in a toast, its cancel, C-c's, and what it is for, run again once the
// library is there.
type libFetch struct {
	d      duckdb.Download
	got    atomic.Int64
	cancel context.CancelFunc
	again  []func() tea.Cmd
	shown  string // the toast it last put up
}

// libFetched is how the download went.
type libFetched struct{ err error }

// libTick shows how far download f is.
type libTick struct{ f *libFetch }

// libTickEvery is how often its toast says so.
const libTickEvery = 500 * time.Millisecond

// mb is n bytes in MB, as the question and the toast give a size.
func mb(n int64) int64 { return (n + 1<<19) >> 20 }

// offerLibrary asks to download libduckdb when err is that there is none to
// load (§8.1「库从哪来」): yes downloads it and runs again, no runs refused,
// which says err; with a download going on, again waits for it. false is no
// question: err is not that, or another question is up.
// shortcut: a second DuckDB connection failing under the question only says
// so; it connects once it is tried again after the download.
func (a *App) offerLibrary(err error, again, refused func() tea.Cmd) bool {
	d, ok := duckdb.Offer(err)
	if ok && a.libFetch != nil {
		a.libFetch.again = append(a.libFetch.again, again)
		return true
	}
	if !ok || a.confirm != nil {
		return false
	}
	text := i18n.T("DuckDB 要用 libduckdb（v%s，约 %d MB），从 github.com/duckdb/duckdb 的 Release 下载到 %s？", duckdb.PinnedVersion, mb(d.Size), d.Path) + "\n  " + d.URL
	if d.Old != "" {
		text = i18n.T("找到的 libduckdb 是 %s，太旧了。", d.Old) + "\n" + text
	}
	a.confirm = &confirmBox{text: text, yes: i18n.T("下载"), no: refused, then: func() tea.Cmd { return a.fetchLibrary(d, again) }}
	return true
}

// fetchLibrary downloads d, the toast saying how far, then runs again.
func (a *App) fetchLibrary(d duckdb.Download, again func() tea.Cmd) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	f := &libFetch{d: d, cancel: cancel, again: []func() tea.Cmd{again}}
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

// gotLibFetched says how the download went, and once the library is there
// runs again what wanted it.
func (a *App) gotLibFetched(m libFetched) tea.Cmd {
	f := a.libFetch
	a.libFetch = nil
	switch {
	case errors.Is(m.err, context.Canceled):
		return a.showToast(i18n.T("已取消下载 libduckdb。") + duckdb.Manual())
	case m.err != nil:
		return a.showToast(i18n.T("下载 libduckdb 失败：%s。", m.err.Error()) + duckdb.Manual())
	}
	cmds := []tea.Cmd{a.showToast(i18n.T("libduckdb 已下载"))}
	for _, again := range f.again {
		cmds = append(cmds, again())
	}
	return tea.Batch(cmds...)
}
