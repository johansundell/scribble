package main

import (
	"bytes"
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

// newClient starts a real scribble server on a temp SQLite file.
func newClient(t *testing.T) *client.Client {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "cli.db"))
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

func TestCmdProjects(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	for _, body := range []string{"# Plans\nfor #Scribble and #home", "# Bugs\n#scribble sync", "# Other\nuntagged"} {
		if _, err := c.Create(ctx, body); err != nil {
			t.Fatal(err)
		}
	}

	cases := map[string]struct {
		args []string
		want string
	}{
		"all projects":     {nil, "home\nscribble\n"},
		"notes in project": {[]string{"scribble"}, "Bugs\nPlans\n"},
		"leading hash":     {[]string{"#Home"}, "Plans\n"},
		"unknown project":  {[]string{"nope"}, ""},
	}
	for name, tc := range cases {
		var out bytes.Buffer
		if err := cmdProjects(ctx, c, &out, tc.args); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out.String() != tc.want {
			t.Errorf("%s: printed %q, want %q", name, out.String(), tc.want)
		}
	}
	if err := cmdProjects(ctx, c, &bytes.Buffer{}, []string{"a", "b"}); err == nil {
		t.Error("two arguments should be refused")
	}
}

func TestCmdAutoLink(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	for _, body := range []string{"# Plans\nsee groceries and Go Tips, [[Go]]", "# Groceries\nmilk", "# Go Tips\nx", "# Go\ny"} {
		if _, err := c.Create(ctx, body); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	if err := cmdAutoLink(ctx, c, &out, []string{"plans"}); err != nil {
		t.Fatal(err)
	}
	if want := "linked 2 mention(s) in \"Plans\"\n"; out.String() != want {
		t.Errorf("printed %q, want %q", out.String(), want)
	}
	n, err := c.ByTitle(ctx, "Plans")
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Plans\nsee [[groceries]] and [[Go Tips]], [[Go]]"; n.Body != want {
		t.Errorf("body = %q, want %q", n.Body, want)
	}

	out.Reset()
	if err := cmdAutoLink(ctx, c, &out, []string{"Plans"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "no mentions to link\n" {
		t.Errorf("second run printed %q", out.String())
	}
	if err := cmdAutoLink(ctx, c, &out, nil); err == nil {
		t.Error("missing title should be refused")
	}
}
