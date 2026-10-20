package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("Failed to read user_version: %v", err)
	}
	return v
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
		t.Fatalf("Failed to look up table %s: %v", name, err)
	}
	return n == 1
}

// openRaw opens the file without running any migrations.
func openRaw(t *testing.T, file string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", FileURI(file))
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestNewSQLite_FreshDatabaseIsAtLatestVersion(t *testing.T) {
	s := newTestSQLite(t)

	if got := userVersion(t, s.db); got != len(migrations) {
		t.Errorf("Expected user_version %d, got %d", len(migrations), got)
	}
	for _, table := range []string{"request_logs", "notes"} {
		if !tableExists(t, s.db, table) {
			t.Errorf("Expected table %s to exist", table)
		}
	}
	if tableExists(t, s.db, "schema_migrations") {
		t.Error("Expected no schema_migrations table")
	}
}

func TestNewSQLite_UpgradesUnversionedDatabase(t *testing.T) {
	file := filepath.Join(t.TempDir(), "old.db")

	// A database created before migrations existed: baseline tables with
	// data, user_version still 0.
	old := openRaw(t, file)
	if _, err := old.Exec(migrations[0].sql); err != nil {
		t.Fatalf("Failed to create old schema: %v", err)
	}
	if _, err := old.Exec(`INSERT INTO notes (title, body, seq, created_at, updated_at) VALUES ('Keep', 'Keep me', 1, 'x', 'x');
		INSERT INTO request_logs (endpoint) VALUES ('/kept')`); err != nil {
		t.Fatalf("Failed to seed old data: %v", err)
	}
	old.Close()

	s, err := NewSQLite(file)
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}
	defer s.Close()

	if got := userVersion(t, s.db); got != len(migrations) {
		t.Errorf("Expected user_version %d, got %d", len(migrations), got)
	}
	var notesCount, logsCount int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM notes WHERE body = 'Keep me'").Scan(&notesCount); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE endpoint = '/kept'").Scan(&logsCount); err != nil {
		t.Fatal(err)
	}
	if notesCount != 1 || logsCount != 1 {
		t.Errorf("Expected existing data intact, got %d notes and %d logs", notesCount, logsCount)
	}
}

func TestMigrate_AppliesPendingInOrder(t *testing.T) {
	db := openRaw(t, filepath.Join(t.TempDir(), "m.db"))

	var ran []string
	ms := []migration{
		{name: "one", sql: `CREATE TABLE a (x INTEGER)`},
		{name: "two", fn: func(tx *sql.Tx) error {
			ran = append(ran, "two")
			_, err := tx.Exec(`INSERT INTO a (x) VALUES (2)`)
			return err
		}},
	}
	if err := migrate(db, ms[:1]); err != nil {
		t.Fatalf("First migrate failed: %v", err)
	}
	if got := userVersion(t, db); got != 1 {
		t.Fatalf("Expected user_version 1, got %d", got)
	}

	// Running again with one more migration applies only the new one.
	if err := migrate(db, ms); err != nil {
		t.Fatalf("Second migrate failed: %v", err)
	}
	if err := migrate(db, ms); err != nil {
		t.Fatalf("Third migrate failed: %v", err)
	}
	if got := userVersion(t, db); got != 2 {
		t.Errorf("Expected user_version 2, got %d", got)
	}
	if len(ran) != 1 {
		t.Errorf("Expected migration two to run once, ran %d times", len(ran))
	}
}

func TestMigrate_RunsSQLBeforeFn(t *testing.T) {
	db := openRaw(t, filepath.Join(t.TempDir(), "m.db"))

	ms := []migration{{
		name: "backfill",
		sql:  `CREATE TABLE a (x INTEGER)`,
		fn: func(tx *sql.Tx) error {
			_, err := tx.Exec(`INSERT INTO a (x) VALUES (1)`)
			return err
		},
	}}
	if err := migrate(db, ms); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM a`).Scan(&n); err != nil || n != 1 {
		t.Errorf("Expected one backfilled row, got %d (%v)", n, err)
	}
}

func TestMigrate_FailureRollsBack(t *testing.T) {
	db := openRaw(t, filepath.Join(t.TempDir(), "m.db"))

	boom := errors.New("boom")
	ms := []migration{
		{name: "one", sql: `CREATE TABLE a (x INTEGER)`},
		{name: "two", sql: `CREATE TABLE b (x INTEGER)`, fn: func(*sql.Tx) error { return boom }},
	}
	err := migrate(db, ms)
	if !errors.Is(err, boom) {
		t.Fatalf("Expected boom, got %v", err)
	}
	if got := userVersion(t, db); got != 1 {
		t.Errorf("Expected user_version to stay at 1, got %d", got)
	}
	if !tableExists(t, db, "a") {
		t.Error("Expected the successful migration's table to remain")
	}
	if tableExists(t, db, "b") {
		t.Error("Expected the failed migration's table to be rolled back")
	}
}

func TestNewSQLite_FailingMigrationAbortsOpen(t *testing.T) {
	file := filepath.Join(t.TempDir(), "bad.db")
	saved := migrations
	t.Cleanup(func() { migrations = saved })
	migrations = append(append([]migration{}, saved...), migration{name: "bad", sql: `NOT VALID SQL`})

	if s, err := NewSQLite(file); err == nil {
		s.Close()
		t.Fatal("Expected NewSQLite to fail")
	}

	db := openRaw(t, file)
	if got := userVersion(t, db); got != len(saved) {
		t.Errorf("Expected user_version %d after the failure, got %d", len(saved), got)
	}
}
