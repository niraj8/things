package gmail

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"chuckterm/internal/model"
	"chuckterm/internal/util"

	gmailv1 "google.golang.org/api/gmail/v1"
)

// FetchGroups retrieves all messages for the authenticated user and aggregates
// them by normalized sender email AND exact, case-sensitive Subject. It streams pages from Gmail and fetches
// message metadata concurrently. The function respects ctx for cancelation.
//
// Notes (MVP):
// - Includes only INBOX by default; spam/trash excluded unless includeSpamTrash.
// - Uses Users.Messages.Get with Format=METADATA to read From/Subject/Date.
// - Concurrency is bounded by workerCount.
// - No hard cap on messages; will run until exhausted or ctx cancelled.
func FetchGroups(ctx context.Context, svc *gmailv1.Service, includeSpamTrash bool) (map[string]*model.SenderGroup, error) {
	user := "me"

	list := svc.Users.Messages.List(user).
		IncludeSpamTrash(includeSpamTrash).
		MaxResults(500) // Gmail will page; this is page size, not a cap overall.

	// Restrict to INBOX for MVP to avoid noise.
	list = list.LabelIds("INBOX")

	type job struct {
		id string
	}
	type result struct {
		from, subject, date    string
		listUnsub, listUnsubPost string
		id                     string
		err                    error
	}

	jobs := make(chan job, 1000)
	results := make(chan result, 1000)

	// Worker pool to fetch message metadata.
	workerCount := 16
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
				results <- result{from: from, subject: subject, date: date, listUnsub: listUnsub, listUnsubPost: listUnsubPost, id: msg.Id}
			}
		}()
	}

	// Collector
	groups := make(map[string]*model.SenderGroup)
	var collectErr error
	var collectWG sync.WaitGroup
	collectWG.Add(1)
	go func() {
		defer collectWG.Done()
		for r := range results {
			if r.err != nil {
				// Record first error but continue to aggregate what we can.
				if collectErr == nil {
					collectErr = r.err
				}
				continue
			}
			email := util.NormalizeSender(r.from)
			if email == "" {
				continue
			}
			subject := r.subject
			key := email + "||" + subject
			g, ok := groups[key]
			if !ok {
				g = &model.SenderGroup{
					Email:   email,
					Subject: subject,
				}
				// Try to preserve display name (prefix before <email>) best-effort.
				g.DisplayName = displayNameFromFrom(r.from, email)
				groups[key] = g
			}
			g.Count++
			if g.Sample == "" && subject != "" {
				g.Sample = subject
			}
			// Normalize date into RFC3339 where possible.
			if ts := parseDateRFC3339(r.date); ts != "" {
				if g.FirstDate == "" || ts < g.FirstDate {
					g.FirstDate = ts
				}
				if g.LastDate == "" || ts > g.LastDate {
					g.LastDate = ts
				}
			}
			g.MessageIDs = append(g.MessageIDs, r.id)
			if g.UnsubscribeURL == "" && r.listUnsub != "" {
				g.UnsubscribeURL = extractHTTPUnsubscribeURL(r.listUnsub)
			}
		}
	}()

	// Page through the message list.
	pageToken := ""
	for {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			close(results)
			collectWG.Wait()
			return groups, ctx.Err()
		default:
		}

		call := list
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			// Stop on list error.
			close(jobs)
			wg.Wait()
			close(results)
			collectWG.Wait()
			return groups, fmt.Errorf("list messages: %w", err)
		}

		for _, m := range resp.Messages {
			select {
			case <-ctx.Done():
				break
			case jobs <- job{id: m.Id}:
			}
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	// Done queuing; close and wait.
	close(jobs)
	wg.Wait()
	close(results)
	collectWG.Wait()

	return groups, collectErr
}

// FetchInitialEmails retrieves the first N messages from the user's inbox.
func FetchInitialEmails(ctx context.Context, svc *gmailv1.Service, n int64) ([]model.MessageRef, error) {
	user := "me"
	list, err := svc.Users.Messages.List(user).
		LabelIds("INBOX").
		MaxResults(n).
		Do()
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	var refs []model.MessageRef
	for _, m := range list.Messages {
		msg, err := svc.Users.Messages.Get(user, m.Id).
			Format("metadata").
			MetadataHeaders("From", "Subject", "Date", "List-Unsubscribe", "List-Unsubscribe-Post").
			Do()
		if err != nil {
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
		refs = append(refs, model.MessageRef{
			ID:                  msg.Id,
			From:                from,
			Subject:             subject,
			DateRFC3339:         parseDateRFC3339(date),
			ListUnsubscribe:     listUnsub,
			ListUnsubscribePost: listUnsubPost,
		})
	}
	return refs, nil
}

// AggregateBySenderSubject builds groups from a provided slice of MessageRef using
// NormalizeSender(msg.From) + exact, case-sensitive msg.Subject as the key.
// DateRFC3339 is expected to already be RFC3339; comparisons are string-based.
func AggregateBySenderSubject(msgs []model.MessageRef) map[string]*model.SenderGroup {
	groups := make(map[string]*model.SenderGroup)
	for _, m := range msgs {
		email := util.NormalizeSender(m.From)
		if email == "" {
			continue
		}
		subject := m.Subject
		key := email + "||" + subject
		g, ok := groups[key]
		if !ok {
			g = &model.SenderGroup{
				Email:       email,
				Subject:     subject,
				DisplayName: displayNameFromFrom(m.From, email),
			}
			groups[key] = g
		}
		g.Count++
		if g.Sample == "" && subject != "" {
			g.Sample = subject
		}
		ts := strings.TrimSpace(m.DateRFC3339)
		if ts != "" {
			if g.FirstDate == "" || ts < g.FirstDate {
				g.FirstDate = ts
			}
			if g.LastDate == "" || ts > g.LastDate {
				g.LastDate = ts
			}
		}
		if m.ID != "" {
			g.MessageIDs = append(g.MessageIDs, m.ID)
		}
		// Propagate unsubscribe URL (prefer HTTP over mailto)
		if g.UnsubscribeURL == "" && m.ListUnsubscribe != "" {
			g.UnsubscribeURL = extractHTTPUnsubscribeURL(m.ListUnsubscribe)
		}
	}
	return groups
}

// extractHTTPUnsubscribeURL finds the first HTTP(S) URL in a List-Unsubscribe header value.
// The header typically contains comma-separated angle-bracketed URLs like:
// <https://example.com/unsub>, <mailto:unsub@example.com>
func extractHTTPUnsubscribeURL(header string) string {
	parts := strings.Split(header, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "<>")
		p = strings.TrimSpace(p)
		lower := strings.ToLower(p)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			return p
		}
	}
	return ""
}

// Helpers

func parseDateRFC3339(h string) string {
	if h == "" {
		return ""
	}
	// Try common formats Gmail uses in Date header.
	layouts := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		time.RFC850,
		time.RFC3339,
		"Mon, 2 Jan 2006 15:04:05 -0700",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, h); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

func displayNameFromFrom(fromHeader, normalized string) string {
	// If header contains a quoted name, strip address and return remaining.
	// E.g., "Twitter <notify@twitter.com>" -> "Twitter"
	if idx := strings.Index(fromHeader, "<"); idx > 0 {
		name := strings.TrimSpace(fromHeader[:idx])
		name = strings.Trim(name, `"'`)
		if name != "" {
			return name
		}
	}
	// Fallback to local-part as "Name".
	if at := strings.IndexByte(normalized, '@'); at > 0 {
		lp := normalized[:at]
		parts := strings.Split(lp, ".")
		for i := range parts {
			if parts[i] == "" {
				continue
			}
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
		return strings.Join(parts, " ")
	}
	return normalized
}

// SortGroups returns a stable slice sorted by Count desc, then Email asc, then Subject asc.
func SortGroups(m map[string]*model.SenderGroup) []model.SenderGroup {
	out := make([]model.SenderGroup, 0, len(m))
	for _, g := range m {
		out = append(out, *g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			if out[i].Email == out[j].Email {
				return out[i].Subject < out[j].Subject
			}
			return out[i].Email < out[j].Email
		}
		return out[i].Count > out[j].Count
	})
	return out
}