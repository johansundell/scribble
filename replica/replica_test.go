package replica

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/store"
)

var ctx = context.Background()

func newTest(t *testing.T) (*Syncer, *fakeFM, *store.SQLStore) {
	t.Helper()
	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	fm := newFakeFM()
	s := New(fm, db, Config{
		Tables:   Tables{Notes: "Notes", Pending: "PendingEdits", Info: "ScribbleInfo"},
		Interval: time.Hour,
		Logger:   quiet{},
	})
	return s, fm, db
}

type quiet struct{}

func (quiet) Infof(string, ...interface{})    {}
func (quiet) Warningf(string, ...interface{}) {}
func (quiet) Errorf(string, ...interface{})   {}

func round(t *testing.T, s *Syncer) {
	t.Helper()
	if err := s.Round(ctx); err != nil {
		t.Fatalf("Round: %v", err)
	}
}

func mustCreate(t *testing.T, db *store.SQLStore, body string) *notes.Note {
	t.Helper()
	n, err := db.CreateNote(ctx, body)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// pushedSeqs records the Seq of every write to Notes.
func pushedSeqs(fm *fakeFM) *[]int64 {
	var seqs []int64
	fm.fail = func(op, table string, rec map[string]any) error {
		if table == "Notes" && op != "GET" {
			seqs = append(seqs, rec["Seq"].(int64))
		}
		return nil
	}
	return &seqs
}

func TestPushCopiesEveryNoteInSeqOrder(t *testing.T) {
	s, fm, db := newTest(t)
	a := mustCreate(t, db, "A")
	b := mustCreate(t, db, "B")
	a, _ = db.UpdateNote(ctx, a.ID, "A\n\nmore", 0)
	db.DeleteNote(ctx, b.ID)
	seqs := pushedSeqs(fm)
	round(t, s)

	// Each new record is a PATCH that finds nothing, then a POST.
	if want := []int64{3, 3, 4, 4}; !slices.Equal(*seqs, want) {
		t.Errorf("pushed seqs %v, want %v (A at 3, B's tombstone at 4)", *seqs, want)
	}
	ra := fm.record("Notes", a.ID)
	if ra["Title"] != "A" || ra["Body"] != "A\r\rmore" || ra["Revision"] != float64(2) || ra["Deleted"] != float64(0) {
		t.Errorf("Notes record of A: %v", ra)
	}
	if ra["CreatedAt"] == "" || ra["SyncedAt"] == "" || strings.ContainsAny(ra["UpdatedAt"].(string), "Z+") {
		t.Errorf("timestamps of A must be UTC without an offset: %v", ra)
	}
	if rb := fm.record("Notes", b.ID); rb["Deleted"] != float64(1) || rb["Body"] != "B" {
		t.Errorf("tombstone of B: %v", rb)
	}
	if st, _ := db.ReplicaState(ctx); st.LastPushedSeq != 4 {
		t.Errorf("last pushed seq = %d, want 4", st.LastPushedSeq)
	}
	if st := s.Status(ctx); st.Status != "ok" || st.PendingPush != 0 || st.LastSync == "" {
		t.Errorf("status after a round: %+v", st)
	}
}

func TestPushResumesAfterAFailurePartway(t *testing.T) {
	s, fm, db := newTest(t)
	for _, title := range []string{"One", "Two", "Three"} {
		mustCreate(t, db, title)
	}
	fm.fail = func(op, table string, rec map[string]any) error {
		if table == "Notes" && op == "POST" && rec["Seq"] == int64(2) {
			return errors.New("connection refused")
		}
		return nil
	}
	if err := s.Round(ctx); err == nil {
		t.Fatal("Round succeeded with FileMaker failing")
	}
	if st, _ := db.ReplicaState(ctx); st.LastPushedSeq != 1 {
		t.Fatalf("last pushed seq = %d, want 1", st.LastPushedSeq)
	}
	if st := s.Status(ctx); st.Status != "unreachable" || st.PendingPush != 2 {
		t.Errorf("status while failing: %+v", st)
	}

	seqs := pushedSeqs(fm)
	round(t, s)
	if want := []int64{2, 2, 3, 3}; !slices.Equal(*seqs, want) {
		t.Errorf("pushed seqs after the outage %v, want %v", *seqs, want)
	}
	if fm.count("Notes") != 3 {
		t.Errorf("%d Notes records, want 3", fm.count("Notes"))
	}
}

func TestPushIsIdempotentAndResyncPushesAll(t *testing.T) {
	s, fm, db := newTest(t)
	a := mustCreate(t, db, "A")
	mustCreate(t, db, "B")
	round(t, s)
	before := fm.record("Notes", a.ID)

	if err := db.ResyncReplica(ctx); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(ctx); st.Status != "behind" || st.PendingPush != 2 {
		t.Errorf("status after resync: %+v", st)
	}
	seqs := pushedSeqs(fm)
	round(t, s)
	if want := []int64{1, 2}; !slices.Equal(*seqs, want) {
		t.Errorf("resync pushed %v, want %v as updates", *seqs, want)
	}
	after := fm.record("Notes", a.ID)
	delete(before, "SyncedAt")
	delete(after, "SyncedAt")
	if fm.count("Notes") != 2 || len(after) != len(before) || after["Body"] != before["Body"] {
		t.Errorf("records changed by pushing them again: %v, then %v", before, after)
	}
}

func TestPullAppliesEdits(t *testing.T) {
	s, fm, db := newTest(t)
	upd := mustCreate(t, db, "Update me")
	del := mustCreate(t, db, "Delete me")
	round(t, s)

	fm.queue(1, "create", 0, "Made in FileMaker\n\ntext", 0)
	fm.queue(2, "update", upd.ID, "Update me\n\nfrom FileMaker", upd.Revision)
	fm.queue(3, "delete", del.ID, "", del.Revision)
	round(t, s)

	if n := fm.count("PendingEdits"); n != 0 {
		t.Errorf("%d PendingEdits rows left, want 0", n)
	}
	created, err := db.GetNoteByTitle(ctx, "Made in FileMaker")
	if err != nil || created.Body != "Made in FileMaker\n\ntext" {
		t.Fatalf("created note: %+v, %v", created, err)
	}
	if got, _ := db.GetNote(ctx, upd.ID); got.Body != "Update me\n\nfrom FileMaker" {
		t.Errorf("updated note: %+v", got)
	}
	if _, err := db.GetNote(ctx, del.ID); !errors.Is(err, store.ErrNoteNotFound) {
		t.Errorf("deleted note: %v", err)
	}
	changes, _ := db.NoteChanges(ctx, 2)
	if len(changes) != 3 {
		t.Errorf("/changes since the first push has %d notes, want 3", len(changes))
	}
	// The pull runs first, so FileMaker sees the result in the same round.
	if r := fm.record("Notes", created.ID); r == nil || r["Revision"] != float64(1) {
		t.Errorf("created note in Notes: %v", r)
	}
	if r := fm.record("Notes", del.ID); r["Deleted"] != float64(1) {
		t.Errorf("deleted note in Notes: %v", r)
	}
}

func TestPullConflictKeepsTheAppText(t *testing.T) {
	s, fm, db := newTest(t)
	n := mustCreate(t, db, "Note\n\nbase")
	app, _ := db.UpdateNote(ctx, n.ID, "Note\n\napp", 0)
	fm.queue(1, "update", n.ID, "Note\n\nfilemaker", n.Revision)
	round(t, s)

	row := fm.record("PendingEdits", 1)
	if row["Status"] != "conflict" || !strings.Contains(row["Message"].(string), "history (r3)") {
		t.Errorf("row: %v", row)
	}
	cur, _ := db.GetNote(ctx, n.ID)
	if cur.Body != app.Body {
		t.Errorf("current text %q, want the app's", cur.Body)
	}
	vs, _ := db.NoteVersions(ctx, n.ID)
	if vs[0].Reason != "conflict-kept-app" || vs[1].Reason != "filemaker-conflict" {
		t.Errorf("history: %+v", vs)
	}
	if r := fm.record("Notes", n.ID); r["Body"] != "Note\r\rapp" {
		t.Errorf("Notes record: %v", r)
	}

	// A marked row is left alone.
	round(t, s)
	if vs2, _ := db.NoteVersions(ctx, n.ID); len(vs2) != len(vs) {
		t.Errorf("the conflict row was applied again")
	}
}

func TestPullRejects(t *testing.T) {
	s, fm, db := newTest(t)
	n := mustCreate(t, db, "Taken")
	k := mustCreate(t, db, "Kept")
	db.UpdateNote(ctx, k.ID, "Kept\n\napp", 0)
	fm.queue(1, "create", 0, "taken\n\ndup", 0)
	fm.queue(2, "update", 999, "Gone", 1)
	fm.queue(3, "delete", k.ID, "", k.Revision)
	round(t, s)

	want := map[int64][2]string{
		1: {"rejected", `A note titled "taken" already exists.`},
		2: {"rejected", "The note was deleted."},
		3: {"conflict", "Changed in the app since; not deleted."},
	}
	for id, w := range want {
		r := fm.record("PendingEdits", id)
		if r["Status"] != w[0] || r["Message"] != w[1] {
			t.Errorf("row %d: %v, want %v", id, r, w)
		}
	}
	if _, err := db.GetNote(ctx, k.ID); err != nil {
		t.Errorf("conflicting delete removed the note: %v", err)
	}
	if list, _ := db.ListNotes(ctx, "", ""); len(list) != 2 || n == nil {
		t.Errorf("%d notes, want 2", len(list))
	}
}

func TestPullAppliesOnceWhenTheCleanupFails(t *testing.T) {
	s, fm, db := newTest(t)
	fm.queue(1, "create", 0, "Once", 0)
	fm.fail = errOn("DELETE", "PendingEdits")
	if err := s.Round(ctx); err == nil {
		t.Fatal("Round succeeded although the cleanup failed")
	}
	if fm.count("PendingEdits") != 1 {
		t.Fatal("row gone although the cleanup failed")
	}
	fm.fail = nil
	round(t, s)
	if fm.count("PendingEdits") != 0 {
		t.Errorf("row not cleaned up on the next round")
	}
	if list, _ := db.ListNotes(ctx, "", ""); len(list) != 1 {
		t.Errorf("%d notes, want 1: the row was applied twice", len(list))
	}
}

func TestBodiesRoundTrip(t *testing.T) {
	bodies := map[string]string{
		"lf":       "Title\nline one\nline two",
		"trailing": "Title\n\ntext\n",
		"blank":    "Title\n\n\n\nafter blank lines\n\n",
		"code":     "Title\n\n```go\nfunc main() {\n\tprintln(\"x\")\n}\n```\n",
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			s, fm, db := newTest(t)
			n := mustCreate(t, db, body)
			round(t, s)
			// Pushed unchanged; FileMaker keeps CR, which reads back as LF.
			if got := normalizeBody(fm.record("Notes", n.ID)["Body"].(string)); got != body {
				t.Errorf("Notes body reads back as %q, want %q", got, body)
			}
			// An edit sent from FileMaker with CRLF, as Windows would.
			crlf := strings.ReplaceAll(body+"edited\n", "\n", "\r\n")
			fm.queue(1, "update", n.ID, "", n.Revision)
			fm.mu.Lock()
			fm.tables["PendingEdits"][1]["Body"] = crlf
			fm.mu.Unlock()
			round(t, s)
			if got, _ := db.GetNote(ctx, n.ID); got.Body != body+"edited\n" {
				t.Errorf("FileMaker edit saved as %q, want %q", got.Body, body+"edited\n")
			}
		})
	}
}

func TestNormalizeBody(t *testing.T) {
	if got := normalizeBody("a\r\nb\rc\n\r\n"); got != "a\nb\nc\n\n" {
		t.Errorf("normalizeBody = %q", got)
	}
}

func TestCheck(t *testing.T) {
	cases := map[string]struct {
		break_ func(*fakeFM)
		setup  bool // the error is a setup error; otherwise none, or retryable
		ok     bool
	}{
		"ok":                 {func(*fakeFM) {}, false, true},
		"missing table":      {func(f *fakeFM) { delete(f.fields, "PendingEdits") }, true, false},
		"missing field":      {func(f *fakeFM) { f.fields["Notes"] = noteFields[:len(noteFields)-1] }, true, false},
		"schema version":     {func(f *fakeFM) { f.tables["ScribbleInfo"][1]["SchemaVersion"] = float64(2) }, true, false},
		"no info record":     {func(f *fakeFM) { delete(f.tables["ScribbleInfo"], 1) }, true, false},
		"two info records":   {func(f *fakeFM) { f.tables["ScribbleInfo"][2] = map[string]any{"SchemaVersion": float64(1)} }, true, false},
		"server unreachable": {func(f *fakeFM) { f.fail = errOn("GET", "Notes") }, false, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, fm, _ := newTest(t)
			c.break_(fm)
			err := s.Check(ctx)
			if (err == nil) != c.ok || isSetupError(err) != c.setup {
				t.Errorf("Check = %v (setup error %v), want ok %v, setup %v", err, isSetupError(err), c.ok, c.setup)
			}
		})
	}
}

func fast(s *Syncer) {
	s.poll = 5 * time.Millisecond
	s.minRetry = 5 * time.Millisecond
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestRunPushesSoonAfterAWrite(t *testing.T) {
	s, fm, db := newTest(t)
	fast(s)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { s.Run(runCtx); close(done) }()
	defer func() { cancel(); <-done }()

	eventually(t, "the first round", func() bool { return s.Status(ctx).Status == "ok" })
	n := mustCreate(t, db, "Soon")
	// The interval is an hour, so only the write can trigger this.
	eventually(t, "the push", func() bool { return fm.record("Notes", n.ID) != nil })
}

func TestRunRetriesWhileFileMakerIsDown(t *testing.T) {
	s, fm, db := newTest(t)
	fast(s)
	mustCreate(t, db, "Waiting")
	fm.mu.Lock()
	fm.fail = errOn("GET", "Notes")
	fm.mu.Unlock()
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { s.Run(runCtx); close(done) }()
	defer func() { cancel(); <-done }()

	eventually(t, "unreachable", func() bool { st := s.Status(ctx); return st.Status == "unreachable" && st.PendingPush == 1 })
	fm.mu.Lock()
	fm.fail = nil
	fm.mu.Unlock()
	eventually(t, "the backlog pushed", func() bool { return s.Status(ctx).Status == "ok" && fm.count("Notes") == 1 })
}

func TestRunTurnsOffOnABadSetup(t *testing.T) {
	s, fm, _ := newTest(t)
	fast(s)
	delete(fm.fields, "ScribbleInfo")
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run kept going with a broken setup")
	}
	if st := s.Status(ctx); st.Status != "off" || !strings.Contains(st.Reason, "ScribbleInfo") {
		t.Errorf("status: %+v", st)
	}
}

func TestDisable(t *testing.T) {
	s, _, _ := newTest(t)
	s.Disable(errors.New("bad FMS_CA_FILE"))
	if st := s.Status(ctx); st.Status != "off" || st.Reason != "bad FMS_CA_FILE" {
		t.Errorf("status: %+v", st)
	}
	s.Run(ctx) // returns at once
}
