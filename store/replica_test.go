package store

import (
	"context"
	"strings"
	"testing"
)

func applyEdit(t *testing.T, s *SQLStore, e PendingEdit) PendingOutcome {
	t.Helper()
	out, err := s.ApplyPendingEdit(context.Background(), e)
	if err != nil {
		t.Fatalf("ApplyPendingEdit(%+v): %v", e, err)
	}
	return out
}

func TestReplicaStateStartsAtZero(t *testing.T) {
	s := newNoteStore(t)
	st, err := s.ReplicaState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st != (ReplicaState{}) {
		t.Errorf("new database: got %+v, want zero", st)
	}
}

func TestAdvancePushIsCompareAndSet(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	if ok, err := s.AdvancePush(ctx, 0, 5); err != nil || !ok {
		t.Fatalf("AdvancePush(0, 5) = %v, %v", ok, err)
	}
	// A resync in between (another process) wins over a round in progress.
	if err := s.ResyncReplica(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.AdvancePush(ctx, 5, 6); err != nil || ok {
		t.Fatalf("AdvancePush(5, 6) after a resync = %v, %v; want false", ok, err)
	}
	if st, _ := s.ReplicaState(ctx); st.LastPushedSeq != 0 {
		t.Errorf("last pushed seq = %d, want 0 after resync", st.LastPushedSeq)
	}
}

func TestPushBacklog(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	a, _ := s.CreateNote(ctx, "A")
	s.CreateNote(ctx, "B")
	if n, err := s.PushBacklog(ctx); err != nil || n != 2 {
		t.Fatalf("PushBacklog = %d, %v; want 2", n, err)
	}
	s.AdvancePush(ctx, 0, a.Seq)
	if n, _ := s.PushBacklog(ctx); n != 1 {
		t.Errorf("PushBacklog after pushing A = %d, want 1", n)
	}
}

func TestApplyPendingCreate(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	out := applyEdit(t, s, PendingEdit{ID: 1, Kind: PendingCreate, Body: "From FileMaker\n\ntext"})
	if out.Status != PendingApplied || out.NoteID == 0 {
		t.Fatalf("create: got %+v", out)
	}
	n, err := s.GetNote(ctx, out.NoteID)
	if err != nil || n.Title != "From FileMaker" {
		t.Fatalf("created note: %+v, %v", n, err)
	}
	wantHistory(t, s, n.ID, reasonCreate)

	out = applyEdit(t, s, PendingEdit{ID: 2, Kind: PendingCreate, Body: "from filemaker"})
	if out.Status != PendingRejected || !strings.Contains(out.Message, "already exists") {
		t.Errorf("title clash: got %+v", out)
	}
	out = applyEdit(t, s, PendingEdit{ID: 3, Kind: PendingCreate, Body: "\n\n"})
	if out.Status != PendingRejected {
		t.Errorf("no title: got %+v", out)
	}
}

func TestApplyPendingUpdate(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	mergeWindow(t, 0)
	n, _ := s.CreateNote(ctx, "Note\n\nold")
	out := applyEdit(t, s, PendingEdit{ID: 1, Kind: PendingUpdate, NoteID: n.ID, BaseRevision: n.Revision, Body: "Note\n\nnew"})
	if out.Status != PendingApplied || out.NoteID != n.ID {
		t.Fatalf("update: got %+v", out)
	}
	got, _ := s.GetNote(ctx, n.ID)
	if got.Body != "Note\n\nnew" || got.Revision != 2 {
		t.Errorf("after update: %+v", got)
	}
	wantHistory(t, s, n.ID, reasonEdit, reasonCreate)
}

func TestApplyPendingUpdateConflictKeepsBothVersions(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	n, _ := s.CreateNote(ctx, "Note\n\nbase")
	app, _ := s.UpdateNote(ctx, n.ID, "Note\n\napp", n.Revision)

	out := applyEdit(t, s, PendingEdit{ID: 1, Kind: PendingUpdate, NoteID: n.ID, BaseRevision: n.Revision, Body: "Note\n\nfilemaker"})
	if out.Status != PendingConflict {
		t.Fatalf("conflict: got %+v", out)
	}
	cur, _ := s.GetNote(ctx, n.ID)
	if cur.Body != app.Body || cur.Revision != app.Revision+2 {
		t.Errorf("current note = %+v, want the app's text at revision %d", cur, app.Revision+2)
	}
	wantHistory(t, s, n.ID, reasonConflictKeptApp, reasonFileMakerConflict, reasonEdit, reasonCreate)
	fm, err := s.NoteVersion(ctx, n.ID, app.Revision+1)
	if err != nil || fm.Body != "Note\n\nfilemaker" {
		t.Fatalf("FileMaker version: %+v, %v", fm, err)
	}
	if want := "(r3)"; !strings.Contains(out.Message, want) {
		t.Errorf("message %q does not name %s", out.Message, want)
	}
	// The FileMaker edit can be restored.
	if _, err := s.RestoreVersion(ctx, n.ID, fm.Revision, 0); err != nil {
		t.Errorf("restore the FileMaker version: %v", err)
	}
	// A later app edit does not merge into the conflict versions.
	s.UpdateNote(ctx, n.ID, "Note\n\nlater", 0)
	if reasons, _ := history(t, s, n.ID); reasons[0] != reasonEdit || reasons[1] != reasonRestore {
		t.Errorf("history after a later edit = %q", reasons)
	}
}

func TestApplyPendingUpdateRejected(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	n, _ := s.CreateNote(ctx, "Note")
	other, _ := s.CreateNote(ctx, "Other")

	out := applyEdit(t, s, PendingEdit{ID: 1, Kind: PendingUpdate, NoteID: n.ID, BaseRevision: n.Revision, Body: "Other"})
	if out.Status != PendingRejected || !strings.Contains(out.Message, "already exists") {
		t.Errorf("title clash: got %+v", out)
	}
	// A title clash in a conflicting edit is rejected too, and changes nothing.
	s.UpdateNote(ctx, n.ID, "Note\n\napp", 0)
	out = applyEdit(t, s, PendingEdit{ID: 2, Kind: PendingUpdate, NoteID: n.ID, BaseRevision: n.Revision, Body: "Other\n\nfm"})
	if out.Status != PendingRejected {
		t.Errorf("title clash in a conflict: got %+v", out)
	}
	wantHistory(t, s, n.ID, reasonEdit, reasonCreate)

	s.DeleteNote(ctx, other.ID)
	out = applyEdit(t, s, PendingEdit{ID: 3, Kind: PendingUpdate, NoteID: other.ID, BaseRevision: other.Revision, Body: "Other\n\nfm"})
	if out.Status != PendingRejected || out.Message != "The note was deleted." {
		t.Errorf("deleted note: got %+v", out)
	}
	out = applyEdit(t, s, PendingEdit{ID: 4, Kind: PendingUpdate, NoteID: 999, BaseRevision: 1, Body: "Gone"})
	if out.Status != PendingRejected || out.Message != "The note was deleted." {
		t.Errorf("missing note: got %+v", out)
	}
	out = applyEdit(t, s, PendingEdit{ID: 5, Kind: "rename", NoteID: n.ID})
	if out.Status != PendingRejected {
		t.Errorf("unknown kind: got %+v", out)
	}
}

func TestApplyPendingDelete(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	n, _ := s.CreateNote(ctx, "Note")
	out := applyEdit(t, s, PendingEdit{ID: 1, Kind: PendingDelete, NoteID: n.ID, BaseRevision: n.Revision})
	if out.Status != PendingApplied {
		t.Fatalf("delete: got %+v", out)
	}
	if _, err := s.GetNote(ctx, n.ID); err != ErrNoteNotFound {
		t.Errorf("note still there: %v", err)
	}

	k, _ := s.CreateNote(ctx, "Kept")
	s.UpdateNote(ctx, k.ID, "Kept\n\napp", 0)
	out = applyEdit(t, s, PendingEdit{ID: 2, Kind: PendingDelete, NoteID: k.ID, BaseRevision: k.Revision})
	if out.Status != PendingConflict || out.Message != "Changed in the app since; not deleted." {
		t.Errorf("delete conflict: got %+v", out)
	}
	if _, err := s.GetNote(ctx, k.ID); err != nil {
		t.Errorf("conflicting delete removed the note: %v", err)
	}
	out = applyEdit(t, s, PendingEdit{ID: 3, Kind: PendingDelete, NoteID: n.ID, BaseRevision: n.Revision + 1})
	if out.Status != PendingRejected || out.Message != "The note was deleted." {
		t.Errorf("delete of a deleted note: got %+v", out)
	}
}

func TestApplyPendingEditOnlyOnce(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	first := applyEdit(t, s, PendingEdit{ID: 7, Kind: PendingCreate, Body: "Once"})
	// The FileMaker cleanup failed, so the sync sees row 7 again.
	again := applyEdit(t, s, PendingEdit{ID: 7, Kind: PendingCreate, Body: "Once"})
	if again != first {
		t.Errorf("second apply = %+v, want the recorded %+v", again, first)
	}
	if list, _ := s.ListNotes(ctx, "", ""); len(list) != 1 {
		t.Errorf("%d notes, want 1", len(list))
	}
	if st, _ := s.ReplicaState(ctx); st.LastAppliedPendingID != 7 {
		t.Errorf("last applied = %d, want 7", st.LastAppliedPendingID)
	}
	// Lower ids were handled before too, even without a recorded outcome.
	if out := applyEdit(t, s, PendingEdit{ID: 3, Kind: PendingCreate, Body: "Late"}); out.Status != PendingRejected {
		t.Errorf("row below the watermark: got %+v", out)
	}

	if err := s.ForgetPendingOutcome(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if out := applyEdit(t, s, PendingEdit{ID: 7, Kind: PendingCreate, Body: "Once"}); out.Status != PendingRejected {
		t.Errorf("after forgetting: got %+v", out)
	}
	if list, _ := s.ListNotes(ctx, "", ""); len(list) != 1 {
		t.Errorf("%d notes after forgetting, want 1", len(list))
	}
}
