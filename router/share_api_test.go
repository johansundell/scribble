package router_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

// shareAnswer is the JSON of the /notes/:id/share endpoints.
type shareAnswer struct {
	Token     string     `json:"token"`
	URL       string     `json:"url"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// apiDo sends an authenticated API request to e's router.
func (e *webEnv) apiDo(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return e.do(req)
}

func decodeShare(t *testing.T, w *httptest.ResponseRecorder, status int) shareAnswer {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d (body %q)", w.Code, status, w.Body.String())
	}
	var a shareAnswer
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatalf("bad JSON %q: %v", w.Body.String(), err)
	}
	return a
}

func TestShareAPI(t *testing.T) {
	e := newWebEnv(t)
	n := e.note(t, "# Alpha\nbody")
	path := fmt.Sprintf("/notes/%d/share", n.ID)

	// The token is required.
	if w := e.do(httptest.NewRequest("GET", path, nil)); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", w.Code)
	}

	// Private: an empty answer, not a 404, which means no such note.
	if a := decodeShare(t, e.apiDo("GET", path, ""), http.StatusOK); a.Token != "" || a.URL != "" {
		t.Fatalf("private note answers %+v", a)
	}

	// Sharing makes a link; sharing again keeps it.
	a := decodeShare(t, e.apiDo("POST", path, ""), http.StatusOK)
	if len(a.Token) != 32 || a.URL != "http://example.com/s/"+a.Token || a.ExpiresAt != nil {
		t.Fatalf("share answered %+v", a)
	}
	if tok := e.shareToken(t, n); tok != a.Token {
		t.Fatalf("store holds %q, answer %q", tok, a.Token)
	}
	if again := decodeShare(t, e.apiDo("POST", path, ""), http.StatusOK); again.Token != a.Token {
		t.Fatalf("sharing again changed the token: %q to %q", a.Token, again.Token)
	}
	if got := decodeShare(t, e.apiDo("GET", path, ""), http.StatusOK); got != a {
		t.Fatalf("GET answers %+v, want %+v", got, a)
	}
	// The link works.
	if w := e.do(get("/s/" + a.Token)); w.Code != http.StatusOK {
		t.Fatalf("share link = %d", w.Code)
	}

	// A ttl gives the link an expiry, as in the web app's share pane.
	exp := decodeShare(t, e.apiDo("POST", path, `{"ttl":"1d"}`), http.StatusOK)
	if exp.ExpiresAt == nil || time.Until(*exp.ExpiresAt) < 23*time.Hour {
		t.Fatalf("1d share answered %+v", exp)
	}
	if w := e.apiDo("POST", path, `{"ttl":"forever"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown ttl = %d, want 400", w.Code)
	}

	// Revoking: the link stops working and the note reads as private.
	if w := e.apiDo("DELETE", path, ""); w.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d, want 204 (%q)", w.Code, w.Body.String())
	}
	if w := e.do(get("/s/" + a.Token)); w.Code != http.StatusNotFound {
		t.Fatalf("revoked link = %d, want 404", w.Code)
	}
	if got := decodeShare(t, e.apiDo("GET", path, ""), http.StatusOK); got.Token != "" {
		t.Fatalf("revoked note answers %+v", got)
	}

	// No such note.
	for _, m := range []string{"GET", "POST", "DELETE"} {
		if w := e.apiDo(m, "/notes/999/share", ""); w.Code != http.StatusNotFound {
			t.Errorf("%s of a missing note = %d, want 404", m, w.Code)
		}
	}
}

// Without the web app a share link would answer 404, so the API refuses to
// make one. Reading and revoking still work.
func TestShareAPI_WebOff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := mustNewHandler(t, s, false, fstest.MapFS{}, "test", "dev")
	r, err := router.NewRouter(router.Config{
		Handler: h, LogSink: &recordingSink{}, Settings: types.AppSettings{AuthToken: "tok"},
		Assets: fstest.MapFS{}, Version: "dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	do := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer tok")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	req := httptest.NewRequest("POST", "/notes", strings.NewReader(`{"body":"# Alpha"}`))
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d", w.Code)
	}

	if w := do("POST", "/notes/1/share"); w.Code != http.StatusNotImplemented {
		t.Fatalf("share with the web app off = %d, want 501", w.Code)
	}
	if a := decodeShare(t, do("GET", "/notes/1/share"), http.StatusOK); a.Token != "" {
		t.Fatalf("GET answers %+v", a)
	}
	if w := do("DELETE", "/notes/1/share"); w.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d, want 204", w.Code)
	}
}
