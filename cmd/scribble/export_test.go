package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johansundell/scribble/notes"
)

func TestCmdExport(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	if _, err := c.Create(ctx, "# Plans\nsoon"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	day := time.Date(2026, 10, 31, 9, 0, 0, 0, time.Local)

	var out bytes.Buffer
	if err := cmdExport(ctx, c, &out, nil, day); err != nil {
		t.Fatal(err)
	}
	if out.String() != "exported to scribble-notes-2026-10-31.zip\n" {
		t.Errorf("printed %q", out.String())
	}
	b, err := os.ReadFile(filepath.Join(dir, "scribble-notes-2026-10-31.zip"))
	if err != nil {
		t.Fatal(err)
	}
	list, err := notes.ReadZip(bytes.NewReader(b), int64(len(b)))
	if err != nil || len(list) != 1 || list[0].Body != "# Plans\nsoon" {
		t.Errorf("archive = %+v, %v", list, err)
	}

	named := filepath.Join(dir, "mine.zip")
	out.Reset()
	if err := cmdExport(ctx, c, &out, []string{named}, day); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(named); err != nil {
		t.Error(err)
	}
	if err := cmdExport(ctx, c, &out, []string{"a.zip", "b.zip"}, day); err == nil {
		t.Error("two paths should be refused")
	}
	// A failed export leaves no file behind, and an existing one as it was.
	bad := filepath.Join(dir, "bad.zip")
	c.BaseURL += "/nope"
	if err := cmdExport(ctx, c, &out, []string{bad}, day); err == nil {
		t.Error("want an error from a bad server")
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Errorf("failed export left %s: %v", bad, err)
	}
	if err := cmdExport(ctx, c, &out, []string{named}, day); err == nil {
		t.Error("want an error from a bad server")
	}
	if b, err := os.ReadFile(named); err != nil || len(b) == 0 {
		t.Errorf("failed export spoiled %s: %d bytes, %v", named, len(b), err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".scribble-export-*")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}

func TestCmdImportZip(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	if _, err := c.Create(ctx, "# Plans\nold"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := notes.WriteZip(f, []notes.Note{{Title: "Plans", Body: "# Plans\nnew"}, {Title: "Ideas", Body: "# Ideas\n"}}); err != nil {
		t.Fatal(err)
	}
	f.Close()
	tomboy := filepath.Join(dir, "plans.note")
	os.WriteFile(tomboy, []byte(`<note xmlns="http://beatniksoftware.com/tomboy"><title>Plans</title><text><note-content>Plans

tomboy</note-content></text></note>`), 0o644)

	var out bytes.Buffer
	if err := cmdImport(ctx, c, &out, []string{path}); err != nil {
		t.Fatal(err)
	}
	if want := "imported 1 note(s), skipped 1\n"; out.String() != want {
		t.Errorf("printed %q, want %q", out.String(), want)
	}
	if n, _ := c.ByTitle(ctx, "Plans"); n.Body != "# Plans\nold" {
		t.Errorf("skip mode changed Plans to %q", n.Body)
	}

	out.Reset()
	if err := cmdImport(ctx, c, &out, []string{"--overwrite", path}); err != nil {
		t.Fatal(err)
	}
	if want := "imported 1 note(s), skipped 1\n"; out.String() != want {
		t.Errorf("overwrite printed %q, want %q", out.String(), want)
	}
	if n, _ := c.ByTitle(ctx, "Plans"); n.Body != "# Plans\nnew" {
		t.Errorf("overwrite left Plans as %q", n.Body)
	}

	// --overwrite works for Tomboy files too, after the path.
	out.Reset()
	if err := cmdImport(ctx, c, &out, []string{tomboy, "--overwrite"}); err != nil {
		t.Fatal(err)
	}
	if want := "overwrote \"Plans\"\nimported 1 note(s), skipped 0\n"; out.String() != want {
		t.Errorf("tomboy overwrite printed %q, want %q", out.String(), want)
	}
	if n, _ := c.ByTitle(ctx, "Plans"); !strings.Contains(n.Body, "tomboy") {
		t.Errorf("Plans = %q", n.Body)
	}
	out.Reset()
	if err := cmdImport(ctx, c, &out, []string{"--overwrite", tomboy}); err != nil {
		t.Fatal(err)
	}
	if want := "skipped \"Plans\" (unchanged)\nimported 0 note(s), skipped 1\n"; out.String() != want {
		t.Errorf("unchanged overwrite printed %q, want %q", out.String(), want)
	}

	bad := filepath.Join(dir, "bad.zip")
	os.WriteFile(bad, []byte("nope"), 0o644)
	if err := cmdImport(ctx, c, &out, []string{bad}); err == nil || !strings.Contains(err.Error(), "bad.zip") {
		t.Errorf("bad archive err = %v", err)
	}
}
