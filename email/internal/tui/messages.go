package tui

import "chuckterm/internal/model"

// Async message types for Bubble Tea commands.

type syncCompleteMsg struct {
	groups []model.SenderGroup
	err    error
}

type actionResultMsg struct {
	action string // "archive", "trash", "unsubscribe"
	err    error
}

type bodyFetchedMsg struct {
	body string
	err  error
}

type statusMsg string
