package gmail

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"chuckterm/internal/model"
	"chuckterm/internal/util"

	gmailv1 "google.golang.org/api/gmail/v1"
)

type SyncProgress struct {
	Done  int
	Total int
	Phase string
}

// MessageStore declares the persistence capabilities required by the historical
// sync routines. Implementations can back this with SQLite, BoltDB, cloud
// storage, or an in-memory cache depending on the rewrite strategy.
type MessageStore interface {
	UpsertMessages(ctx context.Context, msgs []model.MessageRef) error
	DeleteMessages(ctx context.Context, ids []string) error
	LoadAllMessages(ctx context.Context) ([]model.MessageRef, error)
	CountMessages(ctx context.Context) (int, error)
	GetMessagesByIDs(ctx context.Context, ids []string) ([]model.MessageRef, error)
	GetLastHistoryID(ctx context.Context) (string, error)
	SetLastHistoryID(ctx context.Context, historyID string) error
}

// LoadGroupsFromDB loads cached messages from DB and returns sender+subject groups sorted.
func LoadGroupsFromDB(ctx context.Context, store MessageStore) ([]model.SenderGroup, error) {
	if store == nil {
		return nil, fmt.Errorf("message store is required")
	}
	msgs, err := store.LoadAllMessages(ctx)
	if err != nil {
		return nil, err
	}
	m := AggregateBySenderSubject(msgs)
	return SortGroups(m), nil
}

// FullScan performs a first-time scan of INBOX headers and stores them in the cache.
// It also captures the current mailbox historyId for future incremental sync.
func FullScan(ctx context.Context, svc *gmailv1.Service, store MessageStore, includeSpamTrash bool, progress func(SyncProgress)) error {
	if store == nil {
		return fmt.Errorf("message store is required")
	}
	user := "me"
	if progress != nil {
		progress(SyncProgress{Phase: "fullscan-start"})
	}

	// Step 1: get current mailbox historyId
	hid, err := currentHistoryID(ctx, svc)
	if err != nil {
		return fmt.Errorf("get current historyId: %w", err)
	}

	// Step 2: list all message IDs (INBOX only for MVP)
	list := svc.Users.Messages.List(user).
		IncludeSpamTrash(includeSpamTrash).
		MaxResults(500).
		LabelIds("INBOX")

	type job struct{ id string }
	type result struct {
		ref model.MessageRef
		err error
	}

	jobs := make(chan job, 1000)
	results := make(chan result, 1000)

	// Step 3: worker pool to fetch metadata
	const workerCount = 16
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for j := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				msg, err := svc.Users.Messages.Get(user, j.id).
					Format("metadata").
					MetadataHeaders("From", "Subject", "Date", "List-Unsubscribe", "List-Unsubscribe-Post").
					Do()
				if err != nil {
					results <- result{err: err}
					continue
				}
				var from, subject, date, listUnsub, listUnsubPost string
				for _, h := range msg.Payload.Headers {
					switch strings.ToLower(h.Name) {
					case "from":
						from = h.Value
					case "subject":
						subject = h.Value
					case "date":
						date = h.Value
					case "list-unsubscribe":
						listUnsub = h.Value
					case "list-unsubscribe-post":
						listUnsubPost = h.Value
					}
				}
				rfc3339 := parseDateRFC3339(date)
				results <- result{ref: model.MessageRef{
					ID:                  msg.Id,
					From:                util.NormalizeSender(from),
					Subject:             subject,
					DateRFC3339:         rfc3339,
					ListUnsubscribe:     listUnsub,
					ListUnsubscribePost: listUnsubPost,
				}}
			}
		}()
	}

	// Step 4: page through list, queue jobs
	go func() {
		defer close(jobs)
		pageToken := ""
		first := true
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			call := list
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			resp, err := call.Do()
			if err != nil {
				// surface as synthetic result error
				results <- result{err: fmt.Errorf("list messages: %w", err)}
				return
			}
			// Emit total estimate once if available
			if first && progress != nil {
				first = false
				if resp.ResultSizeEstimate > 0 {
					progress(SyncProgress{Phase: "fullscan-start", Total: int(resp.ResultSizeEstimate), Done: 0})
				}
			}
			for _, m := range resp.Messages {
				jobs <- job{id: m.Id}
			}
			if resp.NextPageToken == "" {
				return
			}
			pageToken = resp.NextPageToken
		}
	}()

	// Step 5: collect into batches and write to DB
	var collectErr error
	const batch = 500
	buf := make([]model.MessageRef, 0, batch)
	done := 0

	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		if r.err != nil {
			// Record first error but continue
			if collectErr == nil {
				collectErr = r.err
			}
			continue
		}
		if r.ref.From == "" {
			// skip unparsable sender
			continue
		}
		buf = append(buf, r.ref)
		done++
		if progress != nil && done%50 == 0 {
			progress(SyncProgress{Phase: "fullscan", Done: done})
		}
		if len(buf) >= batch {
			if err := store.UpsertMessages(ctx, buf); err != nil && collectErr == nil {
				collectErr = err
			}
			if progress != nil {
				progress(SyncProgress{Phase: "fullscan", Done: done})
			}
			buf = buf[:0]
		}
	}
	if len(buf) > 0 {
		if err := store.UpsertMessages(ctx, buf); err != nil && collectErr == nil {
			collectErr = err
		}
		if progress != nil {
			progress(SyncProgress{Phase: "fullscan", Done: done})
		}
	}

	// Step 6: store historyId if we processed anything, even if some errors occurred
	if done > 0 {
		if err := store.SetLastHistoryID(ctx, hid); err != nil && collectErr == nil {
			collectErr = err
		}
	}

	if progress != nil {
		progress(SyncProgress{Phase: "fullscan-done", Done: done})
	}
	return collectErr
}

// SyncSinceHistory performs an incremental sync using Gmail History API starting from lastHistoryID.
// It applies INBOX message additions/removals to the local cache and updates the stored historyId.
func SyncSinceHistory(ctx context.Context, svc *gmailv1.Service, store MessageStore, lastHistoryID string, progress func(SyncProgress)) error {
	if store == nil {
		return fmt.Errorf("message store is required")
	}
	if strings.TrimSpace(lastHistoryID) == "" {
		return fmt.Errorf("lastHistoryID is required")
	}
	user := "me"

	addSet := make(map[string]struct{})
	delSet := make(map[string]struct{})

	// Page through history records
	startID, err := strconv.ParseUint(lastHistoryID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid lastHistoryID %q: %w", lastHistoryID, err)
	}
	call := svc.Users.History.List(user).StartHistoryId(startID).
		MaxResults(500).LabelId("INBOX")
	var newestHistoryID string

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		resp, err := call.Do()
		if err != nil {
			return fmt.Errorf("history list: %w", err)
		}
		if resp.HistoryId != 0 {
			newestHistoryID = fmt.Sprintf("%d", resp.HistoryId)
		}
		for _, h := range resp.History {
			if h.Id != 0 {
				newestHistoryID = fmt.Sprintf("%d", h.Id)
			}
			// Messages added to INBOX
			for _, ma := range h.MessagesAdded {
				if ma.Message == nil {
					continue
				}
				// consider as add if message has INBOX
				if hasLabel(ma.Message, "INBOX") {
					addSet[ma.Message.Id] = struct{}{}
					delete(delSet, ma.Message.Id)
				}
			}
			// Messages removed from INBOX (treat as deletion from cache)
			for _, md := range h.MessagesDeleted {
				if md.Message == nil {
					continue
				}
				delSet[md.Message.Id] = struct{}{}
				delete(addSet, md.Message.Id)
			}
			// Labels changes
			for _, la := range h.LabelsAdded {
				if la.Message == nil {
					continue
				}
				if contains(la.LabelIds, "INBOX") {
					addSet[la.Message.Id] = struct{}{}
					delete(delSet, la.Message.Id)
				}
			}
			for _, lr := range h.LabelsRemoved {
				if lr.Message == nil {
					continue
				}
				if contains(lr.LabelIds, "INBOX") {
					delSet[lr.Message.Id] = struct{}{}
					delete(addSet, lr.Message.Id)
				}
			}
		}

		if resp.NextPageToken == "" {
			break
		}
		call = call.PageToken(resp.NextPageToken)
	}

	// Compute totals for progress and start
	total := len(addSet) + len(delSet)
	if progress != nil {
		progress(SyncProgress{Phase: "history-start", Total: total, Done: 0})
	}

	// Fetch metadata for adds
	addIDs := keys(addSet)
	if len(addIDs) > 0 {
		msgs, err := fetchMetadataBatch(ctx, svc, addIDs)
		if err != nil {
			return err
		}
		if err := store.UpsertMessages(ctx, msgs); err != nil {
			return err
		}
		if progress != nil {
			progress(SyncProgress{Phase: "history", Total: total, Done: len(addIDs)})
		}
	}

	// Apply deletions
	if len(delSet) > 0 {
		if err := store.DeleteMessages(ctx, keys(delSet)); err != nil {
			return err
		}
		if progress != nil {
			progress(SyncProgress{Phase: "history", Total: total, Done: total})
		}
	}

	// Update last historyId
	if newestHistoryID == "" {
		// Fallback to current mailbox historyId if API did not return any
		hid, err := currentHistoryID(ctx, svc)
		if err != nil {
			return fmt.Errorf("get current historyId: %w", err)
		}
		newestHistoryID = hid
	}
	if err := store.SetLastHistoryID(ctx, newestHistoryID); err != nil {
		return err
	}

	if progress != nil {
		progress(SyncProgress{Phase: "history-done", Total: total, Done: total})
	}
	return nil
}

func fetchMetadataBatch(ctx context.Context, svc *gmailv1.Service, ids []string) ([]model.MessageRef, error) {
	user := "me"
	type job struct{ id string }
	type result struct {
		ref model.MessageRef
		err error
	}
	jobs := make(chan job, len(ids))
	results := make(chan result, len(ids))

	const workerCount = 8
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for j := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				msg, err := svc.Users.Messages.Get(user, j.id).
					Format("metadata").
					MetadataHeaders("From", "Subject", "Date", "List-Unsubscribe", "List-Unsubscribe-Post").
					Do()
				if err != nil {
					results <- result{err: err}
					continue
				}
				var from, subject, date, listUnsub, listUnsubPost string
				for _, h := range msg.Payload.Headers {
					switch strings.ToLower(h.Name) {
					case "from":
						from = h.Value
					case "subject":
						subject = h.Value
					case "date":
						date = h.Value
					case "list-unsubscribe":
						listUnsub = h.Value
					case "list-unsubscribe-post":
						listUnsubPost = h.Value
					}
				}
				results <- result{ref: model.MessageRef{
					ID:                  msg.Id,
					From:                util.NormalizeSender(from),
					Subject:             subject,
					DateRFC3339:         parseDateRFC3339(date),
					ListUnsubscribe:     listUnsub,
					ListUnsubscribePost: listUnsubPost,
				}}
			}
		}()
	}
	for _, id := range ids {
		jobs <- job{id: id}
	}
	close(jobs)
	wg.Wait()
	close(results)

	out := make([]model.MessageRef, 0, len(ids))
	var firstErr error
	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
			continue
		}
		if r.ref.From == "" {
			continue
		}
		out = append(out, r.ref)
	}
	if firstErr != nil {
		return out, firstErr
	}
	return out, nil
}

func hasLabel(m *gmailv1.Message, id string) bool {
	if m == nil {
		return false
	}
	return contains(m.LabelIds, id)
}

func contains[T comparable](arr []T, v T) bool {
	for _, x := range arr {
		if x == v {
			return true
		}
	}
	return false
}

// keys returns the string keys of a set map[string]struct{} in arbitrary order.
func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// currentHistoryID returns the current mailbox largest historyId as a string.
func currentHistoryID(ctx context.Context, svc *gmailv1.Service) (string, error) {
	profile, err := svc.Users.GetProfile("me").Do()
	if err != nil {
		return "", err
	}
	// profile.HistoryId is uint64 in API; format to string
	return fmt.Sprintf("%d", profile.HistoryId), nil
}

