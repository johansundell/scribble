// Package replica keeps the FileMaker replica of the notes (#66, ADR 10).
// SQLite stays the master: a background sync pushes every note change to
// FileMaker's Notes table in seq order, and pulls the edits made in
// FileMaker from its PendingEdits table, applying them as normal saves.
// The API never waits on it.
package replica

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/johansundell/scribble/fmsodata"
	"github.com/johansundell/scribble/logging"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/store"
)

// SchemaVersion is the ScribbleInfo::SchemaVersion this build expects.
const SchemaVersion = 1

// The fields of each FileMaker table (docs/filemaker/notes.md). The first
// is the primary key.
var (
	noteFields    = []string{"NoteID", "Title", "Body", "Revision", "Seq", "Deleted", "CreatedAt", "UpdatedAt", "SyncedAt"}
	pendingFields = []string{"ID", "Kind", "NoteID", "Body", "BaseRevision", "CreatedAt", "CreatedBy", "Status", "Message"}
	infoFields    = []string{"SchemaVersion"}
)

// timeLayout writes timestamps as UTC without an offset, as for the log
// table: FileMaker timestamps have no time zone.
const timeLayout = "2006-01-02T15:04:05"

// pageSize is how many records a read asks for at a time.
const pageSize = 500

// Client is the part of the FileMaker OData client the sync uses;
// *fmsodata.Client has it.
type Client interface {
	GetRecords(ctx context.Context, table string, query url.Values) ([]map[string]any, error)
	CreateRecord(ctx context.Context, table string, data map[string]any) (map[string]any, error)
	UpdateRecordByKey(ctx context.Context, table, key string, data map[string]any) error
	DeleteRecordByKey(ctx context.Context, table, key string) error
}

// Store is the SQLite side; *store.SQLStore has it.
type Store interface {
	NoteChanges(ctx context.Context, since int64) ([]notes.Note, error)
	ReplicaState(ctx context.Context) (store.ReplicaState, error)
	AdvancePush(ctx context.Context, from, to int64) (bool, error)
	PushBacklog(ctx context.Context) (int, error)
	ApplyPendingEdit(ctx context.Context, e store.PendingEdit) (store.PendingOutcome, error)
	ForgetPendingOutcome(ctx context.Context, id int64) error
}

// Tables names the FileMaker tables (FMS_NOTES_TABLE, FMS_PENDING_TABLE,
// FMS_INFO_TABLE).
type Tables struct {
	Notes, Pending, Info string
}

// Config configures a Syncer.
type Config struct {
	Tables Tables
	// Interval is how often a round runs when nothing has changed
	// (NOTES_REPLICA_INTERVAL).
	Interval time.Duration
	Logger   logging.Logger
}

// Status is the replica's state for /healthz.
type Status struct {
	// Status is ok, behind (notes waiting to be pushed), unreachable (the
	// last round failed) or off (the setup is wrong; see Reason).
	Status      string `json:"status"`
	PendingPush int    `json:"pending_push"`
	// LastSync is when the last round finished, in UTC; empty before one.
	LastSync string `json:"last_sync,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Syncer runs the sync. Create it with New and start it with Run.
type Syncer struct {
	client Client
	store  Store
	cfg    Config
	log    logging.Logger

	// poll is how often Run looks for unpushed notes, so a write reaches
	// FileMaker within about that time. Retries wait from minRetry,
	// doubling up to maxRetry. Tests shorten them.
	poll, minRetry, maxRetry time.Duration

	mu       sync.Mutex
	off      error // the setup is wrong; the sync stays off
	failure  error // the last round failed
	lastSync time.Time
}

// New returns a Syncer between FileMaker c and SQLite s.
func New(c Client, s Store, cfg Config) *Syncer {
	return &Syncer{
		client:   c,
		store:    s,
		cfg:      cfg,
		log:      logging.OrStd(cfg.Logger),
		poll:     2 * time.Second,
		minRetry: 5 * time.Second,
		maxRetry: 5 * time.Minute,
	}
}

// setupError is a FileMaker setup that retrying won't fix.
type setupError struct{ err error }

func (e setupError) Error() string { return e.err.Error() }
func (e setupError) Unwrap() error { return e.err }

// isSetupError reports whether err is a wrong setup rather than FileMaker
// being unreachable: a check that failed, or a client error such as a
// missing table or field or a wrong password.
func isSetupError(err error) bool {
	var se setupError
	if errors.As(err, &se) {
		return true
	}
	var st *fmsodata.StatusError
	return errors.As(err, &st) && st.StatusCode >= 400 && st.StatusCode < 500 &&
		st.StatusCode != http.StatusRequestTimeout && st.StatusCode != http.StatusTooManyRequests
}

func isNotFound(err error) bool {
	var st *fmsodata.StatusError
	return errors.As(err, &st) && st.StatusCode == http.StatusNotFound
}

// Check reads every table with all its fields, and the schema version. The
// service never creates FileMaker tables; a FileMaker developer sets them
// up from docs/filemaker/notes.md.
func (s *Syncer) Check(ctx context.Context) error {
	t := s.cfg.Tables
	for _, tf := range []struct {
		table  string
		fields []string
	}{{t.Notes, noteFields}, {t.Pending, pendingFields}, {t.Info, infoFields}} {
		q := url.Values{}
		q.Set("$select", strings.Join(tf.fields, ","))
		q.Set("$top", "0")
		if _, err := s.client.GetRecords(ctx, tf.table, q); err != nil {
			return fmt.Errorf("check FileMaker table %q: %w", tf.table, err)
		}
	}
	q := url.Values{}
	q.Set("$select", strings.Join(infoFields, ","))
	info, err := s.client.GetRecords(ctx, t.Info, q)
	if err != nil {
		return fmt.Errorf("read FileMaker table %q: %w", t.Info, err)
	}
	if len(info) != 1 {
		return setupError{fmt.Errorf("FileMaker table %q must have exactly one record, has %d", t.Info, len(info))}
	}
	if v := number(info[0]["SchemaVersion"]); v != SchemaVersion {
		return setupError{fmt.Errorf("FileMaker %s::SchemaVersion is %d; this server needs %d", t.Info, v, SchemaVersion)}
	}
	return nil
}

// Disable turns the replica off for good, with err as the reason.
func (s *Syncer) Disable(err error) {
	s.log.Errorf("FileMaker replica is off: %v", err)
	s.mu.Lock()
	s.off = err
	s.mu.Unlock()
}

// Run checks the setup, then runs a round every Interval, and within poll
// of a note write. It retries a failed round with backoff. A wrong setup
// turns the replica off and Run returns; otherwise it returns when ctx is
// done.
func (s *Syncer) Run(ctx context.Context) {
	checked := false
	retry := time.Duration(0) // > 0 while FileMaker is failing
	next := time.Now()        // when a round is due anyway
	for {
		s.mu.Lock()
		off := s.off != nil
		s.mu.Unlock()
		if off {
			return
		}
		if !time.Now().Before(next) || (checked && retry == 0 && s.hasBacklog(ctx)) {
			err := s.step(ctx, &checked)
			switch {
			case ctx.Err() != nil:
				return
			case err == nil:
				retry = 0
				next = time.Now().Add(s.cfg.Interval)
			case !checked && isSetupError(err):
				s.Disable(err)
				return
			default:
				retry = min(max(2*retry, s.minRetry), s.maxRetry)
				next = time.Now().Add(retry)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.poll):
		}
	}
}

// step checks the setup the first time, then runs a round.
func (s *Syncer) step(ctx context.Context, checked *bool) error {
	if !*checked {
		if err := s.Check(ctx); err != nil {
			if !isSetupError(err) {
				s.failed(err)
			}
			return err
		}
		*checked = true
	}
	return s.Round(ctx)
}

func (s *Syncer) hasBacklog(ctx context.Context) bool {
	n, err := s.store.PushBacklog(ctx)
	return err == nil && n > 0
}

// Round pulls the edits waiting in FileMaker, then pushes the notes changed
// since the last push, so FileMaker sees the pulled edits at once.
func (s *Syncer) Round(ctx context.Context) error {
	err := s.pull(ctx)
	if err == nil {
		err = s.push(ctx)
	}
	if err != nil {
		s.failed(err)
		return err
	}
	s.mu.Lock()
	recovered := s.failure != nil
	s.failure = nil
	s.lastSync = time.Now()
	s.mu.Unlock()
	if recovered {
		s.log.Infof("FileMaker replica: FileMaker is reachable again; the sync is running")
	}
	return nil
}

// failed records a failed round, logging only the first of a run of them.
func (s *Syncer) failed(err error) {
	s.mu.Lock()
	first := s.failure == nil
	s.failure = err
	s.mu.Unlock()
	if first {
		s.log.Warningf("FileMaker replica: FileMaker is unreachable, retrying until it is back (the API is unaffected): %v", err)
	}
}

// Status reports the replica's state.
func (s *Syncer) Status(ctx context.Context) Status {
	s.mu.Lock()
	off, failure, last := s.off, s.failure, s.lastSync
	s.mu.Unlock()
	st := Status{}
	if !last.IsZero() {
		st.LastSync = last.UTC().Format(time.RFC3339)
	}
	n, err := s.store.PushBacklog(ctx)
	st.PendingPush = n
	switch {
	case off != nil:
		st.Status, st.Reason = "off", off.Error()
	case failure != nil:
		st.Status, st.Reason = "unreachable", failure.Error()
	case err != nil:
		st.Status, st.Reason = "behind", err.Error()
	case n > 0 || last.IsZero():
		st.Status = "behind"
	default:
		st.Status = "ok"
	}
	return st
}

// push writes the notes changed since the last push to FileMaker, in seq
// order, moving the last pushed seq on after each one.
func (s *Syncer) push(ctx context.Context) error {
	st, err := s.store.ReplicaState(ctx)
	if err != nil {
		return err
	}
	changed, err := s.store.NoteChanges(ctx, st.LastPushedSeq)
	if err != nil {
		return err
	}
	prev := st.LastPushedSeq
	for _, n := range changed {
		if err := s.upsert(ctx, n); err != nil {
			return fmt.Errorf("push note %d to FileMaker: %w", n.ID, err)
		}
		ok, err := s.store.AdvancePush(ctx, prev, n.Seq)
		if err != nil {
			return err
		}
		if !ok {
			return nil // a resync reset the push; the next round starts over
		}
		prev = n.Seq
	}
	return nil
}

// upsert updates note n's record, or creates it when there is none. Writing
// the same note twice is harmless.
func (s *Syncer) upsert(ctx context.Context, n notes.Note) error {
	deleted := 0
	if n.Deleted {
		deleted = 1
	}
	rec := map[string]any{
		"Title":     n.Title,
		"Body":      n.Body,
		"Revision":  n.Revision,
		"Seq":       n.Seq,
		"Deleted":   deleted,
		"CreatedAt": n.CreatedAt.UTC().Format(timeLayout),
		"UpdatedAt": n.UpdatedAt.UTC().Format(timeLayout),
		"SyncedAt":  time.Now().UTC().Format(timeLayout),
	}
	err := s.client.UpdateRecordByKey(ctx, s.cfg.Tables.Notes, strconv.FormatInt(n.ID, 10), rec)
	if !isNotFound(err) {
		return err
	}
	rec["NoteID"] = n.ID
	_, err = s.client.CreateRecord(ctx, s.cfg.Tables.Notes, rec)
	return err
}

// pull applies the waiting PendingEdits rows in ID order, then cleans up
// after each: an applied row is deleted, and a conflict or rejection gets
// its Status and Message. SQLite remembers what it applied, so a row whose
// cleanup failed is only cleaned up next time, never applied again.
func (s *Syncer) pull(ctx context.Context) error {
	q := url.Values{}
	q.Set("$select", strings.Join(pendingFields, ","))
	q.Set("$orderby", "ID asc")
	rows, err := s.readAll(ctx, s.cfg.Tables.Pending, q)
	if err != nil {
		return fmt.Errorf("read FileMaker table %q: %w", s.cfg.Tables.Pending, err)
	}
	for _, r := range rows {
		if text(r["Status"]) != "" {
			continue // handled; it waits for the FileMaker user
		}
		e := store.PendingEdit{
			ID:           number(r["ID"]),
			Kind:         strings.ToLower(strings.TrimSpace(text(r["Kind"]))),
			NoteID:       number(r["NoteID"]),
			Body:         normalizeBody(text(r["Body"])),
			BaseRevision: number(r["BaseRevision"]),
		}
		if e.ID <= 0 {
			continue // no serial number yet
		}
		out, err := s.store.ApplyPendingEdit(ctx, e)
		if err != nil {
			return fmt.Errorf("apply %s row %d: %w", s.cfg.Tables.Pending, e.ID, err)
		}
		s.log.Infof("FileMaker replica: %s row %d (%s of note %d, source=filemaker): %s %s",
			s.cfg.Tables.Pending, e.ID, e.Kind, out.NoteID, out.Status, out.Message)
		if err := s.cleanup(ctx, e.ID, out); err != nil {
			return fmt.Errorf("clean up %s row %d: %w", s.cfg.Tables.Pending, e.ID, err)
		}
		if err := s.store.ForgetPendingOutcome(ctx, e.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Syncer) cleanup(ctx context.Context, id int64, out store.PendingOutcome) error {
	key := strconv.FormatInt(id, 10)
	var err error
	if out.Status == store.PendingApplied {
		err = s.client.DeleteRecordByKey(ctx, s.cfg.Tables.Pending, key)
	} else {
		err = s.client.UpdateRecordByKey(ctx, s.cfg.Tables.Pending, key,
			map[string]any{"Status": out.Status, "Message": out.Message})
	}
	if isNotFound(err) {
		return nil // someone removed the row meanwhile
	}
	return err
}

// readAll reads every record of table, pageSize at a time.
func (s *Syncer) readAll(ctx context.Context, table string, q url.Values) ([]map[string]any, error) {
	var all []map[string]any
	for skip := 0; ; skip += pageSize {
		q.Set("$top", strconv.Itoa(pageSize))
		q.Set("$skip", strconv.Itoa(skip))
		page, err := s.client.GetRecords(ctx, table, q)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < pageSize {
			return all, nil
		}
	}
}

// normalizeBody turns FileMaker's line breaks (CR, or CRLF from a paste)
// back into LF.
func normalizeBody(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

func number(v any) int64 {
	switch v := v.(type) {
	case float64:
		return int64(v)
	case string: // a Number field holding text
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	}
	return 0
}

func text(v any) string {
	s, _ := v.(string)
	return s
}
