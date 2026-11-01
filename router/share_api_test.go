package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/johansundell/scribble/notes"
)

// apiDo sends an authenticated API request to e's router.
func (e *webEnv) apiDo(method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	return e.do(req)
}

// decodeShare checks w's status and reads its share link.
func decodeShare(t *testing.T, w *httptest.ResponseRecorder, status int) notes.Share {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d (body %q)", w.Code, status, w.Body.String())
	}
	var sh notes.Share
	if err := json.Unmarshal(w.Body.Bytes(), &sh); err != nil {
		t.Fatalf("bad JSON %q: %v", w.Body.String(), err)
	}
	return sh
}

func apiSharePath(n *notes.Note) string { return fmt.Sprintf("/notes/%d/share", n.ID) }

func TestShareAPI(t *testing.T) {
	e := newWebEnv(t)
	n := e.note(t, "# Alpha\nbody")
	path := apiSharePath(n)

	// The token is required.
	if w := e.do(httptest.NewRequest("GET", path, nil)); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", w.Code)
	}

	// Private: an empty answer, not a 404, which means no such note.
	if sh := decodeShare(t, e.apiDo("GET", path), http.StatusOK); sh.Token != "" || sh.URL != "" {
		t.Fatalf("private note answers %+v", sh)
	}

	// Sharing makes a link that never expires; sharing again keeps it.
	sh := decodeShare(t, e.apiDo("POST", path), http.StatusOK)
	if len(sh.Token) != 32 || sh.URL != "http://example.com/s/"+sh.Token || sh.ExpiresAt != nil {
		t.Fatalf("share answered %+v", sh)
	}
	if tok := e.shareToken(t, n); tok != sh.Token {
		t.Fatalf("store holds %q, answer %q", tok, sh.Token)
	}
	if again := decodeShare(t, e.apiDo("POST", path), http.StatusOK); again.Token != sh.Token {
		t.Fatalf("sharing again changed the token: %q to %q", sh.Token, again.Token)
	}
	if got := decodeShare(t, e.apiDo("GET", path), http.StatusOK); got != sh {
		t.Fatalf("GET answers %+v, want %+v", got, sh)
	}
	if w := e.do(get("/s/" + sh.Token)); w.Code != http.StatusOK {
		t.Fatalf("share link = %d", w.Code)
	}

	// GET reports an expiry set in the web app's share pane.
	exp := time.Now().Add(time.Hour)
	if _, err := e.store.ShareNote(context.Background(), n.ID, &exp); err != nil {
		t.Fatal(err)
	}
	if got := decodeShare(t, e.apiDo("GET", path), http.StatusOK); got.ExpiresAt == nil || got.ExpiresAt.Sub(exp).Abs() > time.Second {
		t.Fatalf("GET answers expiry %v, want %v", got.ExpiresAt, exp)
	}

	// Revoking: the link stops working and the note reads as private.
	if w := e.apiDo("DELETE", path); w.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d, want 204 (%q)", w.Code, w.Body.String())
	}
	if w := e.do(get("/s/" + sh.Token)); w.Code != http.StatusNotFound {
		t.Fatalf("revoked link = %d, want 404", w.Code)
	}
	if got := decodeShare(t, e.apiDo("GET", path), http.StatusOK); got.Token != "" {
		t.Fatalf("revoked note answers %+v", got)
	}

	for _, m := range []string{"GET", "POST", "DELETE"} {
		if w := e.apiDo(m, "/notes/999/share"); w.Code != http.StatusNotFound {
			t.Errorf("%s of a missing note = %d, want 404", m, w.Code)
		}
	}
}

// Without the web app a share link answers 404, so the API makes none and
// shows none, even one made while it was on. Revoking still works.
func TestShareAPI_WebOff(t *testing.T) {
	on := newWebEnv(t)
	n := on.note(t, "# Alpha")
	if _, err := on.store.ShareNote(context.Background(), n.ID, nil); err != nil {
		t.Fatal(err)
	}
	off := &webEnv{r: newWebRouter(t, on.store, nil, &testLogger{}, nil), store: on.store}
	path := apiSharePath(n)

	if w := off.apiDo("POST", path); w.Code != http.StatusNotImplemented || !strings.Contains(w.Body.String(), "Not Implemented") {
		t.Fatalf("share with the web app off = %d %q, want 501", w.Code, w.Body.String())
	}
	if sh := decodeShare(t, off.apiDo("GET", path), http.StatusOK); sh.Token != "" || sh.URL != "" {
		t.Fatalf("GET shows a link the web app is not serving: %+v", sh)
	}
	if w := off.apiDo("DELETE", path); w.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d, want 204", w.Code)
	}
	if tok := on.shareToken(t, n); tok != "" {
		t.Fatalf("revoke left token %q", tok)
	}
	if w := off.apiDo("GET", "/notes/999/share"); w.Code != http.StatusNotFound {
		t.Errorf("GET of a missing note = %d, want 404", w.Code)
	}
}
