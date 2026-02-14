package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var headerStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("39")).
	PaddingBottom(1)

func bodyHeader(from, subject, date string) string {
	return headerStyle.Render(fmt.Sprintf("From: %s\nSubject: %s\nDate: %s", from, subject, trimDate(date)))
}

func bodyFooter() string {
	return footerStyle.Render("o: open in gmail  esc: back  q: quit")
}
