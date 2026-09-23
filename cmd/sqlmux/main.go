package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/app"
)

func main() {
	if _, err := tea.NewProgram(app.New("nerd")).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sqlmux:", err)
		os.Exit(1)
	}
}
