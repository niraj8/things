package tui

import (
	"fmt"
	"sort"

	"chuckterm/internal/model"

	"github.com/charmbracelet/bubbles/list"
)

// messageItem wraps MessageRef for the list display.
type messageItem struct {
	model.MessageRef
}

func (m messageItem) FilterValue() string { return m.Subject }
func (m messageItem) Title() string       { return m.Subject }
func (m messageItem) Description() string {
	if m.DateRFC3339 != "" {
		return fmt.Sprintf("From: %s  Date: %s", m.From, trimDate(m.DateRFC3339))
	}
	return fmt.Sprintf("From: %s", m.From)
}

func messagesFooter() string {
	return footerStyle.Render("enter: view body  esc: back  q: quit")
}

// sortedMessageItems returns MessageRefs sorted reverse chronologically as list items.
func sortedMessageItems(refs []model.MessageRef) []list.Item {
	sorted := make([]model.MessageRef, len(refs))
	copy(sorted, refs)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].DateRFC3339 > sorted[j].DateRFC3339
	})
	items := make([]list.Item, len(sorted))
	for i, r := range sorted {
		items[i] = messageItem{r}
	}
	return items
}
