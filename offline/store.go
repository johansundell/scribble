// Package offline keeps a local copy of a scribble server's notes, so the
// GUI starts instantly and can browse, search and edit with the server out
// of reach. Sync sends the edits made meanwhile and fetches only what
// changed on the server since the last sync (GET /changes?since=N).
package offline

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/notes"
)

// Edit is a local change that has not reached the server yet.
type Edit struct {
	// ID is the note's server ID, or a negative local ID for a note
	// created offline.
	ID   int64  `json:"id"`
	Body string `json:"body"`
	// BaseRevision is the revision the edit started from, so the server
	// refuses it if someone else saved since. 0 for a new note.
	BaseRevision int64     `json:"base_revision"`
	Queued       time.Time `json:"queued"`
	// Err is the server's answer when it refused the edit (a stale
	// revision, a taken title, ...). A refused edit is kept but not sent
	// again until it is queued anew or dropped.
	Err *client.Error `json:"err,omitempty"`

	version int64 // tells a re-queued edit apart while a sync sends it
}

// Report says what a Sync did with the queued edits.
type Report struct {
	Failed []int64 // edits the server refused, see Edit.Err
}

// Store is the local copy. It is safe for concurrent use.
type Store struct {
	path string // "" keeps the copy in memory only
	lock *lockHandle

	syncing sync.Mutex // one Sync at a time
	mu      sync.Mutex
	seq     int64 // the server's latest_seq as of the last sync
	notes   map[int64]notes.Note
	edits   []Edit          // in the order they were queued
	localID int64           // the last local ID handed out
	sent    map[int64]int64 // local ID -> server ID of notes created offline
	// acked maps a note's server ID and the revision one of our edits
	// started from to the revision the server gave that edit, so an
	// editor still on the old revision is not stale against itself. A
	// note created offline is recorded as an edit from revision 0.
	acked   map[int64]map[int64]int64
	version int64
}

// state is the file format.
type state struct {
	LatestSeq int64           `json:"latest_seq"`
	LocalID   int64           `json:"local_id"`
	Notes     []notes.Note    `json:"notes"`
	Edits     []Edit          `json:"edits"`
	Sent      map[int64]int64 `json:"sent,omitempty"`
}

// Open loads the copy saved at path, or starts an empty one if there is no
// file yet. An empty path keeps the copy in memory only.
// If path is non-empty, Open acquires an exclusive lock on path + ".lock".
// When another process holds the lock, Open returns an error wrapping ErrLocked.
func Open(path string) (*Store, error) {
	s := &Store{path: path, notes: map[int64]notes.Note{}, sent: map[int64]int64{}, acked: map[int64]map[int64]int64{}}
	if path == "" {
		return s, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	lock, err := acquireLock(path + ".lock")
	if err != nil {
		return nil, err
	}
	s.lock = lock

	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		_ = s.lock.Close()
		return nil, err
	}
	var st state
	if err := json.Unmarshal(b, &st); err != nil {
		_ = s.lock.Close()
		return nil, err
	}
	s.seq, s.localID, s.edits = st.LatestSeq, st.LocalID, st.Edits
	if st.Sent != nil {
		s.sent = st.Sent
	}
	for _, n := range st.Notes {
		s.notes[n.ID] = n
	}
	return s, nil
}

// Close releases the store's exclusive lock on the cache file.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock != nil {
		err := s.lock.Close()
		s.lock = nil
		return err
	}
	return nil
}

// DefaultPath is where the GUI keeps its copy of the server at baseURL.
// It holds unsynced edits, so it lives with the user's config rather than
// in a cache the system may clear.
func DefaultPath(baseURL string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	// The host keeps the name readable; the hash of the whole URL keeps
	// servers that differ only in port or path apart.
	baseURL = strings.TrimRight(baseURL, "/")
	host := "server"
	if u, err := url.Parse(baseURL); err == nil && u.Hostname() != "" {
		// Only characters every file system takes: an IPv6 address
		// has colons.
		host = strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' {
				return r
			}
			return '_'
		}, u.Hostname())
	}
	sum := sha256.Sum256([]byte(baseURL))
	return filepath.Join(dir, "scribble", host+"-"+hex.EncodeToString(sum[:8])+".json"), nil
}

// save writes the copy to disk. Callers hold s.mu.
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	st := state{LatestSeq: s.seq, LocalID: s.localID, Edits: s.edits, Sent: s.sent}
	for _, n := range s.notes {
		st.Notes = append(st.Notes, n)
	}
	slices.SortFunc(st.Notes, func(a, b notes.Note) int { return cmp.Compare(a.ID, b.ID) })
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // fails harmlessly once renamed
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// LatestSeq is the server's change counter as of the last sync.
func (s *Store) LatestSeq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seq
}

func (s *Store) editIndex(id int64) int {
	return slices.IndexFunc(s.edits, func(e Edit) bool { return e.ID == id })
}

// ServerID returns the server ID of a note created offline once a Sync has
// sent it, and any other id as it is.
func (s *Store) ServerID(id int64) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serverID(id)
}

func (s *Store) serverID(id int64) int64 {
	if sid, ok := s.sent[id]; ok {
		return sid
	}
	return id
}

// Queue records body as the new text of note id, to be sent by the next
// Sync; id 0 makes a new note, which gets a negative local ID until then.
// baseRevision is the revision the edit started from. It returns the note
// as it now reads locally.
func (s *Store) Queue(id int64, body string, baseRevision int64) (notes.Note, error) {
	if notes.TitleOf(body) == "" {
		return notes.Note{}, errors.New("a note needs a title on its first line")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	edits, localID := slices.Clone(s.edits), s.localID
	if sid, ok := s.sent[id]; ok {
		// Created offline and sent meanwhile: the editor's revision 0 is
		// the one we created it from.
		id, baseRevision = sid, s.rebase(sid, 0)
	} else if baseRevision != 0 {
		baseRevision = s.rebase(id, baseRevision)
	}
	if id == 0 {
		s.localID--
		id = s.localID
	}
	s.version++
	e := Edit{ID: id, Body: body, BaseRevision: baseRevision, Queued: time.Now(), version: s.version}
	if i := s.editIndex(id); i >= 0 {
		s.edits[i] = e
	} else {
		s.edits = append(s.edits, e)
	}
	n, _ := s.get(id)
	if err := s.save(); err != nil {
		// Not on disk, so not queued: it must not be sent later.
		s.edits, s.localID = edits, localID
		return notes.Note{}, err
	}
	return n, nil
}

// rebase moves revision base of note id past our own edits the server has
// taken since, and no further. Callers hold s.mu.
func (s *Store) rebase(id, base int64) int64 {
	for {
		next, ok := s.acked[id][base]
		if !ok || next == base {
			return base
		}
		base = next
	}
}

func (s *Store) ack(id, from, to int64) {
	if s.acked[id] == nil {
		s.acked[id] = map[int64]int64{}
	}
	s.acked[id][from] = to
}

// retryable reports whether the server's answer is a passing trouble, or
// about the token rather than the edit itself, so the edit is worth
// sending again.
func retryable(e *client.Error) bool {
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	}
	return e.Status >= 500
}

// Drop throws away the queued edit of note id, if any.
func (s *Store) Drop(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.editIndex(id); i >= 0 {
		e := s.edits[i]
		s.edits = slices.Delete(s.edits, i, i+1)
		if err := s.save(); err != nil {
			s.edits = slices.Insert(s.edits, i, e)
			return err
		}
	}
	return nil
}

// Put records note n as the server just returned it, unless the copy
// already holds a newer version. It lets the GUI show the result of a
// rename at once, whether or not the sync after it gets through.
func (s *Store) Put(n notes.Note) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.notes[n.ID]; ok && old.Seq > n.Seq {
		return nil
	}
	s.notes[n.ID] = n
	return s.save()
}

// Remove forgets note id and any queued edit of it, once the server has
// deleted it.
func (s *Store) Remove(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.notes, id)
	if i := s.editIndex(id); i >= 0 {
		s.edits = slices.Delete(s.edits, i, i+1)
	}
	return s.save()
}

// Pending returns the queued edit of note id.
func (s *Store) Pending(id int64) (Edit, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.editIndex(id); i >= 0 {
		return s.edits[i], true
	}
	return Edit{}, false
}

// Server returns note id as the server last sent it, without local edits.
func (s *Store) Server(id int64) (notes.Note, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.notes[id]
	return n, ok
}

// Get returns note id as it reads locally: with its queued edit, if any.
// A queued edit's note carries the edit's base revision.
func (s *Store) Get(id int64) (notes.Note, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(id)
}

func (s *Store) get(id int64) (notes.Note, bool) {
	n, ok := s.notes[id]
	if i := s.editIndex(id); i >= 0 {
		e := s.edits[i]
		if !ok {
			n = notes.Note{ID: id, CreatedAt: e.Queued}
		}
		n.Title, n.Body, n.Revision, n.UpdatedAt = notes.TitleOf(e.Body), e.Body, e.BaseRevision, e.Queued
		return n, true
	}
	return n, ok
}

// all returns every note as it reads locally, newest first like the server
// lists them. Callers hold s.mu.
func (s *Store) all() []notes.Note {
	out := make([]notes.Note, 0, len(s.notes)+len(s.edits))
	for id := range s.notes {
		n, _ := s.get(id)
		out = append(out, n)
	}
	for _, e := range s.edits {
		if _, ok := s.notes[e.ID]; !ok {
			n, _ := s.get(e.ID)
			out = append(out, n)
		}
	}
	slices.SortFunc(out, func(a, b notes.Note) int {
		if c := b.UpdatedAt.Compare(a.UpdatedAt); c != 0 {
			return c
		}
		return cmp.Compare(b.ID, a.ID)
	})
	return out
}

// List returns the notes, newest first, narrowed like the server's
// GET /notes: q matches titles and bodies case-insensitively and project
// keeps the notes tagged #project.
func (s *Store) List(project, q string) []notes.Note {
	s.mu.Lock()
	defer s.mu.Unlock()
	q = strings.ToLower(q)
	out := []notes.Note{}
	for _, n := range s.all() {
		if q != "" && !strings.Contains(strings.ToLower(n.Title), q) && !strings.Contains(strings.ToLower(n.Body), q) {
			continue
		}
		if project != "" && !notes.HasProject(n.Body, project) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// Projects returns the #project names in use, lowercase and sorted.
func (s *Store) Projects() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for _, n := range s.all() {
		out = append(out, notes.Projects(n.Body)...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// ByTitle finds a note by exact, case-insensitive title.
func (s *Store) ByTitle(title string) (notes.Note, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.all() {
		if strings.EqualFold(n.Title, title) {
			return n, true
		}
	}
	return notes.Note{}, false
}

// Backlinks returns the notes that contain a [[link]] to note id.
func (s *Store) Backlinks(id int64) []notes.Note {
	s.mu.Lock()
	defer s.mu.Unlock()
	target, ok := s.get(id)
	out := []notes.Note{}
	if !ok {
		return out
	}
	for _, n := range s.all() {
		if n.ID != id && notes.HasLinkTo(n.Body, target.Title) {
			out = append(out, n)
		}
	}
	return out
}

// Sync sends the queued edits to the server, then fetches what changed
// there since the last sync. An edit the server refuses is kept with its
// Err set and listed in the Report; an unreachable server stops the sync
// with an error and leaves the queue as it was.
func (s *Store) Sync(ctx context.Context, c *client.Client) (Report, error) {
	s.syncing.Lock()
	defer s.syncing.Unlock()
	var r Report
	if err := s.push(ctx, c, &r); err != nil {
		return r, err
	}
	return r, s.pull(ctx, c)
}

func (s *Store) push(ctx context.Context, c *client.Client, r *Report) error {
	s.mu.Lock()
	queue := slices.Clone(s.edits)
	s.mu.Unlock()
	for _, e := range queue {
		if e.Err != nil {
			continue
		}
		var n *notes.Note
		var err error
		if e.ID < 0 {
			n, err = c.Create(ctx, e.Body)
		} else {
			n, err = c.Update(ctx, e.ID, e.Body, e.BaseRevision)
		}
		var refused *client.Error
		if err != nil && (!errors.As(err, &refused) || retryable(refused)) {
			return err
		}
		s.mu.Lock()
		i := s.editIndex(e.ID)
		if i < 0 && e.ID < 0 && n != nil {
			// A new note dropped (deleted) while it was being created.
			s.mu.Unlock()
			if err := c.Delete(ctx, n.ID); err != nil {
				return err
			}
			continue
		}
		switch {
		case i < 0:
			// Dropped while it was being sent.
		case refused != nil:
			if s.edits[i].version == e.version {
				s.edits[i].Err = refused
				r.Failed = append(r.Failed, e.ID)
			}
		case s.edits[i].version == e.version:
			s.edits = slices.Delete(s.edits, i, i+1)
		default:
			// Edited again while it was being sent: the newer text
			// goes next time, on top of what the server now has.
			s.edits[i].ID, s.edits[i].BaseRevision = n.ID, n.Revision
		}
		if n != nil {
			s.notes[n.ID] = *n
			s.ack(n.ID, e.BaseRevision, n.Revision)
			if e.ID < 0 {
				s.sent[e.ID] = n.ID
			}
		}
		err = s.save()
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) pull(ctx context.Context, c *client.Client) error {
	changed, latest, err := c.Changes(ctx, s.LatestSeq())
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range changed {
		if n.Deleted {
			delete(s.notes, n.ID)
		} else if old, ok := s.notes[n.ID]; !ok || old.Seq <= n.Seq {
			s.notes[n.ID] = n
		}
	}
	s.seq = max(s.seq, latest)
	return s.save()
}
