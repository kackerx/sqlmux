package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/app"
	"sqlmux/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sqlmux:", err)
		os.Exit(1)
	}
	if _, err := tea.NewProgram(app.New(cfg)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sqlmux:", err)
		os.Exit(1)
	}
}
