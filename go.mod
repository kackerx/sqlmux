module github.com/kackerx/sqlmux

go 1.26.3

require (
	charm.land/bubbletea/v2 v2.0.9
	github.com/BurntSushi/toml v1.6.0
	github.com/charmbracelet/ultraviolet v0.0.0-20260811164956-006e29f97886
	github.com/charmbracelet/x/ansi v0.11.8
	github.com/charmbracelet/x/exp/golden v0.0.0-20250806222409-83e3a29d542f
	github.com/dop251/goja v0.0.0-20260926152631-39ec2650adc9
	// ponytail: pinned; from v1.8 the text protocol's numbers come back
	// parsed, 1.5e+06 for 1500000, 1.5 for 1.50, 7 for a zerofill 007, no
	// longer the server's text (§8.1): upgrade once it can be turned off,
	// or reads raw bytes
	github.com/go-sql-driver/mysql v1.7.1
	github.com/jackc/pgx/v5 v5.11.0
	github.com/junegunn/fzf v0.74.4
)

require (
	github.com/redis/go-redis/v9 v9.23.0
	golang.org/x/sys v0.48.0
	modernc.org/sqlite v1.60.1
)

require (
	github.com/aymanbagabas/go-udiff v0.4.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/dlclark/regexp2/v2 v2.5.2 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20260802141513-ef3492d7dac3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/junegunn/go-shellwords v0.0.0-20250127100254-2aa3b3277741 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-runewidth v0.0.30 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/xo/terminfo v1.0.0 // indirect
	go.uber.org/atomic v1.12.0 // indirect
	golang.org/x/exp v0.0.0-20231006140011-7918f672742d // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.29.0 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
