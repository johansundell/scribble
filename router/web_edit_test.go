package router_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/johansundell/scribble/notes"
)

// postForm posts form to path from the same origin, as the editor does.
func postForm(path string, form url.Values, cookies ...*http.Cookie) *http.Request {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return req
}

func editPath(n *notes.Note) string { return fmt.Sprintf("/app/notes/%d/edit", n.ID) }

func (e *webEnv) get(t *testing.T, id int64) *notes.Note {
	t.Helper()
	n, err := e.store.GetNote(context.Background(), id)
	if err != nil {
		t.Fatalf("GetNote(%d): %v", id, err)
	}
	return n
}

func (e *webEnv) count(t *testing.T) int {
	t.Helper()
	list, err := e.store.ListNotes(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	return len(list)
}

func TestWebEdit_RequireSession(t *testing.T) {
	e := newWebEnv(t)
	n := e.note(t, "# Alpha")
	wantRedirect(t, e.do(get("/app/new")), "/app/login?next=/app/new")
	wantRedirect(t, e.do(get(editPath(n))), fmt.Sprintf("/app/login?next=/app/notes/%d/edit", n.ID))
	// A post from an ended session goes to the plain login, and saves nothing.
	wantRedirect(t, e.do(postForm("/app/new", url.Values{"body": {"# New"}, "action": {"done"}})), "/app/login")
	wantRedirect(t, e.do(postForm(editPath(n), url.Values{"body": {"# Changed"}, "action": {"save"}})), "/app/login")
	if e.count(t) != 1 || e.get(t, n.ID).Body != "# Alpha" {
		t.Error("a post without a session changed the notes")
	}
}

func TestWebEdit_RefusesCrossOrigin(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	req := postForm("/app/new", url.Values{"body": {"# Evil"}, "action": {"done"}}, c)
	req.Header.Set("Origin", "https://evil.test")
	if w := e.do(req); w.Code != http.StatusForbidden {
		t.Errorf("cross-origin post = %d, want 403", w.Code)
	}
	if e.count(t) != 0 {
		t.Error("a cross-origin post created a note")
	}
}

func TestWebEdit_NewButtonsAndEditButton(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha")
	wantBody(t, e.do(get("/app", c)), http.StatusOK, `href="/app/new"`, ">New<")
	w := e.do(get(fmt.Sprintf("/app/notes/%d", n.ID), c))
	wantBody(t, w, http.StatusOK, `href="/app/new"`, fmt.Sprintf(`href="/app/notes/%d/edit"`, n.ID), ">Edit<")
}

func TestWebEdit_NewNote(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	e.note(t, "# Existing")

	w := e.do(get("/app/new", c))
	wantBody(t, w, http.StatusOK,
		`action="/app/new"`, `method="post"`, `<textarea`, `name="body"`, "autofocus",
		`value="save"`, `value="done"`, ">Cancel<", `href="/app"`,
		"Existing", "/assets/js/editor.js",
	)
	if strings.Contains(w.Body.String(), "Done</button>") == false {
		t.Error("no Done button")
	}

	// Done creates the note and shows it.
	w = e.do(postForm("/app/new", url.Values{"body": {"# Fresh\r\nline two"}, "action": {"done"}}, c))
	n, err := e.store.GetNoteByTitle(context.Background(), "Fresh")
	if err != nil {
		t.Fatalf("not created: %v", err)
	}
	wantRedirect(t, w, fmt.Sprintf("/app/notes/%d", n.ID))
	if n.Body != "# Fresh\nline two" {
		t.Errorf("body = %q, want CRLF turned into LF", n.Body)
	}

	// Save creates it and stays in its editor.
	w = e.do(postForm("/app/new", url.Values{"body": {"# Saved"}, "action": {"save"}}, c))
	s, err := e.store.GetNoteByTitle(context.Background(), "Saved")
	if err != nil {
		t.Fatalf("not created: %v", err)
	}
	wantRedirect(t, w, editPath(s))
}

func TestWebEdit_EditNote(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha\n<b>old</b> text")

	w := e.do(get(editPath(n), c))
	wantBody(t, w, http.StatusOK,
		fmt.Sprintf(`action="/app/notes/%d/edit"`, n.ID),
		"# Alpha\n&lt;b&gt;old&lt;/b&gt; text</textarea>",
		fmt.Sprintf(`name="revision" value="%d"`, n.Revision),
		fmt.Sprintf(`href="/app/notes/%d"`, n.ID), ">Cancel<",
		"<title>Alpha – Scribble</title>",
	)

	// Save keeps the editor open, with the new revision.
	w = e.do(postForm(editPath(n), url.Values{"body": {"# Alpha\nnew text"}, "revision": {"1"}, "action": {"save"}}, c))
	wantRedirect(t, w, editPath(n))
	got := e.get(t, n.ID)
	if got.Body != "# Alpha\nnew text" || got.Revision != 2 {
		t.Fatalf("after save: %+v", got)
	}
	wantBody(t, e.do(get(editPath(n), c)), http.StatusOK, `name="revision" value="2"`, "new text</textarea>")

	// Done saves and shows the note.
	w = e.do(postForm(editPath(n), url.Values{"body": {"# Alpha\ndone text"}, "revision": {"2"}, "action": {"done"}}, c))
	wantRedirect(t, w, fmt.Sprintf("/app/notes/%d", n.ID))
	if e.get(t, n.ID).Body != "# Alpha\ndone text" {
		t.Error("Done did not save")
	}
}

func TestWebEdit_UnknownNote(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	gone := e.note(t, "# Gone")
	if err := e.store.DeleteNote(context.Background(), gone.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/app/notes/999/edit", "/app/notes/abc/edit", editPath(gone)} {
		wantBody(t, e.do(get(path, c)), http.StatusNotFound, "Note not found", "Log out")
	}

	// Saving a note deleted elsewhere keeps the text.
	w := e.do(postForm(editPath(gone), url.Values{"body": {"# Gone\nmy words"}, "revision": {"1"}, "action": {"save"}}, c))
	wantBody(t, w, http.StatusNotFound, "This note was deleted elsewhere.", "my words</textarea>")
}

func TestWebEdit_ErrorsKeepText(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	e.note(t, "# Taken")
	n := e.note(t, "# Mine")

	w := e.do(postForm("/app/new", url.Values{"body": {"# taken\nmy draft"}, "action": {"done"}}, c))
	wantBody(t, w, http.StatusConflict, "A note titled taken already exists.", "# taken\nmy draft</textarea>", `action="/app/new"`, `data-dirty="true"`)

	w = e.do(postForm(editPath(n), url.Values{"body": {"# Taken\nedited"}, "revision": {"1"}, "action": {"save"}}, c))
	wantBody(t, w, http.StatusConflict, "A note titled Taken already exists.", "# Taken\nedited</textarea>", `name="revision" value="1"`)

	for _, body := range []string{"", "  \n\n #  \n"} {
		w = e.do(postForm("/app/new", url.Values{"body": {body}, "action": {"save"}}, c))
		wantBody(t, w, http.StatusBadRequest, "Type a title on the first line.")
		w = e.do(postForm(editPath(n), url.Values{"body": {body}, "revision": {"1"}, "action": {"save"}}, c))
		wantBody(t, w, http.StatusBadRequest, "Type a title on the first line.")
	}
	if e.count(t) != 2 || e.get(t, n.ID).Body != "# Mine" {
		t.Error("a refused save changed the notes")
	}
}

func TestWebEdit_ProjectPrefill(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)

	w := e.do(get("/app/new?project=Work", c))
	wantBody(t, w, http.StatusOK, "<textarea", "\n\n#work\n</textarea>", `name="tag" value="#work"`)

	// Not a project name: no prefill.
	for _, p := range []string{"42", "a b", "<x>"} {
		w = e.do(get("/app/new?project="+url.QueryEscape(p), c))
		wantNot(t, w.Body.String(), `name="tag"`, "\n#")
	}

	// Only the tag, no title above it: refused.
	for _, body := range []string{"\n\n#work\n", "#work", "#Work\nsome text"} {
		w = e.do(postForm("/app/new", url.Values{"body": {body}, "tag": {"#work"}, "action": {"done"}}, c))
		wantBody(t, w, http.StatusBadRequest, "Type a title on the first line, above the #tag.", `name="tag" value="#work"`)
	}
	if e.count(t) != 0 {
		t.Fatal("a tag-only note was created")
	}

	w = e.do(postForm("/app/new", url.Values{"body": {"Plan\n\n#work\n"}, "tag": {"#work"}, "action": {"done"}}, c))
	n, err := e.store.GetNoteByTitle(context.Background(), "Plan")
	if err != nil {
		t.Fatalf("not created: %v (%d %s)", err, w.Code, w.Body.String())
	}
	if !notes.HasProject(n.Body, "work") {
		t.Errorf("body %q is not in #work", n.Body)
	}
}

func TestWebEdit_MissingLinkPrefill(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)

	w := e.do(get("/app/title/Missing%20One", c))
	wantBody(t, w, http.StatusOK, `action="/app/new"`, "# Missing One\n</textarea>")
	wantNot(t, w.Body.String(), "No note titled")
	if e.count(t) != 0 {
		t.Fatal("following a missing link created a note")
	}
	w = e.do(get("/app/title/%3Cb%3Ex", c))
	wantBody(t, w, http.StatusOK, "# &lt;b&gt;x\n</textarea>")
	if e.count(t) != 0 {
		t.Fatal("following a missing link created a note")
	}
}

func TestWebEdit_Conflict(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Shared\nfirst")
	// Someone else saves revision 2.
	if _, err := e.store.UpdateNote(context.Background(), n.ID, "# Shared\ntheirs", 1); err != nil {
		t.Fatal(err)
	}
	mine := url.Values{"body": {"# Shared\nmine"}, "revision": {"1"}}
	with := func(action string) url.Values {
		f := url.Values{}
		for k, v := range mine {
			f[k] = v
		}
		f.Set("action", action)
		return f
	}

	// The stale save is refused, and the editor offers the three choices.
	w := e.do(postForm(editPath(n), with("save"), c))
	wantBody(t, w, http.StatusPreconditionFailed,
		"This note was changed elsewhere since you started editing.",
		"# Shared\nmine</textarea>", `name="revision" value="1"`,
		"Keep editing", `value="reload"`, `value="overwrite"`,
	)
	if e.get(t, n.ID).Body != "# Shared\ntheirs" {
		t.Fatal("the stale save went through")
	}

	// Keep editing: the old revision stays, so saving again still conflicts.
	w = e.do(postForm(editPath(n), with("done"), c))
	wantBody(t, w, http.StatusPreconditionFailed, "changed elsewhere")

	// Reload shows the latest version, with my text read-only below it.
	w = e.do(postForm(editPath(n), with("reload"), c))
	body := w.Body.String()
	// Leaving still asks: my text is not saved anywhere.
	wantBody(t, w, http.StatusOK, "# Shared\ntheirs</textarea>", `name="revision" value="2"`, "readonly", "# Shared\nmine</textarea>", `data-dirty="true"`)
	if strings.Index(body, "theirs</textarea>") > strings.Index(body, "mine</textarea>") {
		t.Error("my text is not below the latest version")
	}
	if e.get(t, n.ID).Revision != 2 {
		t.Error("Reload saved")
	}

	// Overwrite saves over their version and shows the note.
	w = e.do(postForm(editPath(n), with("overwrite"), c))
	wantRedirect(t, w, fmt.Sprintf("/app/notes/%d", n.ID))
	if got := e.get(t, n.ID); got.Body != "# Shared\nmine" || got.Revision != 3 {
		t.Errorf("after overwrite: %+v", got)
	}
}
