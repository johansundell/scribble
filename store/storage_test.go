package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/johansundell/scribble/types"
)

func newTestSQLite(t *testing.T) *SQLStore {
	t.Helper()
	s, err := NewSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestLogRequests(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	err := s.LogRequests(ctx, []types.UsageLog{types.UsageLog{Status: 200, Method: "GET", Endpoint: "/test", CreatedAt: time.Now(), Response: "{}", Request: "{}"}})
	if err != nil {
		t.Errorf("LogRequests failed: %v", err)
	}

	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM request_logs").Scan(&count); err != nil {
		t.Fatalf("Failed to query logs: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 log entry, got %d", count)
	}
}

func TestLogRequests_StoresFixedWidthUTC(t *testing.T) {
	s := newTestSQLite(t)

	cest := time.FixedZone("CEST", 2*60*60)
	created := time.Date(2026, 9, 30, 23, 30, 0, 0, cest)
	if err := s.LogRequests(context.Background(), []types.UsageLog{types.UsageLog{Endpoint: "/utc", CreatedAt: created}}); err != nil {
		t.Fatalf("LogRequests failed: %v", err)
	}

	var raw string
	if err := s.db.QueryRow("SELECT CAST(created_at AS TEXT) FROM request_logs").Scan(&raw); err != nil {
		t.Fatalf("Failed to read created_at: %v", err)
	}
	if want := "2026-09-30T21:30:00.000000Z"; raw != want {
		t.Errorf("Expected created_at %q, got %q", want, raw)
	}
}

func TestGetLogs(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	now := time.Now()
	if err := s.LogRequests(ctx, []types.UsageLog{types.UsageLog{Status: 200, Method: "GET", Endpoint: "/test", CreatedAt: now, Response: "{}", Request: "{}"}}); err != nil {
		t.Fatalf("LogRequests failed: %v", err)
	}

	logs, err := s.GetLogs(ctx, now.Add(-time.Hour), now.Add(time.Hour), Page{})
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("Expected 1 log entry, got %d", len(logs))
	}
	if logs[0].Endpoint != "/test" {
		t.Errorf("Expected endpoint /test, got %s", logs[0].Endpoint)
	}
	if !logs[0].CreatedAt.Equal(now.Truncate(time.Microsecond)) {
		t.Errorf("Expected CreatedAt %v, got %v", now, logs[0].CreatedAt)
	}
}

// Entries are written with a non-UTC offset; the range is a whole UTC day.
// Comparing RFC3339 strings with mixed offsets used to drop entries near midnight.
func TestGetLogs_UTCDayBoundaries(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	cest := time.FixedZone("CEST", 2*60*60)
	dayStart := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.AddDate(0, 0, 1)

	entries := []struct {
		endpoint string
		at       time.Time
	}{
		{"before", dayStart.Add(-time.Microsecond)},
		{"at-start", dayStart},
		{"sub-second", dayStart.Add(500 * time.Millisecond)},
		{"late-cest", time.Date(2026, 10, 1, 0, 30, 0, 0, cest)}, // 22:30Z on the 30th
		{"at-end", dayEnd},
	}
	for _, e := range entries {
		if err := s.LogRequests(ctx, []types.UsageLog{types.UsageLog{Endpoint: e.endpoint, CreatedAt: e.at}}); err != nil {
			t.Fatalf("LogRequests(%s) failed: %v", e.endpoint, err)
		}
	}

	logs, err := s.GetLogs(ctx, dayStart, dayEnd, Page{})
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}

	var got []string
	for _, l := range logs {
		got = append(got, l.Endpoint)
	}
	want := []string{"at-start", "sub-second", "late-cest"}
	if len(got) != len(want) {
		t.Fatalf("Expected endpoints %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Expected endpoints %v in order, got %v", want, got)
		}
	}
}

func TestClose(t *testing.T) {
	s, err := NewSQLite(filepath.Join(t.TempDir(), "close.db"))
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := s.Ping(context.Background()); err == nil {
		t.Error("Expected Ping to fail after Close")
	}
}

func TestNewSQLite_Pragmas(t *testing.T) {
	s := newTestSQLite(t)

	var journalMode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("Failed to query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("Expected journal_mode wal, got %q", journalMode)
	}

	var busyTimeout int
	if err := s.db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("Failed to query busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("Expected busy_timeout 5000, got %d", busyTimeout)
	}
}

func TestLogRequests_BatchIsAllOrNothing(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	if _, err := s.db.Exec(`CREATE TRIGGER reject_bad BEFORE INSERT ON request_logs
		WHEN NEW.endpoint = '/bad' BEGIN SELECT RAISE(ABORT, 'rejected'); END`); err != nil {
		t.Fatalf("Failed to create trigger: %v", err)
	}

	batch := []types.UsageLog{{Endpoint: "/ok-1", CreatedAt: time.Now()}, {Endpoint: "/bad", CreatedAt: time.Now()}, {Endpoint: "/ok-2", CreatedAt: time.Now()}}
	if err := s.LogRequests(ctx, batch); err == nil {
		t.Fatal("Expected the batch to fail")
	}

	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM request_logs").Scan(&count); err != nil {
		t.Fatalf("Failed to count logs: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected no rows from a failed batch, got %d", count)
	}

	if err := s.LogRequests(ctx, nil); err != nil {
		t.Errorf("Expected an empty batch to be a no-op, got %v", err)
	}
}

func TestGetLogs_Page(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	base := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	var batch []types.UsageLog
	for i := 0; i < 5; i++ {
		// Entries 1 and 2 share a timestamp: ID breaks the tie.
		at := base.Add(time.Duration(i) * time.Minute)
		if i == 2 {
			at = base.Add(time.Minute)
		}
		batch = append(batch, types.UsageLog{Endpoint: fmt.Sprintf("/e%d", i), CreatedAt: at})
	}
	if err := s.LogRequests(ctx, batch); err != nil {
		t.Fatalf("LogRequests failed: %v", err)
	}

	endpoints := func(page Page) []string {
		t.Helper()
		logs, err := s.GetLogs(ctx, base, base.Add(time.Hour), page)
		if err != nil {
			t.Fatalf("GetLogs(%+v) failed: %v", page, err)
		}
		var out []string
		for _, l := range logs {
			out = append(out, l.Endpoint)
		}
		return out
	}

	cases := []struct {
		page Page
		want string
	}{
		{Page{}, "[/e0 /e1 /e2 /e3 /e4]"},
		{Page{Limit: 2}, "[/e0 /e1]"},
		{Page{Limit: 2, Offset: 2}, "[/e2 /e3]"},
		{Page{Limit: 2, Offset: 4}, "[/e4]"},
		{Page{Limit: 2, Offset: 9}, "[]"},
		{Page{Offset: 3}, "[/e3 /e4]"}, // offset without a limit
	}
	for _, tc := range cases {
		if got := fmt.Sprint(endpoints(tc.page)); got != tc.want {
			t.Errorf("GetLogs(%+v) = %s, want %s", tc.page, got, tc.want)
		}
	}
}
