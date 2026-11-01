package store

import (
	"context"
	"errors"
	"testing"

	"github.com/johansundell/scribble/notes"
)

func titles(ns []notes.Note) []string {
	out := []string{}
	for _, n := range ns {
		out = append(out, n.Title)
	}
	return out
}

func TestPinNote(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	a, _ := s.CreateNote(ctx, "# A")
	b, _ := s.CreateNote(ctx, "# B")
	c, _ := s.CreateNote(ctx, "# C")

	p, err := s.PinNote(ctx, a.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Pinned || p.Seq <= c.Seq || p.Revision != a.Revision || !p.UpdatedAt.Equal(a.UpdatedAt) {
		t.Errorf("pinned note %+v: want Pinned, a new seq, same revision and updated_at as %+v", p, a)
	}

	// Pinned first, then newest first.
	list, err := s.ListNotes(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(list); len(got) != 3 || got[0] != "A" || got[1] != "C" || got[2] != "B" {
		t.Errorf("ListNotes order = %v, want [A C B]", got)
	}
	if !list[0].Pinned || list[1].Pinned {
		t.Errorf("Pinned flags wrong: %+v", list)
	}

	// The change reaches synced clients.
	changes, _ := s.NoteChanges(ctx, c.Seq)
	if len(changes) != 1 || changes[0].ID != a.ID || !changes[0].Pinned {
		t.Errorf("NoteChanges since %d = %+v, want only A pinned", c.Seq, changes)
	}

	// An edit keeps the pin.
	upd, err := s.UpdateNote(ctx, a.ID, "# A\nmore", p.Revision)
	if err != nil || !upd.Pinned {
		t.Errorf("update after pin: %v %+v", err, upd)
	}

	// Pinning twice is no change, but still answers the note.
	again, err := s.PinNote(ctx, a.ID, true)
	if err != nil || !again.Pinned || again.Seq != upd.Seq {
		t.Errorf("pin again: %v %+v, want seq %d kept", err, again, upd.Seq)
	}

	un, err := s.PinNote(ctx, a.ID, false)
	if err != nil || un.Pinned || un.Seq <= upd.Seq {
		t.Errorf("unpin: %v %+v", err, un)
	}
	list, _ = s.ListNotes(ctx, "", "")
	if got := titles(list); got[0] != "A" { // A was edited last
		t.Errorf("after unpin order = %v", got)
	}

	if _, err := s.PinNote(ctx, 999, true); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("pin missing note: want ErrNoteNotFound, got %v", err)
	}
	s.DeleteNote(ctx, b.ID)
	if _, err := s.PinNote(ctx, b.ID, true); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("pin deleted note: want ErrNoteNotFound, got %v", err)
	}
}

func TestPinnedMigration(t *testing.T) {
	s := newNoteStore(t)
	var name string
	if err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'notes_pinned'`).Scan(&name); err != nil {
		t.Errorf("index notes_pinned: %v", err)
	}
	n, err := s.CreateNote(context.Background(), "# Fresh")
	if err != nil || n.Pinned {
		t.Errorf("a new note starts unpinned: %v %+v", err, n)
	}
}

func TestUndeletedNoteKeepsItsPin(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	n, _ := s.CreateNote(ctx, "# A")
	if _, err := s.PinNote(ctx, n.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteNote(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	back, err := s.UndeleteNote(ctx, n.ID)
	if err != nil || !back.Pinned {
		t.Errorf("undeleted note: %v %+v, want it pinned", err, back)
	}
}
