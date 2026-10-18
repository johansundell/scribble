package router_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

func TestAuthCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	settings := types.AppSettings{
		AuthToken: "secret-token",
	}

	tmpFile := filepath.Join(t.TempDir(), "test_router_auth.db")

	s, err := store.NewSQLite(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer s.Close()

	h := mustNewHandler(t, s, false, fstest.MapFS{}, "test", "dev")

	r, err := router.NewRouter(router.Config{
		Handler:  h,
		LogSink:  &recordingSink{},
		Settings: settings,
		Assets:   fstest.MapFS{},
		Version:  "dev",
	})
	if err != nil {
		t.Fatalf("NewRouter failed: %v", err)
	}

	tests := []struct {
		name       string
		authHeader string
		body       string
		wantStatus int
	}{
		{
			name:       "Missing Auth Header",
			authHeader: "",
			body:       `{"test":"data"}`,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "Invalid Auth Header",
			authHeader: "wrong-token",
			body:       `{"test":"data"}`,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "Valid Auth Header",
			authHeader: "secret-token",
			body:       `{"test":"data"}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "Valid Bearer Auth Header",
			authHeader: "Bearer secret-token",
			body:       `{"test":"data"}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "Valid Auth With Invalid JSON",
			authHeader: "secret-token",
			body:       `{invalid`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/pong", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			r.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("Expected status %d, got %d", tc.wantStatus, w.Code)
			}
		})
	}
}

func TestStartupAuthValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// AuthToken is empty, but default routes contain protected routes (Pong, GetLogs)
	settings := types.AppSettings{
		AuthToken: "",
	}

	mockStore := nopStore{}
	h := mustNewHandler(t, mockStore, false, fstest.MapFS{}, "test", "dev")

	_, err := router.NewRouter(router.Config{
		Handler:  h,
		LogSink:  &recordingSink{},
		Settings: settings,
	})
	if err == nil {
		t.Fatalf("Expected NewRouter to fail when protected routes exist without AuthToken, got nil")
	}
	if !strings.Contains(err.Error(), "AUTH_TOKEN must be configured") {
		t.Fatalf("Expected AUTH_TOKEN error, got: %v", err)
	}
}

func TestAuthMiddleware_FailClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Standalone AuthMiddleware with empty token must return 500 and not panic
	mw := router.AuthMiddleware("", nil)
	handler := mw(func(c *gin.Context) error {
		return nil
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/protected", nil)

	err := handler(c)
	if err == nil {
		t.Fatalf("Expected error from AuthMiddleware with empty token, got nil")
	}

	// Verify WrapHandler turns it into 500 Internal Server Error
	wrapped := router.WrapHandler(handler)
	wrapped(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestGetRoutes(t *testing.T) {
	mockStore := nopStore{}
	h := mustNewHandler(t, mockStore, false, fstest.MapFS{}, "test", "dev")

	routes := router.GetRoutes(h)
	if len(routes) == 0 {
		t.Fatal("Expected GetRoutes to return route definitions, got empty")
	}

	expectedRoutes := map[string]struct {
		method    string
		pattern   string
		useLogger bool
		useAuth   bool
	}{
		"HealthCheck": {method: "GET", pattern: "/", useLogger: false, useAuth: false},
		"Ping":        {method: "GET", pattern: "/ping/:argument", useLogger: true, useAuth: false},
		"Pong":        {method: "POST", pattern: "/pong", useLogger: true, useAuth: true},
		"GetLogs":     {method: "GET", pattern: "/logs/:from/:to", useLogger: false, useAuth: true},
		// Notes: authenticated, and never request-logged (the log would copy every note body).
		"ListNotes":     {method: "GET", pattern: "/notes", useLogger: false, useAuth: true},
		"CreateNote":    {method: "POST", pattern: "/notes", useLogger: false, useAuth: true},
		"GetNote":       {method: "GET", pattern: "/notes/:id", useLogger: false, useAuth: true},
		"UpdateNote":    {method: "PUT", pattern: "/notes/:id", useLogger: false, useAuth: true},
		"DeleteNote":    {method: "DELETE", pattern: "/notes/:id", useLogger: false, useAuth: true},
		"NoteBacklinks": {method: "GET", pattern: "/notes/:id/backlinks", useLogger: false, useAuth: true},
		"RenameNote":    {method: "POST", pattern: "/notes/:id/rename", useLogger: false, useAuth: true},
		"NoteChanges":   {method: "GET", pattern: "/changes", useLogger: false, useAuth: true},
		"ListProjects":  {method: "GET", pattern: "/projects", useLogger: false, useAuth: true},
	}

	if len(routes) != len(expectedRoutes) {
		t.Errorf("Expected %d routes, got %d", len(expectedRoutes), len(routes))
	}

	for _, route := range routes {
		expected, exists := expectedRoutes[route.Name]
		if !exists {
			t.Errorf("Unexpected route %q", route.Name)
			continue
		}
		if route.Method != expected.method {
			t.Errorf("Route %q: expected method %q, got %q", route.Name, expected.method, route.Method)
		}
		if route.Pattern != expected.pattern {
			t.Errorf("Route %q: expected pattern %q, got %q", route.Name, expected.pattern, route.Pattern)
		}
		if route.UseLogger != expected.useLogger {
			t.Errorf("Route %q: expected UseLogger=%v, got %v", route.Name, expected.useLogger, route.UseLogger)
		}
		if route.UseAuth != expected.useAuth {
			t.Errorf("Route %q: expected UseAuth=%v, got %v", route.Name, expected.useAuth, route.UseAuth)
		}
		if route.HandlerFunc == nil {
			t.Errorf("Route %q: HandlerFunc should not be nil", route.Name)
		}
	}
}

func TestNewRouter_RequiresHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	_, err := router.NewRouter(router.Config{
		Version: "v1.0.0",
	})
	if err == nil {
		t.Fatalf("Expected NewRouter to fail when Handler is not provided, got nil")
	}
	expected := "handler must be provided"
	if err.Error() != expected {
		t.Errorf("Expected error %q, got %q", expected, err.Error())
	}
}

func TestStartupLogSinkValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	settings := types.AppSettings{
		AuthToken: "secret-token",
	}

	h := mustNewHandler(t, nil, false, fstest.MapFS{}, "test", "dev")

	_, err := router.NewRouter(router.Config{
		Handler:  h,
		LogSink:  nil,
		Settings: settings,
	})
	if err == nil {
		t.Fatalf("Expected NewRouter to fail when logged routes exist without a log sink, got nil")
	}
	expected := `log sink must be configured for logged route "Ping"`
	if err.Error() != expected {
		t.Errorf("Expected error %q, got %q", expected, err.Error())
	}
}

func TestNewRouter_EmbeddedModeNilAssetsReturnsError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockStore := nopStore{}
	h := mustNewHandler(t, mockStore, false, fstest.MapFS{}, "test", "dev")

	_, err := router.NewRouter(router.Config{
		Handler:  h,
		LogSink:  &recordingSink{},
		Assets:   nil,
		Settings: types.AppSettings{AuthToken: "secret-token", UseFileSystem: false},
		Version:  "v1.0.0",
	})
	if err == nil {
		t.Fatalf("Expected NewRouter to fail when Assets is nil in embedded mode, got nil")
	}
	expected := "embedded assets filesystem is nil"
	if err.Error() != expected {
		t.Errorf("Expected error %q, got %q", expected, err.Error())
	}
}

func TestNewRouter_FileSystemModeNilAssetsSucceeds(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockStore := nopStore{}
	h := mustNewHandler(t, mockStore, false, fstest.MapFS{}, "test", "dev")

	r, err := router.NewRouter(router.Config{
		Handler:  h,
		LogSink:  &recordingSink{},
		Assets:   nil,
		Settings: types.AppSettings{AuthToken: "secret-token", UseFileSystem: true},
		Version:  "v1.0.0",
	})
	if err != nil {
		t.Fatalf("Expected NewRouter to succeed in filesystem mode without embedded assets, got: %v", err)
	}
	if r == nil {
		t.Fatal("Expected non-nil router")
	}
}

type testLogEntry struct {
	level   string
	message string
}

type testLogger struct {
	entries  []testLogEntry
	messages []string
}

func (tl *testLogger) Infof(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	tl.entries = append(tl.entries, testLogEntry{level: "INFO", message: msg})
	tl.messages = append(tl.messages, msg)
}

func (tl *testLogger) Warningf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	tl.entries = append(tl.entries, testLogEntry{level: "WARN", message: msg})
	tl.messages = append(tl.messages, msg)
}

func (tl *testLogger) Errorf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	tl.entries = append(tl.entries, testLogEntry{level: "ERROR", message: msg})
	tl.messages = append(tl.messages, msg)
}

func TestNewRouter_InjectedLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)

	settings := types.AppSettings{
		AuthToken: "secret-token",
	}

	tmpFile := filepath.Join(t.TempDir(), "test_router_logger.db")
	s, err := store.NewSQLite(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer s.Close()

	h := mustNewHandler(t, s, false, fstest.MapFS{}, "test", "dev")

	tl := &testLogger{}
	sink := &recordingSink{}
	r, err := router.NewRouter(router.Config{
		Handler:  h,
		LogSink:  sink,
		Settings: settings,
		Assets:   fstest.MapFS{},
		Version:  "dev",
		Logger:   tl,
	})
	if err != nil {
		t.Fatalf("NewRouter failed: %v", err)
	}

	// An authorized logged request is handed to the sink.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/pong", bytes.NewBufferString(`{"test":"data"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "secret-token")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if len(sink.entries) != 1 || sink.entries[0].Method != "POST" || sink.entries[0].Status != http.StatusOK || !strings.HasSuffix(sink.entries[0].Endpoint, "/pong") {
		t.Fatalf("Expected one POST /pong 200 entry in the sink, got %+v", sink.entries)
	}

	// A rejected request is reported through the injected logger.
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/pong", bytes.NewBufferString(`{}`))
	r.ServeHTTP(w, req)

	found := false
	for _, entry := range tl.entries {
		if entry.level == "WARN" && strings.Contains(entry.message, "unauthorized request: POST /pong") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected the injected logger to receive the 401 warning, got %v", tl.messages)
	}
}

func TestAuthMiddleware_InjectedLoggerOnEmptyToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tl := &testLogger{}
	mw := router.AuthMiddleware("", tl)
	handler := mw(func(c *gin.Context) error { return nil })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/protected", nil)

	err := handler(c)
	if err == nil {
		t.Fatalf("Expected error from AuthMiddleware with empty token, got nil")
	}

	if len(tl.entries) == 0 {
		t.Errorf("Expected injected logger to log warning on empty token")
	} else if tl.entries[0].level != "WARN" || !strings.Contains(tl.entries[0].message, "AUTH_TOKEN is not set") {
		t.Errorf("Expected warning message, got %v", tl.entries[0])
	}
}

func TestAuthMiddleware_Logs401OnMissingHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tl := &testLogger{}
	mw := router.AuthMiddleware("secret-token", tl)
	handler := mw(func(c *gin.Context) error { return nil })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/protected", nil)

	wrapped := router.WrapHandler(handler)
	wrapped(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}

	if len(tl.entries) != 1 {
		t.Fatalf("Expected 1 log entry, got %d", len(tl.entries))
	}
	entry := tl.entries[0]
	if entry.level != "WARN" {
		t.Errorf("Expected log level WARN, got %q", entry.level)
	}
	if !strings.Contains(entry.message, "unauthorized request: GET /protected") ||
		!strings.Contains(entry.message, "missing authorization header") {
		t.Errorf("Expected unauthorized log message with method, path, and reason, got %q", entry.message)
	}
}

func TestAuthMiddleware_Logs401OnInvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tl := &testLogger{}
	mw := router.AuthMiddleware("secret-token", tl)
	handler := mw(func(c *gin.Context) error { return nil })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/admin", nil)
	c.Request.Header.Set("Authorization", "Bearer super-secret-wrong-token")

	wrapped := router.WrapHandler(handler)
	wrapped(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}

	if len(tl.entries) != 1 {
		t.Fatalf("Expected 1 log entry, got %d", len(tl.entries))
	}
	entry := tl.entries[0]
	if entry.level != "WARN" {
		t.Errorf("Expected log level WARN, got %q", entry.level)
	}
	if !strings.Contains(entry.message, "unauthorized request: POST /admin") ||
		!strings.Contains(entry.message, "invalid authorization token") {
		t.Errorf("Expected unauthorized log message with method, path, and reason, got %q", entry.message)
	}
	if strings.Contains(entry.message, "super-secret-wrong-token") || strings.Contains(entry.message, "secret-token") {
		t.Errorf("Log message should not contain token values, got %q", entry.message)
	}
}

// nopStore satisfies store.Store for tests that never touch storage.
type nopStore struct{}

func (nopStore) Ping(context.Context) error { return nil }
func (nopStore) GetLogs(context.Context, time.Time, time.Time, store.Page) ([]types.UsageLog, error) {
	return nil, nil
}
func (nopStore) LogRequests(context.Context, []types.UsageLog) error { return nil }
func (nopStore) Close() error                                        { return nil }

// recordingSink collects the entries the logger middleware hands off.
type recordingSink struct {
	entries []types.UsageLog
}

func (r *recordingSink) Enqueue(e types.UsageLog) {
	r.entries = append(r.entries, e)
}

func TestLoggerMiddleware_RejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rs := &recordingSink{}
	tl := &testLogger{}
	called := false
	handler := router.LoggerMiddleware(rs, tl)(func(c *gin.Context) error {
		called = true
		return nil
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/big", bytes.NewReader(make([]byte, 1<<20+1)))

	router.WrapHandler(handler)(c)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status %d, got %d", http.StatusRequestEntityTooLarge, w.Code)
	}
	if called {
		t.Error("Expected handler not to be called for an oversized body")
	}
	if len(rs.entries) != 0 {
		t.Errorf("Expected oversized request not to be logged, got %d entries", len(rs.entries))
	}
	if len(tl.entries) != 1 || tl.entries[0].level != "WARN" {
		t.Errorf("Expected one WARN log entry, got %v", tl.entries)
	}
}

func TestLoggerMiddleware_AcceptsBodyAtLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rs := &recordingSink{}
	var got int
	handler := router.LoggerMiddleware(rs, &testLogger{})(func(c *gin.Context) error {
		b, err := io.ReadAll(c.Request.Body)
		if err != nil {
			return err
		}
		got = len(b)
		c.Status(http.StatusOK)
		return nil
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/limit", bytes.NewReader(make([]byte, 1<<20)))

	router.WrapHandler(handler)(c)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
	if got != 1<<20 {
		t.Errorf("Expected handler to read %d bytes, got %d", 1<<20, got)
	}
}

func TestLoggerMiddleware_CapturesWriteString(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rs := &recordingSink{}
	handler := router.LoggerMiddleware(rs, &testLogger{})(func(c *gin.Context) error {
		c.Status(http.StatusOK)
		_, err := c.Writer.WriteString(`{"via":"WriteString"}`)
		return err
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/write-string", nil)

	router.WrapHandler(handler)(c)

	if len(rs.entries) != 1 || string(rs.entries[0].Response) != `{"via":"WriteString"}` {
		t.Errorf("Expected logged response %q, got %+v", `{"via":"WriteString"}`, rs.entries)
	}
	if w.Body.String() != `{"via":"WriteString"}` {
		t.Errorf("Expected client response %q, got %q", `{"via":"WriteString"}`, w.Body.String())
	}
}

func mustNewHandler(t *testing.T, s store.Store, useFileSystem bool, embedded fs.FS, name, version string) *handlers.Handler {
	t.Helper()
	h, err := handlers.NewHandler(s, useFileSystem, embedded, name, version)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}
	return h
}

func TestNewRouter_Debug(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newRouter := func(debug bool) (*gin.Engine, *testLogger) {
		t.Helper()
		tl := &testLogger{}
		r, err := router.NewRouter(router.Config{
			Handler:  mustNewHandler(t, nopStore{}, false, fstest.MapFS{}, "test", "dev"),
			LogSink:  &recordingSink{},
			Settings: types.AppSettings{AuthToken: "secret-token", Debug: debug},
			Assets:   fstest.MapFS{},
			Logger:   tl,
		})
		if err != nil {
			t.Fatalf("NewRouter failed: %v", err)
		}
		return r, tl
	}

	t.Run("on", func(t *testing.T) {
		r, tl := newRouter(true)

		var routes int
		for _, e := range tl.entries {
			if strings.HasPrefix(e.message, "route ") {
				routes++
			}
		}
		if want := len(router.GetRoutes(mustNewHandler(t, nopStore{}, false, fstest.MapFS{}, "test", "dev"))); routes != want {
			t.Errorf("Expected %d route lines, got %d: %v", want, routes, tl.messages)
		}
		found := false
		for _, m := range tl.messages {
			if strings.Contains(m, "route POST /pong (Pong) auth=true logged=true") {
				found = true
			}
		}
		if !found {
			t.Errorf("Expected a route line for Pong, got %v", tl.messages)
		}

		before := len(tl.entries)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/ping/hello?token=abc", nil))
		access := tl.entries[before:]
		if len(access) != 1 || access[0].level != "INFO" || !strings.HasPrefix(access[0].message, "GET /ping/hello 200 ") {
			t.Fatalf("Expected one access log line for GET /ping/hello 200, got %v", access)
		}
		if strings.Contains(access[0].message, "token=abc") {
			t.Errorf("Access log must not include the query string, got %q", access[0].message)
		}
	})

	t.Run("off", func(t *testing.T) {
		r, tl := newRouter(false)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/ping/hello", nil))
		if len(tl.entries) != 0 {
			t.Errorf("Expected no debug logging, got %v", tl.messages)
		}
	})
}
