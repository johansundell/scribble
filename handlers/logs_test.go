package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

func TestGetLogs(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	// Mock FS (not used but required by NewHandler)
	mockFS := fstest.MapFS{}

	// Create in-memory DB
	s, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("Failed to create db: %v", err)
	}
	defer s.Close()

	// Insert some test data
	now := time.Now().UTC()
	err = s.LogRequests(context.Background(), []types.UsageLog{types.UsageLog{Status: 200, Method: "GET", Endpoint: "/test", CreatedAt: now, Response: "{}", Request: "{}"}})
	if err != nil {
		t.Fatalf("Failed to insert log: %v", err)
	}

	h := mustNewHandler(t, s, false, mockFS, "test-service", "v1.0")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/logs", nil)

	// Set params: dates are UTC days
	c.Params = gin.Params{
		{Key: "from", Value: now.Format("2006-01-02")},
		{Key: "to", Value: now.Format("2006-01-02")},
	}

	err = h.GetLogs(c)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var page logsPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(page.Entries) != 1 {
		t.Errorf("Expected 1 log, got %d", len(page.Entries))
	}
}

func TestGetLogs_ToIsInclusiveUTCDay(t *testing.T) {
	gin.SetMode(gin.TestMode)

	s, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("Failed to create db: %v", err)
	}
	defer s.Close()

	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	for endpoint, at := range map[string]time.Time{
		"/late-on-day": day.Add(23*time.Hour + 59*time.Minute),
		"/next-day":    day.AddDate(0, 0, 1),
	} {
		if err := s.LogRequests(context.Background(), []types.UsageLog{types.UsageLog{Endpoint: endpoint, CreatedAt: at}}); err != nil {
			t.Fatalf("Failed to insert log: %v", err)
		}
	}

	h := mustNewHandler(t, s, false, fstest.MapFS{}, "test-service", "v1.0")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/logs", nil)
	c.Params = gin.Params{{Key: "from", Value: "2026-09-30"}, {Key: "to", Value: "2026-09-30"}}

	if err := h.GetLogs(c); err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	var page logsPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Endpoint != "/late-on-day" {
		t.Errorf("Expected only /late-on-day, got %+v", page.Entries)
	}
}

func TestGetLogs_EmptyRangeReturnsArray(t *testing.T) {
	gin.SetMode(gin.TestMode)

	s, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("Failed to create db: %v", err)
	}
	defer s.Close()

	h := mustNewHandler(t, s, false, fstest.MapFS{}, "test-service", "v1.0")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/logs", nil)
	c.Params = gin.Params{{Key: "from", Value: "2020-01-01"}, {Key: "to", Value: "2020-01-01"}}

	if err := h.GetLogs(c); err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if got := strings.TrimSpace(w.Body.String()); got != `{"entries":[],"next":null}` {
		t.Errorf("Expected an empty page, got %q", got)
	}
}

// pagingStore returns n entries and records the page it was asked for.
type pagingStore struct {
	store.Store
	n    int
	page store.Page
}

func (p *pagingStore) GetLogs(_ context.Context, _, _ time.Time, page store.Page) ([]types.UsageLog, error) {
	p.page = page
	logs := make([]types.UsageLog, p.n)
	for i := range logs {
		logs[i] = types.UsageLog{ID: page.Offset + i + 1}
	}
	return logs, nil
}

func getLogsPage(t *testing.T, ps *pagingStore, query string) (*httptest.ResponseRecorder, logsPage, error) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := mustNewHandler(t, ps, false, fstest.MapFS{}, "test-service", "v1.0")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/logs/2026-09-30/2026-09-30"+query, nil)
	c.Params = gin.Params{{Key: "from", Value: "2026-09-30"}, {Key: "to", Value: "2026-09-30"}}
	err := h.GetLogs(c)
	var page logsPage
	if err == nil {
		if jerr := json.Unmarshal(w.Body.Bytes(), &page); jerr != nil {
			t.Fatalf("Failed to unmarshal response %q: %v", w.Body.String(), jerr)
		}
	}
	return w, page, err
}

func TestGetLogs_DefaultLimitAndNext(t *testing.T) {
	ps := &pagingStore{n: 1001} // one more than the default limit: there is a next page
	_, page, err := getLogsPage(t, ps, "")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if ps.page != (store.Page{Limit: 1001, Offset: 0}) {
		t.Errorf("Expected the store to be asked for limit+1 = 1001, got %+v", ps.page)
	}
	if len(page.Entries) != 1000 {
		t.Errorf("Expected 1000 entries, got %d", len(page.Entries))
	}
	if page.Next == nil || *page.Next != "/logs/2026-09-30/2026-09-30?limit=1000&offset=1000" {
		t.Errorf("Unexpected next link %v", page.Next)
	}
}

func TestGetLogs_LastPageHasNoNext(t *testing.T) {
	ps := &pagingStore{n: 2}
	_, page, err := getLogsPage(t, ps, "?limit=2&offset=4")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if ps.page != (store.Page{Limit: 3, Offset: 4}) {
		t.Errorf("Expected page {3 4}, got %+v", ps.page)
	}
	if len(page.Entries) != 2 || page.Entries[0].ID != 5 || page.Next != nil {
		t.Errorf("Expected 2 entries from ID 5 and no next link, got %+v next=%v", page.Entries, page.Next)
	}
}

func TestGetLogs_InvalidPaging(t *testing.T) {
	for _, q := range []string{"?limit=0", "?limit=10001", "?limit=abc", "?offset=-1", "?offset=x"} {
		_, _, err := getLogsPage(t, &pagingStore{}, q)
		if err == nil || httperror.HTTPStatus(err) != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %v", q, err)
		}
	}
	if _, _, err := getLogsPage(t, &pagingStore{}, "?limit=10000"); err != nil {
		t.Errorf("limit=10000 should be allowed, got %v", err)
	}
}

func TestGetLogs_NonJSONBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)

	s, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("Failed to create db: %v", err)
	}
	defer s.Close()

	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	entries := []types.UsageLog{
		{Endpoint: "/ping/bad", CreatedAt: day.Add(time.Hour), Request: "hello", Response: `{"result":"bad"}`},
		{Endpoint: "/ping/notfound", CreatedAt: day.Add(2 * time.Hour), Request: "{}", Response: "Not Found"},
	}
	if err := s.LogRequests(context.Background(), entries); err != nil {
		t.Fatalf("Failed to insert logs: %v", err)
	}

	h := mustNewHandler(t, s, false, fstest.MapFS{}, "test-service", "v1.0")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/logs/2026-09-30/2026-09-30", nil)
	c.Params = gin.Params{{Key: "from", Value: "2026-09-30"}, {Key: "to", Value: "2026-09-30"}}

	if err := h.GetLogs(c); err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	var page struct {
		Entries []map[string]json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("Expected a parseable page, got %q: %v", w.Body.String(), err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(page.Entries))
	}
	if got := string(page.Entries[0]["request"]); got != `"hello"` {
		t.Errorf("Expected the plain-text request as a JSON string, got %s", got)
	}
	if got := string(page.Entries[0]["response"]); got != `{"result":"bad"}` {
		t.Errorf("Expected the JSON response unchanged, got %s", got)
	}
	if got := string(page.Entries[1]["response"]); got != `"Not Found"` {
		t.Errorf("Expected the plain-text response as a JSON string, got %s", got)
	}
}
