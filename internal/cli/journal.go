package cli

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const journalSchema = `
CREATE TABLE IF NOT EXISTS generations (
    id         INTEGER PRIMARY KEY,
    prompt_id  TEXT NOT NULL,
    backend    TEXT NOT NULL,
    inputs     TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS generations_by_time ON generations(created_at DESC);
`

// JournalEntry represents a recorded generation.
type JournalEntry struct {
	ID        int64
	PromptID  string
	Backend   string
	Inputs    string
	CreatedAt time.Time
}

// journal is the local generation journal backed by SQLite.
type journal struct {
	db *sql.DB
}

// openJournal opens or creates the journal database at the given path.
func openJournal(path string) (*journal, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=ON")
	if err != nil {
		return nil, fmt.Errorf("failed to open journal: %w", err)
	}
	if _, err := db.Exec(journalSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize journal schema: %w", err)
	}
	return &journal{db: db}, nil
}

// openDefaultJournal opens the journal at the default Evoke home location.
func openDefaultJournal() (*journal, error) {
	homeDir, err := home()
	if err != nil {
		return nil, err
	}
	return openJournal(homeDir + "/journal.db")
}

func (j *journal) Close() error {
	return j.db.Close()
}

// Record stores a new generation entry.
func (j *journal) Record(ctx context.Context, promptID, backend, inputs string) error {
	_, err := j.db.ExecContext(ctx,
		"INSERT INTO generations (prompt_id, backend, inputs, created_at) VALUES (?, ?, ?, ?)",
		promptID, backend, inputs, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to record generation: %w", err)
	}
	return nil
}

// Recent returns the most recent N journal entries.
func (j *journal) Recent(ctx context.Context, limit int) ([]JournalEntry, error) {
	rows, err := j.db.QueryContext(ctx,
		"SELECT id, prompt_id, backend, inputs, created_at FROM generations ORDER BY id DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query journal: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []JournalEntry
	for rows.Next() {
		var e JournalEntry
		var createdAt string
		if err := rows.Scan(&e.ID, &e.PromptID, &e.Backend, &e.Inputs, &createdAt); err != nil {
			return nil, fmt.Errorf("failed to scan journal entry: %w", err)
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
