package client_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

type discardSink struct{}

func (discardSink) Enqueue(types.UsageLog) {}

func newClient(t *testing.T) *client.Client {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "c.db"))
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
	return client.New(srv.URL, "tok")
}

func TestClientRoundTrip(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	n, err := c.Create(ctx, "# Ideas\nfirst")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create(ctx, "ideas\ndup"); !client.IsTitleTaken(err) {
		t.Errorf("want title-taken, got %v", err)
	}
	if _, err := c.ByTitle(ctx, "nope"); !client.IsNotFound(err) {
		t.Errorf("want not-found, got %v", err)
	}
	got, err := c.ByTitle(ctx, "IDEAS")
	if err != nil || got.ID != n.ID {
		t.Fatalf("ByTitle: %v %+v", err, got)
	}

	upd, err := c.Update(ctx, n.ID, "# Ideas\nsecond", n.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Update(ctx, n.ID, "# Ideas\nlost", n.Revision); !client.IsStale(err) {
		t.Errorf("want stale, got %v", err)
	}
	if _, err := c.Update(ctx, n.ID, "# Ideas\nforced", 0); err != nil {
		t.Errorf("baseRevision 0 should overwrite: %v", err)
	}

	other, _ := c.Create(ctx, "# Other\nsee [[Ideas]]")
	bl, err := c.Backlinks(ctx, n.ID)
	if err != nil || len(bl) != 1 || bl[0].ID != other.ID {
		t.Fatalf("backlinks: %v %+v", err, bl)
	}
	if _, err := c.Rename(ctx, n.ID, "Thoughts"); err != nil {
		t.Fatal(err)
	}
	o2, _ := c.Get(ctx, other.ID)
	if o2.Body != "# Other\nsee [[Thoughts]]" {
		t.Errorf("link not rewritten: %q", o2.Body)
	}

	changed, seq, err := c.Changes(ctx, upd.Seq)
	if err != nil || len(changed) == 0 || seq < upd.Seq {
		t.Fatalf("changes: %v %d %+v", err, seq, changed)
	}
	if err := c.Delete(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	changed, _, _ = c.Changes(ctx, seq)
	if len(changed) != 1 || !changed[0].Deleted {
		t.Errorf("want a tombstone, got %+v", changed)
	}
	list, _ := c.List(ctx, "thoughts")
	if len(list) != 1 {
		t.Errorf("search: %+v", list)
	}
}

func TestClientProjects(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	plans, _ := c.Create(ctx, "# Plans\nfor #Scribble and #home")
	bugs, _ := c.Create(ctx, "# Bugs\n#scribble sync bug")
	_, _ = c.Create(ctx, "# Other\nno tags")

	got, err := c.Projects(ctx)
	if err != nil || !slices.Equal(got, []string{"home", "scribble"}) {
		t.Fatalf("Projects = %q, %v", got, err)
	}
	list, err := c.ListInProject(ctx, "#scribble", "")
	if err != nil || len(list) != 2 || list[0].ID != bugs.ID || list[1].ID != plans.ID {
		t.Fatalf("ListInProject: %v %+v", err, list)
	}
	list, _ = c.ListInProject(ctx, "scribble", "sync")
	if len(list) != 1 || list[0].ID != bugs.ID {
		t.Errorf("project + search: %+v", list)
	}
}

func TestClientBadToken(t *testing.T) {
	c := newClient(t)
	c.Token = "wrong"
	if _, err := c.List(context.Background(), ""); !client.IsUnauthorized(err) {
		t.Errorf("want unauthorized, got %v", err)
	}
}

func TestClientHistory(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	n, _ := c.Create(ctx, "# Ideas\nfirst")
	cur, _ := c.Update(ctx, n.ID, "# Ideas\nsecond", 0)

	vs, err := c.Versions(ctx, n.ID)
	if err != nil || len(vs) != 2 || vs[0].Reason != "edit" || vs[1].Revision != 1 || vs[0].Body != "" {
		t.Fatalf("Versions: %v %+v", err, vs)
	}
	if _, err := c.Versions(ctx, 99); !client.IsNotFound(err) {
		t.Errorf("Versions of a missing note: want not-found, got %v", err)
	}
	v, err := c.GetVersion(ctx, n.ID, 1)
	if err != nil || v.Body != "# Ideas\nfirst" {
		t.Fatalf("Version: %v %+v", err, v)
	}
	if _, err := c.GetVersion(ctx, n.ID, 9); !client.IsNotFound(err) {
		t.Errorf("Version 9: want not-found, got %v", err)
	}

	if _, err := c.Restore(ctx, n.ID, 1, n.Revision); !client.IsStale(err) {
		t.Errorf("Restore from a stale base: want stale, got %v", err)
	}
	r, err := c.Restore(ctx, n.ID, 1, cur.Revision)
	if err != nil || r.Body != "# Ideas\nfirst" || r.Revision != 3 {
		t.Fatalf("Restore: %v %+v", err, r)
	}
	c.Rename(ctx, n.ID, "Thoughts")
	c.Create(ctx, "# Ideas\nsomeone else")
	if _, err := c.Restore(ctx, n.ID, 1, 0); !client.IsTitleTaken(err) {
		t.Errorf("Restore onto a taken title: want title-taken, got %v", err)
	}

	if err := c.Delete(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	del, err := c.Deleted(ctx)
	if err != nil || len(del) != 1 || del[0].ID != n.ID || del[0].Title != "Thoughts" {
		t.Fatalf("Deleted: %v %+v", err, del)
	}
	back, err := c.Undelete(ctx, n.ID)
	if err != nil || back.Deleted || back.Title != "Thoughts" {
		t.Fatalf("Undelete: %v %+v", err, back)
	}
	if _, err := c.Undelete(ctx, n.ID); !client.IsNotFound(err) {
		t.Errorf("Undelete a live note: want not-found, got %v", err)
	}
	c.Delete(ctx, n.ID)
	c.Create(ctx, "# thoughts\nnew")
	if _, err := c.Undelete(ctx, n.ID); !client.IsTitleTaken(err) {
		t.Errorf("Undelete onto a taken title: want title-taken, got %v", err)
	}
}

func TestServerURLDefaultsToNotesSuddeNet(t *testing.T) {
	t.Setenv("SCRIBBLE_URL", "")
	if got, want := client.ServerURL(), "https://notes.sudde.net"; got != want {
		t.Errorf("unset: got %q, want %q", got, want)
	}
	t.Setenv("SCRIBBLE_URL", "http://notes:8080")
	if got, want := client.ServerURL(), "http://notes:8080"; got != want {
		t.Errorf("set: got %q, want %q", got, want)
	}
}

// newSharingClient is newClient with the web app on, which serves share
// links.
func newSharingClient(t *testing.T) *client.Client {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a, err := auth.New(auth.Config{PIN: "246813", Secret: []byte("0123456789abcdef0123456789abcdef"), SessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	h, err := handlers.NewHandler(s, false, fstest.MapFS{}, "test", "dev", handlers.WithAuth(a))
	if err != nil {
		t.Fatal(err)
	}
	r, err := router.NewRouter(router.Config{
		Handler: h, Auth: a, LogSink: discardSink{}, Settings: types.AppSettings{AuthToken: "tok"},
		Assets: fstest.MapFS{}, Version: "dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return client.New(srv.URL, "tok")
}

func TestClientShare(t *testing.T) {
	ctx := context.Background()
	c := newSharingClient(t)
	n, err := c.Create(ctx, "# Ideas\nfirst")
	if err != nil {
		t.Fatal(err)
	}

	if sh, err := c.GetShare(ctx, n.ID); err != nil || sh.Token != "" || sh.URL != "" {
		t.Fatalf("private note: %+v %v", sh, err)
	}
	sh, err := c.ShareNote(ctx, n.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sh.Token) != 32 || sh.URL != c.BaseURL+"/s/"+sh.Token || sh.ExpiresAt != nil {
		t.Fatalf("ShareNote: %+v", sh)
	}
	if got, err := c.GetShare(ctx, n.ID); err != nil || got.Token != sh.Token || got.URL != sh.URL {
		t.Fatalf("GetShare: %+v %v", got, err)
	}
	if exp, err := c.ShareNote(ctx, n.ID, "7d"); err != nil || exp.Token != sh.Token || exp.ExpiresAt == nil {
		t.Fatalf("ShareNote 7d: %+v %v", exp, err)
	}
	if err := c.UnshareNote(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := c.GetShare(ctx, n.ID); err != nil || got.Token != "" {
		t.Fatalf("after UnshareNote: %+v %v", got, err)
	}
	if _, err := c.ShareNote(ctx, 999, ""); !client.IsNotFound(err) {
		t.Fatalf("missing note: want not-found, got %v", err)
	}
}

func TestClientShareRefusedWithTheWebAppOff(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	n, err := c.Create(ctx, "# Ideas")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ShareNote(ctx, n.ID, ""); !client.IsSharingOff(err) {
		t.Fatalf("want sharing-off, got %v", err)
	}
}
