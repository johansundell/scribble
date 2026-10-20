package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// uriEscaper escapes characters that end or escape the path in an SQLite
// URI filename (RFC 3986 / SQLite URI specification).
var uriEscaper = strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23")

// FileURI turns a file path into an SQLite URI filename ("file:<escaped_path>"),
// so a '?', '#' or '%' in the path cannot be misparsed as a URI query string,
// fragment or escape sequence.
func FileURI(path string) string {
	return "file:" + uriEscaper.Replace(path)
}

// sqliteTimeLayout is a fixed-width UTC layout, so created_at values compare
// and sort correctly as strings. The driver's default (RFC3339Nano) varies in
// length and does not.
const sqliteTimeLayout = "2006-01-02T15:04:05.000000Z07:00"

// NewSQLite opens (or creates) the SQLite database at file and returns a store
// that owns the connection.
func NewSQLite(file string) (*SQLStore, error) {
	db, err := openSQLite(file, migrations)
	if err != nil {
		return nil, err
	}
	var schema int
	if err := db.QueryRow("PRAGMA user_version").Scan(&schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("read schema version: %w", err)
	}
	return &SQLStore{
		db:           db,
		timeArg:      func(t time.Time) any { return t.UTC().Format(sqliteTimeLayout) },
		noLimit:      -1, // SQLite: a negative LIMIT means no limit
		notesEnabled: true,
		schema:       schema,
	}, nil
}

// openSQLite opens file and brings it to the schema version of ms.
func openSQLite(file string, ms []migration) (*sql.DB, error) {
	// SQLite-specific pragmas for better concurrency and durability, set in the
	// DSN so the driver applies them to every connection it opens.
	// WAL mode and a busy timeout reduce SQLITE_BUSY errors under contention.
	db, err := sql.Open("sqlite3", FileURI(file)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}

	// Connection pool: limit to a single writer connection for file-backed SQLite
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0 * time.Second)

	err = migrate(db, ms)
	if err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
