package gmail

import (
	"testing"

	"chuckterm/internal/model"
)

func hasString(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func TestAggregateBySenderSubject_BasicGrouping(t *testing.T) {
	msgs := []model.MessageRef{
		{ID: "1", From: `Alice <user+ads@Example.com>`, Subject: "Promo", DateRFC3339: "2024-01-02T15:04:05Z"},
		{ID: "2", From: `"Alice" <user@EXAMPLE.com>`, Subject: "Promo", DateRFC3339: "2024-01-03T15:04:05Z"},
		{ID: "3", From: `bob@example.com`, Subject: "Promo", DateRFC3339: "2024-01-01T00:00:00Z"},
		{ID: "4", From: `bob@example.com`, Subject: "Other", DateRFC3339: "2024-01-05T00:00:00Z"},
		{ID: "5", From: `bob@example.com`, Subject: "other", DateRFC3339: "2024-01-06T00:00:00Z"},
	}

	groups := AggregateBySenderSubject(msgs)

	if len(groups) != 4 {
		t.Fatalf("expected 4 groups, got %d", len(groups))
	}

	keyAlice := "user@example.com||Promo"
	gAlice, ok := groups[keyAlice]
	if !ok {
		t.Fatalf("missing group %s", keyAlice)
	}
	if gAlice.Email != "user@example.com" {
		t.Fatalf("alice email got %q", gAlice.Email)
	}
	if gAlice.Subject != "Promo" {
		t.Fatalf("alice subject got %q", gAlice.Subject)
	}
	if gAlice.Count != 2 {
		t.Fatalf("alice count want 2 got %d", gAlice.Count)
	}
	if gAlice.FirstDate != "2024-01-02T15:04:05Z" {
		t.Fatalf("alice first want 2024-01-02T15:04:05Z got %s", gAlice.FirstDate)
	}
	if gAlice.LastDate != "2024-01-03T15:04:05Z" {
		t.Fatalf("alice last want 2024-01-03T15:04:05Z got %s", gAlice.LastDate)
	}
	if !(hasString(gAlice.MessageIDs, "1") && hasString(gAlice.MessageIDs, "2")) {
		t.Fatalf("alice ids missing: %v", gAlice.MessageIDs)
	}

	keyBobPromo := "bob@example.com||Promo"
	if g, ok := groups[keyBobPromo]; !ok || g.Count != 1 {
		t.Fatalf("bob promo want count 1, ok=%v count=%v", ok, func() int { if !ok { return -1 } ; return g.Count }())
	}

	keyBobOther := "bob@example.com||Other"
	if g, ok := groups[keyBobOther]; !ok || g.Count != 1 {
		t.Fatalf("bob Other want count 1, ok=%v", ok)
	}
	keyBobother := "bob@example.com||other"
	if g, ok := groups[keyBobother]; !ok || g.Count != 1 {
		t.Fatalf("bob other want count 1, ok=%v", ok)
	}
}

func TestSortGroups_TieBreakers(t *testing.T) {
	m := map[string]*model.SenderGroup{
		"a1": {Email: "a@example.com", Subject: "A", Count: 5},
		"a2": {Email: "a@example.com", Subject: "B", Count: 5},
		"b1": {Email: "b@example.com", Subject: "A", Count: 5},
		"c1": {Email: "c@example.com", Subject: "A", Count: 3},
	}
	out := SortGroups(m)
	if len(out) != 4 {
		t.Fatalf("len=%d", len(out))
	}
	exp := []struct {
		Email, Subject string
	}{
		{"a@example.com", "A"},
		{"a@example.com", "B"},
		{"b@example.com", "A"},
		{"c@example.com", "A"},
	}
	for i, e := range exp {
		if out[i].Email != e.Email || out[i].Subject != e.Subject {
			t.Fatalf("idx %d want %s|%s got %s|%s", i, e.Email, e.Subject, out[i].Email, out[i].Subject)
		}
	}
}