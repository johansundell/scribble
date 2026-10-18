package client_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
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

func TestClientBadToken(t *testing.T) {
	c := newClient(t)
	c.Token = "wrong"
	if _, err := c.List(context.Background(), ""); !client.IsUnauthorized(err) {
		t.Errorf("want unauthorized, got %v", err)
	}
}
