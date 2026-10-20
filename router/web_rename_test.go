package router_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/johansundell/scribble/notes"
)

func renamePath(n *notes.Note) string { return fmt.Sprintf("/note/%d/rename", n.ID) }
func deletePath(n *notes.Note) string { return fmt.Sprintf("/note/%d/delete", n.ID) }

func TestWebRename_RequireSessionAndOrigin(t *testing.T) {
	e := newWebEnv(t)
	n := e.note(t, "# Alpha")
	wantRedirect(t, e.do(get(renamePath(n))), fmt.Sprintf("/login?next=/note/%d/rename", n.ID))
	wantRedirect(t, e.do(get(deletePath(n))), fmt.Sprintf("/login?next=/note/%d/delete", n.ID))
	wantRedirect(t, e.do(postForm(renamePath(n), url.Values{"title": {"Beta"}})), "/login")
	wantRedirect(t, e.do(postForm(deletePath(n), nil)), "/login")

	c := e.login(t)
	for _, path := range []string{renamePath(n), deletePath(n)} {
		req := postForm(path, url.Values{"title": {"Beta"}}, c)
		req.Header.Set("Origin", "https://evil.test")
		if w := e.do(req); w.Code != http.StatusForbidden {
			t.Errorf("cross-origin POST %s = %d, want 403", path, w.Code)
		}
		req = postForm(path, url.Values{"title": {"Beta"}}, c)
		req.Header.Del("Origin")
		if w := e.do(req); w.Code != http.StatusForbidden {
			t.Errorf("POST %s with no Origin = %d, want 403", path, w.Code)
		}
	}
	if got := e.get(t, n.ID); got.Title != "Alpha" {
		t.Errorf("a refused post changed the note: %+v", got)
	}
}

func TestWebRename_Buttons(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha")

	w := e.do(get(fmt.Sprintf("/note/%d?project=work", n.ID), c))
	wantBody(t, w, http.StatusOK,
		fmt.Sprintf(`href="/note/%d/rename?project=work"`, n.ID), ">Rename<",
		fmt.Sprintf(`href="/note/%d/delete?project=work"`, n.ID), ">Delete<",
		`data-confirm="Delete &#34;Alpha&#34;? This cannot be undone."`, "/assets/js/note.js",
	)

	// The editor offers Rename while nothing is unsaved, and not after a
	// refused save, which leaves the text unsaved.
	wantBody(t, e.do(get(editPath(n), c)), http.StatusOK, fmt.Sprintf(`href="/note/%d/rename"`, n.ID), "data-rename")
	w = e.do(postForm(editPath(n), url.Values{"body": {""}, "revision": {"1"}, "action": {"save"}}, c))
	wantNot(t, w.Body.String(), "data-rename")
	// Nor in a new note's editor.
	wantNot(t, e.do(get("/new", c)).Body.String(), "data-rename", "/rename")
}

func TestWebRename_Rename(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	old := e.note(t, "# Old\nbody")
	linker := e.note(t, "# Linker\nsee [[Old]] and [[old]]")
	other := e.note(t, "# Other")

	w := e.do(get(renamePath(old)+"?project=work", c))
	wantBody(t, w, http.StatusOK,
		fmt.Sprintf(`action="/note/%d/rename?project=work"`, old.ID), `method="post"`,
		`name="title"`, `value="Old"`, ">Rename</button>",
		fmt.Sprintf(`href="/note/%d?project=work"`, old.ID), ">Cancel<",
	)
	if e.get(t, old.ID).Title != "Old" {
		t.Fatal("GET rename changed the note")
	}

	w = e.do(postForm(renamePath(old)+"?project=work", url.Values{"title": {"  New Name "}}, c))
	wantRedirect(t, w, fmt.Sprintf("/note/%d?project=work", old.ID))
	if got := e.get(t, old.ID); got.Title != "New Name" || got.Body != "# New Name\nbody" {
		t.Errorf("renamed note: %+v", got)
	}
	if got := e.get(t, linker.ID).Body; got != "# Linker\nsee [[New Name]] and [[New Name]]" {
		t.Errorf("links not rewritten: %q", got)
	}

	// Taken and empty titles keep the form, with the typed title.
	w = e.do(postForm(renamePath(old), url.Values{"title": {"other"}}, c))
	wantBody(t, w, http.StatusConflict, "A note titled other already exists.", `value="other"`, `name="title"`)
	for _, title := range []string{"", "   "} {
		w = e.do(postForm(renamePath(old), url.Values{"title": {title}}, c))
		wantBody(t, w, http.StatusBadRequest, "Type a title.", `name="title"`)
	}
	w = e.do(postForm(renamePath(old), url.Values{"title": {"a [[b]]"}}, c))
	wantBody(t, w, http.StatusBadRequest, "A title can&#39;t contain [ or ].", `value="a [[b]]"`)
	if got := e.get(t, old.ID).Title; got != "New Name" {
		t.Errorf("a refused rename changed the title to %q", got)
	}
	if e.get(t, other.ID).Title != "Other" {
		t.Error("the other note changed")
	}

	for _, path := range []string{"/note/999/rename", "/note/abc/rename"} {
		wantBody(t, e.do(get(path, c)), http.StatusNotFound, "Note not found")
		wantBody(t, e.do(postForm(path, url.Values{"title": {"X"}}, c)), http.StatusNotFound, "Note not found")
	}
}

func TestWebRename_Delete(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Doomed <b>\n#work")
	e.note(t, "# Kept\n#work")

	w := e.do(get(deletePath(n)+"?project=work", c))
	wantBody(t, w, http.StatusOK,
		"Delete &#34;Doomed &lt;b&gt;&#34;? This cannot be undone.",
		fmt.Sprintf(`action="/note/%d/delete?project=work"`, n.ID), `method="post"`, ">Delete</button>",
		fmt.Sprintf(`href="/note/%d?project=work"`, n.ID), ">Cancel<",
	)
	if _, err := e.store.GetNote(context.Background(), n.ID); err != nil {
		t.Fatal("GET delete deleted the note")
	}

	wantRedirect(t, e.do(postForm(deletePath(n)+"?project=work", nil, c)), "/?project=work")
	if _, err := e.store.GetNote(context.Background(), n.ID); err == nil {
		t.Fatal("not deleted")
	}
	w = e.do(get("/?project=work", c))
	wantListed(t, w.Body.String(), []string{"Kept"}, []string{"Doomed"})

	// Deleting it again, or an unknown note: 404 inside the layout.
	wantBody(t, e.do(postForm(deletePath(n), nil, c)), http.StatusNotFound, "Note not found", "Log out")
	wantBody(t, e.do(get(deletePath(n), c)), http.StatusNotFound, "Note not found")
	wantBody(t, e.do(get("/note/abc/delete", c)), http.StatusNotFound, "Note not found")
}
