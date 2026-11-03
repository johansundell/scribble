package offline_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/offline"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

type discardSink struct{}

func (discardSink) Enqueue(types.UsageLog) {}

// network sits between the client and the server. It records the requests
// and fails them all while down, like an unreachable server, or answers
// them all with status while that is set.
type network struct {
	down      atomic.Bool
	status    atomic.Int32
	onRequest func(*http.Request) // called before each request is sent
	mu        sync.Mutex
	urls      []string
}

func (n *network) RoundTrip(r *http.Request) (*http.Response, error) {
	if n.down.Load() {
		return nil, errors.New("network is down")
	}
	if status := int(n.status.Load()); status != 0 {
		return &http.Response{StatusCode: status, Body: http.NoBody, Request: r}, nil
	}
	if n.onRequest != nil {
		n.onRequest(r)
	}
	n.mu.Lock()
	n.urls = append(n.urls, r.Method+" "+r.URL.RequestURI())
	n.mu.Unlock()
	return http.DefaultTransport.RoundTrip(r)
}

func (n *network) requests() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := n.urls
	n.urls = nil
	return out
}

// newServer starts a real scribble server on a temp SQLite file and returns
// a client for it that talks through the returned network.
func newServer(t *testing.T) (*client.Client, *network) {
	c, net, _ := newResettableServer(t)
	return c, net
}

// newResettableServer is newServer with a reset that swaps the server's
// database for a new, empty one at the same URL, as the nightly demo reset
// (#161) or a rebuilt server does.
func newResettableServer(t *testing.T) (*client.Client, *network, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var current atomic.Pointer[http.Handler]
	reset := func() {
		t.Helper()
		s, err := store.NewSQLite(filepath.Join(t.TempDir(), "offline.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		h, err := handlers.NewHandler(s, false, fstest.MapFS{}, "test", "dev")
		if err != nil {
			t.Fatal(err)
		}
		r, err := router.NewRouter(router.Config{
			Handler: h, LogSink: discardSink{}, Settings: types.AppSettings{AuthToken: "tok"},
			Assets: fstest.MapFS{}, Version: "dev",
		})
		if err != nil {
			t.Fatal(err)
		}
		var handler http.Handler = r
		current.Store(&handler)
	}
	reset()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		(*current.Load()).ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	net := &network{}
	c := client.New(srv.URL, "tok")
	c.HTTP.Transport = net
	return c, net, reset
}

func create(t *testing.T, c *client.Client, body string) notes.Note {
	t.Helper()
	n, err := c.Create(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	return *n
}

func open(t *testing.T, path string) *offline.Store {
	t.Helper()
	s, err := offline.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func syncNow(t *testing.T, s *offline.Store, c *client.Client) offline.Report {
	t.Helper()
	r, err := s.Sync(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func titles(list []notes.Note) []string {
	var out []string
	for _, n := range list {
		out = append(out, n.Title)
	}
	return out
}

func assertTitles(t *testing.T, list []notes.Note, want ...string) {
	t.Helper()
	if got := titles(list); !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInitialSyncDownloadsEveryNote(t *testing.T) {
	c, _ := newServer(t)
	create(t, c, "# Groceries\nmilk #home")
	create(t, c, "# Ideas\nfly, see [[Groceries]]")
	s := open(t, "")

	syncNow(t, s, c)

	assertTitles(t, s.List("", ""), "Ideas", "Groceries")
	assertTitles(t, s.List("", "MILK"), "Groceries")
	assertTitles(t, s.List("#home", ""), "Groceries")
	if got := s.Projects(); !slices.Equal(got, []string{"home"}) {
		t.Fatalf("projects %q", got)
	}
	g, ok := s.ByTitle("groceries")
	if !ok {
		t.Fatal("ByTitle found nothing")
	}
	assertTitles(t, s.Backlinks(g.ID), "Ideas")
	if s.LatestSeq() == 0 {
		t.Fatal("latest seq not recorded")
	}
}

func TestSyncAsksOnlyForChangesSinceTheLastOne(t *testing.T) {
	c, net := newServer(t)
	create(t, c, "# Groceries\nmilk")
	s := open(t, "")
	syncNow(t, s, c)
	seq := s.LatestSeq()
	create(t, c, "# Ideas\nfly")
	net.requests()

	syncNow(t, s, c)

	want := []string{"GET /changes?since=" + strconv.FormatInt(seq, 10)}
	if got := net.requests(); !slices.Equal(got, want) {
		t.Fatalf("requests %q, want %q", got, want)
	}
	assertTitles(t, s.List("", ""), "Ideas", "Groceries")
	if s.LatestSeq() <= seq {
		t.Fatalf("latest seq %d did not move past %d", s.LatestSeq(), seq)
	}
}

func TestSyncRemovesDeletedNotes(t *testing.T) {
	c, _ := newServer(t)
	g := create(t, c, "# Groceries\nmilk")
	create(t, c, "# Ideas\nfly")
	s := open(t, "")
	syncNow(t, s, c)

	if err := c.Delete(context.Background(), g.ID); err != nil {
		t.Fatal(err)
	}
	syncNow(t, s, c)

	assertTitles(t, s.List("", ""), "Ideas")
	if _, ok := s.Get(g.ID); ok {
		t.Fatal("deleted note still cached")
	}
}

func TestSyncBringsBackUndeletedNotes(t *testing.T) {
	ctx := context.Background()
	c, _ := newServer(t)
	g := create(t, c, "# Groceries\nmilk")
	s := open(t, "")
	syncNow(t, s, c)
	if err := c.Delete(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	syncNow(t, s, c)
	assertTitles(t, s.List("", ""))

	if _, err := c.Undelete(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	syncNow(t, s, c)
	assertTitles(t, s.List("", ""), "Groceries")
	if n, ok := s.Get(g.ID); !ok || n.Body != "# Groceries\nmilk" || n.Deleted {
		t.Fatalf("undeleted note = %+v, %v", n, ok)
	}
}

func TestSyncShowsARestoredVersion(t *testing.T) {
	ctx := context.Background()
	c, _ := newServer(t)
	g := create(t, c, "# Groceries\nmilk")
	if _, err := c.Update(ctx, g.ID, "# Groceries\neggs", 0); err != nil {
		t.Fatal(err)
	}
	s := open(t, "")
	syncNow(t, s, c)

	if _, err := c.Restore(ctx, g.ID, 1, 0); err != nil {
		t.Fatal(err)
	}
	syncNow(t, s, c)
	if n, _ := s.Get(g.ID); n.Body != "# Groceries\nmilk" {
		t.Fatalf("restored note reads %q", n.Body)
	}
}

func TestCacheOnDiskServesReadsWhileOffline(t *testing.T) {
	c, net := newServer(t)
	create(t, c, "# Groceries\nmilk #home")
	create(t, c, "# Ideas\nfly")
	path := filepath.Join(t.TempDir(), "cache.json")
	first := open(t, path)
	syncNow(t, first, c)
	first.Close()

	net.down.Store(true)
	s := open(t, path)

	assertTitles(t, s.List("", ""), "Ideas", "Groceries")
	assertTitles(t, s.List("", "milk"), "Groceries")
	if _, err := s.Sync(context.Background(), c); err == nil {
		t.Fatal("sync while offline did not fail")
	}
	assertTitles(t, s.List("", ""), "Ideas", "Groceries")
}

func TestEditQueuedOfflineReachesTheServerOnReconnect(t *testing.T) {
	c, net := newServer(t)
	g := create(t, c, "# Groceries\nmilk")
	path := filepath.Join(t.TempDir(), "cache.json")
	s := open(t, path)
	syncNow(t, s, c)

	net.down.Store(true)
	if _, err := s.Queue(g.ID, "# Groceries\nmilk and eggs", g.Revision); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Get(g.ID); n.Body != "# Groceries\nmilk and eggs" {
		t.Fatalf("cached body %q", n.Body)
	}
	if _, err := s.Sync(context.Background(), c); err == nil {
		t.Fatal("sync while offline did not fail")
	}

	net.down.Store(false)
	s.Close()
	s = open(t, path) // the queue survives a restart
	syncNow(t, s, c)

	got, err := c.Get(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "# Groceries\nmilk and eggs" {
		t.Fatalf("server body %q", got.Body)
	}
	if _, ok := s.Pending(g.ID); ok {
		t.Fatal("edit still queued after sync")
	}
	if n, _ := s.Get(g.ID); n.Revision != got.Revision {
		t.Fatalf("cached revision %d, server %d", n.Revision, got.Revision)
	}
}

func TestNoteCreatedOfflineGetsItsServerID(t *testing.T) {
	c, net := newServer(t)
	s := open(t, "")
	net.down.Store(true)

	local, err := s.Queue(0, "# Ideas\nfly", 0)
	if err != nil {
		t.Fatal(err)
	}
	if local.ID >= 0 {
		t.Fatalf("local note has id %d, want a negative one", local.ID)
	}
	assertTitles(t, s.List("", ""), "Ideas")

	net.down.Store(false)
	syncNow(t, s, c)

	id := s.ServerID(local.ID)
	if id <= 0 {
		t.Fatalf("no server id for %d, got %d", local.ID, id)
	}
	if n, ok := s.Get(id); !ok || n.Title != "Ideas" {
		t.Fatalf("Get(%d) = %+v, %v", id, n, ok)
	}
	if _, ok := s.Get(local.ID); ok {
		t.Fatal("local id still cached")
	}
	assertTitles(t, s.List("", ""), "Ideas")
}

func TestStaleOfflineEditIsKeptAsAConflict(t *testing.T) {
	c, net := newServer(t)
	g := create(t, c, "# Groceries\nmilk")
	s := open(t, "")
	syncNow(t, s, c)

	net.down.Store(true)
	if _, err := s.Queue(g.ID, "# Groceries\nmine", g.Revision); err != nil {
		t.Fatal(err)
	}
	net.down.Store(false)
	if _, err := c.Update(context.Background(), g.ID, "# Groceries\ntheirs", g.Revision); err != nil {
		t.Fatal(err)
	}

	r := syncNow(t, s, c)

	if !slices.Equal(r.Failed, []int64{g.ID}) {
		t.Fatalf("failed %v", r.Failed)
	}
	e, ok := s.Pending(g.ID)
	if !ok || !client.IsStale(e.Err) {
		t.Fatalf("pending %+v, %v; want a stale conflict", e, ok)
	}
	if n, _ := s.Get(g.ID); n.Body != "# Groceries\nmine" {
		t.Fatalf("local edit lost: %q", n.Body)
	}
	if n, _ := s.Server(g.ID); n.Body != "# Groceries\ntheirs" {
		t.Fatalf("server version %q", n.Body)
	}
	// A conflict is not retried on every sync.
	if r := syncNow(t, s, c); len(r.Failed) != 0 {
		t.Fatalf("conflict retried: %+v", r)
	}

	if err := s.Drop(g.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Get(g.ID); n.Body != "# Groceries\ntheirs" {
		t.Fatalf("after drop %q", n.Body)
	}
}

func TestEditingASentOfflineNoteByItsLocalIDUpdatesIt(t *testing.T) {
	c, net := newServer(t)
	s := open(t, "")
	net.down.Store(true)
	local, err := s.Queue(0, "# Ideas\nfly", 0)
	if err != nil {
		t.Fatal(err)
	}
	net.down.Store(false)
	syncNow(t, s, c) // say, the background sync

	if _, err := s.Queue(local.ID, "# Ideas\nfly high", 0); err != nil {
		t.Fatal(err)
	}
	syncNow(t, s, c)

	list, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Body != "# Ideas\nfly high" {
		t.Fatalf("server holds %+v, want one updated note", list)
	}
}

func TestDefaultPathsOfDifferentServersDiffer(t *testing.T) {
	a, err := offline.DefaultPath("https://host/a_b")
	if err != nil {
		t.Fatal(err)
	}
	b, err := offline.DefaultPath("https://host/a/b")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("both servers use %s", a)
	}
}

func TestEditFromTheRevisionBeforeOwnSentEditIsNotStale(t *testing.T) {
	c, net := newServer(t)
	g := create(t, c, "# Groceries\nmilk")
	s := open(t, "")
	syncNow(t, s, c)

	net.down.Store(true)
	if _, err := s.Queue(g.ID, "# Groceries\nmilk and eggs", g.Revision); err != nil {
		t.Fatal(err)
	}
	net.down.Store(false)
	syncNow(t, s, c)
	// The editor still holds the revision it started from.
	if _, err := s.Queue(g.ID, "# Groceries\nmilk, eggs and tea", g.Revision); err != nil {
		t.Fatal(err)
	}

	if r := syncNow(t, s, c); len(r.Failed) != 0 {
		e, _ := s.Pending(g.ID)
		t.Fatalf("own edit made the next one stale: %v", e.Err)
	}
	if got, _ := c.Get(context.Background(), g.ID); got.Body != "# Groceries\nmilk, eggs and tea" {
		t.Fatalf("server holds %q", got.Body)
	}
}

func TestEditOfSentOfflineNoteStillDetectsOtherWriters(t *testing.T) {
	c, net := newServer(t)
	s := open(t, "")
	net.down.Store(true)
	local, err := s.Queue(0, "# Ideas\nfly", 0)
	if err != nil {
		t.Fatal(err)
	}
	net.down.Store(false)
	syncNow(t, s, c)
	id := s.ServerID(local.ID)
	n, err := c.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Update(context.Background(), id, "# Ideas\ntheirs", n.Revision); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Queue(local.ID, "# Ideas\nmine", 0); err != nil {
		t.Fatal(err)
	}
	syncNow(t, s, c)

	if e, ok := s.Pending(id); !ok || !client.IsStale(e.Err) {
		t.Fatalf("pending %+v, %v; want a stale conflict", e, ok)
	}
}

func TestServerTroubleKeepsTheEditQueued(t *testing.T) {
	c, net := newServer(t)
	g := create(t, c, "# Groceries\nmilk")
	s := open(t, "")
	syncNow(t, s, c)
	if _, err := s.Queue(g.ID, "# Groceries\nmilk and eggs", g.Revision); err != nil {
		t.Fatal(err)
	}

	net.status.Store(http.StatusServiceUnavailable)
	if _, err := s.Sync(context.Background(), c); err == nil {
		t.Fatal("sync during an outage did not fail")
	}
	if e, ok := s.Pending(g.ID); !ok || e.Err != nil {
		t.Fatalf("pending %+v, %v; want it queued for retry", e, ok)
	}

	net.status.Store(0)
	syncNow(t, s, c)
	if got, _ := c.Get(context.Background(), g.ID); got.Body != "# Groceries\nmilk and eggs" {
		t.Fatalf("server holds %q", got.Body)
	}
}

func TestNoteDroppedWhileBeingCreatedIsDeletedOnTheServer(t *testing.T) {
	c, net := newServer(t)
	s := open(t, "")
	local, err := s.Queue(0, "# Ideas\nfly", 0)
	if err != nil {
		t.Fatal(err)
	}
	net.onRequest = func(r *http.Request) {
		if r.Method == "POST" {
			if err := s.Drop(local.ID); err != nil {
				t.Error(err)
			}
		}
	}

	syncNow(t, s, c)

	net.onRequest = nil
	if list, _ := c.List(context.Background(), ""); len(list) != 0 {
		t.Fatalf("server still holds %q", titles(list))
	}
	assertTitles(t, s.List("", ""))
}

func TestDefaultPathOfIPv6ServerIsAPortableFileName(t *testing.T) {
	p, err := offline.DefaultPath("http://[::1]:8080")
	if err != nil {
		t.Fatal(err)
	}
	if name := filepath.Base(p); strings.ContainsAny(name, `:<>"/\\|?*`) {
		t.Fatalf("file name %q is not valid on Windows", name)
	}
}

func TestQueueThatCannotBeSavedLeavesNothingQueued(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes to read-only directories")
	}
	dir := t.TempDir()
	s := open(t, filepath.Join(dir, "cache.json"))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	local, err := s.Queue(0, "# Ideas\nfly", 0)
	if err == nil {
		t.Fatal("Queue into a read-only directory did not fail")
	}
	if _, ok := s.Pending(local.ID); ok || len(s.List("", "")) != 0 {
		t.Fatalf("the failed edit is still queued: %q", titles(s.List("", "")))
	}
}

func TestRejectedTokenKeepsTheEditQueued(t *testing.T) {
	c, net := newServer(t)
	g := create(t, c, "# Groceries\nmilk")
	s := open(t, "")
	syncNow(t, s, c)
	if _, err := s.Queue(g.ID, "# Groceries\nmilk and eggs", g.Revision); err != nil {
		t.Fatal(err)
	}

	net.status.Store(http.StatusUnauthorized)
	if _, err := s.Sync(context.Background(), c); !client.IsUnauthorized(err) {
		t.Fatalf("sync with a bad token returned %v", err)
	}
	if e, ok := s.Pending(g.ID); !ok || e.Err != nil {
		t.Fatalf("pending %+v, %v; want it queued for retry", e, ok)
	}
}

// TestTooOldClientKeepsTheEditQueued checks that a server that refuses this
// client as too old (426) stops the sync and leaves the edit queued, unmarked,
// on disk, for a newer version of the app to send.
func TestTooOldClientKeepsTheEditQueued(t *testing.T) {
	c, net := newServer(t)
	g := create(t, c, "# Groceries\nmilk")
	path := filepath.Join(t.TempDir(), "cache.json")
	s := open(t, path)
	syncNow(t, s, c)
	if _, err := s.Queue(g.ID, "# Groceries\nmilk and eggs", g.Revision); err != nil {
		t.Fatal(err)
	}

	net.status.Store(http.StatusUpgradeRequired)
	r, err := s.Sync(context.Background(), c)
	if !client.IsUpgradeRequired(err) {
		t.Fatalf("sync of a too-old client returned %v", err)
	}
	if len(r.Failed) != 0 {
		t.Errorf("the edit was reported refused: %v", r.Failed)
	}
	s.Close()
	s = open(t, path)
	if e, ok := s.Pending(g.ID); !ok || e.Err != nil || e.Body != "# Groceries\nmilk and eggs" {
		t.Fatalf("pending %+v, %v; want it still queued on disk", e, ok)
	}
}

func TestPinnedNotesAreListedFirst(t *testing.T) {
	ctx := context.Background()
	c, _ := newServer(t)
	g := create(t, c, "# Groceries\nmilk")
	create(t, c, "# Ideas\nfly")
	s := open(t, "")
	syncNow(t, s, c)
	assertTitles(t, s.List("", ""), "Ideas", "Groceries")

	if _, err := c.Pin(ctx, g.ID, true); err != nil {
		t.Fatal(err)
	}
	syncNow(t, s, c)
	assertTitles(t, s.List("", ""), "Groceries", "Ideas")

	// A queued edit of another note doesn't jump the pin.
	ideas, _ := s.ByTitle("Ideas")
	if _, err := s.Queue(ideas.ID, "# Ideas\nfly higher", ideas.Revision); err != nil {
		t.Fatal(err)
	}
	assertTitles(t, s.List("", ""), "Groceries", "Ideas")
	// A queued edit of the pinned note keeps its pin.
	if _, err := s.Queue(g.ID, "# Groceries\neggs", 0); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Get(g.ID); !n.Pinned {
		t.Errorf("queued edit lost the pin: %+v", n)
	}
}

func get(t *testing.T, c *client.Client, id int64) notes.Note {
	t.Helper()
	n, err := c.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return *n
}

func serverTitles(t *testing.T, c *client.Client) []string {
	t.Helper()
	list, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	out := titles(list)
	slices.Sort(out)
	return out
}

// After the server's database is replaced, the copy holds what the new one
// holds, not what the old one did (#167).
func TestSyncAfterAResetReplacesTheCopy(t *testing.T) {
	c, _, reset := newResettableServer(t)
	create(t, c, "# Old one")
	create(t, c, "# Old two")
	s := open(t, filepath.Join(t.TempDir(), "cache.json"))
	if r := syncNow(t, s, c); r.Reset {
		t.Fatal("first sync reported a reset")
	}

	reset()
	create(t, c, "# New")
	r := syncNow(t, s, c)
	if !r.Reset {
		t.Error("sync did not report the reset")
	}
	assertTitles(t, s.List("", ""), "New")
	if r := syncNow(t, s, c); r.Reset {
		t.Error("the sync after a reset reported another one")
	}
}

// The counters alone miss a reset once the new database has seen more
// changes than the old one had; the database ID doesn't.
func TestResetIsNoticedWhenTheNewCounterIsAhead(t *testing.T) {
	c, _, reset := newResettableServer(t)
	create(t, c, "# Old")
	s := open(t, filepath.Join(t.TempDir(), "cache.json"))
	syncNow(t, s, c)

	reset()
	for i := range 5 {
		create(t, c, "# New "+strconv.Itoa(i))
	}
	if r := syncNow(t, s, c); !r.Reset {
		t.Error("sync did not report the reset")
	}
	assertTitles(t, s.List("", ""), "New 4", "New 3", "New 2", "New 1", "New 0")
}

// Edits made before the reset that never reached the server are sent as
// new notes, and never written over the new database's note that now has
// the old ID.
func TestUnsentEditsBecomeNewNotesAfterAReset(t *testing.T) {
	c, _, reset := newResettableServer(t)
	g := create(t, c, "# Groceries\nmilk")
	path := filepath.Join(t.TempDir(), "cache.json")
	s := open(t, path)
	syncNow(t, s, c)

	if _, err := s.Queue(g.ID, "# Groceries\nmilk and eggs", g.Revision); err != nil {
		t.Fatal(err)
	}
	offlineNote, err := s.Queue(0, "# Written offline", 0)
	if err != nil {
		t.Fatal(err)
	}
	reset()
	other := create(t, c, "# Other") // takes the ID Groceries had
	if other.ID != g.ID {
		t.Fatalf("test needs the IDs to collide: %d, %d", other.ID, g.ID)
	}
	s.Close()
	s = open(t, path) // the database ID survives a restart

	r := syncNow(t, s, c)
	if !r.Reset {
		t.Fatal("sync did not report the reset")
	}
	if got := get(t, c, other.ID); got.Body != "# Other" {
		t.Errorf("the new database's note was overwritten: %q", got.Body)
	}
	if got := serverTitles(t, c); !slices.Equal(got, []string{"Groceries", "Other", "Written offline"}) {
		t.Errorf("server has %q", got)
	}
	// The GUI follows the open note to where its edit went.
	moved, ok := r.Moved[g.ID]
	if !ok {
		t.Fatalf("Moved = %v, want an entry for %d", r.Moved, g.ID)
	}
	if n, _ := s.Get(s.ServerID(moved)); n.Body != "# Groceries\nmilk and eggs" {
		t.Errorf("moved edit reads %q", n.Body)
	}
	if n, _ := s.Get(s.ServerID(offlineNote.ID)); n.Title != "Written offline" {
		t.Errorf("note created offline reads %q", n.Title)
	}
	if len(slices.DeleteFunc(s.List("", ""), func(n notes.Note) bool { return n.ID > 0 })) != 0 {
		t.Error("local notes left after the sync")
	}
}

// An unsent edit whose title the new database already has keeps both.
func TestUnsentEditWithATakenTitleIsKeptUnderAnotherAfterAReset(t *testing.T) {
	c, _, reset := newResettableServer(t)
	g := create(t, c, "# Groceries\nmilk")
	s := open(t, filepath.Join(t.TempDir(), "cache.json"))
	syncNow(t, s, c)

	if _, err := s.Queue(g.ID, "# Groceries\nmilk and eggs", g.Revision); err != nil {
		t.Fatal(err)
	}
	reset()
	create(t, c, "# Groceries\nbread")

	r := syncNow(t, s, c)
	if len(r.Failed) != 0 {
		t.Errorf("failed: %v", r.Failed)
	}
	if got := serverTitles(t, c); !slices.Equal(got, []string{"Groceries", "Groceries (from this computer)"}) {
		t.Errorf("server has %q", got)
	}
	n, err := c.ByTitle(context.Background(), "Groceries (from this computer)")
	if err != nil {
		t.Fatal(err)
	}
	if n.Body != "# Groceries (from this computer)\nmilk and eggs" {
		t.Errorf("kept as %q", n.Body)
	}
}

// A copy saved before database IDs existed takes the server's ID without
// treating it as a reset, so upgrading the GUI keeps its notes and edits.
func TestCopyFromBeforeDatabaseIDsAdoptsTheServers(t *testing.T) {
	c, _ := newServer(t)
	g := create(t, c, "# Groceries\nmilk")
	path := filepath.Join(t.TempDir(), "cache.json")
	old := `{"latest_seq":` + strconv.FormatInt(g.Seq, 10) + `,"local_id":0,"notes":[{"id":` + strconv.FormatInt(g.ID, 10) +
		`,"title":"Groceries","body":"# Groceries\nmilk","revision":` + strconv.FormatInt(g.Revision, 10) +
		`,"seq":` + strconv.FormatInt(g.Seq, 10) + `}],"edits":[{"id":` + strconv.FormatInt(g.ID, 10) +
		`,"body":"# Groceries\nmilk and eggs","base_revision":` + strconv.FormatInt(g.Revision, 10) + `}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s := open(t, path)
	if r := syncNow(t, s, c); r.Reset {
		t.Error("an old copy was treated as a reset")
	}
	if got := get(t, c, g.ID); got.Body != "# Groceries\nmilk and eggs" {
		t.Errorf("edit not sent as an update: %q", got.Body)
	}
	if got := serverTitles(t, c); !slices.Equal(got, []string{"Groceries"}) {
		t.Errorf("server has %q", got)
	}
}
