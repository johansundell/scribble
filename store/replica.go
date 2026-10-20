package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/johansundell/scribble/notes"
)

// The FileMaker replica's side of SQLite (#66, ADR 10). The sync itself is
// in package replica; these methods keep its state, and apply the edits
// made in FileMaker, in SQLite.

// ReplicaState is how far the sync has got.
type ReplicaState struct {
	// LastPushedSeq is the seq of the last note written to FileMaker.
	LastPushedSeq int64
	// LastAppliedPendingID is the last PendingEdits row applied to SQLite.
	LastAppliedPendingID int64
}

// Kinds of PendingEdit.
const (
	PendingCreate = "create"
	PendingUpdate = "update"
	PendingDelete = "delete"
)

// What became of a PendingEdit.
const (
	PendingApplied  = "applied"
	PendingConflict = "conflict"
	PendingRejected = "rejected"
)

// PendingEdit is a row of FileMaker's PendingEdits table: an edit made in
// FileMaker. Body has LF line endings.
type PendingEdit struct {
	ID           int64
	Kind         string
	NoteID       int64 // 0 for a create
	Body         string
	BaseRevision int64
}

// PendingOutcome is what applying a PendingEdit did. Message, empty when
// it was applied, tells the FileMaker user why it wasn't.
type PendingOutcome struct {
	Status  string
	Message string
	NoteID  int64
}

// ReplicaState returns the sync's state.
func (s *SQLStore) ReplicaState(ctx context.Context) (ReplicaState, error) {
	var st ReplicaState
	err := s.db.QueryRowContext(ctx, `SELECT last_pushed_seq, last_applied_pending_id FROM replica_state`).
		Scan(&st.LastPushedSeq, &st.LastAppliedPendingID)
	return st, err
}

// AdvancePush moves the last pushed seq from from to to. It reports false,
// changing nothing, when it is no longer from: a resync reset it meanwhile,
// and the push must start over.
func (s *SQLStore) AdvancePush(ctx context.Context, from, to int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE replica_state SET last_pushed_seq = ? WHERE last_pushed_seq = ?`, to, from)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ResyncReplica makes the next sync push every note again.
func (s *SQLStore) ResyncReplica(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE replica_state SET last_pushed_seq = 0`)
	return err
}

// PushBacklog counts the notes changed since the last push.
func (s *SQLStore) PushBacklog(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notes
		WHERE seq > (SELECT last_pushed_seq FROM replica_state)`).Scan(&n)
	return n, err
}

// ApplyPendingEdit applies e as a normal save, and records it as the last
// applied row and its outcome, in one transaction. A row at or below the
// last applied one was applied before: its recorded outcome is returned
// and nothing changes, so a row is never applied twice. Only database
// failures are errors; an edit that can't be applied is an outcome.
func (s *SQLStore) ApplyPendingEdit(ctx context.Context, e PendingEdit) (PendingOutcome, error) {
	if err := s.checkNotes(); err != nil {
		return PendingOutcome{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PendingOutcome{}, err
	}
	defer tx.Rollback()

	var last int64
	if err := tx.QueryRowContext(ctx, `SELECT last_applied_pending_id FROM replica_state`).Scan(&last); err != nil {
		return PendingOutcome{}, err
	}
	if e.ID <= last {
		return recordedOutcome(ctx, tx, e.ID)
	}
	out, err := s.applyPending(ctx, tx, e)
	if err != nil {
		return PendingOutcome{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE replica_state SET last_applied_pending_id = ?`, e.ID); err != nil {
		return PendingOutcome{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO replica_pending_results (id, status, message, note_id) VALUES (?, ?, ?, ?)`,
		e.ID, out.Status, out.Message, out.NoteID); err != nil {
		return PendingOutcome{}, err
	}
	return out, tx.Commit()
}

func recordedOutcome(ctx context.Context, x dbx, id int64) (PendingOutcome, error) {
	var out PendingOutcome
	err := x.QueryRowContext(ctx, `SELECT status, message, note_id FROM replica_pending_results WHERE id = ?`, id).
		Scan(&out.Status, &out.Message, &out.NoteID)
	if errors.Is(err, sql.ErrNoRows) {
		// Its cleanup finished, yet the row is back waiting: someone
		// cleared its Status. It must not be applied again.
		return PendingOutcome{Status: PendingRejected, Message: "This edit was already handled. Make the change again as a new edit."}, nil
	}
	return out, err
}

// ForgetPendingOutcome drops row id's recorded outcome, once FileMaker has
// been cleaned up after it.
func (s *SQLStore) ForgetPendingOutcome(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM replica_pending_results WHERE id = ?`, id)
	return err
}

// applyPending applies e inside x. The failed statement of a refused write
// changes nothing, so the transaction stays usable for the bookkeeping.
func (s *SQLStore) applyPending(ctx context.Context, x dbx, e PendingEdit) (PendingOutcome, error) {
	if e.Kind == PendingCreate {
		id, err := s.createNote(ctx, x, e.Body)
		if out, ok := refused(err, e.Body, 0); ok {
			return out, nil
		}
		return PendingOutcome{Status: PendingApplied, NoteID: id}, err
	}
	if e.Kind != PendingUpdate && e.Kind != PendingDelete {
		return PendingOutcome{Status: PendingRejected, NoteID: e.NoteID,
			Message: fmt.Sprintf("Unknown Kind %q: use create, update or delete.", e.Kind)}, nil
	}

	cur, err := getNote(ctx, x, e.NoteID)
	if errors.Is(err, ErrNoteNotFound) {
		return PendingOutcome{Status: PendingRejected, Message: "The note was deleted.", NoteID: e.NoteID}, nil
	}
	if err != nil {
		return PendingOutcome{}, err
	}
	stale := cur.Revision != e.BaseRevision
	now := time.Now()

	if e.Kind == PendingDelete {
		if stale {
			return PendingOutcome{Status: PendingConflict, Message: "Changed in the app since; not deleted.", NoteID: cur.ID}, nil
		}
		return PendingOutcome{Status: PendingApplied, NoteID: cur.ID}, s.deleteNote(ctx, x, cur.ID)
	}

	if !stale {
		err := s.writeNote(ctx, x, now, cur.ID, e.Body, reasonEdit)
		if out, ok := refused(err, e.Body, cur.ID); ok {
			return out, nil
		}
		return PendingOutcome{Status: PendingApplied, NoteID: cur.ID}, err
	}
	// The app wins: the FileMaker text becomes a version, and the app's
	// text is written again on top of it.
	err = s.writeNote(ctx, x, now, cur.ID, e.Body, reasonFileMakerConflict)
	if out, ok := refused(err, e.Body, cur.ID); ok {
		return out, nil
	}
	if err != nil {
		return PendingOutcome{}, err
	}
	if err := s.writeNote(ctx, x, now, cur.ID, cur.Body, reasonConflictKeptApp); err != nil {
		return PendingOutcome{}, err
	}
	return PendingOutcome{Status: PendingConflict, NoteID: cur.ID,
		Message: fmt.Sprintf("Changed in the app at the same time. Your edit is saved in the note's history (r%d).", cur.Revision+1)}, nil
}

// refused turns a write to note id refused for its title into a rejected
// outcome.
func refused(err error, body string, id int64) (PendingOutcome, bool) {
	switch {
	case errors.Is(err, ErrTitleTaken):
		return PendingOutcome{Status: PendingRejected, NoteID: id, Message: fmt.Sprintf("A note titled %q already exists.", notes.TitleOf(body))}, true
	case errors.Is(err, ErrNoTitle):
		return PendingOutcome{Status: PendingRejected, NoteID: id, Message: "The note needs a title (its first line)."}, true
	}
	return PendingOutcome{}, false
}
