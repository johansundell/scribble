package router_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

// TestNotesAPI drives the whole notes API over HTTP against a real SQLite file.
func TestNotesAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := mustNewHandler(t, s, false, fstest.MapFS{}, "test", "dev")
	r, err := router.NewRouter(router.Config{
		Handler:  h,
		LogSink:  &recordingSink{},
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

	// Auth is required.
	req := httptest.NewRequest(http.MethodGet, "/notes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	wantStatus(w, http.StatusUnauthorized)

	// Create, duplicate, bad input.
	w = do("POST", "/notes", `{"body":"# Alpha\nfirst"}`)
	wantStatus(w, http.StatusCreated)
	var alpha notes.Note
	decode(w, &alpha)
	wantStatus(do("POST", "/notes", `{"body":"alpha"}`), http.StatusConflict)
	wantStatus(do("POST", "/notes", `{"body":""}`), http.StatusBadRequest)
	wantStatus(do("POST", "/notes", `{nope`), http.StatusBadRequest)
	wantStatus(do("GET", "/notes/abc", ""), http.StatusBadRequest)
	wantStatus(do("GET", "/notes/999", ""), http.StatusNotFound)

	// Lookup by title and by search.
	var list []notes.Note
	w = do("GET", "/notes?title=ALPHA", "")
	wantStatus(w, http.StatusOK)
	decode(w, &list)
	if len(list) != 1 || list[0].ID != alpha.ID {
		t.Fatalf("title lookup: %+v", list)
	}
	w = do("GET", "/notes?title=missing", "")
	wantStatus(w, http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("missing title should be [], got %q", w.Body.String())
	}

	// Optimistic concurrency: second PUT from the old revision gets 412.
	w = do("PUT", "/notes/1", `{"body":"# Alpha\nsecond","base_revision":1}`)
	wantStatus(w, http.StatusOK)
	wantStatus(do("PUT", "/notes/1", `{"body":"# Alpha\nthird","base_revision":1}`), http.StatusPreconditionFailed)

	// Backlinks + rename rewriting links.
	wantStatus(do("POST", "/notes", `{"body":"# Beta\nsee [[Alpha]]"}`), http.StatusCreated)
	w = do("GET", "/notes/1/backlinks", "")
	wantStatus(w, http.StatusOK)
	decode(w, &list)
	if len(list) != 1 || list[0].Title != "Beta" {
		t.Fatalf("backlinks: %+v", list)
	}
	wantStatus(do("POST", "/notes/1/rename", `{"title":"Omega"}`), http.StatusOK)
	var beta notes.Note
	decode(do("GET", "/notes/2", ""), &beta)
	if !strings.Contains(beta.Body, "[[Omega]]") {
		t.Errorf("link not rewritten: %q", beta.Body)
	}

	// Delete and sync.
	wantStatus(do("DELETE", "/notes/2", ""), http.StatusNoContent)
	wantStatus(do("DELETE", "/notes/2", ""), http.StatusNotFound)
	var ch struct {
		Notes     []notes.Note `json:"notes"`
		LatestSeq int64        `json:"latest_seq"`
	}
	w = do("GET", "/changes?since=0", "")
	wantStatus(w, http.StatusOK)
	decode(w, &ch)
	if len(ch.Notes) != 2 || ch.LatestSeq < 3 {
		t.Fatalf("changes: %+v", ch)
	}
	var sawTombstone bool
	for _, n := range ch.Notes {
		sawTombstone = sawTombstone || (n.ID == 2 && n.Deleted)
	}
	if !sawTombstone {
		t.Error("deleted note missing from changes")
	}
	decode(do("GET", "/changes?since="+itoa(ch.LatestSeq), ""), &ch)
	if len(ch.Notes) != 0 {
		t.Errorf("nothing new expected, got %d", len(ch.Notes))
	}
	wantStatus(do("GET", "/changes?since=x", ""), http.StatusBadRequest)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
