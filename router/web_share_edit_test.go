package router_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/johansundell/scribble/notes"
)

// editToken is the note's edit link token in e's store, "" when it has none.
func (e *webEnv) editToken(t *testing.T, n *notes.Note) string {
	t.Helper()
	sh, err := e.store.NoteEditShare(context.Background(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	return sh.Token
}

// shareEdit gives n an edit link that never expires and returns its token.
func (e *webEnv) shareEdit(t *testing.T, n *notes.Note) string {
	t.Helper()
	tok, err := e.store.ShareNoteEdit(context.Background(), n.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// wantPublicHeaders checks the headers every share link page is sent with.
func wantPublicHeaders(t *testing.T, w interface{ Header() http.Header }) {
	t.Helper()
	for k, want := range map[string]string{"Referrer-Policy": "no-referrer", "X-Robots-Tag": "noindex", "Cache-Control": "no-store"} {
		if got := w.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestWebShareEdit_Pane(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha\nbody")

	// Two sections, each with its own expiry and button.
	w := e.do(get(sharePath(n)+"?project=work", c))
	wantBody(t, w, http.StatusOK,
		">View link<", ">Edit link<",
		`name="action" value="share"`, `name="action" value="share-edit"`,
		`id="share-ttl"`, `id="edit-ttl"`, ">Create edit link</button>",
		"Anyone with the edit link can change the note",
	)
	wantNot(t, w.Body.String(), "/e/", "<svg", "Revoke")

	// Making an edit link leaves the view link alone.
	wantRedirect(t, e.do(postForm(sharePath(n)+"?project=work", url.Values{"action": {"share-edit"}, "ttl": {"7d"}}, c)), sharePath(n)+"?project=work")
	tok := e.editToken(t, n)
	if len(tok) != 32 {
		t.Fatalf("edit token %q is not 128 bits of hex", tok)
	}
	if v := e.shareToken(t, n); v != "" {
		t.Fatalf("edit link made a view link %q", v)
	}
	sh, _ := e.store.NoteEditShare(context.Background(), n.ID)
	if sh.ExpiresAt == nil || time.Until(*sh.ExpiresAt) < 6*24*time.Hour {
		t.Errorf("edit link expires %v, want in 7 days", sh.ExpiresAt)
	}
	w = e.do(get(sharePath(n), c))
	wantBody(t, w, http.StatusOK,
		`value="http://example.com/e/`+tok+`"`, `data-copy="edit-url"`, `class="qr"`,
		`name="action" value="unshare-edit"`, ">Revoke edit link</button>",
		// The view section still offers a link of its own.
		">Create public link</button>",
		"Anyone with the edit link can change the note",
	)
	wantNot(t, w.Body.String(), `value="unshare"`)

	// Both shared: two links, two QR codes.
	e.do(postForm(sharePath(n), url.Values{"action": {"share"}}, c))
	w = e.do(get(sharePath(n), c))
	wantBody(t, w, http.StatusOK, "/s/"+e.shareToken(t, n), "/e/"+tok, `value="unshare"`, `value="unshare-edit"`)
	if got := strings.Count(w.Body.String(), `class="qr"`); got != 2 {
		t.Errorf("%d QR codes, want 2", got)
	}

	// Revoking the edit link leaves the view link.
	wantRedirect(t, e.do(postForm(sharePath(n), url.Values{"action": {"unshare-edit"}}, c)), sharePath(n))
	if e.editToken(t, n) != "" || e.shareToken(t, n) == "" {
		t.Errorf("after revoking the edit link: edit %q, view %q", e.editToken(t, n), e.shareToken(t, n))
	}
	if w := e.do(get("/e/" + tok)); w.Code != http.StatusNotFound {
		t.Errorf("revoked edit link = %d, want 404", w.Code)
	}
	// And the other way round.
	e.do(postForm(sharePath(n), url.Values{"action": {"share-edit"}}, c))
	e.do(postForm(sharePath(n), url.Values{"action": {"unshare"}}, c))
	if e.editToken(t, n) == "" || e.shareToken(t, n) != "" {
		t.Error("revoking the view link touched the edit link")
	}
	if w := e.do(postForm(sharePath(n), url.Values{"action": {"share-edit"}, "ttl": {"2y"}}, c)); w.Code != http.StatusBadRequest {
		t.Errorf("unknown ttl = %d, want 400", w.Code)
	}
}

func TestWebShareEdit_Page(t *testing.T) {
	e := newWebEnv(t)
	e.note(t, "# Other")
	n := e.note(t, "# Shared Note\nsee [[Other]]\n\n#work\n\n<script>alert(1)</script>")
	tok := e.shareEdit(t, n)

	w := e.do(get("/e/" + tok))
	wantBody(t, w, http.StatusOK,
		"<title>Shared Note – Scribble</title>", "<h1>Shared Note</h1>",
		"&lt;script&gt;alert(1)&lt;/script&gt;",
		`<a href="/login?next=/title/Other">Other</a>`,
		`href="/e/`+tok+`/edit"`, ">Edit<", `href="/e/`+tok+`/history"`, ">History<",
		"Never expires",
	)
	wantPublicHeaders(t, w)
	wantNot(t, w.Body.String(), "<script>alert", "note-list", "Log out", "Delete", "Rename", "Pin", "/note/")

	// The view token is no edit token, nor the other way round.
	view, _ := e.store.ShareNote(context.Background(), n.ID, nil)
	for _, bad := range []string{"/e/" + view, "/e/nope", "/e/" + strings.ToUpper(tok), "/e/" + tok + "x/edit", "/e/nope/history"} {
		w := e.do(get(bad))
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", bad, w.Code)
		}
		wantPublicHeaders(t, w)
	}
	if w := e.do(get("/s/" + tok)); w.Code != http.StatusNotFound {
		t.Errorf("edit token at /s/ = %d, want 404", w.Code)
	}

	// An expiry is shown; an expired link is a 404.
	soon := time.Now().Add(time.Hour)
	if _, err := e.store.ShareNoteEdit(context.Background(), n.ID, &soon); err != nil {
		t.Fatal(err)
	}
	wantBody(t, e.do(get("/e/"+tok)), http.StatusOK, "Expires on "+soon.UTC().Format("2006-01-02 15:04 UTC"))
	past := time.Now().Add(-time.Minute)
	e.store.ShareNoteEdit(context.Background(), n.ID, &past)
	for _, p := range []string{"", "/edit", "/history"} {
		if w := e.do(get("/e/" + tok + p)); w.Code != http.StatusNotFound {
			t.Errorf("expired GET /e/…%s = %d, want 404", p, w.Code)
		}
	}

	// A deleted note's link is dead.
	tok = e.shareEdit(t, n)
	if err := e.store.DeleteNote(context.Background(), n.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.do(get("/e/" + tok)); w.Code != http.StatusNotFound {
		t.Errorf("deleted note's edit link = %d, want 404", w.Code)
	}
}

func TestWebShareEdit_Editor(t *testing.T) {
	e := newWebEnv(t)
	n := e.note(t, "# Shared\nfirst")
	tok := e.shareEdit(t, n)
	path := "/e/" + tok + "/edit"

	w := e.do(get(path))
	wantBody(t, w, http.StatusOK,
		`class="editor"`, `action="`+path+`"`, `name="revision" value="1"`,
		"# Shared\nfirst</textarea>", "/assets/js/editor.js",
		`href="/e/`+tok+`"`, // Cancel
	)
	wantPublicHeaders(t, w)
	wantNot(t, w.Body.String(), "/rename", "/note/")

	// No session and no Origin needed: the token is the key.
	req := postForm(path, url.Values{"body": {"# Shared\nsecond"}, "revision": {"1"}, "action": {"save"}})
	req.Header.Del("Origin")
	wantRedirect(t, e.do(req), path)
	got := e.get(t, n.ID)
	if got.Body != "# Shared\nsecond" || got.Revision != 2 {
		t.Fatalf("after save: %+v", got)
	}
	// Done goes back to the link's page.
	wantRedirect(t, e.do(postForm(path, url.Values{"body": {"# Shared\nthird"}, "revision": {"2"}, "action": {"done"}})), "/e/"+tok)

	// The title can't change; the text is kept.
	w = e.do(postForm(path, url.Values{"body": {"# Renamed\nmine"}, "revision": {"3"}, "action": {"save"}}))
	wantBody(t, w, http.StatusBadRequest, "The title can&#39;t be changed through a share link", "# Renamed\nmine</textarea>")
	if e.get(t, n.ID).Title != "Shared" {
		t.Fatal("the title changed")
	}

	// A stale revision offers Reload and Overwrite.
	w = e.do(postForm(path, url.Values{"body": {"# Shared\nmine"}, "revision": {"1"}, "action": {"save"}}))
	wantBody(t, w, http.StatusPreconditionFailed, "changed elsewhere", `value="reload"`, `value="overwrite"`)
	w = e.do(postForm(path, url.Values{"body": {"# Shared\nmine"}, "revision": {"1"}, "action": {"reload"}}))
	wantBody(t, w, http.StatusOK, "# Shared\nthird</textarea>", `name="revision" value="3"`, "# Shared\nmine</textarea>")
	wantRedirect(t, e.do(postForm(path, url.Values{"body": {"# Shared\nmine"}, "revision": {"1"}, "action": {"overwrite"}})), "/e/"+tok)
	if got := e.get(t, n.ID); got.Body != "# Shared\nmine" {
		t.Errorf("after overwrite: %q", got.Body)
	}

	// Saves are kept as "Edited via link" in the owner's history.
	c := e.login(t)
	wantBody(t, e.do(get(historyPath(n), c)), http.StatusOK, ">Edited via link<")

	// Too large a body is refused.
	big := url.Values{"body": {"# Shared\n" + strings.Repeat("x", 5<<20)}, "revision": {"4"}, "action": {"save"}}
	if w := e.do(postForm(path, big)); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("huge save = %d, want 413", w.Code)
	}

	// A revoked link saves nothing.
	if err := e.store.UnshareNoteEdit(context.Background(), n.ID); err != nil {
		t.Fatal(err)
	}
	// It says so in the editor, which keeps the text.
	w = e.do(postForm(path, url.Values{"body": {"# Shared\nlate"}, "revision": {"4"}, "action": {"save"}}))
	wantBody(t, w, http.StatusNotFound, "This link no longer works", "# Shared\nlate</textarea>", `class="note-view"`)
	wantPublicHeaders(t, w)
	if e.get(t, n.ID).Body != "# Shared\nmine" {
		t.Error("a revoked link saved")
	}
}

func TestWebShareEdit_History(t *testing.T) {
	e := newWebEnv(t)
	ctx := context.Background()
	n := e.note(t, "# Shared\nsecret before sharing")
	if _, err := e.store.UpdateNote(ctx, n.ID, "# Shared\nwhat was shared", 0); err != nil {
		t.Fatal(err)
	}
	tok := e.shareEdit(t, n)
	base := "/e/" + tok + "/history"

	// Nothing changed since the link was made.
	w := e.do(get(base))
	wantBody(t, w, http.StatusOK, "No changes since this link was made")
	wantPublicHeaders(t, w)
	wantNot(t, w.Body.String(), "secret before sharing")

	up, err := e.store.UpdateEditSharedNote(ctx, tok, "# Shared\nfirst via link", 0)
	if err != nil {
		t.Fatal(err)
	}
	up2, err := e.store.UpdateEditSharedNote(ctx, tok, "# Shared\nsecond via link", 0)
	if err != nil {
		t.Fatal(err)
	}
	w = e.do(get(base))
	body := w.Body.String()
	wantBody(t, w, http.StatusOK,
		fmt.Sprintf(`href="%s?rev=%d"`, base, up2.Revision), fmt.Sprintf(`href="%s?rev=%d"`, base, up.Revision),
		">Edited via link<", ">Current version<", "first via link",
		`href="/e/`+tok+`"`,
	)
	wantNot(t, body, "secret before sharing", "what was shared", "Restore", `method="post"`, "/note/")
	if got := strings.Count(body, `<span class="rev-badge">`); got != 2 {
		t.Errorf("%d versions listed, want the 2 made since sharing", got)
	}

	// ?rev= picks one of those, and no other.
	wantBody(t, e.do(get(fmt.Sprintf("%s?rev=%d", base, up2.Revision))), http.StatusOK, "second via link")
	for _, rev := range []int64{1, n.Revision + 1, 99} {
		w := e.do(get(fmt.Sprintf("%s?rev=%d", base, rev)))
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "secret") {
			t.Errorf("rev %d = %d, want 404", rev, w.Code)
		}
	}
	if w := e.do(get(base + "?rev=x")); w.Code != http.StatusNotFound {
		t.Errorf("rev x = %d, want 404", w.Code)
	}

	// A new link starts a new window.
	e.store.UnshareNoteEdit(ctx, n.ID)
	tok = e.shareEdit(t, n)
	wantBody(t, e.do(get("/e/"+tok+"/history")), http.StatusOK, "No changes since this link was made")
}
