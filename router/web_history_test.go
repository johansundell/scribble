package router_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/johansundell/scribble/notes"
)

func historyPath(n *notes.Note) string { return fmt.Sprintf("/note/%d/history", n.ID) }

// historyNote is a note with four kept versions: r1 created, r2 edited,
// r3 renamed and r4 edited. Saves within the merge window only merge into
// an edit, so the rename keeps the two edits apart.
func (e *webEnv) historyNote(t *testing.T) *notes.Note {
	t.Helper()
	ctx := context.Background()
	n := e.note(t, "# Alpha\nfirst text")
	if _, err := e.store.UpdateNote(ctx, n.ID, "# Alpha\nsecond text with [[Other]]", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.RenameNote(ctx, n.ID, "Beta"); err != nil {
		t.Fatal(err)
	}
	n, err := e.store.UpdateNote(ctx, n.ID, "# Beta\nlatest text", 0)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestWebHistory_RequireSessionAndOrigin(t *testing.T) {
	e := newWebEnv(t)
	n := e.historyNote(t)
	wantRedirect(t, e.do(get(historyPath(n))), fmt.Sprintf("/login?next=/note/%d/history", n.ID))
	wantRedirect(t, e.do(postForm(historyPath(n), url.Values{"action": {"restore"}, "revision": {"1"}})), "/login")

	c := e.login(t)
	req := postForm(historyPath(n), url.Values{"action": {"restore"}, "revision": {"1"}}, c)
	req.Header.Set("Origin", "https://evil.test")
	if w := e.do(req); w.Code != http.StatusForbidden {
		t.Errorf("cross-origin restore = %d, want 403", w.Code)
	}
	if got := e.get(t, n.ID); got.Revision != n.Revision {
		t.Errorf("a refused post restored the note: revision %d, want %d", got.Revision, n.Revision)
	}
}

func TestWebHistory_NotePageShowsRevision(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.historyNote(t)
	w := e.do(get(notePath(n)+"?project=work", c))
	wantBody(t, w, http.StatusOK,
		// The action bar, wide and in the ⋯ menu, keeping the filter.
		fmt.Sprintf(`href="/note/%d/history?project=work" data-filtered="history">History (r4)</a>`, n.ID),
		fmt.Sprintf(`href="/note/%d/history?project=work" data-filtered="history-more">History (r4)</a>`, n.ID),
		// The header: three changes kept since the note was created.
		"Revision 4 (3 edits) · Updated <time",
	)

	// A new note has one version and no edits yet.
	fresh := e.note(t, "# Fresh")
	wantBody(t, e.do(get(notePath(fresh), c)), http.StatusOK, "History (r1)", "Revision 1 (0 edits)")
	// One edit is singular.
	if _, err := e.store.UpdateNote(context.Background(), fresh.ID, "# Fresh\nmore", 0); err != nil {
		t.Fatal(err)
	}
	wantBody(t, e.do(get(notePath(fresh), c)), http.StatusOK, "Revision 2 (1 edit)")
}

func TestWebHistory_Page(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	e.note(t, "# Other")
	n := e.historyNote(t)

	// Without ?rev= the previous version shows, so a change is visible.
	w := e.do(get(historyPath(n)+"?project=work", c))
	body := w.Body.String()
	wantBody(t, w, http.StatusOK,
		"<title>History of Beta – Scribble</title>",
		// Newest first, each with its event and a link keeping the filter.
		`class="history-list"`,
		fmt.Sprintf(`href="/note/%d/history?project=work&amp;rev=4"`, n.ID),
		fmt.Sprintf(`href="/note/%d/history?project=work&amp;rev=1"`, n.ID),
		">Created<", ">Edited<", ">Renamed<", ">Current version<",
		// The banner, with the way back and the restore form.
		"Viewing revision r3 from <time",
		fmt.Sprintf(`href="/note/%d?project=work"`, n.ID), "← Back to note",
		fmt.Sprintf(`action="/note/%d/history?project=work"`, n.ID), `method="post"`,
		`name="action" value="restore"`, `name="revision" value="3"`,
		`name="base_revision" value="4"`,
		`data-confirm="Restore revision r3 of &#34;Beta&#34;?"`, ">Restore this version</button>",
		// The version's text as Markdown, with [[links]] to notes.
		`<article class="note-body">`, "second text with", `<a href="/title/Other">Other</a>`,
		"/assets/js/note.js",
	)
	if strings.Index(body, "rev=4") > strings.Index(body, "rev=1") {
		t.Error("versions are not newest first")
	}
	// Only the shown version is marked.
	if got := strings.Count(body, `aria-current="true"`); got != 1 {
		t.Errorf("%d versions marked as shown, want 1", got)
	}
	wantBody(t, w, http.StatusOK, fmt.Sprintf(`href="/note/%d/history?project=work&amp;rev=3" aria-current="true"`, n.ID))
	wantNot(t, body, "latest text")

	// ?rev= picks a version.
	w = e.do(get(historyPath(n)+"?rev=1", c))
	wantBody(t, w, http.StatusOK, "Viewing revision r1 from", "first text", `name="revision" value="1"`)
	wantNot(t, w.Body.String(), "second text")

	// The current version has no restore button.
	w = e.do(get(historyPath(n)+"?rev=4", c))
	wantBody(t, w, http.StatusOK, "Viewing revision r4 from", "latest text", ">Current version<")
	wantNot(t, w.Body.String(), "Restore this version", `value="restore"`)

	// A note with one version shows that one.
	fresh := e.note(t, "# Fresh\nonly text")
	w = e.do(get(historyPath(fresh), c))
	wantBody(t, w, http.StatusOK, "Viewing revision r1 from", "only text", ">Created<")
	wantNot(t, w.Body.String(), "Restore this version")
}

func TestWebHistory_Restore(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.historyNote(t)

	form := url.Values{"action": {"restore"}, "revision": {"1"}, "base_revision": {"4"}}
	wantRedirect(t, e.do(postForm(historyPath(n)+"?project=work", form, c)), notePath(n)+"?project=work")
	got := e.get(t, n.ID)
	if got.Body != "# Alpha\nfirst text" || got.Revision != 5 {
		t.Errorf("after restore: revision %d, body %q", got.Revision, got.Body)
	}
	wantBody(t, e.do(get(historyPath(n)+"?rev=5", c)), http.StatusOK, ">Restored<")

	// A restore from a page older than the note is refused.
	w := e.do(postForm(historyPath(n), form, c))
	wantBody(t, w, http.StatusConflict, "The note has changed since this page was loaded.")
	if again := e.get(t, n.ID); again.Revision != 5 {
		t.Errorf("a stale restore wrote revision %d", again.Revision)
	}
}

func TestWebHistory_EventLabels(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	target := e.note(t, "# Target")
	linker := e.note(t, "# Linker\nsee [[Target]]")
	// Renaming Target rewrites the link in Linker.
	if _, err := e.store.RenameNote(context.Background(), target.ID, "Moved"); err != nil {
		t.Fatal(err)
	}
	wantBody(t, e.do(get(historyPath(linker), c)), http.StatusOK, ">Links updated<")
}

func TestWebHistory_NotFound(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	wantBody(t, e.do(get("/note/999/history", c)), http.StatusNotFound, "Note not found")
	wantBody(t, e.do(postForm("/note/999/history", url.Values{"action": {"restore"}, "revision": {"1"}}, c)), http.StatusNotFound, "Note not found")

	n := e.historyNote(t)
	for _, rev := range []string{"99", "0", "x"} {
		wantBody(t, e.do(get(historyPath(n)+"?rev="+rev, c)), http.StatusNotFound, "That version of the note was not kept.")
	}
	wantBody(t, e.do(postForm(historyPath(n), url.Values{"action": {"restore"}, "revision": {"99"}}, c)), http.StatusNotFound, "That version of the note was not kept.")
	if w := e.do(postForm(historyPath(n), url.Values{"action": {"bogus"}, "revision": {"1"}}, c)); w.Code != http.StatusBadRequest {
		t.Errorf("unknown action = %d, want 400", w.Code)
	}
}
