package store

import (
	"context"
	"errors"
	"path/filepath"
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

	found, _ := s.ListNotes(ctx, "eggs")
	if len(found) != 1 {
		t.Errorf("search eggs: got %d", len(found))
	}
	if found, _ = s.ListNotes(ctx, "%"); len(found) != 0 {
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
