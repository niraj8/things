package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"chuckterm/internal/model"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements gmail.MessageStore backed by a local SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) the database at the given path and runs migrations.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db}, nil
}

func migrate(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS messages (
	id                    TEXT PRIMARY KEY,
	from_email            TEXT NOT NULL,
	subject               TEXT NOT NULL DEFAULT '',
	date_rfc3339          TEXT NOT NULL DEFAULT '',
	list_unsubscribe      TEXT NOT NULL DEFAULT '',
	list_unsubscribe_post TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS metadata (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);
`
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) UpsertMessages(ctx context.Context, msgs []model.MessageRef) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO messages (id, from_email, subject, date_rfc3339, list_unsubscribe, list_unsubscribe_post)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			from_email            = excluded.from_email,
			subject               = excluded.subject,
			date_rfc3339          = excluded.date_rfc3339,
			list_unsubscribe      = excluded.list_unsubscribe,
			list_unsubscribe_post = excluded.list_unsubscribe_post
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, m := range msgs {
		_, err := stmt.ExecContext(ctx, m.ID, m.From, m.Subject, m.DateRFC3339, m.ListUnsubscribe, m.ListUnsubscribePost)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) DeleteMessages(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, "DELETE FROM messages WHERE id = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) LoadAllMessages(ctx context.Context) ([]model.MessageRef, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, from_email, subject, date_rfc3339, list_unsubscribe, list_unsubscribe_post FROM messages")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []model.MessageRef
	for rows.Next() {
		var m model.MessageRef
		if err := rows.Scan(&m.ID, &m.From, &m.Subject, &m.DateRFC3339, &m.ListUnsubscribe, &m.ListUnsubscribePost); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *SQLiteStore) GetMessagesByIDs(ctx context.Context, ids []string) ([]model.MessageRef, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := "SELECT id, from_email, subject, date_rfc3339, list_unsubscribe, list_unsubscribe_post FROM messages WHERE id IN (" + strings.Join(placeholders, ",") + ")"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []model.MessageRef
	for rows.Next() {
		var m model.MessageRef
		if err := rows.Scan(&m.ID, &m.From, &m.Subject, &m.DateRFC3339, &m.ListUnsubscribe, &m.ListUnsubscribePost); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *SQLiteStore) CountMessages(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM messages").Scan(&count)
	return count, err
}

func (s *SQLiteStore) GetLastHistoryID(ctx context.Context) (string, error) {
	var val string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM metadata WHERE key = 'last_history_id'").Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

func (s *SQLiteStore) SetLastHistoryID(ctx context.Context, historyID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO metadata (key, value) VALUES ('last_history_id', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, historyID)
	return err
}
