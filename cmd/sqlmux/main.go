package main

import (
	"context"
	"errors"
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
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sqlmux:", err)
		os.Exit(1)
	}
}

// run is `sqlmux [connection]`. Whatever fails before the UI is up ends the
// program with the error on the terminal (§14「启动时找不到连接」).
func run(args []string) error {
	name := ""
	switch len(args) {
	case 0:
	case 1:
		name = args[0]
	default:
		return errors.New("用法：sqlmux [连接名]")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sess, warning, err := open(name)
	if err != nil {
		return err
	}
	defer sess.Close()
	// ponytail: problems are only reported by `sqlmux keys --check`; the startup
	// conflict overlay (§6.7) is M6.
	keys, _ := keymap.New(cfg)
	_, err = tea.NewProgram(app.New(cfg, keys, sess, warning)).Run()
	return err
}

// open connects to connection name, or the first one when name is "".
func open(name string) (*app.Session, string, error) {
	c, warning, err := config.LoadConnection(name)
	if err != nil {
		return nil, "", err
	}
	sess, err := app.Open(context.Background(), c)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", c.Name, err)
	}
	return sess, warning, nil
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
