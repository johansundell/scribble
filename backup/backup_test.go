package backup_test

import (
	"compress/gzip"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/johansundell/scribble/backup"
	"github.com/johansundell/scribble/store"
)

var fixedNow = func() time.Time { return time.Date(2026, 10, 19, 3, 0, 0, 0, time.UTC) }

func newLiveStore(t *testing.T) (*store.SQLStore, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "scribble.db")
	s, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	_, err = s.CreateNote(context.Background(), "# Project Alpha\nInitial note content")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	return s, dbPath
}

func restore(t *testing.T, archive string) *sql.DB {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("archive is not gzip: %v", err)
	}
	restored := filepath.Join(t.TempDir(), "restored.db")
	out, err := os.Create(restored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, zr); err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", store.FileURI(restored)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRunWritesCompressedTimestampedSnapshot(t *testing.T) {
	_, dbPath := newLiveStore(t)
	dir := filepath.Join(t.TempDir(), "backups")

	archive, err := backup.Run(context.Background(), backup.Options{DBPath: dbPath, Dir: dir, Now: fixedNow})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := filepath.Join(dir, "scribble_20261019T030000Z.db.gz")
	if archive != want {
		t.Errorf("archive = %q, want %q", archive, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("backup dir should hold only the archive, has %v", names)
	}

	db := restore(t, archive)
	var title, body string
	if err := db.QueryRow(`SELECT title, body FROM notes WHERE id = 1`).Scan(&title, &body); err != nil {
		t.Fatalf("read restored note: %v", err)
	}
	if title != "Project Alpha" || body != "# Project Alpha\nInitial note content" {
		t.Errorf("restored note: title=%q body=%q", title, body)
	}

	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatalf("integrity_check: %v", err)
	}
	if integrity != "ok" {
		t.Errorf("integrity_check = %q, want ok", integrity)
	}
}

func TestRunIncludesWALWrites(t *testing.T) {
	s, dbPath := newLiveStore(t)
	dir := filepath.Join(t.TempDir(), "backups")

	// Write more notes that go into the WAL while connection is held open
	for i := 2; i <= 5; i++ {
		_, err := s.CreateNote(context.Background(), "# Note "+string(rune('0'+i))+"\nBody")
		if err != nil {
			t.Fatalf("CreateNote %d: %v", i, err)
		}
	}

	archive, err := backup.Run(context.Background(), backup.Options{DBPath: dbPath, Dir: dir, Now: fixedNow})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	db := restore(t, archive)
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notes WHERE deleted = 0`).Scan(&count); err != nil {
		t.Fatalf("count notes: %v", err)
	}
	if count != 5 {
		t.Errorf("restored notes count = %d, want 5 (WAL writes must be captured)", count)
	}
}

func TestRunFailsForMissingDatabase(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.db")
	_, err := backup.Run(context.Background(), backup.Options{DBPath: missing, Dir: dir})
	if err == nil {
		t.Fatal("expected Run to fail for missing database")
	}
}

func TestRunFailsIfArchiveExists(t *testing.T) {
	_, dbPath := newLiveStore(t)
	dir := filepath.Join(t.TempDir(), "backups")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, "scribble_20261019T030000Z.db.gz")
	if err := os.WriteFile(existing, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := backup.Run(context.Background(), backup.Options{DBPath: dbPath, Dir: dir, Now: fixedNow})
	if err == nil {
		t.Fatal("expected Run to fail if archive exists")
	}
}

func TestRunRejectsCorruptSnapshot(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "garbage.db")
	if err := os.WriteFile(dbPath, []byte("this is not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(dir, "backups")
	if _, err := backup.Run(context.Background(), backup.Options{DBPath: dbPath, Dir: backups, Now: fixedNow}); err == nil {
		t.Fatal("Run should fail on a file that isn't a SQLite database")
	}
	if entries, _ := os.ReadDir(backups); len(entries) != 0 {
		t.Errorf("failed Run left files behind: %v", entries)
	}
}
