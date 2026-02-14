package util

import "testing"

func TestNormalizeSender_Basic(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{`Name <User@Example.COM>`, "user@example.com"},
		{`"Name" <user+news@Example.com>`, "user@example.com"},
		{`user+tag@EXAMPLE.com`, "user@example.com"},
		{`user.name+tag@EXAMPLE.com`, "user.name@example.com"}, // dots preserved
		{`user.name@example.com`, "user.name@example.com"},
		{`bad address`, ""}, // unparsable
		{`"A" <not-an-email> , "B" <c@D.com>`, "c@d.com"}, // list fallback picks first valid
		{``, ""},
	}
	for _, tc := range tests {
		if got := NormalizeSender(tc.in); got != tc.want {
			t.Errorf("NormalizeSender(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}