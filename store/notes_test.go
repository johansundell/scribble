package store

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func newNoteStore(t *testing.T) *SQLStore {
	t.Helper()
	s, err := NewSQLite(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNotesCRUD(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)

	n, err := s.CreateNote(ctx, "# Groceries\n\nmilk")
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "Groceries" || n.Revision != 1 || n.Seq != 1 {
		t.Fatalf("unexpected note %+v", n)
	}
	if _, err := s.CreateNote(ctx, "groceries\nduplicate"); !errors.Is(err, ErrTitleTaken) {
		t.Errorf("want ErrTitleTaken (case-insensitive), got %v", err)
	}
	if _, err := s.CreateNote(ctx, "  \n "); !errors.Is(err, ErrNoTitle) {
		t.Errorf("want ErrNoTitle, got %v", err)
	}

	got, err := s.GetNoteByTitle(ctx, "GROCERIES")
	if err != nil || got.ID != n.ID {
		t.Fatalf("GetNoteByTitle: %v %+v", err, got)
	}

	upd, err := s.UpdateNote(ctx, n.ID, "# Groceries\n\nmilk, eggs", n.Revision)
	if err != nil || upd.Revision != 2 || upd.Seq <= n.Seq {
		t.Fatalf("update: %v %+v", err, upd)
	}
	// Writing again from the old revision must fail.
	if _, err := s.UpdateNote(ctx, n.ID, "# Groceries\n\nstale", n.Revision); !errors.Is(err, ErrStaleRevision) {
		t.Errorf("want ErrStaleRevision, got %v", err)
	}

	found, _ := s.ListNotes(ctx, "eggs", "")
	if len(found) != 1 {
		t.Errorf("search eggs: got %d", len(found))
	}
	if found, _ = s.ListNotes(ctx, "%", ""); len(found) != 0 {
		t.Errorf("'%%' must be literal, got %d matches", len(found))
	}
}

func TestNotesDeleteLeavesTombstone(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	a, _ := s.CreateNote(ctx, "A\nx")
	since := a.Seq
	if err := s.DeleteNote(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetNote(ctx, a.ID); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("deleted note should be gone, got %v", err)
	}
	if err := s.DeleteNote(ctx, a.ID); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("second delete: %v", err)
	}
	ch, _ := s.NoteChanges(ctx, since)
	if len(ch) != 1 || !ch[0].Deleted {
		t.Errorf("want one tombstone in changes, got %+v", ch)
	}
	// Title is free again after deletion.
	if _, err := s.CreateNote(ctx, "A\nagain"); err != nil {
		t.Errorf("recreate after delete: %v", err)
	}
}

func TestNotesBacklinksAndRename(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	target, _ := s.CreateNote(ctx, "# Project X\nplans")
	a, _ := s.CreateNote(ctx, "# Todo\nsee [[Project X]] and [[project x]]")
	_, _ = s.CreateNote(ctx, "# Other\nsee [[Something Else]]")

	bl, err := s.NoteBacklinks(ctx, target.ID)
	if err != nil || len(bl) != 1 || bl[0].ID != a.ID {
		t.Fatalf("backlinks: %v %+v", err, bl)
	}

	renamed, err := s.RenameNote(ctx, target.ID, "Project Y")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Body != "# Project Y\nplans" {
		t.Errorf("body after rename: %q", renamed.Body)
	}
	a2, _ := s.GetNote(ctx, a.ID)
	if !strings.Contains(a2.Body, "[[Project Y]] and [[Project Y]]") {
		t.Errorf("links not rewritten: %q", a2.Body)
	}
	if a2.Revision != a.Revision+1 {
		t.Errorf("linking note revision should bump, got %d", a2.Revision)
	}

	// Renaming onto an existing title fails and changes nothing.
	if _, err := s.RenameNote(ctx, target.ID, "todo"); !errors.Is(err, ErrTitleTaken) {
		t.Errorf("want ErrTitleTaken, got %v", err)
	}
	if _, err := s.RenameNote(ctx, target.ID, "bad [[title]]"); !errors.Is(err, ErrNoTitle) {
		t.Errorf("brackets in title should be rejected, got %v", err)
	}
}

func TestNotesProjects(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	a, _ := s.CreateNote(ctx, "# Plans\nfor #Scribble and #home")
	b, _ := s.CreateNote(ctx, "# Bugs\n#scribble: fix sync")
	_, _ = s.CreateNote(ctx, "# Scribblings\nmentions scribble and #scribbler")
	gone, _ := s.CreateNote(ctx, "# Old\n#archive")
	_, _ = s.CreateNote(ctx, "#title-only")
	if err := s.DeleteNote(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}

	got, err := s.Projects(ctx)
	if err != nil || !slices.Equal(got, []string{"home", "scribble", "scribbler"}) {
		t.Fatalf("Projects = %q, %v", got, err)
	}

	list, err := s.ListNotes(ctx, "", "#SCRIBBLE")
	if err != nil || len(list) != 2 || list[0].ID != b.ID || list[1].ID != a.ID {
		t.Fatalf("project filter: %v %+v", err, list)
	}
	list, _ = s.ListNotes(ctx, "sync", "scribble")
	if len(list) != 1 || list[0].ID != b.ID {
		t.Errorf("project + search: %+v", list)
	}
	if list, _ = s.ListNotes(ctx, "", "archive"); len(list) != 0 {
		t.Errorf("deleted notes must not be listed: %+v", list)
	}
	if list, _ = s.ListNotes(ctx, "", "scri%"); len(list) != 0 {
		t.Errorf("'%%' must be literal: %+v", list)
	}
}

func TestShareNote(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	n, err := s.CreateNote(ctx, "# Shared\nbody")
	if err != nil {
		t.Fatal(err)
	}

	if tok, err := s.NoteShareToken(ctx, n.ID); err != nil || tok != "" {
		t.Fatalf("a new note is shared: %q %v", tok, err)
	}
	tok, err := s.ShareNote(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 32 || strings.Trim(tok, "0123456789abcdef") != "" {
		t.Errorf("token %q is not 128 bits of hex", tok)
	}
	// Sharing again keeps the link.
	if again, err := s.ShareNote(ctx, n.ID); err != nil || again != tok {
		t.Errorf("share again: %q %v, want %q", again, err, tok)
	}
	if got, err := s.NoteShareToken(ctx, n.ID); err != nil || got != tok {
		t.Errorf("NoteShareToken: %q %v, want %q", got, err, tok)
	}
	got, err := s.GetSharedNote(ctx, tok)
	if err != nil || got.ID != n.ID || got.Body != n.Body {
		t.Fatalf("GetSharedNote: %v %+v", err, got)
	}
	// Sharing is not an edit: synced clients have nothing to fetch.
	if got.Revision != n.Revision || got.Seq != n.Seq {
		t.Errorf("sharing changed revision or seq: %+v", got)
	}

	other, _ := s.CreateNote(ctx, "# Other")
	otherTok, err := s.ShareNote(ctx, other.ID)
	if err != nil || otherTok == tok {
		t.Errorf("second note's token %q (%v) repeats %q", otherTok, err, tok)
	}

	if err := s.UnshareNote(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSharedNote(ctx, tok); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("revoked token: want ErrNoteNotFound, got %v", err)
	}
	if tok, _ := s.NoteShareToken(ctx, n.ID); tok != "" {
		t.Errorf("token %q left after unsharing", tok)
	}
	// Sharing anew gives a new link; the old one stays dead.
	if fresh, err := s.ShareNote(ctx, n.ID); err != nil || fresh == tok {
		t.Errorf("reshare: %q %v, want a new token", fresh, err)
	}

	for _, bad := range []string{"", "nope"} {
		if _, err := s.GetSharedNote(ctx, bad); !errors.Is(err, ErrNoteNotFound) {
			t.Errorf("GetSharedNote(%q): want ErrNoteNotFound, got %v", bad, err)
		}
	}
	if err := s.DeleteNote(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSharedNote(ctx, otherTok); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("deleted note's token: want ErrNoteNotFound, got %v", err)
	}
	if _, err := s.ShareNote(ctx, other.ID); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("share deleted note: want ErrNoteNotFound, got %v", err)
	}
	if err := s.UnshareNote(ctx, other.ID); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("unshare deleted note: want ErrNoteNotFound, got %v", err)
	}
}
