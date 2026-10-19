// Package backup takes consistent snapshots of Scribble's SQLite database
// while the service keeps running.
package backup

import (
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/johansundell/scribble/store"
)

const (
	// archivePrefix and archiveSuffix frame every archive name:
	// scribble_<timestamp>.db.gz.
	archivePrefix = "scribble_"
	archiveSuffix = ".db.gz"
	// timestampLayout is UTC and fixed-width, so archive names sort by age.
	timestampLayout = "20060102T150405Z"
)

// Options says which database to back up and where the archive goes.
type Options struct {
	DBPath string // the live database file
	Dir    string // created when missing
	// Now stamps the archive name; time.Now when nil.
	Now func() time.Time
}

// Run snapshots the database at opts.DBPath with VACUUM INTO, checks the
// snapshot with PRAGMA integrity_check and stores it gzipped in opts.Dir as
// scribble_<timestamp>.db.gz. It returns the archive path. The service may keep
// reading and writing meanwhile: in WAL mode the snapshot is one read
// transaction and doesn't block writers. Run never overwrites an archive, and
// when it fails it leaves no files behind in opts.Dir.
func Run(ctx context.Context, opts Options) (string, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	archive := filepath.Join(opts.Dir, archivePrefix+now().UTC().Format(timestampLayout)+archiveSuffix)

	if _, err := os.Stat(opts.DBPath); err != nil {
		return "", fmt.Errorf("database: %w", err)
	}
	if err := os.MkdirAll(opts.Dir, 0o750); err != nil {
		return "", fmt.Errorf("backup directory: %w", err)
	}
	if _, err := os.Stat(archive); err == nil {
		return "", fmt.Errorf("%s already exists", archive)
	}

	// Each run works in its own hidden folder, so runs started in the same
	// second don't share files, and nothing that lists archives sees them.
	work, err := os.MkdirTemp(opts.Dir, ".backup-")
	if err != nil {
		return "", fmt.Errorf("backup directory: %w", err)
	}
	defer os.RemoveAll(work)

	snapshot := filepath.Join(work, "snapshot.db")
	if err := takeSnapshot(ctx, opts.DBPath, snapshot); err != nil {
		return "", err
	}
	if err := finalizeSnapshot(ctx, snapshot); err != nil {
		return "", err
	}
	tmp := filepath.Join(work, "snapshot.db.gz")
	if err := compress(snapshot, tmp); err != nil {
		return "", fmt.Errorf("compress snapshot: %w", err)
	}
	// Link, unlike Rename, fails when the archive exists.
	if err := os.Link(tmp, archive); err != nil {
		return "", fmt.Errorf("store archive: %w", err)
	}
	return archive, nil
}

// takeSnapshot copies the database at src into dst as one self-contained file.
func takeSnapshot(ctx context.Context, src, dst string) error {
	// mode=rw: never create an empty database where the live one should be.
	// The busy timeout matches the service's, in case it holds a lock.
	db, err := sql.Open("sqlite3", store.FileURI(src)+"?mode=rw&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, dst); err != nil {
		return fmt.Errorf("snapshot %s: %w", src, err)
	}
	return nil
}

// finalizeSnapshot runs PRAGMA integrity_check on the snapshot, then switches
// it out of WAL mode so the archive restores as a single file.
func finalizeSnapshot(ctx context.Context, file string) error {
	db, err := sql.Open("sqlite3", store.FileURI(file)+"?mode=rw")
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return fmt.Errorf("snapshot integrity check: %w", err)
	}
	var problems []error
	for rows.Next() {
		var msg string
		if err := rows.Scan(&msg); err != nil {
			rows.Close()
			return err
		}
		if msg != "ok" {
			problems = append(problems, errors.New(msg))
		}
	}
	// Close before switching journal mode: the pool has one connection here.
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("snapshot integrity check: %w", err)
	}
	if len(problems) > 0 {
		return fmt.Errorf("snapshot failed integrity check: %w", errors.Join(problems...))
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=DELETE`); err != nil {
		return fmt.Errorf("snapshot journal mode: %w", err)
	}
	return nil
}

func compress(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	zw := gzip.NewWriter(out)
	if _, err := io.Copy(zw, in); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return out.Sync()
}
