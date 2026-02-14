package util

import (
	"net/mail"
	"strings"
)

// NormalizeSender extracts and normalizes an email address from a From header.
// - Parses RFC 5322 "From" values like "Name <user+alias@Example.COM>"
// - Lowercases
// - Strips +alias in local part: user+news@x.com -> user@x.com
// Returns empty string if parsing fails or address is missing.
func NormalizeSender(fromHeader string) string {
	if fromHeader == "" {
		return ""
	}
	addr, err := mail.ParseAddress(fromHeader)
	if err != nil || addr == nil {
		// Some headers may be a list; try a crude fallback by splitting on comma.
		parts := strings.Split(fromHeader, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			a, e := mail.ParseAddress(p)
			if e == nil && a != nil {
				addr = a
				break
			}
		}
		if addr == nil {
			return ""
		}
	}

	email := strings.ToLower(strings.TrimSpace(addr.Address))
	at := strings.LastIndexByte(email, '@')
	if at <= 0 {
		return email
	}
	local := email[:at]
	domain := email[at+1:]

	// Strip +alias in local part.
	if plus := strings.IndexByte(local, '+'); plus > -1 {
		local = local[:plus]
	}
	// Some providers ignore dots in local part (e.g., Gmail). We WON'T remove dots
	// by default to avoid over-grouping across providers. Keep dots as-is.

	return local + "@" + domain
}