package offline_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
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
// and fails them all while down, like an unreachable server.
type network struct {
	down atomic.Bool
	mu   sync.Mutex
	urls []string
}

func (n *network) RoundTrip(r *http.Request) (*http.Response, error) {
	if n.down.Load() {
		return nil, errors.New("network is down")
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
	t.Helper()
	gin.SetMode(gin.TestMode)
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
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	net := &network{}
	c := client.New(srv.URL, "tok")
	c.HTTP.Transport = net
	return c, net
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

func TestCacheOnDiskServesReadsWhileOffline(t *testing.T) {
	c, net := newServer(t)
	create(t, c, "# Groceries\nmilk #home")
	create(t, c, "# Ideas\nfly")
	path := filepath.Join(t.TempDir(), "cache.json")
	syncNow(t, open(t, path), c)

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
