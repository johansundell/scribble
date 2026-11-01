package handlers

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/replica"
	"github.com/johansundell/scribble/store"
)

func TestHealthCheck(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	// Mock FS
	mockFS := fstest.MapFS{
		"tmpl/base.html":   {Data: []byte(`{{define "base"}}{{template "content" .}}{{end}}`)},
		"tmpl/health.html": {Data: []byte(`{{define "content"}}Database: {{.dbStatus}}{{end}}`)},
	}

	s, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("Failed to create db: %v", err)
	}
	defer s.Close()

	h := mustNewHandler(t, s, false, mockFS, "test-service", "v1.0")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	err = h.HealthCheck(c)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	expectedBody := "Database: OK"
	if w.Body.String() != expectedBody {
		t.Errorf("Expected body '%s', got '%s'", expectedBody, w.Body.String())
	}
}

func TestHealthCheckJSON(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	// Mock FS (not needed for JSON, but required for NewHandler)
	mockFS := fstest.MapFS{}

	s, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("Failed to create db: %v", err)
	}
	defer s.Close()

	h := mustNewHandler(t, s, false, mockFS, "test-service", "v1.0")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Set Accept header
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Accept", "application/json")

	err = h.HealthCheck(c)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	expectedJSON := `{"dbStatus":"OK","name":"test-service","schema":5,"title":"Health Check","version":"v1.0"}`
	if w.Body.String() != expectedJSON {
		t.Errorf("Expected body '%s', got '%s'", expectedJSON, w.Body.String())
	}
}

func TestHealthCheck_StorageDownReturns503(t *testing.T) {
	gin.SetMode(gin.TestMode)

	s, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("Failed to create db: %v", err)
	}
	s.Close() // Ping now fails

	mockFS := fstest.MapFS{
		"tmpl/base.html":   {Data: []byte(`{{define "base"}}{{template "content" .}}{{end}}`)},
		"tmpl/health.html": {Data: []byte(`{{define "content"}}Database: {{.dbStatus}}{{end}}`)},
	}
	h := mustNewHandler(t, s, false, mockFS, "test-service", "v1.0")

	for _, accept := range []string{"application/json", "text/html"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.Header.Set("Accept", accept)

		if err := h.HealthCheck(c); err != nil {
			t.Fatalf("%s: expected no error, got %v", accept, err)
		}
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: expected status 503, got %d", accept, w.Code)
		}
		if !strings.Contains(w.Body.String(), "closed") {
			t.Errorf("%s: expected the storage error in the body, got %q", accept, w.Body.String())
		}
	}
}

// noSchemaStore is a store without a schema version, like MySQL and FileMaker.
type noSchemaStore struct{ store.Store }

func TestHealthCheck_SchemaVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("Failed to create db: %v", err)
	}
	defer s.Close()
	down, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("Failed to create db: %v", err)
	}
	down.Close() // Ping now fails
	mockFS := fstest.MapFS{
		"tmpl/base.html":   {Data: []byte(`{{define "base"}}{{template "content" .}}{{end}}`)},
		"tmpl/health.html": {Data: []byte(`{{define "content"}}api={{.api}} schema={{.schema}}{{end}}`)},
	}

	for _, tc := range []struct {
		name               string
		store              store.Store
		wantJSON, wantHTML string
	}{
		{"sqlite", s, `"schema":5`, "api=2 schema=5"},
		{"no schema version", noSchemaStore{s}, "", "api=2 schema=<no value>"},
		// A failing database still reports its schema version.
		{"storage down", down, `"schema":5`, "api=2 schema=5"},
	} {
		h := mustNewHandler(t, tc.store, false, mockFS, "test-service", "v1.0")
		for _, accept := range []string{"application/json", "text/html"} {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/", nil)
			c.Request.Header.Set("Accept", accept)
			c.Set(APILevelKey, 2)
			if err := h.HealthCheck(c); err != nil {
				t.Fatalf("%s %s: expected no error, got %v", tc.name, accept, err)
			}
			body := w.Body.String()
			if accept == "text/html" {
				if body != tc.wantHTML {
					t.Errorf("%s: HTML %q, want %q", tc.name, body, tc.wantHTML)
				}
				continue
			}
			if tc.wantJSON == "" {
				if strings.Contains(body, `"schema"`) {
					t.Errorf("%s: JSON %s has a schema key", tc.name, body)
				}
			} else if !strings.Contains(body, tc.wantJSON) {
				t.Errorf("%s: JSON %s lacks %s", tc.name, body, tc.wantJSON)
			}
		}
	}
}

func mustNewHandler(t *testing.T, s store.Store, useFileSystem bool, embedded fs.FS, name, version string) *Handler {
	t.Helper()
	h, err := NewHandler(s, useFileSystem, embedded, name, version)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}
	return h
}

func TestNewHandler_RequiresTemplatesInEmbeddedMode(t *testing.T) {
	if _, err := NewHandler(nil, false, nil, "test", "dev"); err == nil {
		t.Error("expected an error for a nil templates filesystem in embedded mode")
	}
	if _, err := NewHandler(nil, true, nil, "test", "dev"); err != nil {
		t.Errorf("expected filesystem mode to work without an embedded FS, got %v", err)
	}
}

func TestHealthCheck_ContentNegotiation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("Failed to create db: %v", err)
	}
	defer s.Close()
	mockFS := fstest.MapFS{
		"tmpl/base.html":   {Data: []byte(`{{define "base"}}{{template "content" .}}{{end}}`)},
		"tmpl/health.html": {Data: []byte(`{{define "content"}}Database: {{.dbStatus}}{{end}}`)},
	}
	h := mustNewHandler(t, s, false, mockFS, "test-service", "v1.0")

	for accept, wantJSON := range map[string]bool{
		"application/json":                  true,
		"application/json, text/plain, */*": true,
		"":                                  false,
		"*/*":                               false,
		"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8": false,
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		if accept != "" {
			c.Request.Header.Set("Accept", accept)
		}
		if err := h.HealthCheck(c); err != nil {
			t.Fatalf("Accept %q: expected no error, got %v", accept, err)
		}
		isJSON := strings.HasPrefix(w.Body.String(), "{")
		if isJSON != wantJSON {
			t.Errorf("Accept %q: expected JSON=%v, got body %q", accept, wantJSON, w.Body.String())
		}
	}
}

type fakeReplica struct{ st replica.Status }

func (f fakeReplica) Status(context.Context) replica.Status { return f.st }

func TestHealthCheck_Replica(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("Failed to create db: %v", err)
	}
	defer s.Close()
	mockFS := fstest.MapFS{
		"tmpl/base.html":   {Data: []byte(`{{define "base"}}{{template "content" .}}{{end}}`)},
		"tmpl/health.html": {Data: []byte(`{{define "content"}}{{with .replica}}{{.Status}} {{.PendingPush}}{{end}}{{end}}`)},
	}
	get := func(h *Handler, accept string) (int, string) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.Header.Set("Accept", accept)
		if err := h.HealthCheck(c); err != nil {
			t.Fatalf("HealthCheck: %v", err)
		}
		return w.Code, w.Body.String()
	}

	h := mustNewHandler(t, s, false, mockFS, "test-service", "v1.0")
	if _, body := get(h, "application/json"); strings.Contains(body, `"replica"`) {
		t.Errorf("no replica configured, yet JSON %s has one", body)
	}

	// SQLite, the master, is fine, so FileMaker being down is still a 200.
	h, err = NewHandler(s, false, mockFS, "test-service", "v1.0",
		WithReplica(fakeReplica{replica.Status{Status: "unreachable", PendingPush: 3, LastSync: "2026-10-20T10:00:00Z"}}))
	if err != nil {
		t.Fatal(err)
	}
	code, body := get(h, "application/json")
	if code != http.StatusOK || !strings.Contains(body, `"replica":{"status":"unreachable","pending_push":3,"last_sync":"2026-10-20T10:00:00Z"}`) {
		t.Errorf("JSON %d %s", code, body)
	}
	if code, body := get(h, "text/html"); code != http.StatusOK || body != "unreachable 3" {
		t.Errorf("HTML %d %q", code, body)
	}
}
