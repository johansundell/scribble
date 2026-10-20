package store

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// history returns the reasons and revisions of note id's versions, newest
// first.
func history(t *testing.T, s *SQLStore, id int64) (reasons []string, revs []int64) {
	t.Helper()
	vs, err := s.NoteVersions(context.Background(), id)
	if err != nil {
		t.Fatalf("NoteVersions(%d): %v", id, err)
	}
	for _, v := range vs {
		reasons = append(reasons, v.Reason)
		revs = append(revs, v.Revision)
	}
	return reasons, revs
}

func wantHistory(t *testing.T, s *SQLStore, id int64, want ...string) {
	t.Helper()
	if got, revs := history(t, s, id); !slices.Equal(got, want) {
		t.Errorf("note %d history = %q (revisions %v), want %q", id, got, revs, want)
	}
}

// mergeWindow sets versionMergeWindow to d for one test.
func mergeWindow(t *testing.T, d time.Duration) {
	t.Helper()
	old := versionMergeWindow
	versionMergeWindow = d
	t.Cleanup(func() { versionMergeWindow = old })
}

func TestEveryWriteRecordsAVersion(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	mergeWindow(t, 0)

	a, _ := s.CreateNote(ctx, "# Alpha\nfirst")
	b, _ := s.CreateNote(ctx, "# Beta\nsee [[Alpha]]")
	if _, err := s.UpdateNote(ctx, a.ID, "# Alpha\nsecond", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameNote(ctx, a.ID, "Omega"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteNote(ctx, a.ID); err != nil {
		t.Fatal(err)
	}

	wantHistory(t, s, a.ID, "delete", "rename", "edit", "create")
	wantHistory(t, s, b.ID, "link-rewrite", "create")

	vs, _ := s.NoteVersions(ctx, a.ID)
	if !vs[0].Deleted || vs[1].Deleted || vs[0].Revision != 4 || vs[1].Title != "Omega" {
		t.Errorf("versions %+v", vs)
	}
	if vs[0].Body != "" {
		t.Errorf("the list must leave out bodies, got %q", vs[0].Body)
	}
	v, err := s.NoteVersion(ctx, a.ID, 2)
	if err != nil || v.Body != "# Alpha\nsecond" || v.Reason != "edit" {
		t.Errorf("NoteVersion(2) = %+v, %v", v, err)
	}
	link, _ := s.NoteVersion(ctx, b.ID, 2)
	if link.Body != "# Beta\nsee [[Omega]]" {
		t.Errorf("link-rewrite version = %q", link.Body)
	}
}

func TestFailedWriteRecordsNoVersion(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	a, _ := s.CreateNote(ctx, "# Alpha\nx")
	s.CreateNote(ctx, "# Beta\nx")
	if _, err := s.UpdateNote(ctx, a.ID, "# Beta\nclash", 0); !errors.Is(err, ErrTitleTaken) {
		t.Fatalf("want ErrTitleTaken, got %v", err)
	}
	if _, err := s.CreateNote(ctx, "# beta\ndup"); !errors.Is(err, ErrTitleTaken) {
		t.Fatalf("want ErrTitleTaken, got %v", err)
	}
	wantHistory(t, s, a.ID, "create")
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM note_versions`).Scan(&n)
	if n != 2 {
		t.Errorf("want 2 versions in all, got %d", n)
	}
}

func TestRapidEditsMerge(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	mergeWindow(t, time.Hour)

	a, _ := s.CreateNote(ctx, "# A\none")
	s.UpdateNote(ctx, a.ID, "# A\ntwo", 0)
	// The only edit row after create is never merged into create.
	wantHistory(t, s, a.ID, "edit", "create")
	s.UpdateNote(ctx, a.ID, "# A\nthree", 0)
	s.UpdateNote(ctx, a.ID, "# A\nfour", 0)
	wantHistory(t, s, a.ID, "edit", "create")

	_, revs := history(t, s, a.ID)
	if !slices.Equal(revs, []int64{4, 1}) {
		t.Errorf("revisions %v, want [4 1]", revs)
	}
	v, _ := s.NoteVersion(ctx, a.ID, 4)
	if v.Body != "# A\nfour" {
		t.Errorf("merged version holds %q", v.Body)
	}
	if !v.UpdatedAt.After(v.CreatedAt) {
		t.Errorf("a merged version's updated_at should move on: %+v", v)
	}
	if _, err := s.NoteVersion(ctx, a.ID, 2); !errors.Is(err, ErrVersionNotFound) {
		t.Errorf("merged-away revision: want ErrVersionNotFound, got %v", err)
	}
	if first, _ := s.NoteVersion(ctx, a.ID, 1); first.Body != "# A\none" {
		t.Errorf("version before the burst = %q", first.Body)
	}

	// A rename is never merged, nor merged into.
	s.RenameNote(ctx, a.ID, "B")
	s.UpdateNote(ctx, a.ID, "# B\nfive", 0)
	wantHistory(t, s, a.ID, "edit", "rename", "edit", "create")

	// Nor is a restore.
	s.RestoreVersion(ctx, a.ID, 1, 0)
	s.UpdateNote(ctx, a.ID, "# A\nsix", 0)
	wantHistory(t, s, a.ID, "edit", "restore", "edit", "rename", "edit", "create")
}

func TestEditAfterTheWindowAddsAVersion(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	mergeWindow(t, time.Millisecond)
	a, _ := s.CreateNote(ctx, "# A\none")
	s.UpdateNote(ctx, a.ID, "# A\ntwo", 0)
	time.Sleep(5 * time.Millisecond)
	s.UpdateNote(ctx, a.ID, "# A\nthree", 0)
	wantHistory(t, s, a.ID, "edit", "edit", "create")
}

func TestVersionsOfAMissingNote(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	if _, err := s.NoteVersions(ctx, 99); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("NoteVersions: want ErrNoteNotFound, got %v", err)
	}
	if _, err := s.NoteVersion(ctx, 99, 1); !errors.Is(err, ErrVersionNotFound) {
		t.Errorf("NoteVersion: want ErrVersionNotFound, got %v", err)
	}
}

func TestRestoreVersion(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	mergeWindow(t, 0)

	a, _ := s.CreateNote(ctx, "# Alpha\nfirst")
	cur, _ := s.UpdateNote(ctx, a.ID, "# Alpha\nsecond", 0)

	if _, err := s.RestoreVersion(ctx, a.ID, 1, cur.Revision-1); !errors.Is(err, ErrStaleRevision) {
		t.Errorf("stale base: want ErrStaleRevision, got %v", err)
	}
	if _, err := s.RestoreVersion(ctx, a.ID, 7, 0); !errors.Is(err, ErrVersionNotFound) {
		t.Errorf("missing revision: want ErrVersionNotFound, got %v", err)
	}
	if _, err := s.RestoreVersion(ctx, 99, 1, 0); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("missing note: want ErrNoteNotFound, got %v", err)
	}

	n, err := s.RestoreVersion(ctx, a.ID, 1, cur.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if n.Body != "# Alpha\nfirst" || n.Revision != 3 || n.Seq <= cur.Seq {
		t.Errorf("restored note %+v", n)
	}
	wantHistory(t, s, a.ID, "restore", "edit", "create")
	if ch, _ := s.NoteChanges(ctx, cur.Seq); len(ch) != 1 || ch[0].ID != a.ID {
		t.Errorf("restore missing from changes: %+v", ch)
	}

	// The old title now belongs to another note.
	s.RenameNote(ctx, a.ID, "Renamed")
	s.CreateNote(ctx, "# Alpha\nsomeone else")
	if _, err := s.RestoreVersion(ctx, a.ID, 1, 0); !errors.Is(err, ErrTitleTaken) {
		t.Errorf("title clash: want ErrTitleTaken, got %v", err)
	}

	// A deleted note must be undeleted first.
	s.DeleteNote(ctx, a.ID)
	if _, err := s.RestoreVersion(ctx, a.ID, 1, 0); !errors.Is(err, ErrNoteDeleted) {
		t.Errorf("deleted note: want ErrNoteDeleted, got %v", err)
	}
}

func TestDeletedNotesAndUndelete(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)

	a, _ := s.CreateNote(ctx, "# Alpha\nlast words")
	b, _ := s.CreateNote(ctx, "# Beta\nx")
	live, _ := s.CreateNote(ctx, "# Live\nx")
	s.DeleteNote(ctx, a.ID)
	s.DeleteNote(ctx, b.ID)

	del, err := s.DeletedNotes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(del) != 2 || del[0].ID != b.ID || del[1].ID != a.ID || del[1].Title != "Alpha" || del[0].DeletedAt.IsZero() {
		t.Fatalf("DeletedNotes = %+v", del)
	}
	v, _ := s.NoteVersions(ctx, b.ID)
	if !del[0].DeletedAt.Equal(v[0].CreatedAt) {
		t.Errorf("deleted_at %v, want the delete version's created_at %v", del[0].DeletedAt, v[0].CreatedAt)
	}

	if _, err := s.UndeleteNote(ctx, live.ID); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("undelete a live note: want ErrNoteNotFound, got %v", err)
	}
	if _, err := s.UndeleteNote(ctx, 99); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("undelete a missing note: want ErrNoteNotFound, got %v", err)
	}

	before := s.latestSeq(t)
	n, err := s.UndeleteNote(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n.Deleted || n.Body != "# Alpha\nlast words" || n.Revision != 3 || n.Seq <= before {
		t.Errorf("undeleted note %+v", n)
	}
	wantHistory(t, s, a.ID, "undelete", "delete", "create")
	if ch, _ := s.NoteChanges(ctx, before); len(ch) != 1 || ch[0].ID != a.ID || ch[0].Deleted {
		t.Errorf("undelete missing from changes: %+v", ch)
	}
	if del, _ = s.DeletedNotes(ctx); len(del) != 1 || del[0].ID != b.ID {
		t.Errorf("after undelete, DeletedNotes = %+v", del)
	}

	// A live note took the title meanwhile.
	s.CreateNote(ctx, "# beta\nnew")
	if _, err := s.UndeleteNote(ctx, b.ID); !errors.Is(err, ErrTitleTaken) {
		t.Errorf("title clash: want ErrTitleTaken, got %v", err)
	}
}

func (s *SQLStore) latestSeq(t *testing.T) int64 {
	t.Helper()
	var seq int64
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM notes`).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	return seq
}

func TestNoteVersionsMigrationBackfillsExistingNotes(t *testing.T) {
	file := filepath.Join(t.TempDir(), "old.db")

	// A database from before history: notes at user_version 2.
	old, err := openSQLite(file, migrations[:2])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO notes (title, body, revision, seq, deleted, created_at, updated_at) VALUES
		('Live', '# Live', 3, 1, 0, '2025-01-02T03:04:05.000000Z', '2025-02-01T00:00:00.000000Z'),
		('Gone', '# Gone', 2, 2, 1, '2025-01-03T00:00:00.000000Z', '2025-03-01T00:00:00.000000Z')`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := NewSQLite(file)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	vs, err := s.NoteVersions(context.Background(), 1)
	if err != nil || len(vs) != 1 {
		t.Fatalf("live note versions: %v %+v", err, vs)
	}
	want := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if v := vs[0]; v.Revision != 3 || v.Reason != "create" || v.Deleted || !v.CreatedAt.Equal(want) || v.Title != "Live" {
		t.Errorf("backfilled version %+v", v)
	}
	if vs, _ := s.NoteVersions(context.Background(), 2); len(vs) != 1 || !vs[0].Deleted || vs[0].Revision != 2 {
		t.Errorf("deleted note versions %+v", vs)
	}

	// Running the migration again adds nothing.
	if _, err := s.db.Exec(migrations[2].sql); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM note_versions`).Scan(&n)
	if n != 2 {
		t.Errorf("want 2 versions after a rerun, got %d", n)
	}
}

func TestHistoryNeedsNotes(t *testing.T) {
	ctx := context.Background()
	s := &SQLStore{} // as under STORAGE=mysql
	_, e1 := s.NoteVersions(ctx, 1)
	_, e2 := s.NoteVersion(ctx, 1, 1)
	_, e3 := s.RestoreVersion(ctx, 1, 1, 0)
	_, e4 := s.DeletedNotes(ctx)
	_, e5 := s.UndeleteNote(ctx, 1)
	for i, err := range []error{e1, e2, e3, e4, e5} {
		if !errors.Is(err, ErrNotesUnsupported) {
			t.Errorf("call %d: want ErrNotesUnsupported, got %v", i+1, err)
		}
	}
}
