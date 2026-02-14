# Chuckterm

A terminal Gmail inbox manager that groups messages by sender and subject.

## Prerequisites

- Go 1.24+
- A Google Cloud project with the Gmail API enabled
- OAuth 2.0 desktop credentials (`client_secret.json`) from that project

## Setup

Place your OAuth credentials at:

```
~/.config/chuckterm/client_secret.json
```

## Running

```bash
go run ./cmd/chuckterm
```

The first run opens a browser for Google OAuth consent. After authorization, a token is cached at `~/.config/chuckterm/token.json` and reused for future sessions. Message metadata is stored locally in `~/.config/chuckterm/chuckterm.db` (SQLite).

## Keybindings

### Groups view

| Key     | Action                |
|---------|-----------------------|
| `enter` | Open group            |
| `e`     | Archive group         |
| `#`     | Trash group           |
| `u`     | Unsubscribe           |
| `/`     | Filter groups         |
| `q`     | Quit                  |

### Messages view

| Key     | Action    |
|---------|-----------|
| `enter` | View body |
| `esc`   | Back      |
| `q`     | Quit      |

### Body view

| Key   | Action |
|-------|--------|
| `esc` | Back   |
| `q`   | Quit   |

## Development

```bash
go build ./...       # compile all packages
go test ./...        # run all tests
go mod tidy          # fetch/sync dependencies
```

### Project structure

```
cmd/chuckterm/       CLI entrypoint (Bubble Tea app)
internal/
  gmail/             OAuth, fetch, sync, actions, MIME parsing
  model/             Shared types (MessageRef, SenderGroup)
  store/             SQLite persistence (MessageStore implementation)
  tui/               Bubble Tea views and keybindings
  util/              Sender normalization helpers
```
