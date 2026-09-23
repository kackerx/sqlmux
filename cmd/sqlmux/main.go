package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/app"
	"sqlmux/internal/config"
	"sqlmux/internal/keymap"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "keys" {
		os.Exit(keysCmd(os.Args[2:], os.Stdout, os.Stderr))
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sqlmux:", err)
		os.Exit(1)
	}
	// ponytail: problems are only reported by `sqlmux keys --check`; the startup
	// conflict overlay (§6.7) is M6.
	keys, _ := keymap.New(cfg)
	if _, err := tea.NewProgram(app.New(cfg, keys)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sqlmux:", err)
		os.Exit(1)
	}
}

// keysCmd is `sqlmux keys [--format md|toml] [--check]` (§6.7).
func keysCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sqlmux keys", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "md", "输出格式：md | toml")
	check := fs.Bool("check", false, "只检查冲突；有冲突时退出码为 1")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, "sqlmux:", err)
		return 1
	}
	keys, problems := keymap.New(cfg)
	for _, p := range problems {
		fmt.Fprintln(stderr, p)
	}
	if *check {
		if len(problems) > 0 {
			return 1
		}
		return 0
	}
	switch *format {
	case "md":
		fmt.Fprint(stdout, keys.Markdown())
	case "toml":
		fmt.Fprint(stdout, keys.TOML())
	default:
		fmt.Fprintf(stderr, "sqlmux keys：不支持的格式 %q\n", *format)
		return 2
	}
	return 0
}
