package gmail

import (
	"context"
	"fmt"

	gmailv1 "google.golang.org/api/gmail/v1"
)

// ArchiveMessages removes the INBOX label from the given messages (batch).
func ArchiveMessages(ctx context.Context, svc *gmailv1.Service, messageIDs []string) error {
	user := "me"
	req := &gmailv1.ModifyMessageRequest{
		RemoveLabelIds: []string{"INBOX"},
	}
	for _, id := range messageIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if _, err := svc.Users.Messages.Modify(user, id, req).Do(); err != nil {
			return fmt.Errorf("archive message %s: %w", id, err)
		}
	}
	return nil
}

// TrashMessages moves the given messages to trash.
func TrashMessages(ctx context.Context, svc *gmailv1.Service, messageIDs []string) error {
	user := "me"
	for _, id := range messageIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if _, err := svc.Users.Messages.Trash(user, id).Do(); err != nil {
			return fmt.Errorf("trash message %s: %w", id, err)
		}
	}
	return nil
}

// GetMessageBody fetches the full message and extracts the body as plain text.
// It prefers text/plain, falls back to stripped HTML, then the message snippet.
func GetMessageBody(ctx context.Context, svc *gmailv1.Service, messageID string) (string, error) {
	user := "me"
	msg, err := svc.Users.Messages.Get(user, messageID).Format("full").Do()
	if err != nil {
		return "", fmt.Errorf("get message %s: %w", messageID, err)
	}
	if msg.Payload != nil {
		if body := extractPlainText(msg.Payload); body != "" {
			return body, nil
		}
		if html := extractHTML(msg.Payload); html != "" {
			if text := stripHTMLTags(html); text != "" {
				return text, nil
			}
		}
	}
	if msg.Snippet != "" {
		return msg.Snippet, nil
	}
	return "(no content)", nil
}
