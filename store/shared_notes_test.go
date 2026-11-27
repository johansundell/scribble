package store

import (
	"context"
	"testing"
	"time"
)

// SharedNotes (#208) is which live notes have a working view or edit link,
// for the web list's badges.
func TestSharedNotes(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	mk := func(body string) int64 {
		n, err := s.CreateNote(ctx, body)
		if err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	private, view, edit, both := mk("# Private"), mk("# View"), mk("# Edit"), mk("# Both")
	expired, revoked, deleted := mk("# Expired"), mk("# Revoked"), mk("# Deleted")
	later, past := time.Now().Add(time.Hour), time.Now().Add(-time.Hour)

	must := func(_ string, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.ShareNote(ctx, view, &later))
	must(s.ShareNoteEdit(ctx, edit, nil))
	must(s.ShareNote(ctx, both, nil))
	must(s.ShareNoteEdit(ctx, both, nil))
	must(s.ShareNote(ctx, expired, &past))
	must(s.ShareNoteEdit(ctx, expired, &past))
	must(s.ShareNote(ctx, revoked, nil))
	must(s.ShareNoteEdit(ctx, revoked, nil))
	if err := s.UnshareNote(ctx, revoked); err != nil {
		t.Fatal(err)
	}
	if err := s.UnshareNoteEdit(ctx, revoked); err != nil {
		t.Fatal(err)
	}
	must(s.ShareNote(ctx, deleted, nil))
	if err := s.DeleteNote(ctx, deleted); err != nil {
		t.Fatal(err)
	}

	got, err := s.SharedNotes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]ShareFlags{
		view: {View: true},
		edit: {Edit: true},
		both: {View: true, Edit: true},
	}
	if len(got) != len(want) {
		t.Errorf("SharedNotes = %+v, want %+v (private %d, expired %d, revoked %d, deleted %d left out)",
			got, want, private, expired, revoked, deleted)
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("SharedNotes[%d] = %+v, want %+v", id, got[id], w)
		}
	}
}
