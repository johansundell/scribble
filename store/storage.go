package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/johansundell/scribble/types"
)

// Store is the storage contract handlers and middleware depend on. Each
// backend owns its connection; the caller must Close it.
type Store interface {
	Ping(ctx context.Context) error
	// GetLogs returns the entries with from <= CreatedAt < to, oldest first
	// (by CreatedAt, then ID), limited to page.
	GetLogs(ctx context.Context, from, to time.Time, page Page) ([]types.UsageLog, error)
	// LogRequests persists a batch of entries, all or nothing where the
	// backend supports it. Wrap errors that retrying cannot fix with Permanent.
	LogRequests(ctx context.Context, entries []types.UsageLog) error
	Close() error
}

// Page selects a slice of an ordered result: skip Offset entries, then return
// at most Limit. A Limit of 0 or less means no limit.
type Page struct {
	Limit  int
	Offset int
}

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent marks err as one that retrying will not fix, such as a rejected
// record, so the log writer drops the batch instead of retrying it.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// IsPermanent reports whether err was marked with Permanent.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

// SQLStore keeps request logs in a SQL database (SQLite or MySQL).
type SQLStore struct {
	db *sql.DB
	// timeArg converts a timestamp into the driver argument for created_at.
	timeArg func(time.Time) any
	// noLimit is the LIMIT value meaning "all rows", needed with OFFSET.
	noLimit int64
	// notesEnabled is true when the notes table exists (SQLite only).
	notesEnabled bool
	// schema is the SQLite schema version once migrated; 0 for MySQL.
	schema int
}

// SchemaVersioner is a store with a schema version: how many migrations its
// database has run. Only SQLite has one.
type SchemaVersioner interface {
	// SchemaVersion returns the version read at open, and false when the
	// store has none.
	SchemaVersion() (int, bool)
}

// SchemaVersion returns the SQLite schema version read at open; MySQL has
// none.
func (s *SQLStore) SchemaVersion() (int, bool) {
	return s.schema, s.schema > 0
}

func (s *SQLStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *SQLStore) Close() error {
	return s.db.Close()
}

// LogRequests writes the batch in one transaction.
func (s *SQLStore) LogRequests(ctx context.Context, entries []types.UsageLog) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO request_logs (status, method, error, endpoint, created_at, response, request) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		if _, err := stmt.ExecContext(ctx, e.Status, e.Method, e.Error, e.Endpoint, s.timeArg(e.CreatedAt), string(e.Response), string(e.Request)); err != nil {
			return fmt.Errorf("insert request log: %w", err)
		}
	}
	return tx.Commit()
}

func (s *SQLStore) GetLogs(ctx context.Context, from, to time.Time, page Page) ([]types.UsageLog, error) {
	query := `SELECT id, status, method, error, endpoint, created_at, response, request FROM request_logs WHERE created_at >= ? AND created_at < ? ORDER BY created_at, id`
	args := []any{s.timeArg(from), s.timeArg(to)}
	if page.Limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, page.Limit, max(page.Offset, 0))
	} else if page.Offset > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, s.noLimit, page.Offset)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []types.UsageLog
	for rows.Next() {
		var l types.UsageLog
		if err := rows.Scan(&l.ID, &l.Status, &l.Method, &l.Error, &l.Endpoint, &l.CreatedAt, &l.Response, &l.Request); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return logs, nil
}
