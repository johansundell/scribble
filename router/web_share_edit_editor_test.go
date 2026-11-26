package router_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The edit link's editor gets the app's Markdown toolbar, less Link to
// note, the Preview tab and the Markdown help (#197).
func TestWebShareEdit_Toolbar(t *testing.T) {
	e := newWebEnv(t)
	n := e.note(t, "# Shared\nbody")
	tok := e.shareEdit(t, n)

	w := e.do(get("/e/" + tok + "/edit"))
	body := w.Body.String()
	wantBody(t, w, http.StatusOK,
		`<script type="module" src="/assets/js/app.js">`,
		"data-md-bar", "<markdown-toolbar", `data-tab="preview"`, "data-code-block",
		`data-preview="/e/`+tok+`/preview"`,
		"<summary>Markdown help</summary>", "**bold**", "- [ ] task", "[text](https://",
	)
	// Nothing that needs a session, and no help for what only works there.
	wantNot(t, body, "data-note-link", "Link to note", "[[Note title]]", "#project")
}

func TestWebEdit_MarkdownHelp(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha")
	for _, path := range []string{editPath(n), "/new"} {
		w := e.do(get(path, c))
		wantBody(t, w, http.StatusOK,
			"<summary>Markdown help</summary>", "**bold**", "[[Note title]]", "#project",
			"data-note-link",
		)
		// The app's own editor previews at /preview.
		wantBody(t, w, http.StatusOK, `data-preview="/preview"`)
	}
}

func TestWebShareEdit_Preview(t *testing.T) {
	e := newWebEnv(t)
	e.note(t, "# Other")
	n := e.note(t, "# Shared\nbody")
	tok := e.shareEdit(t, n)
	path := "/e/" + tok + "/preview"

	// No session and no Origin: the token is the key. Rendered as the public
	// page renders it: links go through the login, none marked missing.
	req := postForm(path, url.Values{"body": {"# Draft\r\nsee [[Other]] and [[Private Plans]] in #work\r\n\r\n**bold**\r\n\r\n<script>alert(1)</script>"}})
	req.Header.Del("Origin")
	w := e.do(req)
	wantBody(t, w, http.StatusOK,
		"<h1>Draft</h1>", "<strong>bold</strong>",
		`<a href="/login?next=/title/Other">Other</a>`,
		`<a href="/login?next=/title/Private%2520Plans">Private Plans</a>`,
		`<a href="/login?next=/%3Fproject%3Dwork">#work</a>`,
		"&lt;script&gt;",
	)
	wantNot(t, w.Body.String(), `class="missing"`, "<script>alert", "<html")
	wantPublicHeaders(t, w)
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
	// It saves nothing.
	if got := e.get(t, n.ID); got.Body != "# Shared\nbody" {
		t.Errorf("preview saved: %q", got.Body)
	}

	big := url.Values{"body": {"# Shared\n" + strings.Repeat("x", 5<<20)}}
	if w := e.do(postForm(path, big)); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("huge preview = %d, want 413", w.Code)
	}

	view, _ := e.store.ShareNote(context.Background(), n.ID, nil)
	for _, bad := range []string{"/e/nope/preview", "/e/" + view + "/preview"} {
		w := e.do(postForm(bad, url.Values{"body": {"# x"}}))
		if w.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", bad, w.Code)
		}
		wantPublicHeaders(t, w)
	}
	if err := e.store.UnshareNoteEdit(context.Background(), n.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.do(postForm(path, url.Values{"body": {"# x"}})); w.Code != http.StatusNotFound {
		t.Errorf("preview through a revoked link = %d, want 404", w.Code)
	}
}
