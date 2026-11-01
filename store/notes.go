package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/johansundell/scribble/notes"
)

// Errors returned by NoteStore. Handlers map them to HTTP statuses.
var (
	ErrNoteNotFound     = errors.New("note not found")
	ErrStaleRevision    = errors.New("note was changed by someone else")
	ErrTitleTaken       = errors.New("a note with that title already exists")
	ErrNoTitle          = errors.New("a note needs a title (first line)")
	ErrNotesUnsupported = errors.New("notes need STORAGE=sqlite")
)

// NoteStore is the notes contract. SQLStore implements it; only the SQLite
// backend actually enables it (the others return ErrNotesUnsupported).
type NoteStore interface {
	CreateNote(ctx context.Context, body string) (*notes.Note, error)
	GetNote(ctx context.Context, id int64) (*notes.Note, error)
	GetNoteByTitle(ctx context.Context, title string) (*notes.Note, error)
	// ListNotes returns live notes, newest first; q filters on title/body
	// and a non-empty project keeps only notes tagged #project.
	ListNotes(ctx context.Context, q, project string) ([]notes.Note, error)
	// Projects returns the distinct #project names across live notes, sorted.
	Projects(ctx context.Context) ([]string, error)
	// NoteChanges returns every note, tombstones included, with Seq > since.
	NoteChanges(ctx context.Context, since int64) ([]notes.Note, error)
	// UpdateNote replaces the body. baseRev > 0 must match the stored
	// revision or ErrStaleRevision is returned.
	UpdateNote(ctx context.Context, id int64, body string, baseRev int64) (*notes.Note, error)
	DeleteNote(ctx context.Context, id int64) error
	NoteBacklinks(ctx context.Context, id int64) ([]notes.Note, error)
	// RenameNote changes the title and rewrites [[links]] in other notes.
	RenameNote(ctx context.Context, id int64, newTitle string) (*notes.Note, error)
	// ShareNote gives the note a public share link token, keeping a live
	// one, sets when it stops working (expires, nil for never) and returns
	// it. An expired token is replaced. Sharing is not an edit: revision
	// and seq stay.
	ShareNote(ctx context.Context, id int64, expires *time.Time) (string, error)
	// UnshareNote clears the token, so its link stops working.
	UnshareNote(ctx context.Context, id int64) error
	// NoteShare returns the note's live share link, or the zero Share when
	// it isn't shared or its link has expired.
	NoteShare(ctx context.Context, id int64) (Share, error)
	// GetSharedNote returns the live note with an unexpired token and when
	// the link expires (nil for never), or ErrNoteNotFound.
	GetSharedNote(ctx context.Context, token string) (*notes.Note, *time.Time, error)

	// History (#55, ADR 7). Every write above records a version.
	// NoteVersions lists note id's versions, newest first, without bodies.
	NoteVersions(ctx context.Context, id int64) ([]notes.Version, error)
	// NoteVersion returns one version with its body, or ErrVersionNotFound.
	NoteVersion(ctx context.Context, id, rev int64) (*notes.Version, error)
	// RestoreVersion makes version rev the note's text again, as a new
	// revision; baseRev works as in UpdateNote.
	RestoreVersion(ctx context.Context, id, rev, baseRev int64) (*notes.Note, error)
	// DeletedNotes lists deleted notes, most recently deleted first.
	DeletedNotes(ctx context.Context) ([]notes.DeletedNote, error)
	// UndeleteNote brings a deleted note back with its last text.
	UndeleteNote(ctx context.Context, id int64) (*notes.Note, error)
}

const noteCols = `id, title, body, revision, seq, deleted, created_at, updated_at`

// nextSeq is the global change counter: every write takes the next number.
const nextSeq = `(SELECT COALESCE(MAX(seq), 0) + 1 FROM notes)`

// dbx is what *sql.DB and *sql.Tx have in common, so helpers can run inside
// a transaction.
type dbx interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

type rowScanner interface{ Scan(...any) error }

// scanFunc lets a function stand in for a row, e.g. to scan extra columns
// after the ones scanNote reads.
type scanFunc func(...any) error

func (f scanFunc) Scan(dest ...any) error { return f(dest...) }

func scanNote(r rowScanner) (*notes.Note, error) {
	var n notes.Note
	var del int
	var created, updated string
	if err := r.Scan(&n.ID, &n.Title, &n.Body, &n.Revision, &n.Seq, &del, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoteNotFound
		}
		return nil, err
	}
	n.Deleted = del == 1
	n.CreatedAt, _ = time.Parse(sqliteTimeLayout, created)
	n.UpdatedAt, _ = time.Parse(sqliteTimeLayout, updated)
	return &n, nil
}

func (s *SQLStore) checkNotes() error {
	if !s.notesEnabled {
		return ErrNotesUnsupported
	}
	return nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE")
}

func queryNotes(ctx context.Context, x dbx, q string, args ...any) ([]notes.Note, error) {
	rows, err := x.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []notes.Note{}
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

func getNote(ctx context.Context, x dbx, id int64) (*notes.Note, error) {
	return scanNote(x.QueryRowContext(ctx, `SELECT `+noteCols+` FROM notes WHERE id = ? AND deleted = 0`, id))
}

func (s *SQLStore) CreateNote(ctx context.Context, body string) (*notes.Note, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id, err := s.createNote(ctx, tx, body)
	if err != nil {
		return nil, err
	}
	n, err := getNote(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return n, tx.Commit()
}

// createNote adds a note with body and its first version, and returns its id.
func (s *SQLStore) createNote(ctx context.Context, x dbx, body string) (int64, error) {
	if notes.TitleOf(body) == "" {
		return 0, ErrNoTitle
	}
	now := time.Now()
	res, err := x.ExecContext(ctx, `INSERT INTO notes (title, body, seq, created_at, updated_at)
		VALUES (?, ?, `+nextSeq+`, ?, ?)`, notes.TitleOf(body), body, s.timeArg(now), s.timeArg(now))
	if isUniqueViolation(err) {
		return 0, ErrTitleTaken
	}
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, s.recordVersion(ctx, x, now, id, reasonCreate)
}

func (s *SQLStore) GetNote(ctx context.Context, id int64) (*notes.Note, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	return getNote(ctx, s.db, id)
}

func (s *SQLStore) GetNoteByTitle(ctx context.Context, title string) (*notes.Note, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	return scanNote(s.db.QueryRowContext(ctx,
		`SELECT `+noteCols+` FROM notes WHERE lower(title) = lower(?) AND deleted = 0`, title))
}

func (s *SQLStore) ListNotes(ctx context.Context, q, project string) ([]notes.Note, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	query := `SELECT ` + noteCols + ` FROM notes WHERE deleted = 0`
	var args []any
	if q != "" {
		query += ` AND (title LIKE ? ESCAPE '\' OR body LIKE ? ESCAPE '\')`
		like := "%" + escapeLike(q) + "%"
		args = append(args, like, like)
	}
	project = notes.NormalizeProject(project)
	if project != "" {
		// LIKE (case-insensitive for ASCII) only narrows the candidates;
		// HasProject does the exact match.
		query += ` AND body LIKE ? ESCAPE '\'`
		args = append(args, "%#"+escapeLike(project)+"%")
	}
	query += ` ORDER BY updated_at DESC, id DESC`
	list, err := queryNotes(ctx, s.db, query, args...)
	if err != nil || project == "" {
		return list, err
	}
	out := []notes.Note{}
	for _, n := range list {
		if notes.HasProject(n.Body, project) {
			out = append(out, n)
		}
	}
	return out, nil
}

func (s *SQLStore) Projects(ctx context.Context) ([]string, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	list, err := queryNotes(ctx, s.db, `SELECT `+noteCols+` FROM notes WHERE deleted = 0 AND body LIKE '%#%'`)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, n := range list {
		out = append(out, notes.Projects(n.Body)...)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// escapeLike makes user text match literally inside a LIKE pattern.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (s *SQLStore) NoteChanges(ctx context.Context, since int64) ([]notes.Note, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	return queryNotes(ctx, s.db, `SELECT `+noteCols+` FROM notes WHERE seq > ? ORDER BY seq`, since)
}

// writeNote replaces live note id's body and records the new version with
// reason.
func (s *SQLStore) writeNote(ctx context.Context, x dbx, now time.Time, id int64, body, reason string) error {
	title := notes.TitleOf(body)
	if title == "" {
		return ErrNoTitle
	}
	_, err := x.ExecContext(ctx, `UPDATE notes SET title = ?, body = ?, revision = revision + 1,
		seq = `+nextSeq+`, updated_at = ? WHERE id = ? AND deleted = 0`, title, body, s.timeArg(now), id)
	if isUniqueViolation(err) {
		return ErrTitleTaken
	}
	if err != nil {
		return err
	}
	return s.recordVersion(ctx, x, now, id, reason)
}

func (s *SQLStore) UpdateNote(ctx context.Context, id int64, body string, baseRev int64) (*notes.Note, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	cur, err := getNote(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if baseRev > 0 && cur.Revision != baseRev {
		return nil, ErrStaleRevision
	}
	if err := s.writeNote(ctx, tx, time.Now(), id, body, reasonEdit); err != nil {
		return nil, err
	}
	n, err := getNote(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return n, tx.Commit()
}

// DeleteNote soft-deletes, leaving a tombstone so synced clients learn of it.
// The share link goes with the note.
func (s *SQLStore) DeleteNote(ctx context.Context, id int64) error {
	if err := s.checkNotes(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.deleteNote(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStore) deleteNote(ctx context.Context, x dbx, id int64) error {
	now := time.Now()
	res, err := x.ExecContext(ctx, `UPDATE notes SET deleted = 1, share_token = NULL, share_expires_at = NULL, revision = revision + 1,
		seq = `+nextSeq+`, updated_at = ? WHERE id = ? AND deleted = 0`, s.timeArg(now), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoteNotFound
	}
	return s.recordVersion(ctx, x, now, id, reasonDelete)
}

func backlinks(ctx context.Context, x dbx, n *notes.Note) ([]notes.Note, error) {
	// LIKE only narrows the candidates; HasLinkTo does the exact match.
	cands, err := queryNotes(ctx, x,
		`SELECT `+noteCols+` FROM notes WHERE deleted = 0 AND id <> ? AND body LIKE ? ESCAPE '\'`,
		n.ID, "%[[%"+escapeLike(n.Title)+"%]]%")
	if err != nil {
		return nil, err
	}
	out := []notes.Note{}
	for _, c := range cands {
		if notes.HasLinkTo(c.Body, n.Title) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *SQLStore) NoteBacklinks(ctx context.Context, id int64) ([]notes.Note, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	n, err := getNote(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	return backlinks(ctx, s.db, n)
}

func (s *SQLStore) RenameNote(ctx context.Context, id int64, newTitle string) (*notes.Note, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	newTitle = strings.TrimSpace(newTitle)
	if newTitle == "" || strings.ContainsAny(newTitle, "\n[]") {
		return nil, ErrNoTitle
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	n, err := getNote(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	linking, err := backlinks(ctx, tx, n)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.writeNote(ctx, tx, now, id, notes.SetTitle(n.Body, newTitle), reasonRename); err != nil {
		return nil, err
	}
	for _, other := range linking {
		if err := s.writeNote(ctx, tx, now, other.ID, notes.RewriteLinks(other.Body, n.Title, newTitle), reasonLinkRewrite); err != nil {
			return nil, err
		}
	}
	renamed, err := getNote(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return renamed, tx.Commit()
}

// newShareToken returns 128 random bits as hex.
func newShareToken() string {
	b := make([]byte, 16)
	rand.Read(b) // never fails (crypto/rand)
	return hex.EncodeToString(b)
}

// Share is a note's public share link: its token and when it stops working
// (nil for never).
type Share struct {
	Token     string
	ExpiresAt *time.Time
}

// liveShare is the SQL condition of a share link that has not expired; it
// takes the current time as its one argument.
const liveShare = `(share_expires_at IS NULL OR share_expires_at > ?)`

// expiryArg is the driver argument for share_expires_at: NULL for never.
func (s *SQLStore) expiryArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return s.timeArg(*t)
}

// parseExpiry reads share_expires_at back; NULL is never.
func parseExpiry(v sql.NullString) *time.Time {
	if !v.Valid {
		return nil
	}
	t, _ := time.Parse(sqliteTimeLayout, v.String)
	return &t
}

func (s *SQLStore) ShareNote(ctx context.Context, id int64, expires *time.Time) (string, error) {
	if err := s.checkNotes(); err != nil {
		return "", err
	}
	// A live link keeps its token; a missing or expired one gets a new one.
	var tok string
	err := s.db.QueryRowContext(ctx, `UPDATE notes SET
		share_token = CASE WHEN share_token IS NOT NULL AND `+liveShare+` THEN share_token ELSE ? END,
		share_expires_at = ?
		WHERE id = ? AND deleted = 0 RETURNING share_token`,
		s.timeArg(time.Now()), newShareToken(), s.expiryArg(expires), id).Scan(&tok)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoteNotFound
	}
	return tok, err
}

func (s *SQLStore) UnshareNote(ctx context.Context, id int64) error {
	if err := s.checkNotes(); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE notes SET share_token = NULL, share_expires_at = NULL
		WHERE id = ? AND deleted = 0`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoteNotFound
	}
	return nil
}

func (s *SQLStore) NoteShare(ctx context.Context, id int64) (Share, error) {
	if err := s.checkNotes(); err != nil {
		return Share{}, err
	}
	var tok, exp sql.NullString
	var live bool
	err := s.db.QueryRowContext(ctx, `SELECT share_token, share_expires_at, `+liveShare+`
		FROM notes WHERE id = ? AND deleted = 0`, s.timeArg(time.Now()), id).Scan(&tok, &exp, &live)
	if errors.Is(err, sql.ErrNoRows) {
		return Share{}, ErrNoteNotFound
	}
	if err != nil || !tok.Valid || !live {
		return Share{}, err
	}
	return Share{Token: tok.String, ExpiresAt: parseExpiry(exp)}, nil
}

func (s *SQLStore) GetSharedNote(ctx context.Context, token string) (*notes.Note, *time.Time, error) {
	if err := s.checkNotes(); err != nil {
		return nil, nil, err
	}
	if token == "" {
		return nil, nil, ErrNoteNotFound
	}
	var exp sql.NullString
	r := s.db.QueryRowContext(ctx, `SELECT `+noteCols+`, share_expires_at FROM notes
		WHERE share_token = ? AND deleted = 0 AND `+liveShare, token, s.timeArg(time.Now()))
	n, err := scanNote(scanFunc(func(dest ...any) error { return r.Scan(append(dest, &exp)...) }))
	if err != nil {
		return nil, nil, err
	}
	return n, parseExpiry(exp), nil
}
