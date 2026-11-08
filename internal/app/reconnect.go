package app

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
)

// Send hands a message to the program from outside its Update: the
// Workers' redials happen in the requests' goroutines (§8.2「断线重连」).
// main sets it to the program's; a test sets what it likes.
var Send = func(tea.Msg) {}

// connEvent is a Worker's redial, as Worker.Notify tells it; Meta says
// which of the connection's two.
type connEvent struct {
	conn *Conn
	meta bool
	db.Event
}

// worker is a Worker over conn that dials again when the connection is
// lost, with the connection as it is now (a rename or a new address
// included: the old keychain item or address would never connect), its
// engine and lock those of from, the spec it was dialed from; the
// password is asked for again as the first time (§8.2「断线重连」).
func (c *Conn) worker(conn db.Conn, from config.Connection, meta bool) *db.Worker {
	w := db.NewWorker(conn)
	w.Redial = func(ctx context.Context) (db.Conn, error) {
		spec := c.liveSpec()
		spec.Engine = from.Engine
		secrets.Delete(spec.Name)
		cn, _, err := connectTo(ctx, spec, meta || from.ReadOnly)
		return cn, err
	}
	w.Broken = driverOf(from).Broken
	w.Notify = func(e db.Event) { Send(connEvent{c, meta, e}) }
	return w
}

// down is why c's last redial failed, Main's or Meta's; nil when both went
// through, or none was needed.
func (c *Conn) down() error {
	if c.downMain != nil {
		return c.downMain
	}
	return c.downMeta
}

// gotConnEvent takes in a redial. A failed one turns c's tree node red,
// with the reason in a toast, until a redial goes through. A good one
// says so only when the user has something to do: the request is not run
// again, or the transaction is gone, which Main's status goes back from
// (F8.19). The manual mode stays: leaving it would have the next write
// commit by itself.
func (a *App) gotConnEvent(m connEvent) tea.Cmd {
	c := m.conn
	if a.sessionOf(c) == nil { // its session closed meanwhile
		return nil
	}
	err := &c.downMain
	if m.meta {
		err = &c.downMeta
	}
	*err = m.Err
	if m.meta && m.Err == nil { // the index that could not be made may be, on the new connection (F21.7)
		c.indexErr = ""
	}
	name := c.label()
	switch {
	case m.Err != nil:
		return a.showToast(i18n.T("%s：%s", name, m.Err))
	case m.TxLost:
		c.tx = 'I'
		return tea.Batch(append(a.txEnded(c), a.showToast(i18n.T("%s：连接断开，事务已丢失", name)))...)
	case !m.Rerun:
		return a.showToast(i18n.T("%s：已重连，请重新执行", name))
	}
	return nil
}
