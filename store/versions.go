package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/johansundell/scribble/notes"
)

// Errors returned by the note history (#55, ADR 7).
var (
	ErrVersionNotFound = errors.New("that version of the note was not kept")
	ErrNoteDeleted     = errors.New("the note is deleted: undelete it first")
)

// Why a version was written: note_versions.reason.
const (
	reasonCreate      = "create"
	reasonEdit        = "edit"
	reasonRename      = "rename"
	reasonLinkRewrite = "link-rewrite"
	reasonRestore     = "restore"
	reasonDelete      = "delete"
	reasonUndelete    = "undelete"
	// A FileMaker edit that lost a conflict (#66), and the app's text
	// written again straight after it.
	reasonFileMakerConflict = "filemaker-conflict"
	reasonConflictKeptApp   = "conflict-kept-app"
)

// versionMergeWindow is how long an edit keeps replacing the edit version
// before it, so a burst of saves becomes one version. Tests change it.
var versionMergeWindow = 5 * time.Minute

const versionCols = `revision, title, body, deleted, reason, created_at, updated_at`

func scanVersion(r rowScanner) (*notes.Version, error) {
	var v notes.Version
	var del int
	var created, updated string
	if err := r.Scan(&v.Revision, &v.Title, &v.Body, &del, &v.Reason, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrVersionNotFound
		}
		return nil, err
	}
	v.Deleted = del == 1
	v.CreatedAt, _ = time.Parse(sqliteTimeLayout, created)
	v.UpdatedAt, _ = time.Parse(sqliteTimeLayout, updated)
	return &v, nil
}

// recordVersion copies note id as it now is into its history. An edit
// replaces the note's newest version instead when that is an edit too,
// younger than versionMergeWindow, and not the note's only version.
// Call it in the transaction that changed the note.
func (s *SQLStore) recordVersion(ctx context.Context, x dbx, now time.Time, id int64, reason string) error {
	ts := s.timeArg(now)
	if reason == reasonEdit {
		var rev, count int64
		var lastReason, created string
		err := x.QueryRowContext(ctx, `SELECT revision, reason, created_at,
			(SELECT COUNT(*) FROM note_versions WHERE note_id = ?)
			FROM note_versions WHERE note_id = ? ORDER BY revision DESC LIMIT 1`, id, id).Scan(&rev, &lastReason, &created, &count)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		lastAt, _ := time.Parse(sqliteTimeLayout, created) // zero, so no merge, when unreadable
		merge := err == nil && lastReason == reasonEdit && count > 1 && now.Sub(lastAt) < versionMergeWindow
		if merge {
			_, err := x.ExecContext(ctx, `UPDATE note_versions SET (revision, title, body, deleted, updated_at) =
				(SELECT revision, title, body, deleted, ? FROM notes WHERE id = ?)
				WHERE note_id = ? AND revision = ?`, ts, id, id, rev)
			return err
		}
	}
	_, err := x.ExecContext(ctx, `INSERT INTO note_versions (note_id, `+versionCols+`)
		SELECT id, revision, title, body, deleted, ?, ?, ? FROM notes WHERE id = ?`, reason, ts, ts, id)
	return err
}

// NoteVersions returns note id's versions, newest first, without bodies.
// A deleted note has them too.
func (s *SQLStore) NoteVersions(ctx context.Context, id int64) ([]notes.Version, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM notes WHERE id = ?)`, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNoteNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT revision, title, '', deleted, reason, created_at, updated_at
		FROM note_versions WHERE note_id = ? ORDER BY revision DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []notes.Version{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// NoteVersion returns the version of note id at revision rev, with its
// body, or ErrVersionNotFound when that revision was not kept.
func (s *SQLStore) NoteVersion(ctx context.Context, id, rev int64) (*notes.Version, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	return scanVersion(s.db.QueryRowContext(ctx,
		`SELECT `+versionCols+` FROM note_versions WHERE note_id = ? AND revision = ?`, id, rev))
}

// RestoreVersion sets note id's text to its version at rev, as a new
// revision. baseRev works as in UpdateNote; a deleted note gives
// ErrNoteDeleted.
func (s *SQLStore) RestoreVersion(ctx context.Context, id, rev, baseRev int64) (*notes.Note, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	cur, err := scanNote(tx.QueryRowContext(ctx, `SELECT `+noteCols+` FROM notes WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	if cur.Deleted {
		return nil, ErrNoteDeleted
	}
	if baseRev > 0 && cur.Revision != baseRev {
		return nil, ErrStaleRevision
	}
	v, err := scanVersion(tx.QueryRowContext(ctx,
		`SELECT `+versionCols+` FROM note_versions WHERE note_id = ? AND revision = ?`, id, rev))
	if err != nil {
		return nil, err
	}
	if err := s.writeNote(ctx, tx, time.Now(), id, v.Body, reasonRestore); err != nil {
		return nil, err
	}
	n, err := getNote(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return n, tx.Commit()
}

// DeletedNotes returns the deleted notes, most recently deleted first.
func (s *SQLStore) DeletedNotes(ctx context.Context) ([]notes.DeletedNote, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	// A delete sets updated_at to its version's created_at, and nothing
	// touches a deleted note after; for notes deleted before history
	// existed it is the only time there is.
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, updated_at FROM notes WHERE deleted = 1 ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []notes.DeletedNote{}
	for rows.Next() {
		var d notes.DeletedNote
		var at string
		if err := rows.Scan(&d.ID, &d.Title, &at); err != nil {
			return nil, err
		}
		d.DeletedAt, _ = time.Parse(sqliteTimeLayout, at)
		out = append(out, d)
	}
	return out, rows.Err()
}

// UndeleteNote brings deleted note id back with its last text. It gives
// ErrNoteNotFound when id is not a deleted note, and ErrTitleTaken when a
// live note has its title.
func (s *SQLStore) UndeleteNote(ctx context.Context, id int64) (*notes.Note, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now()
	res, err := tx.ExecContext(ctx, `UPDATE notes SET deleted = 0, revision = revision + 1,
		seq = `+nextSeq+`, updated_at = ? WHERE id = ? AND deleted = 1`, s.timeArg(now), id)
	if isUniqueViolation(err) {
		return nil, ErrTitleTaken
	}
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNoteNotFound
	}
	if err := s.recordVersion(ctx, tx, now, id, reasonUndelete); err != nil {
		return nil, err
	}
	n, err := getNote(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return n, tx.Commit()
}
