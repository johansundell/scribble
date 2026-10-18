package handlers

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
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

	expectedJSON := `{"dbStatus":"OK","name":"test-service","title":"Health Check","version":"v1.0"}`
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
