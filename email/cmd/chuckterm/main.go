package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"chuckterm/internal/store"
	"chuckterm/internal/tui"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot determine home directory: %v\n", err)
		os.Exit(1)
	}

	configDir := filepath.Join(home, ".config", "chuckterm")
	dbPath := filepath.Join(configDir, "chuckterm.db")
	db, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	appModel := tui.NewAppModel(db, configDir)
	p := tea.NewProgram(&appModel, tea.WithAltScreen())
	appModel.SetProgram(p)
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Alas, there's been an error: %v\n", err)
		os.Exit(1)
	}
	if m, ok := finalModel.(*tui.AppModel); ok && m.Err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", m.Err)
		os.Exit(1)
	}
}
