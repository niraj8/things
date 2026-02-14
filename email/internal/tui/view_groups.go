package tui

import (
	"fmt"

	"chuckterm/internal/model"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

// groupItem wraps SenderGroup to customize list display.
type groupItem struct {
	model.SenderGroup
}

func (g groupItem) FilterValue() string { return g.DisplayName + " " + g.Subject }
func (g groupItem) Title() string {
	indicator := " "
	if g.UnsubscribeURL != "" {
		indicator = "@ "
	}
	return fmt.Sprintf("%s%s (%d)", indicator, g.DisplayName, g.Count)
}
func (g groupItem) Description() string {
	if g.Subject != "" {
		return g.Subject
	}
	return g.Sample
}

var footerStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("241")).
	PaddingTop(1)

func groupsFooter() string {
	return footerStyle.Render("enter: open  e: archive  #: trash  u: unsubscribe  s: sync  q: quit  @=unsubscribe available")
}

func groupsToItems(groups []model.SenderGroup) []list.Item {
	items := make([]list.Item, len(groups))
	for i, g := range groups {
		items[i] = groupItem{g}
	}
	return items
}
