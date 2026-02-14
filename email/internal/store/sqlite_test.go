package store

import (
	"context"
	"path/filepath"
	"testing"

	"chuckterm/internal/model"
)

func testStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpsertAndLoad(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	msgs := []model.MessageRef{
		{ID: "1", From: "a@b.com", Subject: "hello", DateRFC3339: "2024-01-01T00:00:00Z"},
		{ID: "2", From: "c@d.com", Subject: "world", DateRFC3339: "2024-01-02T00:00:00Z", ListUnsubscribe: "<https://unsub.example.com>"},
	}
	if err := s.UpsertMessages(ctx, msgs); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}

	count, err := s.CountMessages(ctx)
	if err != nil {
		t.Fatalf("CountMessages: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2, got %d", count)
	}

	loaded, err := s.LoadAllMessages(ctx)
	if err != nil {
		t.Fatalf("LoadAllMessages: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 loaded, got %d", len(loaded))
	}

	// Upsert should update existing
	msgs[0].Subject = "updated"
	if err := s.UpsertMessages(ctx, msgs[:1]); err != nil {
		t.Fatalf("UpsertMessages update: %v", err)
	}
	loaded, _ = s.LoadAllMessages(ctx)
	found := false
	for _, m := range loaded {
		if m.ID == "1" && m.Subject == "updated" {
			found = true
		}
	}
	if !found {
		t.Fatal("upsert did not update existing message")
	}
}

func TestDeleteMessages(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	msgs := []model.MessageRef{
		{ID: "1", From: "a@b.com"},
		{ID: "2", From: "c@d.com"},
	}
	s.UpsertMessages(ctx, msgs)
	if err := s.DeleteMessages(ctx, []string{"1"}); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}
	count, _ := s.CountMessages(ctx)
	if count != 1 {
		t.Fatalf("expected 1 after delete, got %d", count)
	}
}

func TestHistoryID(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	hid, err := s.GetLastHistoryID(ctx)
	if err != nil {
		t.Fatalf("GetLastHistoryID: %v", err)
	}
	if hid != "" {
		t.Fatalf("expected empty, got %q", hid)
	}

	if err := s.SetLastHistoryID(ctx, "12345"); err != nil {
		t.Fatalf("SetLastHistoryID: %v", err)
	}
	hid, _ = s.GetLastHistoryID(ctx)
	if hid != "12345" {
		t.Fatalf("expected 12345, got %q", hid)
	}

	// Update
	s.SetLastHistoryID(ctx, "99999")
	hid, _ = s.GetLastHistoryID(ctx)
	if hid != "99999" {
		t.Fatalf("expected 99999, got %q", hid)
	}
}
