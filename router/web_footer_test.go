package router_test

import (
	"net/http"
	"testing"

	"github.com/johansundell/scribble/handlers"
)

// The footer links "Scribble" to the repo and carries the copyright (#206).
func TestWebFooter(t *testing.T) {
	const link = `<a class="footer-link" href="` + handlers.RepoURL + `" target="_blank" rel="noopener">Scribble</a>`
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Shared")
	if _, err := e.store.ShareNote(t.Context(), n.ID, nil); err != nil {
		t.Fatal(err)
	}
	share := "/s/" + e.shareToken(t, n)
	edit := "/e/" + e.shareEdit(t, n)

	// The app and the login page show the version.
	wantBody(t, e.do(get("/login")), http.StatusOK, link+" v9.8.7 · © Johan Sundell")
	wantBody(t, e.do(get("/", c)), http.StatusOK, link+" v9.8.7 · © Johan Sundell", "Export notes")

	// Share links don't tell strangers which release the server runs.
	for _, path := range []string{share, edit} {
		w := e.do(get(path))
		wantBody(t, w, http.StatusOK, "Shared from "+link+" · © Johan Sundell")
		wantNot(t, w.Body.String(), "v9.8.7")
	}
}
