package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/johansundell/scribble/notes"
)

// Share links (ADR 11, ADR 15). A note has two, each with its own token,
// expiry and revoke: the view link (#83) and the edit link (#195). Sharing
// is not an edit: revision and seq stay.

// ErrTitleChanged is why a save through an edit link was refused: renaming
// is RenameNote's job, which rewrites the [[links]] to the note.
var ErrTitleChanged = errors.New("the title can't be changed through a share link")

// reasonShareEdit is the history reason of a save through an edit link.
const reasonShareEdit = "share-edit"

// Share is a note's share link: its token, when it stops working (nil for
// never) and, for an edit link, when it was made.
type Share struct {
	Token     string
	ExpiresAt *time.Time
	CreatedAt time.Time
}

// shareLink names the columns of one kind of share link.
type shareLink struct {
	token, expires string
	// created is "" for the view link, which keeps no creation time.
	created string
}

var (
	viewLink = shareLink{token: "share_token", expires: "share_expires_at"}
	editLink = shareLink{token: "share_edit_token", expires: "share_edit_expires_at", created: "share_edit_created_at"}
)

// live is the SQL condition of a link that has not expired; it takes the
// current time as its one argument.
func (l shareLink) live() string {
	return `(` + l.expires + ` IS NULL OR ` + l.expires + ` > ?)`
}

// createdCol is the column read back as the creation time: NULL for the view
// link.
func (l shareLink) createdCol() string {
	if l.created == "" {
		return "NULL"
	}
	return l.created
}

// clear is the SET clause that revokes the link.
func (l shareLink) clear() string {
	set := l.token + ` = NULL, ` + l.expires + ` = NULL`
	if l.created != "" {
		set += `, ` + l.created + ` = NULL`
	}
	return set
}

// clearShares is the SET clause that revokes both links, for a delete.
var clearShares = viewLink.clear() + `, ` + editLink.clear()

// newShareToken returns 128 random bits as hex.
func newShareToken() string {
	b := make([]byte, 16)
	rand.Read(b) // never fails (crypto/rand)
	return hex.EncodeToString(b)
}

// expiryArg is the driver argument for an expiry column: NULL for never.
func (s *SQLStore) expiryArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return s.timeArg(*t)
}

// parseExpiry reads an expiry column back; NULL is never.
func parseExpiry(v sql.NullString) *time.Time {
	if !v.Valid {
		return nil
	}
	t, _ := time.Parse(sqliteTimeLayout, v.String)
	return &t
}

// share gives note id link l, keeping a live token (and its creation time),
// sets its expiry and returns the token.
func (s *SQLStore) share(ctx context.Context, l shareLink, id int64, expires *time.Time) (string, error) {
	if err := s.checkNotes(); err != nil {
		return "", err
	}
	now := s.timeArg(time.Now())
	keep := l.token + ` IS NOT NULL AND ` + l.live()
	set := l.token + ` = CASE WHEN ` + keep + ` THEN ` + l.token + ` ELSE ? END, ` + l.expires + ` = ?`
	args := []any{now, newShareToken(), s.expiryArg(expires)}
	if l.created != "" {
		// SET reads the row as it was, so keep sees the old token.
		set = l.created + ` = CASE WHEN ` + keep + ` THEN ` + l.created + ` ELSE ? END, ` + set
		args = append([]any{now, now}, args...)
	}
	var tok string
	err := s.db.QueryRowContext(ctx, `UPDATE notes SET `+set+` WHERE id = ? AND deleted = 0 RETURNING `+l.token,
		append(args, id)...).Scan(&tok)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoteNotFound
	}
	return tok, err
}

func (s *SQLStore) unshare(ctx context.Context, l shareLink, id int64) error {
	if err := s.checkNotes(); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE notes SET `+l.clear()+` WHERE id = ? AND deleted = 0`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoteNotFound
	}
	return nil
}

// noteShare is note id's live link l, or the zero Share.
func (s *SQLStore) noteShare(ctx context.Context, l shareLink, id int64) (Share, error) {
	if err := s.checkNotes(); err != nil {
		return Share{}, err
	}
	var tok, exp, created sql.NullString
	var live bool
	err := s.db.QueryRowContext(ctx, `SELECT `+l.token+`, `+l.expires+`, `+l.createdCol()+`, `+l.live()+`
		FROM notes WHERE id = ? AND deleted = 0`, s.timeArg(time.Now()), id).Scan(&tok, &exp, &created, &live)
	if errors.Is(err, sql.ErrNoRows) {
		return Share{}, ErrNoteNotFound
	}
	if err != nil || !tok.Valid || !live {
		return Share{}, err
	}
	return shareOf(tok.String, exp, created), nil
}

func shareOf(tok string, exp, created sql.NullString) Share {
	sh := Share{Token: tok, ExpiresAt: parseExpiry(exp)}
	if created.Valid {
		sh.CreatedAt, _ = time.Parse(sqliteTimeLayout, created.String)
	}
	return sh
}

// sharedNote is the live note whose live link l has token, read in x.
func (s *SQLStore) sharedNote(ctx context.Context, x dbx, l shareLink, token string) (*notes.Note, Share, error) {
	if err := s.checkNotes(); err != nil {
		return nil, Share{}, err
	}
	if token == "" {
		return nil, Share{}, ErrNoteNotFound
	}
	var exp, created sql.NullString
	r := x.QueryRowContext(ctx, `SELECT `+noteCols+`, `+l.expires+`, `+l.createdCol()+` FROM notes
		WHERE `+l.token+` = ? AND deleted = 0 AND `+l.live(), token, s.timeArg(time.Now()))
	n, err := scanNote(scanFunc(func(dest ...any) error { return r.Scan(append(dest, &exp, &created)...) }))
	if err != nil {
		return nil, Share{}, err
	}
	return n, shareOf(token, exp, created), nil
}

func (s *SQLStore) ShareNote(ctx context.Context, id int64, expires *time.Time) (string, error) {
	return s.share(ctx, viewLink, id, expires)
}

func (s *SQLStore) UnshareNote(ctx context.Context, id int64) error {
	return s.unshare(ctx, viewLink, id)
}

func (s *SQLStore) NoteShare(ctx context.Context, id int64) (Share, error) {
	return s.noteShare(ctx, viewLink, id)
}

func (s *SQLStore) GetSharedNote(ctx context.Context, token string) (*notes.Note, *time.Time, error) {
	n, sh, err := s.sharedNote(ctx, s.db, viewLink, token)
	return n, sh.ExpiresAt, err
}

func (s *SQLStore) ShareNoteEdit(ctx context.Context, id int64, expires *time.Time) (string, error) {
	return s.share(ctx, editLink, id, expires)
}

func (s *SQLStore) UnshareNoteEdit(ctx context.Context, id int64) error {
	return s.unshare(ctx, editLink, id)
}

func (s *SQLStore) NoteEditShare(ctx context.Context, id int64) (Share, error) {
	return s.noteShare(ctx, editLink, id)
}

func (s *SQLStore) GetEditSharedNote(ctx context.Context, token string) (*notes.Note, Share, error) {
	return s.sharedNote(ctx, s.db, editLink, token)
}

func (s *SQLStore) UpdateEditSharedNote(ctx context.Context, token, body string, baseRev int64) (*notes.Note, error) {
	if err := s.checkNotes(); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	cur, _, err := s.sharedNote(ctx, tx, editLink, token)
	if err != nil {
		return nil, err
	}
	if baseRev > 0 && cur.Revision != baseRev {
		return nil, ErrStaleRevision
	}
	if notes.TitleOf(body) != cur.Title {
		return nil, ErrTitleChanged
	}
	if err := s.writeNote(ctx, tx, time.Now(), cur.ID, body, reasonShareEdit); err != nil {
		return nil, err
	}
	n, err := getNote(ctx, tx, cur.ID)
	if err != nil {
		return nil, err
	}
	return n, tx.Commit()
}
