package router_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/router"
)

// note creates a note in e's store.
func (e *webEnv) note(t *testing.T, body string) *notes.Note {
	t.Helper()
	n, err := e.store.CreateNote(context.Background(), body)
	if err != nil {
		t.Fatalf("CreateNote(%q): %v", body, err)
	}
	return n
}

func noteLink(n *notes.Note) string {
	return fmt.Sprintf(`href="/note/%d"`, n.ID)
}

func wantNot(t *testing.T, body string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if strings.Contains(body, p) {
			t.Errorf("body has %q:\n%s", p, body)
		}
	}
}

func TestWebNotes_RequireSession(t *testing.T) {
	e := newWebEnv(t)
	n := e.note(t, "# Alpha")
	wantRedirect(t, e.do(get(fmt.Sprintf("/note/%d", n.ID))), fmt.Sprintf("/login?next=/note/%d", n.ID))
	wantRedirect(t, e.do(get("/title/Alpha")), "/login?next=/title/Alpha")
}

func TestWebNotes_List(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)

	w := e.do(get("/", c))
	wantBody(t, w, http.StatusOK, "Select a note", "No notes yet")

	older := e.note(t, "# Older note\ntext")
	newer := e.note(t, "# Newer note\ntext")
	w = e.do(get("/", c))
	body := w.Body.String()
	wantBody(t, w, http.StatusOK, "Select a note", "Older note", "Newer note", noteLink(older), noteLink(newer))
	if strings.Index(body, "Newer note") > strings.Index(body, "Older note") {
		t.Error("the list is not newest first")
	}
	wantNot(t, body, `aria-current="page"`, "No notes yet")
}

func TestWebNotes_View(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	other := e.note(t, "# Other")
	n := e.note(t, strings.Join([]string{
		"# Shopping",
		"## Subheading",
		"- milk",
		"- eggs",
		"",
		"```",
		"code here",
		"```",
		"",
		"| a | b |",
		"|---|---|",
		"| 1 | 2 |",
		"",
		"- [x] done task",
		"- [ ] open task",
		"",
		"~~struck~~ and https://example.com and [site](https://example.org) and [mail](mailto:me@example.com)",
	}, "\n"))

	w := e.do(get(fmt.Sprintf("/note/%d", n.ID), c))
	wantBody(t, w, http.StatusOK,
		"<title>Shopping – Scribble</title>",
		"<h1>Shopping</h1>", "<h2>Subheading</h2>", "<li>milk</li>",
		"<pre><code>code here", "<table>", "<td>1</td>",
		`checked="" disabled="" type="checkbox"`, "done task",
		"<del>struck</del>",
		`<a href="https://example.com">https://example.com</a>`,
		`<a href="https://example.org">site</a>`,
		`<a href="mailto:me@example.com">mail</a>`,
		// The list is there too, with this note highlighted.
		fmt.Sprintf(`href="/note/%d" aria-current="page"`, n.ID), noteLink(other),
	)
	wantNot(t, w.Body.String(), "Select a note", "Linked from")
}

// On a phone the list is hidden behind a note, so the note bar has its own
// New, which keeps the filter (#125).
func TestWebNotes_NoteBarNew(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha\n#work nginx")

	w := e.do(get(fmt.Sprintf("/note/%d", n.ID), c))
	wantBody(t, w, http.StatusOK, `<a class="button phone-only" href="/new" data-filtered="note-new">New</a>`)

	w = e.do(get(fmt.Sprintf("/note/%d?project=work&q=nginx", n.ID), c))
	wantBody(t, w, http.StatusOK, `<a class="button phone-only" href="/new?project=work&amp;q=nginx" data-filtered="note-new">New</a>`)

	// Not on the list page, where no note is open.
	wantNot(t, e.do(get("/", c)).Body.String(), `data-filtered="note-new"`)
}

func TestWebNotes_EscapesHTML(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, strings.Join([]string{
		"# Dangerous",
		"",
		"<script>alert(1)</script>",
		"",
		"inline <img src=x onerror=alert(2)> here",
		"",
		"[click](javascript:alert(3)) [data](data:text/html,hi) [rel](/healthz)",
		"",
		"<javascript:alert(4)>",
		"",
		"![pic](javascript:alert(5))",
	}, "\n"))

	w := e.do(get(fmt.Sprintf("/note/%d", n.ID), c))
	body := w.Body.String()
	wantBody(t, w, http.StatusOK,
		"&lt;script&gt;alert(1)&lt;/script&gt;",
		"inline &lt;img src=x onerror=alert(2)&gt; here",
		// Dropped links keep their text.
		"<p>click data rel</p>", "<p>javascript:alert(4)</p>", "<p>pic</p>",
	)
	wantNot(t, body, "<script>alert", "<img", `href="javascript:`, "data:text", `href="/healthz"`, "alert(5)")
}

func TestWebNotes_Links(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	target := e.note(t, "# Target Note")
	slash := e.note(t, "# A/B")
	dots := e.note(t, "# ..")
	n := e.note(t, "# Source\nsee [[target note]], [[A/B]], [[..]] and [[Missing One]]\n\n`[[In code]]`")

	w := e.do(get(fmt.Sprintf("/note/%d", n.ID), c))
	body := w.Body.String()
	wantBody(t, w, http.StatusOK,
		`<a href="/title/target%20note">target note</a>`,
		`<a href="/title/A%2FB">A/B</a>`,
		`<a href="/title/%2E%2E">..</a>`,
		`<a href="/title/Missing%20One" class="missing">Missing One</a>`,
		"<code>[[In code]]</code>",
	)
	wantNot(t, body, "note:")

	// Following a link: the lookup ignores case.
	wantRedirect(t, e.do(get("/title/target%20note", c)), fmt.Sprintf("/note/%d", target.ID))
	wantRedirect(t, e.do(get("/title/TARGET%20NOTE", c)), fmt.Sprintf("/note/%d", target.ID))
	wantRedirect(t, e.do(get("/title/A%2FB", c)), fmt.Sprintf("/note/%d", slash.ID))
	wantRedirect(t, e.do(get("/title/%2E%2E", c)), fmt.Sprintf("/note/%d", dots.ID))
	// A missing note opens the new-note editor: TestWebEdit_MissingLinkPrefill.
}

func TestWebNotes_Backlinks(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	target := e.note(t, "# Target")
	from1 := e.note(t, "# First Source\nlinks to [[Target]]")
	from2 := e.note(t, "# Second Source\nalso [[target]]")
	e.note(t, "# Unrelated\nno links")

	w := e.do(get(fmt.Sprintf("/note/%d", target.ID), c))
	body := w.Body.String()
	wantBody(t, w, http.StatusOK, "Linked from", noteLink(from1), noteLink(from2))
	panel := body[strings.Index(body, "Linked from"):]
	for _, want := range []string{"First Source", "Second Source"} {
		if !strings.Contains(panel, want) {
			t.Errorf("Linked from lacks %q", want)
		}
	}
	if strings.Contains(panel, "Unrelated") {
		t.Error("Linked from lists a note that doesn't link here")
	}

	w = e.do(get(fmt.Sprintf("/note/%d", from1.ID), c))
	wantNot(t, w.Body.String(), "Linked from")
}

func TestWebNotes_NotFound(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	kept := e.note(t, "# Kept")
	gone := e.note(t, "# Gone")
	if err := e.store.DeleteNote(context.Background(), gone.ID); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/note/999", "/note/abc", "/note/0", fmt.Sprintf("/note/%d", gone.ID)} {
		w := e.do(get(path, c))
		wantBody(t, w, http.StatusNotFound, "Note not found", "Log out", noteLink(kept))
		if !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s: Content-Type = %q", path, w.Header().Get("Content-Type"))
		}
		wantNot(t, w.Body.String(), "Gone")
	}
}

// TestWeb_BesideAPI checks that the web app at the root leaves the JSON API
// and the health check as they were (#92): /notes is the API, /note the web
// app.
func TestWeb_BesideAPI(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha")

	api := func(path string) *http.Request {
		req := get(path)
		req.Header.Set("Authorization", "Bearer secret-token")
		req.Header.Set(router.ClientHeader, fmt.Sprintf("test/%d", router.APILevel))
		return req
	}
	for _, path := range []string{"/notes", fmt.Sprintf("/notes/%d", n.ID)} {
		w := e.do(api(path))
		wantBody(t, w, http.StatusOK, `"title":"Alpha"`)
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("%s: Content-Type = %q", path, ct)
		}
		// Without the token it is refused, not sent to the PIN screen.
		if w := e.do(get(path, c)); w.Code != http.StatusUnauthorized {
			t.Errorf("%s with only a session = %d, want 401", path, w.Code)
		}
	}

	wantBody(t, e.do(get(fmt.Sprintf("/note/%d", n.ID), c)), http.StatusOK, "<h1>Alpha</h1>")
	w := e.do(get("/healthz"))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "Log out") || !strings.Contains(w.Body.String(), "API level 4 · schema 5") {
		t.Errorf("/healthz = %d:\n%s", w.Code, w.Body.String())
	}
}
