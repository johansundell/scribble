package store

import (
	"database/sql"
	"fmt"
)

// migration is one forward-only step of the SQLite schema. sql runs first,
// then fn (for backfills that need Go), both inside one transaction.
type migration struct {
	name string
	sql  string
	fn   func(tx *sql.Tx) error
}

// migrations is the SQLite schema history. Migration i brings the database to
// PRAGMA user_version i+1. Never edit or reorder a released migration; append
// a new one instead. Rollback is restoring a backup archive.
var migrations = []migration{
	{
		// Databases from before migrations already have these tables, so
		// every statement must stay idempotent.
		name: "baseline",
		sql: `
CREATE TABLE IF NOT EXISTS request_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	status INTEGER,
	method TEXT,
	error TEXT,
	endpoint TEXT,
	created_at DATETIME,
	response TEXT,
	request TEXT
);
CREATE TABLE IF NOT EXISTS notes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL,
	body TEXT NOT NULL,
	revision INTEGER NOT NULL DEFAULT 1,
	seq INTEGER NOT NULL,
	deleted INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS notes_title ON notes(lower(title)) WHERE deleted = 0;
CREATE INDEX IF NOT EXISTS notes_seq ON notes(seq);
`,
	},
	{
		// A note's public share link (#83); NULL while it is not shared.
		// ALTER TABLE cannot add a UNIQUE column, so the index enforces it.
		name: "share_token",
		sql: `
ALTER TABLE notes ADD COLUMN share_token TEXT;
CREATE UNIQUE INDEX notes_share_token ON notes(share_token);
`,
	},
}

// migrate applies the migrations the database's user_version has not yet
// seen, in order, each in its own transaction. It stops at the first failure;
// that migration is rolled back and user_version stays at the last success.
// A user_version above len(ms) means a newer release migrated the database;
// migrate refuses it rather than run this binary against an unknown schema.
func migrate(db *sql.DB, ms []migration) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > len(ms) {
		return fmt.Errorf("database schema version %d is newer than this binary (%d); upgrade, or restore a backup taken before the upgrade", version, len(ms))
	}
	for i := version; i < len(ms); i++ {
		if err := apply(db, ms[i], i+1); err != nil {
			return fmt.Errorf("migration %d (%s): %w", i+1, ms[i].name, err)
		}
	}
	return nil
}

// apply runs m and sets user_version to version in one transaction.
func apply(db *sql.DB, m migration, version int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if m.sql != "" {
		if _, err := tx.Exec(m.sql); err != nil {
			return err
		}
	}
	if m.fn != nil {
		if err := m.fn(tx); err != nil {
			return err
		}
	}
	// PRAGMA takes no bound parameters; version is an int we computed.
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return err
	}
	return tx.Commit()
}
