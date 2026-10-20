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
	// ShareNote gives the note a public share link token, keeping the one
	// it has, and returns it. Sharing is not an edit: revision and seq stay.
	ShareNote(ctx context.Context, id int64) (string, error)
	// UnshareNote clears the token, so its link stops working.
	UnshareNote(ctx context.Context, id int64) error
	// NoteShareToken returns the note's token, or "" when it isn't shared.
	NoteShareToken(ctx context.Context, id int64) (string, error)
	// GetSharedNote returns the live note with token, or ErrNoteNotFound.
	GetSharedNote(ctx context.Context, token string) (*notes.Note, error)
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

func (s *SQLStore) nowArg() any { return s.timeArg(time.Now()) }

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
	if notes.TitleOf(body) == "" {
		return nil, ErrNoTitle
	}
	now := s.nowArg()
	res, err := s.db.ExecContext(ctx, `INSERT INTO notes (title, body, seq, created_at, updated_at)
		VALUES (?, ?, `+nextSeq+`, ?, ?)`, notes.TitleOf(body), body, now, now)
	if isUniqueViolation(err) {
		return nil, ErrTitleTaken
	}
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return getNote(ctx, s.db, id)
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

func writeNote(ctx context.Context, x dbx, now any, id int64, body string) error {
	title := notes.TitleOf(body)
	if title == "" {
		return ErrNoTitle
	}
	_, err := x.ExecContext(ctx, `UPDATE notes SET title = ?, body = ?, revision = revision + 1,
		seq = `+nextSeq+`, updated_at = ? WHERE id = ? AND deleted = 0`, title, body, now, id)
	if isUniqueViolation(err) {
		return ErrTitleTaken
	}
	return err
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
	if err := writeNote(ctx, tx, s.nowArg(), id, body); err != nil {
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
	res, err := s.db.ExecContext(ctx, `UPDATE notes SET deleted = 1, share_token = NULL, revision = revision + 1,
		seq = `+nextSeq+`, updated_at = ? WHERE id = ? AND deleted = 0`, s.nowArg(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoteNotFound
	}
	return nil
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
	now := s.nowArg()
	if err := writeNote(ctx, tx, now, id, notes.SetTitle(n.Body, newTitle)); err != nil {
		return nil, err
	}
	for _, other := range linking {
		if err := writeNote(ctx, tx, now, other.ID, notes.RewriteLinks(other.Body, n.Title, newTitle)); err != nil {
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

func (s *SQLStore) ShareNote(ctx context.Context, id int64) (string, error) {
	if err := s.checkNotes(); err != nil {
		return "", err
	}
	// A note that is already shared keeps its token.
	if _, err := s.db.ExecContext(ctx, `UPDATE notes SET share_token = ?
		WHERE id = ? AND deleted = 0 AND share_token IS NULL`, newShareToken(), id); err != nil {
		return "", err
	}
	return s.NoteShareToken(ctx, id)
}

func (s *SQLStore) UnshareNote(ctx context.Context, id int64) error {
	if err := s.checkNotes(); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE notes SET share_token = NULL WHERE id = ? AND deleted = 0`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoteNotFound
	}
	return nil
}

func (s *SQLStore) NoteShareToken(ctx context.Context, id int64) (string, error) {
	if err := s.checkNotes(); err != nil {
		return "", err
	}
	var tok sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT share_token FROM notes WHERE id = ? AND deleted = 0`, id).Scan(&tok)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoteNotFound
	}
	return tok.String, err
}

func (s *SQLStore) GetSharedNote(ctx context.Context, token string) (*notes.Note, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	if token == "" {
		return nil, ErrNoteNotFound
	}
	return scanNote(s.db.QueryRowContext(ctx,
		`SELECT `+noteCols+` FROM notes WHERE share_token = ? AND deleted = 0`, token))
}
