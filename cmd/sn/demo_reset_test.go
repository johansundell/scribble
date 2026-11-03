package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/notes"
)

// The demo server's nightly reset (#161), against a real server, with a
// systemctl that only writes down what it was asked to do and this test
// binary as sn.
func TestDemoReset(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("the reset script runs on the Linux VPS")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	script, err := filepath.Abs("../../scripts/demo-reset.sh")
	if err != nil {
		t.Fatal(err)
	}
	notesDir, err := filepath.Abs("../../demo/notes")
	if err != nil {
		t.Fatal(err)
	}
	sn, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	url := newServerURL(t, "demo")
	dir := t.TempDir()
	for _, f := range []string{"scribble-server.db", "scribble-server.db-wal", "scribble-server.db-shm"} {
		writeFile(t, filepath.Join(dir, f), "visitors' notes")
	}
	bin := t.TempDir()
	calls := filepath.Join(t.TempDir(), "systemctl.log")
	writeFile(t, filepath.Join(bin, "systemctl"), "#!/bin/sh\necho \"$*\" >> '"+calls+"'\n")
	if err := os.Chmod(filepath.Join(bin, "systemctl"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SN_TEST_AS_SN=1",
		"DIR="+dir,
		"NOTES="+notesDir,
		"SN="+sn,
		"SCRIBBLE_URL="+url,
		"SCRIBBLE_TOKEN=", // empty: the script's default, demo
		"HEALTH_URL="+url+"/healthz",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("demo-reset.sh: %v\n%s", err, out)
	}

	// The service was stopped before its database went, and started again.
	got, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if want := "stop scribble-demo\nstart scribble-demo\n"; string(got) != want {
		t.Errorf("systemctl was called with %q, want %q", got, want)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "scribble-server.db*"))
	if len(left) != 0 {
		t.Errorf("database files left behind: %v", left)
	}

	// Every sample note is on the server, and they show what scribble does.
	ctx := context.Background()
	c := client.New(url, "demo")
	list, err := c.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(notesDir, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 || len(list) != len(files) {
		t.Fatalf("%d notes on the server, want one per file in demo/notes (%d)", len(list), len(files))
	}
	titles := map[string]bool{}
	for _, n := range list {
		titles[strings.ToLower(n.Title)] = true
	}
	var pinned, projects, code, links int
	for _, n := range list {
		if n.Pinned {
			pinned++
		}
		if len(notes.Projects(n.Body)) > 0 {
			projects++
		}
		if strings.Contains(n.Body, "\n```") {
			code++
		}
		for _, l := range notes.Links(n.Body) {
			links++
			if !titles[strings.ToLower(l)] {
				t.Errorf("%q links to [[%s]], which is not a demo note", n.Title, l)
			}
		}
	}
	if pinned != 1 || projects == 0 || code == 0 || links == 0 {
		t.Errorf("pinned %d (want 1), with #projects %d, with code blocks %d, [[links]] %d; want some of each",
			pinned, projects, code, links)
	}

	// The reset gave some notes a history.
	var history int
	for _, n := range list {
		vs, err := c.Versions(ctx, n.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(vs) > 1 {
			history++
		}
	}
	if history < 2 {
		t.Errorf("%d notes have more than one version, want at least 2", history)
	}
}
