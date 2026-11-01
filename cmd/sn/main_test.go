package main

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/notes"
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

// TestCmdAutoLinkResolves checks that links AutoLink writes for non-ASCII
// mentions find their note, show up as backlinks and follow a rename.
func TestCmdAutoLinkResolves(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	ecole, err := c.Create(ctx, "# École\nx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create(ctx, "# Plans\nback to école, then ÉCOLE"); err != nil {
		t.Fatal(err)
	}
	if err := cmdAutoLink(ctx, c, &bytes.Buffer{}, []string{"Plans"}); err != nil {
		t.Fatal(err)
	}

	plans, err := c.ByTitle(ctx, "Plans")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range notes.Links(plans.Body) {
		if n, err := c.ByTitle(ctx, l); err != nil || n.ID != ecole.ID {
			t.Errorf("[[%s]] does not resolve to École: %v", l, err)
		}
	}
	back, err := c.Backlinks(ctx, ecole.ID)
	if err != nil || len(back) != 1 || back[0].Title != "Plans" {
		t.Errorf("backlinks = %v, %v", back, err)
	}
	if _, err := c.Rename(ctx, ecole.ID, "School"); err != nil {
		t.Fatal(err)
	}
	if plans, err = c.ByTitle(ctx, "Plans"); err != nil {
		t.Fatal(err)
	}
	if want := "# Plans\nback to [[School]], then [[School]]"; plans.Body != want {
		t.Errorf("after rename body = %q, want %q", plans.Body, want)
	}
}

func TestCmdImport(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	dir := t.TempDir()
	subDir := filepath.Join(dir, "notes")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	const startHereXML = `<?xml version="1.0" encoding="utf-8"?>
<note version="0.3" xmlns:link="http://beatniksoftware.com/tomboy/link" xmlns:size="http://beatniksoftware.com/tomboy/size" xmlns="http://beatniksoftware.com/tomboy">
  <title>Start Here</title>
  <text xml:space="preserve"><note-content version="0.1">Start Here

Welcome to <bold>Tomboy</bold>!
See <link:internal>Ideas</link:internal> for more.
</note-content></text>
</note>`

	const ideasXML = `<?xml version="1.0" encoding="utf-8"?>
<note version="0.3" xmlns:link="http://beatniksoftware.com/tomboy/link" xmlns:size="http://beatniksoftware.com/tomboy/size" xmlns="http://beatniksoftware.com/tomboy">
  <title>Ideas</title>
  <text xml:space="preserve"><note-content version="0.1">Ideas

<list><list-item dir="ltr">Build <monospace>scribble import</monospace></list-item></list>
</note-content></text>
</note>`

	if err := os.WriteFile(filepath.Join(subDir, "a-start.note"), []byte(startHereXML), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-.note files in a directory are ignored.
	if err := os.WriteFile(filepath.Join(subDir, "README.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}
	ideasPath := filepath.Join(dir, "ideas.note")
	if err := os.WriteFile(ideasPath, []byte(ideasXML), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := cmdImport(ctx, c, &out, []string{subDir, ideasPath}); err != nil {
		t.Fatal(err)
	}
	wantOut := "created \"Start Here\"\ncreated \"Ideas\"\nimported 2 note(s), skipped 0\n"
	if out.String() != wantOut {
		t.Errorf("printed:\n%q\nwant:\n%q", out.String(), wantOut)
	}

	startNote, err := c.ByTitle(ctx, "Start Here")
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Start Here\n\nWelcome to **Tomboy**!\nSee [[Ideas]] for more.\n"; startNote.Body != want {
		t.Errorf("Start Here body = %q, want %q", startNote.Body, want)
	}
	ideasNote, err := c.ByTitle(ctx, "Ideas")
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Ideas\n\n- Build `scribble import`\n"; ideasNote.Body != want {
		t.Errorf("Ideas body = %q, want %q", ideasNote.Body, want)
	}

	// Imported [[Ideas]] link resolves as a backlink on the server.
	back, err := c.Backlinks(ctx, ideasNote.ID)
	if err != nil || len(back) != 1 || back[0].Title != "Start Here" {
		t.Errorf("Backlinks(Ideas) = %v, %v", back, err)
	}

	// Re-importing skips existing titles and creates only new ones.
	const thirdXML = `<note xmlns="http://beatniksoftware.com/tomboy"><title>Third</title><text><note-content>Third

Fresh note</note-content></text></note>`
	thirdPath := filepath.Join(dir, "third.note")
	if err := os.WriteFile(thirdPath, []byte(thirdXML), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := cmdImport(ctx, c, &out, []string{ideasPath, thirdPath}); err != nil {
		t.Fatal(err)
	}
	wantSecond := "skipped \"Ideas\" (already exists)\ncreated \"Third\"\nimported 1 note(s), skipped 1\n"
	if out.String() != wantSecond {
		t.Errorf("second run printed:\n%q\nwant:\n%q", out.String(), wantSecond)
	}

	// Error cases: no arguments, missing path, empty directory.
	if err := cmdImport(ctx, c, &bytes.Buffer{}, nil); err == nil {
		t.Error("missing path should be refused")
	}
	if err := cmdImport(ctx, c, &bytes.Buffer{}, []string{filepath.Join(dir, "missing.note")}); err == nil {
		t.Error("non-existent path should fail")
	}
	emptyDir := filepath.Join(dir, "empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cmdImport(ctx, c, &bytes.Buffer{}, []string{emptyDir}); err == nil || !strings.Contains(err.Error(), ".note") {
		t.Errorf("empty dir err = %v, want mention of .note", err)
	}
}


func TestCmdPin(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	for _, body := range []string{"# Plans\nx", "# Bugs\ny"} {
		if _, err := c.Create(ctx, body); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		if err := cmdPin(ctx, c, &out, args[0] == "pin", args[1:]); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out.String()
	}
	ls := func() string {
		t.Helper()
		var out bytes.Buffer
		if err := cmdLs(ctx, c, &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	if got := run("pin", "plans"); got != "pinned \"Plans\"\n" {
		t.Errorf("pin printed %q", got)
	}
	if got, want := ls(), "📌 Plans\nBugs\n"; got != want {
		t.Errorf("ls after pin = %q, want %q", got, want)
	}
	// Other lists stay plain titles, for pipes.
	list, _ := c.List(ctx, "plans")
	var plain bytes.Buffer
	printList(&plain, list)
	if plain.String() != "Plans\n" {
		t.Errorf("search printed %q, want no pin mark", plain.String())
	}
	if got := run("unpin", "Plans"); got != "unpinned \"Plans\"\n" {
		t.Errorf("unpin printed %q", got)
	}
	if got, want := ls(), "Bugs\nPlans\n"; got != want {
		t.Errorf("ls after unpin = %q, want %q", got, want)
	}
	if err := cmdPin(ctx, c, &bytes.Buffer{}, true, []string{"nope"}); err == nil {
		t.Error("pinning a missing note should fail")
	}
	if err := cmdPin(ctx, c, &bytes.Buffer{}, true, nil); err == nil {
		t.Error("pin without a title should be refused")
	}
}
