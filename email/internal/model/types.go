package model

// MessageRef holds the minimal info we need for trash/undo and previews.
type MessageRef struct {
	ID                 string
	Subject            string
	DateRFC3339        string
	From               string
	ListUnsubscribe    string // List-Unsubscribe header value
	ListUnsubscribePost string // List-Unsubscribe-Post header value
}

// SenderGroup aggregates messages by normalized sender email.
type SenderGroup struct {
	Email          string
	Subject        string   // exact, case-sensitive subject used for grouping (may be empty)
	DisplayName    string
	Count          int
	Sample         string   // representative subject/snippet
	FirstDate      string   // oldest RFC3339 among grouped
	LastDate       string   // newest RFC3339 among grouped
	MessageIDs     []string // all Gmail message IDs in this group
	UnsubscribeURL string   // first HTTP unsubscribe link found in group (empty if none)
}

func (g SenderGroup) FilterValue() string { return g.DisplayName }
func (g SenderGroup) Title() string       { return g.DisplayName }
func (g SenderGroup) Description() string { return g.Subject }

// FetchProgress is sent from the fetcher to the UI as pages stream in.
type FetchProgress struct {
	AddOrUpdate []SenderGroup // incremental snapshot for replacements
	Completed   bool
	Err         error
}