package router_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestWebPreview_RendersLikeTheNoteView(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	e.note(t, "# Target Note")
	body := strings.Join([]string{
		"# Draft",
		"see [[target note]] and [[Missing One]] in #work",
		"",
		"**bold**",
		"",
		"<script>alert(1)</script>",
		"",
		"[click](javascript:alert(2))",
	}, "\r\n")

	w := e.do(postForm("/preview", url.Values{"body": {body}}, c))
	wantBody(t, w, http.StatusOK,
		"<h1>Draft</h1>",
		`<a href="/title/target%20note">target note</a>`,
		`<a href="/title/Missing%20One" class="missing">Missing One</a>`,
		`href="/?project=work"`,
		"<strong>bold</strong>",
		"&lt;script&gt;alert(1)&lt;/script&gt;",
		"<p>click</p>",
	)
	got := w.Body.String()
	// A fragment for the Preview tab, not a page.
	wantNot(t, got, "<script>alert", `href="javascript:`, "<html", "<body", "note-list")
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if e.count(t) != 1 {
		t.Error("preview saved a note")
	}
}

func TestWebPreview_EditorHasToolbarAndTabs(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	w := e.do(get("/new", c))
	wantBody(t, w, http.StatusOK,
		`<script type="module" src="/assets/js/app.js">`,
		// Hidden until app.js shows it: without JavaScript, the plain textarea.
		`<div class="md-bar" data-md-bar hidden>`,
		`<markdown-toolbar for="note-text"`,
		`aria-label="Bold"`, `aria-label="Task list"`,
		`data-tab="write"`, `data-tab="preview"`,
		`id="note-preview"`,
	)
	// The PIN screen needs none of it.
	wantNot(t, e.do(get("/login")).Body.String(), "/assets/js/app.js")
	// The vendored files are served from here, not a CDN.
	for _, p := range []string{
		"/assets/js/app.js",
		"/assets/js/vendor/markdown-toolbar-element/markdown-toolbar-element.js",
		"/assets/js/vendor/paste-markdown/paste-markdown.js",
	} {
		if w := e.do(get(p)); w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", p, w.Code)
		}
	}
}

func TestWebPreview_RequireSession(t *testing.T) {
	e := newWebEnv(t)
	wantRedirect(t, e.do(postForm("/preview", url.Values{"body": {"# New"}})), "/login")
	if e.count(t) != 0 {
		t.Error("a preview without a session saved a note")
	}
}

func TestWebPreview_RefusesCrossOrigin(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	req := postForm("/preview", url.Values{"body": {"# Evil"}}, c)
	req.Header.Set("Origin", "https://evil.test")
	if w := e.do(req); w.Code != http.StatusForbidden {
		t.Errorf("cross-origin preview = %d, want 403", w.Code)
	}
}
