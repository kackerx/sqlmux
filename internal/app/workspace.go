package app

import "fmt"

// PaneKind is what a pane shows.
type PaneKind uint8

const (
	KindSchema PaneKind = iota // the ⟨0⟩ sidebar
	KindData
	KindConsole
	KindResult
)

func (k PaneKind) String() string {
	return [...]string{"schema", "data", "console", "result"}[k]
}

type Pane struct {
	ID        int // stable; the sidebar is 0
	Kind      PaneKind
	Tabs      []string
	Cur, Prev int      // tab bar * and - (T-01)
	Lines     []string // M0 placeholder content
	Scroll    int
}

// Object is the title's "· name" part: the current tab.
func (p *Pane) Object() string {
	if p.Cur < len(p.Tabs) {
		return p.Tabs[p.Cur]
	}
	return ""
}

type Window struct {
	Name     string
	TreeOpen bool
	Tree     *Pane // ⟨0⟩ sidebar, not part of the split tree (D-04)
	Root     *Node
	Focus    int // pane ID
	Zoom     int // 0 = none (P-03)
}

type Session struct {
	Name, Engine, Addr string
	Windows            []*Window
	ActiveWin          int
}

func (s *Session) Win() *Window { return s.Windows[s.ActiveWin] }

// fakeSession is M0's stand-in workspace: no database behind it.
func fakeSession() *Session {
	tree := &Pane{ID: 0, Kind: KindSchema}
	data := &Pane{ID: 1, Kind: KindData, Tabs: []string{"t_order", "t_user"}, Prev: 1, Lines: fakeRows()}
	cons := &Pane{ID: 2, Kind: KindConsole, Tabs: []string{"console_1"}, Prev: -1, Lines: fakeSQL}
	w := &Window{
		Name: "main", TreeOpen: true, Tree: tree, Focus: 1,
		Root: &Node{Split: Horiz, Ratio: 5.0 / 9, A: leaf(data), B: leaf(cons)}, // data : console = 5 : 4 (§7.8)
	}
	return &Session{
		Name: "doraemon", Engine: "postgres", Addr: "ctw@localhost:5432/doraemon",
		Windows: []*Window{w, {Name: "report", TreeOpen: true, Tree: &Pane{ID: 0, Kind: KindSchema}, Focus: 1,
			Root: leaf(&Pane{ID: 1, Kind: KindData, Tabs: []string{"t_report"}, Prev: -1})}},
	}
}

type fakeTable struct{ name, rows string }

var fakeTables = []fakeTable{
	{"agent", "124"}, {"agent_version", "530"}, {"goal", "57"},
	{"mt_task", "812"}, {"mt_task_log", "96k"}, {"schema_migrations", "88"},
	{"t_order", "1.2M"}, {"t_order_item", "3.4M"}, {"t_payment", "410k"},
	{"t_refund", "12k"}, {"t_sku", "8.1k"}, {"t_user", "38k"},
	{"t_user_address", "52k"}, {"t_user_profile", "38k"},
}

func fakeRows() []string {
	rows := []string{"WHERE deleted_at is null", "id    biz_type   status     created_at"}
	status := []string{"running", "done", "failed", "pending"}
	biz := []string{"goal", "task", "report"}
	for i := range 60 {
		rows = append(rows, fmt.Sprintf("%-5d %-10s %-10s 2026-09-21 10:%02d", 689+i, biz[i%3], status[i%4], i))
	}
	return rows
}

var fakeSQL = []string{
	"select * from mt_task",
	"where status = 'running';",
	"",
	"select id, biz_type, status",
	"from t_order",
	"where deleted_at is null",
	"order by created_at desc;",
}
