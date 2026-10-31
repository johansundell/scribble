package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/johansundell/scribble/client"
)

// historyNote makes a note Plans with four versions: r1 create, r2 edit,
// r3 rename (from Draft) and r4 edit.
func historyNote(t *testing.T, c *client.Client) {
	t.Helper()
	ctx := context.Background()
	n, err := c.Create(ctx, "# Draft\none\ntwo\n")
	if err != nil {
		t.Fatal(err)
	}
	if n, err = c.Update(ctx, n.ID, "# Draft\none\n2\n", n.Revision); err != nil {
		t.Fatal(err)
	}
	if n, err = c.Rename(ctx, n.ID, "Plans"); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Update(ctx, n.ID, "# Plans\none\n2\nthree\n", n.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestCmdHistory(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	historyNote(t, c)
	n, _ := c.ByTitle(ctx, "Plans")
	vs, err := c.Versions(ctx, n.ID)
	if err != nil || len(vs) != 4 {
		t.Fatalf("versions = %v, %v", vs, err)
	}

	var out bytes.Buffer
	if err := cmdHistory(ctx, c, &out, []string{"plans"}); err != nil {
		t.Fatal(err)
	}
	at := func(i int) string { return vs[i].UpdatedAt.Local().Format(timeLayout) }
	want := fmt.Sprintf("r4  %s  edit  Plans  (current)\nr3  %s  rename  Plans\nr2  %s  edit  Draft\nr1  %s  create  Draft\n",
		at(0), at(1), at(2), at(3))
	if out.String() != want {
		t.Errorf("printed:\n%s\nwant:\n%s", out.String(), want)
	}

	if err := cmdHistory(ctx, c, &out, []string{"nope"}); err == nil || err.Error() != `no note titled "nope"` {
		t.Errorf("unknown note err = %v", err)
	}
}

func TestCmdShowRevision(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	historyNote(t, c)

	cases := map[string]struct {
		args []string
		want string
	}{
		"current":       {[]string{"Plans"}, "# Plans\none\n2\nthree\n"},
		"-r after":      {[]string{"Plans", "-r", "2"}, "# Draft\none\n2\n"},
		"-r r-prefixed": {[]string{"Plans", "-r", "r1"}, "# Draft\none\ntwo\n"},
		"-r before":     {[]string{"-r=3", "Plans"}, "# Plans\none\n2\n"},
	}
	for name, tc := range cases {
		var out bytes.Buffer
		if err := cmdShow(ctx, c, &out, tc.args); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out.String() != tc.want {
			t.Errorf("%s: printed %q, want %q", name, out.String(), tc.want)
		}
	}

	err := cmdShow(ctx, c, &bytes.Buffer{}, []string{"Plans", "-r", "12"})
	if want := `no revision r12 of "Plans" (see: sn history Plans)`; err == nil || err.Error() != want {
		t.Errorf("unknown revision err = %v, want %q", err, want)
	}
	if err := cmdShow(ctx, c, &bytes.Buffer{}, []string{"Plans", "-r", "x"}); err == nil {
		t.Error("a bad revision should be refused")
	}
	if err := cmdShow(ctx, c, &bytes.Buffer{}, []string{"Plans", "extra"}); err == nil {
		t.Error("two titles should be refused")
	}

	if _, err := c.Create(ctx, "# -todo\nx"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdShow(ctx, c, &out, []string{"-r", "1", "--", "-todo"}); err != nil || out.String() != "# -todo\nx\n" {
		t.Errorf("show after -- printed %q, %v", out.String(), err)
	}
}

func TestCmdDiff(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	historyNote(t, c)

	cases := map[string]struct {
		args []string
		want string
	}{
		"previous against current": {[]string{"Plans"},
			"--- Plans r3\n+++ Plans r4\n@@ -1,3 +1,4 @@\n # Plans\n one\n 2\n+three\n"},
		"one revision against current": {[]string{"Plans", "r2"},
			"--- Draft r2\n+++ Plans r4\n@@ -1,3 +1,4 @@\n-# Draft\n+# Plans\n one\n 2\n+three\n"},
		"two revisions": {[]string{"Plans", "1", "2"},
			"--- Draft r1\n+++ Draft r2\n@@ -1,3 +1,3 @@\n # Draft\n one\n-two\n+2\n"},
		"no difference": {[]string{"Plans", "4"}, ""},
	}
	for name, tc := range cases {
		var out bytes.Buffer
		if err := cmdDiff(ctx, c, &out, false, tc.args); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out.String() != tc.want {
			t.Errorf("%s: printed:\n%s\nwant:\n%s", name, out.String(), tc.want)
		}
	}

	var out bytes.Buffer
	if err := cmdDiff(ctx, c, &out, true, []string{"Plans"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b[32m+three\x1b[0m") {
		t.Errorf("colour diff = %q", out.String())
	}

	err := cmdDiff(ctx, c, &bytes.Buffer{}, false, []string{"Plans", "9"})
	if want := `no revision r9 of "Plans" (see: sn history Plans)`; err == nil || err.Error() != want {
		t.Errorf("unknown revision err = %v, want %q", err, want)
	}
	if err := cmdDiff(ctx, c, &bytes.Buffer{}, false, nil); err == nil {
		t.Error("a missing title should be refused")
	}

	if _, err := c.Create(ctx, "# Fresh\nx"); err != nil {
		t.Fatal(err)
	}
	if err := cmdDiff(ctx, c, &bytes.Buffer{}, false, []string{"Fresh"}); err == nil {
		t.Error("diff of a note with one version should say there is no earlier one")
	}
}

func TestDiffColour(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if diffColour() {
		t.Error("stdout under go test is not a terminal, so there should be no colour")
	}
	t.Setenv("NO_COLOR", "1")
	if diffColour() {
		t.Error("NO_COLOR should turn colour off")
	}
}

// editingReader answers the restore prompt with answer, after running edit,
// as if someone saved the note while the question was on screen.
type editingReader struct {
	edit   func()
	answer io.Reader
}

func (r *editingReader) Read(p []byte) (int, error) {
	if r.edit != nil {
		r.edit()
		r.edit = nil
	}
	return r.answer.Read(p)
}

func TestCmdRestore(t *testing.T) {
	ctx := context.Background()
	body := func(c *client.Client) string {
		n, err := c.ByTitle(ctx, "Plans")
		if err != nil {
			t.Fatal(err)
		}
		return n.Body
	}

	t.Run("asks on a terminal", func(t *testing.T) {
		c := newClient(t)
		historyNote(t, c)
		var out bytes.Buffer
		if err := cmdRestore(ctx, c, &out, strings.NewReader("y\n"), true, []string{"Plans", "3"}); err != nil {
			t.Fatal(err)
		}
		if want := "Restore \"Plans\" to r3? [y/N] restored \"Plans\" to r3\n"; out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
		if body(c) != "# Plans\none\n2\n" {
			t.Errorf("body = %q", body(c))
		}
	})

	t.Run("the question warns of a new title", func(t *testing.T) {
		c := newClient(t)
		historyNote(t, c)
		var out bytes.Buffer
		if err := cmdRestore(ctx, c, &out, strings.NewReader("y\n"), true, []string{"Plans", "1"}); err != nil {
			t.Fatal(err)
		}
		want := "Restore \"Plans\" to r1 (renames it to \"Draft\")? [y/N] restored \"Plans\" to r1 (now titled \"Draft\")\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("no on the terminal keeps the note", func(t *testing.T) {
		c := newClient(t)
		historyNote(t, c)
		var out bytes.Buffer
		if err := cmdRestore(ctx, c, &out, strings.NewReader("\n"), true, []string{"Plans", "3"}); err != nil {
			t.Fatal(err)
		}
		if want := "Restore \"Plans\" to r3? [y/N] not restored\n"; out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
		if body(c) != "# Plans\none\n2\nthree\n" {
			t.Errorf("body changed to %q", body(c))
		}
	})

	for name, tc := range map[string]struct {
		ask  bool
		args []string
	}{
		"-y skips the question": {true, []string{"-y", "Plans", "r1"}},
		"-y after the args":     {true, []string{"Plans", "r1", "-y"}},
		"piped stdin":           {false, []string{"Plans", "1"}},
	} {
		t.Run(name, func(t *testing.T) {
			c := newClient(t)
			historyNote(t, c)
			var out bytes.Buffer
			if err := cmdRestore(ctx, c, &out, strings.NewReader("n\n"), tc.ask, tc.args); err != nil {
				t.Fatal(err)
			}
			if want := "restored \"Plans\" to r1 (now titled \"Draft\")\n"; out.String() != want {
				t.Errorf("printed %q, want %q", out.String(), want)
			}
			// r1 was titled Draft, so the note has that title again.
			if n, err := c.ByTitle(ctx, "Draft"); err != nil || n.Body != "# Draft\none\ntwo\n" {
				t.Errorf("Draft = %v, %v", n, err)
			}
		})
	}

	t.Run("errors", func(t *testing.T) {
		c := newClient(t)
		historyNote(t, c)
		run := func(in io.Reader, args ...string) error {
			return cmdRestore(ctx, c, &bytes.Buffer{}, in, true, args)
		}
		if err := run(nil, "-y", "Plans", "12"); err == nil || err.Error() != `no revision r12 of "Plans" (see: sn history Plans)` {
			t.Errorf("unknown revision err = %v", err)
		}
		if _, err := c.Create(ctx, "# Draft\nanother"); err != nil {
			t.Fatal(err)
		}
		if err := run(nil, "-y", "Plans", "1"); err == nil || err.Error() != `a note titled "Draft" already exists` {
			t.Errorf("title clash err = %v", err)
		}
		edit := func() {
			n, _ := c.ByTitle(ctx, "Plans")
			if _, err := c.Update(ctx, n.ID, n.Body+"more\n", n.Revision); err != nil {
				t.Error(err)
			}
		}
		if err := run(&editingReader{edit, strings.NewReader("y\n")}, "Plans", "3"); err == nil || err.Error() != "the note was changed elsewhere since you started" {
			t.Errorf("stale err = %v", err)
		}
		if err := run(nil, "Plans"); err == nil {
			t.Error("a missing revision should be refused")
		}
		if err := run(nil, "-x", "Plans", "1"); err == nil || !strings.Contains(err.Error(), "-x") {
			t.Errorf("unknown flag err = %v, want it named", err)
		}
	})
}

func TestCmdDeletedAndUndelete(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	for _, body := range []string{"# Old\nfirst", "# Gone\nx"} {
		n, err := c.Create(ctx, body)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Delete(ctx, n.ID); err != nil {
			t.Fatal(err)
		}
	}
	// A second note titled Old, deleted later: undelete brings this one back.
	n, err := c.Create(ctx, "# Old\nsecond")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, n.ID); err != nil {
		t.Fatal(err)
	}

	list, err := c.Deleted(ctx)
	if err != nil || len(list) != 3 {
		t.Fatalf("deleted = %v, %v", list, err)
	}
	var out bytes.Buffer
	if err := cmdDeleted(ctx, c, &out, nil); err != nil {
		t.Fatal(err)
	}
	var want strings.Builder
	for _, d := range list {
		fmt.Fprintf(&want, "%s  %s\n", d.DeletedAt.Local().Format(timeLayout), d.Title)
	}
	if out.String() != want.String() || !strings.HasSuffix(strings.SplitN(out.String(), "\n", 2)[0], "  Old") {
		t.Errorf("printed:\n%s\nwant:\n%s", out.String(), want.String())
	}

	out.Reset()
	if err := cmdUndelete(ctx, c, &out, []string{"old"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "undeleted \"Old\"\n" {
		t.Errorf("printed %q", out.String())
	}
	if got, err := c.ByTitle(ctx, "Old"); err != nil || got.Body != "# Old\nsecond" {
		t.Errorf("Old = %v, %v", got, err)
	}

	if err := cmdUndelete(ctx, c, &out, []string{"Old"}); err == nil || err.Error() != `a note titled "Old" already exists` {
		t.Errorf("title clash err = %v", err)
	}
	if err := cmdUndelete(ctx, c, &out, []string{"Never"}); err == nil || err.Error() != `no deleted note titled "Never"` {
		t.Errorf("missing err = %v", err)
	}
	if err := cmdDeleted(ctx, c, &out, []string{"x"}); err == nil {
		t.Error("deleted takes no arguments")
	}
}

// TestHistoryCommandsAreWired checks that runWith reaches each new command.
func TestHistoryCommandsAreWired(t *testing.T) {
	serveVersion(t, "dev", 1)
	for _, args := range [][]string{
		{"history", "Nope"}, {"diff", "Nope"}, {"restore", "-y", "Nope", "1"}, {"undelete", "Nope"},
	} {
		if err := runWith(&bytes.Buffer{}, args); err == nil || !strings.Contains(err.Error(), `"Nope"`) {
			t.Errorf("%v: err = %v", args, err)
		}
	}
	var out bytes.Buffer
	if err := runWith(&out, []string{"deleted"}); err != nil || out.String() != "" {
		t.Errorf("deleted printed %q, %v", out.String(), err)
	}
	out.Reset()
	if err := runWith(&out, []string{"help"}); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"history", "show <title> [-r <rev>]", "diff", "restore", "deleted", "undelete"} {
		if !strings.Contains(out.String(), "sn "+cmd) {
			t.Errorf("help does not mention %q", cmd)
		}
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"Plans": "Plans", "École-2": "École-2", "Work log": "'Work log'",
		"$HOME": "'$HOME'", "it's": `'it'\''s'`, "": "''",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}
