package gmail

import (
	"encoding/base64"
	"strings"

	gmailv1 "google.golang.org/api/gmail/v1"
)

// extractPlainText recursively walks a MIME part tree and returns the first
// text/plain body found (base64url decoded). For multipart/alternative it
// prefers text/plain over text/html.
func extractPlainText(part *gmailv1.MessagePart) string {
	if part == nil {
		return ""
	}

	mime := strings.ToLower(part.MimeType)

	// Leaf node with text/plain body data
	if mime == "text/plain" && part.Body != nil && part.Body.Data != "" {
		return decodeBase64URL(part.Body.Data)
	}

	// Recurse into sub-parts (multipart/*)
	if len(part.Parts) > 0 {
		// For multipart/alternative, prefer text/plain
		for _, sub := range part.Parts {
			if strings.ToLower(sub.MimeType) == "text/plain" {
				if body := extractPlainText(sub); body != "" {
					return body
				}
			}
		}
		// Otherwise try all parts
		for _, sub := range part.Parts {
			if body := extractPlainText(sub); body != "" {
				return body
			}
		}
	}

	return ""
}

// extractHTML recursively walks a MIME part tree and returns the first
// text/html body found (base64url decoded).
func extractHTML(part *gmailv1.MessagePart) string {
	if part == nil {
		return ""
	}

	mime := strings.ToLower(part.MimeType)

	if mime == "text/html" && part.Body != nil && part.Body.Data != "" {
		return decodeBase64URL(part.Body.Data)
	}

	for _, sub := range part.Parts {
		if body := extractHTML(sub); body != "" {
			return body
		}
	}

	return ""
}

// stripHTMLTags removes HTML tags and decodes common entities to produce readable text.
func stripHTMLTags(html string) string {
	// Replace block-level elements with newlines
	for _, tag := range []string{"<br>", "<br/>", "<br />", "</p>", "</div>", "</tr>", "</li>", "</h1>", "</h2>", "</h3>", "</h4>", "</h5>", "</h6>"} {
		html = strings.ReplaceAll(html, tag, "\n")
		html = strings.ReplaceAll(html, strings.ToUpper(tag), "\n")
	}

	// Strip all remaining tags
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	result := b.String()

	// Decode common HTML entities
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
		"&apos;", "'",
		"&nbsp;", " ",
	)
	result = replacer.Replace(result)

	// Collapse multiple blank lines
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}

	return strings.TrimSpace(result)
}

func decodeBase64URL(data string) string {
	b, err := base64.URLEncoding.DecodeString(data)
	if err != nil {
		// Gmail uses unpadded base64url
		b, err = base64.RawURLEncoding.DecodeString(data)
		if err != nil {
			return ""
		}
	}
	return string(b)
}
