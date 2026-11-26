package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

// The edit link (#195) lives beside the view link: shared, expired and
// revoked on its own.
func TestShareNoteEdit(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	n, err := s.CreateNote(ctx, "# Shared\nbody")
	if err != nil {
		t.Fatal(err)
	}
	if sh, err := s.NoteEditShare(ctx, n.ID); err != nil || sh != (Share{}) {
		t.Fatalf("a new note has an edit link: %+v %v", sh, err)
	}

	before := time.Now()
	tok, err := s.ShareNoteEdit(ctx, n.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 32 || strings.Trim(tok, "0123456789abcdef") != "" {
		t.Errorf("token %q is not 128 bits of hex", tok)
	}
	sh, err := s.NoteEditShare(ctx, n.ID)
	if err != nil || sh.Token != tok || sh.ExpiresAt != nil || sh.CreatedAt.Before(before.Add(-time.Second)) {
		t.Fatalf("NoteEditShare: %+v %v, want %q made now", sh, err, tok)
	}
	// The view link is untouched, and the edit token opens nothing at /s/.
	if view, _ := s.NoteShare(ctx, n.ID); view != (Share{}) {
		t.Errorf("edit link made a view link: %+v", view)
	}
	if _, _, err := s.GetSharedNote(ctx, tok); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("edit token works as a view token: %v", err)
	}
	viewTok, err := s.ShareNote(ctx, n.ID, nil)
	if err != nil || viewTok == tok {
		t.Fatalf("view token %q %v", viewTok, err)
	}
	if _, _, err := s.GetEditSharedNote(ctx, viewTok); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("view token works as an edit token: %v", err)
	}

	got, gsh, err := s.GetEditSharedNote(ctx, tok)
	if err != nil || got.ID != n.ID || gsh.Token != tok || !gsh.CreatedAt.Equal(sh.CreatedAt) {
		t.Fatalf("GetEditSharedNote: %+v %+v %v", got, gsh, err)
	}
	// Sharing is not an edit.
	if got.Revision != n.Revision || got.Seq != n.Seq {
		t.Errorf("sharing changed revision or seq: %+v", got)
	}
	// Sharing a live link again keeps it and its history window.
	if again, err := s.ShareNoteEdit(ctx, n.ID, nil); err != nil || again != tok {
		t.Errorf("share again: %q %v, want %q", again, err, tok)
	}
	if again, _ := s.NoteEditShare(ctx, n.ID); !again.CreatedAt.Equal(sh.CreatedAt) {
		t.Errorf("share again moved created_at: %v, want %v", again.CreatedAt, sh.CreatedAt)
	}

	// Revoking the edit link leaves the view link.
	if err := s.UnshareNoteEdit(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetEditSharedNote(ctx, tok); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("revoked edit token: %v", err)
	}
	if view, _ := s.NoteShare(ctx, n.ID); view.Token != viewTok {
		t.Errorf("revoking the edit link revoked the view link: %+v", view)
	}
	var left, leftExp, leftAt sql.NullString
	if err := s.db.QueryRow(`SELECT share_edit_token, share_edit_expires_at, share_edit_created_at FROM notes WHERE id = ?`, n.ID).
		Scan(&left, &leftExp, &leftAt); err != nil || left.Valid || leftExp.Valid || leftAt.Valid {
		t.Errorf("after revoke: %v %v %v (%v), want NULL", left, leftExp, leftAt, err)
	}
	// A new edit link gets a new token and a new window.
	fresh, err := s.ShareNoteEdit(ctx, n.ID, nil)
	if err != nil || fresh == tok {
		t.Fatalf("reshare: %q %v, want a new token", fresh, err)
	}
	if fsh, _ := s.NoteEditShare(ctx, n.ID); !fsh.CreatedAt.After(sh.CreatedAt) {
		t.Errorf("reshare kept created_at %v", fsh.CreatedAt)
	}

	// Deleting the note clears both links.
	if err := s.DeleteNote(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetEditSharedNote(ctx, fresh); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("deleted note's edit token: %v", err)
	}
	if err := s.db.QueryRow(`SELECT share_edit_token, share_edit_expires_at, share_edit_created_at FROM notes WHERE id = ?`, n.ID).
		Scan(&left, &leftExp, &leftAt); err != nil || left.Valid || leftExp.Valid || leftAt.Valid {
		t.Errorf("tombstone: %v %v %v (%v), want NULL", left, leftExp, leftAt, err)
	}
	if _, err := s.ShareNoteEdit(ctx, n.ID, nil); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("share deleted note: %v", err)
	}
	if err := s.UnshareNoteEdit(ctx, n.ID); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("unshare deleted note: %v", err)
	}
	for _, bad := range []string{"", "nope"} {
		if _, _, err := s.GetEditSharedNote(ctx, bad); !errors.Is(err, ErrNoteNotFound) {
			t.Errorf("GetEditSharedNote(%q): %v", bad, err)
		}
	}
}

func TestShareNoteEdit_Expiry(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	n, _ := s.CreateNote(ctx, "# Timed\nbody")
	later := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	tok, err := s.ShareNoteEdit(ctx, n.ID, &later)
	if err != nil {
		t.Fatal(err)
	}
	if _, sh, err := s.GetEditSharedNote(ctx, tok); err != nil || sh.ExpiresAt == nil || !sh.ExpiresAt.Equal(later) {
		t.Fatalf("before expiry: %+v %v, want expiry %v", sh, err, later)
	}
	past := time.Now().Add(-time.Minute)
	if _, err := s.ShareNoteEdit(ctx, n.ID, &past); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetEditSharedNote(ctx, tok); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("expired edit token: %v", err)
	}
	if sh, _ := s.NoteEditShare(ctx, n.ID); sh != (Share{}) {
		t.Errorf("expired edit link reads as %+v", sh)
	}
	if _, err := s.UpdateEditSharedNote(ctx, tok, "# Timed\nnew", n.Revision); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("save through an expired link: %v", err)
	}
	if fresh, err := s.ShareNoteEdit(ctx, n.ID, nil); err != nil || fresh == tok {
		t.Errorf("reshare after expiry: %q %v, want a new token", fresh, err)
	}
}

func TestUpdateEditSharedNote(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	n, _ := s.CreateNote(ctx, "# Shared\nbody")
	tok, err := s.ShareNoteEdit(ctx, n.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	up, err := s.UpdateEditSharedNote(ctx, tok, "# Shared\nnew body", n.Revision)
	if err != nil || up.Body != "# Shared\nnew body" || up.Revision != n.Revision+1 || up.Seq <= n.Seq {
		t.Fatalf("save: %+v %v", up, err)
	}
	vs, err := s.NoteVersions(ctx, n.ID)
	if err != nil || len(vs) != 2 || vs[0].Reason != "share-edit" || vs[0].Revision != up.Revision {
		t.Fatalf("versions %+v %v, want a share-edit version on top", vs, err)
	}
	// Each save is its own version, never merged into the one before.
	up2, err := s.UpdateEditSharedNote(ctx, tok, "# Shared\nnewer", up.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if vs, _ := s.NoteVersions(ctx, n.ID); len(vs) != 3 || vs[0].Revision != up2.Revision {
		t.Errorf("second save merged: %+v", vs)
	}

	// A stale base revision is a conflict; 0 overwrites.
	if _, err := s.UpdateEditSharedNote(ctx, tok, "# Shared\nstale", up.Revision); !errors.Is(err, ErrStaleRevision) {
		t.Errorf("stale save: %v", err)
	}
	if _, err := s.UpdateEditSharedNote(ctx, tok, "# Shared\nforced", 0); err != nil {
		t.Errorf("overwrite: %v", err)
	}

	// The title can't change, not even its case; its markup can.
	cur, _ := s.GetNote(ctx, n.ID)
	for _, body := range []string{"# Renamed\nbody", "# shared\nbody", "\nbody", "# Shared [[x]]\nbody"} {
		if _, err := s.UpdateEditSharedNote(ctx, tok, body, cur.Revision); !errors.Is(err, ErrTitleChanged) {
			t.Errorf("save %q: %v, want ErrTitleChanged", body, err)
		}
	}
	if same, _ := s.GetNote(ctx, n.ID); same.Body != cur.Body || same.Revision != cur.Revision {
		t.Errorf("a refused save changed the note: %+v", same)
	}
	if _, err := s.UpdateEditSharedNote(ctx, tok, "Shared\nplain title line", cur.Revision); err != nil {
		t.Errorf("same title without '#': %v", err)
	}

	for _, bad := range []string{"", "nope"} {
		if _, err := s.UpdateEditSharedNote(ctx, bad, "# Shared\nx", 0); !errors.Is(err, ErrNoteNotFound) {
			t.Errorf("save with token %q: %v", bad, err)
		}
	}
	if err := s.UnshareNoteEdit(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateEditSharedNote(ctx, tok, "# Shared\nx", 0); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("save through a revoked link: %v", err)
	}
}
