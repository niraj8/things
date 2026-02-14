package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"chuckterm/internal/gmail"
	"chuckterm/internal/model"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	gmailv1 "google.golang.org/api/gmail/v1"
)

type viewState int

const (
	viewLoading  viewState = iota
	viewAuth               // waiting for auth code input
	viewGroups             // main groups list
	viewMessages           // messages within a group
	viewBody               // single message body
)

type AppModel struct {
	// Core state
	service   *gmailv1.Service
	store     gmail.MessageStore
	configDir string
	Err       error
	status    string

	// Auth flow
	uiEvents      chan interface{}
	userResponses chan string
	textInput     textinput.Model
	authURL       string

	// View state machine
	view          viewState
	groups        []model.SenderGroup
	selectedGroup *model.SenderGroup
	selectedMsg   *model.MessageRef

	// Sub-models
	groupsList   list.Model
	messagesList list.Model
	bodyViewport viewport.Model

	// Layout
	width, height int

	// Program reference for sending messages from goroutines
	program *tea.Program
}

// SetProgram stores a reference to the tea.Program so goroutines can send
// progress messages back to the Update loop.
func (m *AppModel) SetProgram(p *tea.Program) {
	m.program = p
}

type authResultMsg struct {
	service *gmailv1.Service
	err     error
}

type authURLMsg string

type syncProgressMsg struct {
	phase string
	done  int
	total int
}

func NewAppModel(store gmail.MessageStore, configDir string) AppModel {
	ti := textinput.New()
	ti.Placeholder = "Paste auth code here"
	ti.Focus()

	gl := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	// Remove esc from the list's built-in Quit binding so it doesn't exit on home
	gl.KeyMap.Quit.SetKeys("q")

	return AppModel{
		store:        store,
		configDir:    configDir,
		status:       "Authenticating...",
		view:         viewLoading,
		uiEvents:     make(chan interface{}),
		userResponses: make(chan string),
		textInput:    ti,
		groupsList:   gl,
		messagesList: list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0),
		bodyViewport: viewport.New(0, 0),
	}
}

func (m *AppModel) Init() tea.Cmd {
	return tea.Batch(m.authenticateCmd(), textinput.Blink)
}

func (m *AppModel) authenticateCmd() tea.Cmd {
	return func() tea.Msg {
		go func() {
			svc, err := gmail.NewServiceInteractive(context.Background(), m.configDir, m.uiEvents, m.userResponses)
			m.uiEvents <- authResultMsg{service: svc, err: err}
		}()

		// The gmail auth flow sends a raw string (the auth URL) first,
		// then the goroutine above sends authResultMsg when done.
		// Convert the string to our named type so Update can match it.
		event := <-m.uiEvents
		switch v := event.(type) {
		case string:
			return authURLMsg(v)
		default:
			return event
		}
	}
}

func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		listH := msg.Height - 4 // room for footer
		m.groupsList.SetSize(msg.Width, listH)
		m.messagesList.SetSize(msg.Width, listH)
		m.bodyViewport.Width = msg.Width
		m.bodyViewport.Height = msg.Height - 6 // room for header + footer
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case authResultMsg:
		if msg.err != nil {
			m.Err = msg.err
			m.status = "Authentication failed!"
			return m, tea.Quit
		}
		m.service = msg.service
		m.status = "Syncing..."
		return m, m.syncCmd()

	case authURLMsg:
		m.authURL = string(msg)
		m.view = viewAuth
		return m, nil

	case syncProgressMsg:
		if msg.total > 0 {
			m.status = fmt.Sprintf("Syncing... %d / %d messages", msg.done, msg.total)
		} else {
			m.status = fmt.Sprintf("Syncing... %d messages", msg.done)
		}
		return m, nil

	case syncCompleteMsg:
		if msg.err != nil {
			m.Err = msg.err
			m.status = "Sync failed!"
			return m, tea.Quit
		}
		m.groups = msg.groups
		m.groupsList.SetItems(groupsToItems(m.groups))
		m.groupsList.Title = fmt.Sprintf("Inbox (%d groups)", len(m.groups))
		m.view = viewGroups
		m.status = ""
		return m, nil

	case actionResultMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("%s failed: %v", msg.action, msg.err)
		} else {
			m.status = fmt.Sprintf("%s complete", msg.action)
		}
		return m, clearStatusAfter(2 * time.Second)

	case bodyFetchedMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("Failed to load body: %v", msg.err)
			return m, nil
		}
		header := ""
		if m.selectedMsg != nil {
			header = bodyHeader(m.selectedMsg.From, m.selectedMsg.Subject, m.selectedMsg.DateRFC3339) + "\n\n"
		}
		m.bodyViewport.SetContent(header + msg.body)
		m.bodyViewport.GotoTop()
		m.view = viewBody
		m.status = ""
		return m, nil

	case statusMsg:
		if string(msg) == "" {
			m.status = ""
		}
		return m, nil
	}

	// Delegate to active sub-model
	var cmd tea.Cmd
	switch m.view {
	case viewAuth:
		m.textInput, cmd = m.textInput.Update(msg)
	case viewGroups:
		m.groupsList, cmd = m.groupsList.Update(msg)
	case viewMessages:
		m.messagesList, cmd = m.messagesList.Update(msg)
	case viewBody:
		m.bodyViewport, cmd = m.bodyViewport.Update(msg)
	}
	return m, cmd
}

func (m *AppModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global keys
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	}

	switch m.view {
	case viewAuth:
		switch key {
		case "enter":
			val := m.textInput.Value()
			m.textInput.Reset()
			return m, func() tea.Msg {
				m.userResponses <- val
				return <-m.uiEvents
			}
		case "q":
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd

	case viewGroups:
		// When the list is filtering, let it handle all keys except ctrl+c
		if m.groupsList.FilterState() == list.Filtering {
			var cmd tea.Cmd
			m.groupsList, cmd = m.groupsList.Update(msg)
			return m, cmd
		}
		switch key {
		case "q":
			return m, tea.Quit
		case "enter":
			return m.enterGroup()
		case "e":
			return m.archiveSelectedGroup()
		case "#":
			return m.trashSelectedGroup()
		case "u":
			return m.unsubscribeSelectedGroup()
		case "s":
			m.status = "Syncing..."
			return m, m.syncCmd()
		}
		var cmd tea.Cmd
		m.groupsList, cmd = m.groupsList.Update(msg)
		return m, cmd

	case viewMessages:
		switch key {
		case "q":
			return m, tea.Quit
		case "esc":
			m.view = viewGroups
			m.selectedGroup = nil
			return m, nil
		case "enter":
			return m.enterMessage()
		}
		var cmd tea.Cmd
		m.messagesList, cmd = m.messagesList.Update(msg)
		return m, cmd

	case viewBody:
		switch key {
		case "q":
			return m, tea.Quit
		case "esc":
			m.view = viewMessages
			m.selectedMsg = nil
			return m, nil
		case "o":
			if m.selectedMsg != nil {
				url := fmt.Sprintf("https://mail.google.com/mail/u/0/#inbox/%s", m.selectedMsg.ID)
				gmail.OpenBrowser(url)
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.bodyViewport, cmd = m.bodyViewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *AppModel) enterGroup() (tea.Model, tea.Cmd) {
	selected := m.groupsList.SelectedItem()
	if selected == nil {
		return m, nil
	}
	gi := selected.(groupItem)
	g := gi.SenderGroup
	m.selectedGroup = &g

	// Load full message data from store to get dates and other fields.
	var msgs []model.MessageRef
	if m.store != nil {
		loaded, err := m.store.GetMessagesByIDs(context.Background(), g.MessageIDs)
		if err == nil && len(loaded) > 0 {
			msgs = loaded
		}
	}
	if msgs == nil {
		msgs = buildMessageRefsFromGroup(g)
	}
	m.messagesList.SetItems(sortedMessageItems(msgs))
	m.messagesList.Title = fmt.Sprintf("%s — %s (%d messages)", g.DisplayName, g.Subject, g.Count)
	m.view = viewMessages
	return m, nil
}

// buildMessageRefsFromGroup creates minimal MessageRef stubs from the group's
// message IDs. Used as a fallback when the store is unavailable.
func buildMessageRefsFromGroup(g model.SenderGroup) []model.MessageRef {
	refs := make([]model.MessageRef, len(g.MessageIDs))
	for i, id := range g.MessageIDs {
		refs[i] = model.MessageRef{
			ID:      id,
			From:    g.Email,
			Subject: g.Subject,
		}
	}
	return refs
}

func (m *AppModel) enterMessage() (tea.Model, tea.Cmd) {
	selected := m.messagesList.SelectedItem()
	if selected == nil {
		return m, nil
	}
	mi := selected.(messageItem)
	ref := mi.MessageRef
	m.selectedMsg = &ref
	m.status = "Loading message..."
	return m, m.fetchBodyCmd(ref.ID)
}

func (m *AppModel) archiveSelectedGroup() (tea.Model, tea.Cmd) {
	selected := m.groupsList.SelectedItem()
	if selected == nil {
		return m, nil
	}
	gi := selected.(groupItem)
	ids := gi.MessageIDs

	// Optimistically remove from list
	idx := m.groupsList.Index()
	m.groupsList.RemoveItem(idx)
	m.status = "Archiving..."

	return m, m.archiveCmd(ids)
}

func (m *AppModel) trashSelectedGroup() (tea.Model, tea.Cmd) {
	selected := m.groupsList.SelectedItem()
	if selected == nil {
		return m, nil
	}
	gi := selected.(groupItem)
	ids := gi.MessageIDs

	idx := m.groupsList.Index()
	m.groupsList.RemoveItem(idx)
	m.status = "Trashing..."

	return m, m.trashCmd(ids)
}

func (m *AppModel) unsubscribeSelectedGroup() (tea.Model, tea.Cmd) {
	selected := m.groupsList.SelectedItem()
	if selected == nil {
		return m, nil
	}
	gi := selected.(groupItem)
	if gi.UnsubscribeURL == "" {
		m.status = "No unsubscribe URL available for this group"
		return m, clearStatusAfter(2 * time.Second)
	}

	// Open in browser (non-blocking)
	return m, func() tea.Msg {
		err := gmail.OpenUnsubscribeURL(gi.SenderGroup.UnsubscribeURL)
		if err != nil {
			return actionResultMsg{action: "Unsubscribe", err: err}
		}
		return actionResultMsg{action: "Unsubscribe (opened browser)"}
	}
}

// Commands

func (m *AppModel) syncCmd() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		progress := func(sp gmail.SyncProgress) {
			if m.program != nil {
				m.program.Send(syncProgressMsg{
					phase: sp.Phase,
					done:  sp.Done,
					total: sp.Total,
				})
			}
		}

		if m.store != nil {
			count, _ := m.store.CountMessages(ctx)
			if count > 0 {
				// Load cached groups first
				groups, err := gmail.LoadGroupsFromDB(ctx, m.store)
				if err == nil && len(groups) > 0 {
					// Background incremental sync
					go func() {
						hid, _ := m.store.GetLastHistoryID(ctx)
						if hid != "" {
							gmail.SyncSinceHistory(ctx, m.service, m.store, hid, progress)
						}
					}()
					return syncCompleteMsg{groups: groups}
				}
			}

			// Empty DB: do full scan
			err := gmail.FullScan(ctx, m.service, m.store, false, progress)
			if err != nil {
				return syncCompleteMsg{err: err}
			}
			groups, err := gmail.LoadGroupsFromDB(ctx, m.store)
			return syncCompleteMsg{groups: groups, err: err}
		}

		// No store: fetch directly (legacy path)
		emails, err := gmail.FetchInitialEmails(ctx, m.service, 200)
		if err != nil {
			return syncCompleteMsg{err: err}
		}
		groupMap := gmail.AggregateBySenderSubject(emails)
		return syncCompleteMsg{groups: gmail.SortGroups(groupMap)}
	}
}

func (m *AppModel) archiveCmd(ids []string) tea.Cmd {
	return func() tea.Msg {
		err := gmail.ArchiveMessages(context.Background(), m.service, ids)
		if err == nil && m.store != nil {
			m.store.DeleteMessages(context.Background(), ids)
		}
		return actionResultMsg{action: "Archive", err: err}
	}
}

func (m *AppModel) trashCmd(ids []string) tea.Cmd {
	return func() tea.Msg {
		err := gmail.TrashMessages(context.Background(), m.service, ids)
		if err == nil && m.store != nil {
			m.store.DeleteMessages(context.Background(), ids)
		}
		return actionResultMsg{action: "Trash", err: err}
	}
}

func (m *AppModel) fetchBodyCmd(messageID string) tea.Cmd {
	return func() tea.Msg {
		body, err := gmail.GetMessageBody(context.Background(), m.service, messageID)
		return bodyFetchedMsg{body: body, err: err}
	}
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return statusMsg("")
	})
}

// View renders the appropriate view based on current state.
func (m *AppModel) View() string {
	// Auth code input
	if m.view == viewAuth {
		return "Please open this URL in your browser to authenticate:\n\n" +
			m.authURL + "\n\n" +
			m.textInput.View()
	}

	// Error state
	if m.Err != nil {
		return "Error: " + m.Err.Error() + "\n"
	}

	// Loading/syncing
	if m.view == viewLoading {
		if m.status != "" {
			return m.status + "\n"
		}
		return "Loading...\n"
	}

	var b strings.Builder

	switch m.view {
	case viewGroups:
		b.WriteString(m.groupsList.View())
		b.WriteString("\n")
		b.WriteString(groupsFooter())
	case viewMessages:
		b.WriteString(m.messagesList.View())
		b.WriteString("\n")
		b.WriteString(messagesFooter())
	case viewBody:
		b.WriteString(m.bodyViewport.View())
		b.WriteString("\n")
		b.WriteString(bodyFooter())
	}

	if m.status != "" {
		b.WriteString("\n")
		b.WriteString(m.status)
	}

	return b.String()
}

// trimDate converts an RFC3339 timestamp to a short date string.
func trimDate(rfc3339 string) string {
	if rfc3339 == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, rfc3339); err == nil {
		return t.Format("Jan 2, 2006")
	}
	return rfc3339
}
