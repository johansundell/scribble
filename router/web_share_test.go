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

func notePath(n *notes.Note) string  { return fmt.Sprintf("/note/%d", n.ID) }
func sharePath(n *notes.Note) string { return fmt.Sprintf("/note/%d/share", n.ID) }

// shareToken is the note's token in e's store, "" when it isn't shared.
func (e *webEnv) shareToken(t *testing.T, n *notes.Note) string {
	t.Helper()
	tok, err := e.store.NoteShareToken(context.Background(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestWebShare_RequireSessionAndOrigin(t *testing.T) {
	e := newWebEnv(t)
	n := e.note(t, "# Alpha")
	wantRedirect(t, e.do(get(sharePath(n))), fmt.Sprintf("/login?next=/note/%d/share", n.ID))
	wantRedirect(t, e.do(postForm(sharePath(n), url.Values{"action": {"share"}})), "/login")

	c := e.login(t)
	req := postForm(sharePath(n), url.Values{"action": {"share"}}, c)
	req.Header.Set("Origin", "https://evil.test")
	if w := e.do(req); w.Code != http.StatusForbidden {
		t.Errorf("cross-origin share = %d, want 403", w.Code)
	}
	if tok := e.shareToken(t, n); tok != "" {
		t.Errorf("a refused post shared the note: %q", tok)
	}
}

func TestWebShare_Pane(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha\nbody")

	// The note page links to the share pane, keeping the filter.
	wantBody(t, e.do(get(notePath(n)+"?project=work", c)), http.StatusOK,
		fmt.Sprintf(`href="/note/%d/share?project=work"`, n.ID), ">Share<")

	// Not shared yet: only the way to share it.
	w := e.do(get(sharePath(n)+"?project=work", c))
	wantBody(t, w, http.StatusOK,
		"<title>Share Alpha – Scribble</title>",
		fmt.Sprintf(`action="/note/%d/share?project=work"`, n.ID), `method="post"`,
		`name="action" value="share"`, ">Create public link</button>",
		fmt.Sprintf(`href="/note/%d?project=work"`, n.ID),
	)
	wantNot(t, w.Body.String(), "/s/", "<svg", "Revoke")
	if tok := e.shareToken(t, n); tok != "" {
		t.Fatalf("GET share shared the note: %q", tok)
	}

	// Sharing goes back to the pane, which now shows the link.
	wantRedirect(t, e.do(postForm(sharePath(n)+"?project=work", url.Values{"action": {"share"}}, c)), sharePath(n)+"?project=work")
	tok := e.shareToken(t, n)
	if len(tok) != 32 {
		t.Fatalf("token %q is not 128 bits of hex", tok)
	}
	link := "http://example.com/s/" + tok
	w = e.do(get(sharePath(n), c))
	wantBody(t, w, http.StatusOK,
		`value="`+link+`"`, "data-copy", ">Copy link</button>",
		`<svg`, `class="qr"`, `name="action" value="unshare"`, ">Revoke link</button>",
		"/assets/js/share.js",
	)
	// The QR code is drawn here, never fetched from anywhere.
	svg := w.Body.String()[strings.Index(w.Body.String(), "<svg"):]
	svg = svg[:strings.Index(svg, "</svg>")]
	wantNot(t, svg, "http", "<image", "<script")

	// Sharing again keeps the link.
	e.do(postForm(sharePath(n), url.Values{"action": {"share"}}, c))
	if again := e.shareToken(t, n); again != tok {
		t.Errorf("sharing again changed the token: %q, want %q", again, tok)
	}

	// Behind a TLS proxy the link is https.
	req := get(sharePath(n), c)
	req.Header.Set("X-Forwarded-Proto", "https")
	wantBody(t, e.do(req), http.StatusOK, `value="https://example.com/s/`+tok+`"`)

	// Revoking clears the token and the old link stops working.
	wantRedirect(t, e.do(postForm(sharePath(n), url.Values{"action": {"unshare"}}, c)), sharePath(n))
	if got := e.shareToken(t, n); got != "" {
		t.Errorf("token %q left after revoking", got)
	}
	if w := e.do(get("/s/" + tok)); w.Code != http.StatusNotFound {
		t.Errorf("revoked link = %d, want 404", w.Code)
	}
}

func TestWebShare_NotFound(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	wantBody(t, e.do(get("/note/999/share", c)), http.StatusNotFound, "Note not found")
	wantBody(t, e.do(postForm("/note/999/share", url.Values{"action": {"share"}}, c)), http.StatusNotFound, "Note not found")
	n := e.note(t, "# Alpha")
	if w := e.do(postForm(sharePath(n), url.Values{"action": {"bogus"}}, c)); w.Code != http.StatusBadRequest {
		t.Errorf("unknown action = %d, want 400", w.Code)
	}
}

func TestWebShare_PublicPage(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	e.note(t, "# Other")
	n := e.note(t, "# Shared Note\nsee [[Other]] and [[Private Plans]] and [site](https://example.org)\n\n#work\n\n<script>alert(1)</script>")
	e.do(postForm(sharePath(n), url.Values{"action": {"share"}}, c))
	tok := e.shareToken(t, n)

	// No cookie: anyone with the link can read it.
	w := e.do(get("/s/" + tok))
	body := w.Body.String()
	wantBody(t, w, http.StatusOK,
		"<title>Shared Note – Scribble</title>",
		"<h1>Shared Note</h1>", `<a href="https://example.org">site</a>`,
		"&lt;script&gt;alert(1)&lt;/script&gt;",
		"Updated 2",
		// [[links]] and #project tags go through the PIN screen, and a link
		// to a note that doesn't exist looks like any other.
		`<a href="/login?next=/title/Other">Other</a>`,
		`<a href="/login?next=/title/Private%2520Plans">Private Plans</a>`,
		`<a href="/login?next=/%3Fproject%3Dwork">#work</a>`,
	)
	wantNot(t, body, "<script>alert", `class="missing"`, "note-list", "/new", "Log out", "Edit", "Delete", "Search", "app.js", "manifest")
	// The token must not leak to sites the note links to.
	if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	for _, bad := range []string{"/s/nope", "/s/" + strings.ToUpper(tok), "/s/"} {
		if w := e.do(get(bad)); w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", bad, w.Code)
		}
	}

	// A deleted note's link is dead too.
	if err := e.store.DeleteNote(context.Background(), n.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.do(get("/s/" + tok)); w.Code != http.StatusNotFound {
		t.Errorf("deleted note's link = %d, want 404", w.Code)
	}
}
