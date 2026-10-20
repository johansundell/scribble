package router_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

// TestNoteHistoryAPI drives the history endpoints (#55) over HTTP against a
// real SQLite file.
func TestNoteHistoryAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sink := &recordingSink{}
	r, err := router.NewRouter(router.Config{
		Handler:  mustNewHandler(t, s, false, fstest.MapFS{}, "test", "dev"),
		LogSink:  sink,
		Settings: types.AppSettings{AuthToken: "tok"},
		Assets:   fstest.MapFS{},
		Version:  "dev",
	})
	if err != nil {
		t.Fatal(err)
	}

	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer tok")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	decode := func(w *httptest.ResponseRecorder, v any) {
		t.Helper()
		if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
			t.Fatalf("bad JSON %q: %v", w.Body.String(), err)
		}
	}
	wantStatus := func(w *httptest.ResponseRecorder, want int) {
		t.Helper()
		if w.Code != want {
			t.Fatalf("status %d, want %d (body %q)", w.Code, want, w.Body.String())
		}
	}

	// The token is required.
	for _, path := range []string{"/notes/1/versions", "/notes/deleted"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		wantStatus(w, http.StatusUnauthorized)
	}

	var alpha notes.Note
	decode(do("POST", "/notes", `{"body":"# Alpha\nfirst"}`), &alpha)
	wantStatus(do("PUT", "/notes/1", `{"body":"# Alpha\nsecond"}`), http.StatusOK)

	// The list leaves out bodies; one version has its body.
	w := do("GET", "/notes/1/versions", "")
	wantStatus(w, http.StatusOK)
	if strings.Contains(w.Body.String(), `"body"`) {
		t.Errorf("version list carries bodies: %s", w.Body.String())
	}
	var vs []notes.Version
	decode(w, &vs)
	if len(vs) != 2 || vs[0].Revision != 2 || vs[0].Reason != "edit" || vs[1].Reason != "create" {
		t.Fatalf("versions %+v", vs)
	}
	var v notes.Version
	w = do("GET", "/notes/1/versions/1", "")
	wantStatus(w, http.StatusOK)
	decode(w, &v)
	if v.Body != "# Alpha\nfirst" {
		t.Errorf("version 1 body %q", v.Body)
	}
	wantStatus(do("GET", "/notes/1/versions/9", ""), http.StatusNotFound)
	wantStatus(do("GET", "/notes/1/versions/x", ""), http.StatusBadRequest)
	wantStatus(do("GET", "/notes/9/versions", ""), http.StatusNotFound)

	// Restore: 412 from a stale base, then a new revision seen by /changes.
	wantStatus(do("POST", "/notes/1/versions/1/restore", `{"base_revision":1}`), http.StatusPreconditionFailed)
	w = do("POST", "/notes/1/versions/1/restore", `{"base_revision":2}`)
	wantStatus(w, http.StatusOK)
	var restored notes.Note
	decode(w, &restored)
	if restored.Body != "# Alpha\nfirst" || restored.Revision != 3 {
		t.Errorf("restored %+v", restored)
	}
	var ch struct {
		Notes []notes.Note `json:"notes"`
	}
	decode(do("GET", "/changes?since="+itoa(restored.Seq-1), ""), &ch)
	if len(ch.Notes) != 1 || ch.Notes[0].Revision != 3 {
		t.Errorf("restore missing from changes: %+v", ch)
	}
	// No body restores without the revision check.
	wantStatus(do("POST", "/notes/1/versions/2/restore", ""), http.StatusOK)
	wantStatus(do("POST", "/notes/1/versions/9/restore", ""), http.StatusNotFound)

	// Title clash on restore: rename, then take the old title.
	wantStatus(do("POST", "/notes/1/rename", `{"title":"Omega"}`), http.StatusOK)
	wantStatus(do("POST", "/notes", `{"body":"# Alpha\nother"}`), http.StatusCreated)
	wantStatus(do("POST", "/notes/1/versions/1/restore", `{}`), http.StatusConflict)

	// Delete, list deleted, refuse restore, undelete.
	wantStatus(do("DELETE", "/notes/1", ""), http.StatusNoContent)
	wantStatus(do("POST", "/notes/1/versions/1/restore", `{}`), http.StatusConflict)
	w = do("GET", "/notes/deleted", "")
	wantStatus(w, http.StatusOK)
	var del []notes.DeletedNote
	decode(w, &del)
	if len(del) != 1 || del[0].ID != 1 || del[0].Title != "Omega" || del[0].DeletedAt.IsZero() {
		t.Fatalf("deleted %+v", del)
	}
	wantStatus(do("POST", "/notes/2/undelete", ""), http.StatusNotFound)
	w = do("POST", "/notes/1/undelete", "")
	wantStatus(w, http.StatusOK)
	var back notes.Note
	decode(w, &back)
	if back.Deleted || back.Title != "Omega" {
		t.Errorf("undeleted %+v", back)
	}
	decode(do("GET", "/changes?since="+itoa(back.Seq-1), ""), &ch)
	if len(ch.Notes) != 1 || ch.Notes[0].ID != 1 || ch.Notes[0].Deleted {
		t.Errorf("undelete missing from changes: %+v", ch)
	}

	// Undelete with the title taken.
	wantStatus(do("DELETE", "/notes/1", ""), http.StatusNoContent)
	wantStatus(do("POST", "/notes", `{"body":"# omega\nnew"}`), http.StatusCreated)
	wantStatus(do("POST", "/notes/1/undelete", ""), http.StatusConflict)

	if len(sink.entries) != 0 {
		t.Errorf("history endpoints must not be request-logged, got %+v", sink.entries)
	}
}

// TestNoteHistoryAPIWithoutNotes: backends without notes answer 501.
func TestNoteHistoryAPIWithoutNotes(t *testing.T) {
	r := newLevelRouter(t, 0, 0, &testLogger{})
	for _, tc := range []struct{ method, path string }{
		{"GET", "/notes/1/versions"},
		{"GET", "/notes/1/versions/1"},
		{"POST", "/notes/1/versions/1/restore"},
		{"GET", "/notes/deleted"},
		{"POST", "/notes/1/undelete"},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Authorization", "Bearer tok")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotImplemented {
			t.Errorf("%s %s: status %d, want 501", tc.method, tc.path, w.Code)
		}
	}
}
