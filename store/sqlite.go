package store

import (
	"database/sql"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// sqliteTimeLayout is a fixed-width UTC layout, so created_at values compare
// and sort correctly as strings. The driver's default (RFC3339Nano) varies in
// length and does not.
const sqliteTimeLayout = "2006-01-02T15:04:05.000000Z07:00"

// NewSQLite opens (or creates) the SQLite database at file and returns a store
// that owns the connection.
func NewSQLite(file string) (*SQLStore, error) {
	db, err := openSQLite(file)
	if err != nil {
		return nil, err
	}
	return &SQLStore{
		db:           db,
		timeArg:      func(t time.Time) any { return t.UTC().Format(sqliteTimeLayout) },
		noLimit:      -1, // SQLite: a negative LIMIT means no limit
		notesEnabled: true,
	}, nil
}

func openSQLite(file string) (*sql.DB, error) {
	// SQLite-specific pragmas for better concurrency and durability, set in the
	// DSN so the driver applies them to every connection it opens.
	// WAL mode and a busy timeout reduce SQLITE_BUSY errors under contention.
	db, err := sql.Open("sqlite3", "file:"+file+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}

	// Connection pool: limit to a single writer connection for file-backed SQLite
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0 * time.Second)

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS request_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		status INTEGER,
		method TEXT,
		error TEXT,
		endpoint TEXT,
		created_at DATETIME,
		response TEXT,
		request TEXT
	)`)
	if err == nil {
		_, err = db.Exec(createNotesTable)
	}
	if err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
