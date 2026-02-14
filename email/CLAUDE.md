# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

```bash
go mod tidy          # fetch/sync dependencies
go build ./...       # compile all packages
go run ./cmd/chuckterm  # run the TUI app
go test ./...        # run all tests
go test ./internal/util  # run tests for a single package
```

## Project Overview

**Chuckterm** is a Gmail inbox manager that groups messages by normalized sender+subject. It uses OAuth2 for Gmail API access, SQLite for persistence, and a Bubble Tea TUI for the interface.

**Current state:** Core Gmail integration (auth, fetch, sync) is production-ready. SQLite store implements the `MessageStore` interface. TUI has view states wired: loading → auth → groups → messages → body.

## Architecture

### Data Pipeline

1. **Auth** (`internal/gmail/client.go`): OAuth2 desktop flow using `~/.config/chuckterm/client_secret.json` and cached `token.json`. Supports both loopback redirect and manual code paste. Scopes: `gmail.readonly`, `gmail.modify`.

2. **Fetch** (`internal/gmail/fetch.go`): `FetchGroups` pages through INBOX message IDs, fans out metadata fetches to a 16-worker pool, normalizes senders, and aggregates into `map[string]*SenderGroup` keyed by `normalizedEmail||Subject`. `FetchInitialEmails` is a simpler variant that returns raw `MessageRef` slices.

3. **Sync** (`internal/gmail/sync.go`): `FullScan` does a full INBOX crawl writing batches through `MessageStore`. `SyncSinceHistory` uses the Gmail History API for incremental updates (adds/deletes/label changes). Both track a `historyId` cursor for resume.

4. **Aggregation**: `AggregateBySenderSubject` builds groups from `[]MessageRef`. `SortGroups` produces a stable slice sorted by count desc, then email asc, then subject asc.

5. **Actions** (`internal/gmail/actions.go`): `ArchiveMessages` (remove INBOX label), `TrashMessages`, `GetMessageBody` (full MIME fetch with plain text extraction).

6. **TUI** (`internal/tui/`): Bubble Tea app with view states: `viewLoading` → `viewAuth` → `viewGroups` → `viewMessages` → `viewBody`. Auth flow uses channels (`uiEvents`/`userResponses`). Sub-views in `view_groups.go`, `view_messages.go`, `view_body.go`.

### Key Types (`internal/model/types.go`)

- `MessageRef` — minimal message metadata (ID, From, Subject, DateRFC3339, ListUnsubscribe, ListUnsubscribePost)
- `SenderGroup` — aggregated group with count, date range, message IDs, unsubscribe URL; implements `list.Item` for Bubble Tea
- `FetchProgress` — progress events from fetcher to UI

### Sender Normalization (`internal/util/normalize.go`)

Parses RFC 5322 `From` headers, lowercases, strips `+alias` suffixes. Does **not** strip dots (to avoid over-grouping across providers). Falls back to comma-splitting for multi-address headers.

### Persistence (`internal/store/sqlite.go`)

`SQLiteStore` implements `MessageStore` with WAL mode. Tables: `messages` (id PK, from_email, subject, date_rfc3339, unsubscribe fields) and `metadata` (key-value for last_history_id). DB path: `~/.config/chuckterm/chuckterm.db`.

### MessageStore Interface (`internal/gmail/sync.go`)

Pluggable persistence required by sync routines. Methods: `UpsertMessages`, `DeleteMessages`, `LoadAllMessages`, `CountMessages`, `GetLastHistoryID`, `SetLastHistoryID`.

### Other Modules

- **MIME** (`internal/gmail/mime.go`): Recursive MIME tree walker, prefers `text/plain`, handles base64url decoding.
- **Unsubscribe** (`internal/gmail/unsubscribe.go`): Extracts HTTP URLs from `List-Unsubscribe` headers, opens via platform-specific browser command.

## Module

Module name is `chuckterm` (not a full URL path). Go 1.24+. Key deps: `charmbracelet/bubbletea`, `golang.org/x/oauth2`, `google.golang.org/api`, `modernc.org/sqlite`.
