//go:build filemaker

// Integration test against a real FileMaker Server. It runs only with
//
//	FMS_TEST_HOST=https://fms.example.com FMS_TEST_DATABASE=... \
//	FMS_TEST_USERNAME=... FMS_TEST_PASSWORD=... go test -tags filemaker ./store
//
// The account needs to create and delete tables (fmodata and schema
// privileges). It uses its own FMS_TEST_* variables so it never touches a
// service's FMS_* database, and creates and deletes a temporary table.
//
// OData cannot create an auto-enter serial field, so ID stays empty in the
// temporary table; check the auto-enter ID once by hand on a table set up in
// FileMaker. FileMaker only returns @odata.nextLink above 10,000 records, so
// paging is covered by the unit tests.
package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/johansundell/scribble/fmsodata"
	"github.com/johansundell/scribble/types"
)

func TestFileMakerIntegration(t *testing.T) {
	cfg := types.FileMakerSettings{
		Host:     os.Getenv("FMS_TEST_HOST"),
		Database: os.Getenv("FMS_TEST_DATABASE"),
		Username: os.Getenv("FMS_TEST_USERNAME"),
		Password: os.Getenv("FMS_TEST_PASSWORD"),
		CAFile:   os.Getenv("FMS_TEST_CA_FILE"),
		Timeout:  30 * time.Second,
		LogTable: fmt.Sprintf("LogsTest_%d", time.Now().Unix()),
	}
	if cfg.Host == "" || cfg.Database == "" || cfg.Username == "" {
		t.Skip("set FMS_TEST_HOST, FMS_TEST_DATABASE, FMS_TEST_USERNAME and FMS_TEST_PASSWORD to run")
	}
	ctx := context.Background()

	tlsConfig, err := fileMakerTLSConfig(cfg)
	if err != nil {
		t.Fatalf("TLS config: %v", err)
	}
	admin := fmsodata.NewClient(fmsodata.ClientConfig{
		Host: cfg.Host, Database: cfg.Database, Username: cfg.Username, Password: cfg.Password,
		Timeout: cfg.Timeout, TLSConfig: tlsConfig,
	})
	table := logTableDefinition(cfg.LogTable)
	if err := admin.CreateTable(ctx, table); err != nil {
		t.Fatalf("create table %s: %v", cfg.LogTable, err)
	}
	t.Cleanup(func() {
		if err := admin.DeleteTable(context.Background(), cfg.LogTable); err != nil {
			t.Errorf("delete table %s: %v", cfg.LogTable, err)
		}
	})

	s, err := NewFileMaker(ctx, cfg)
	if err != nil {
		t.Fatalf("NewFileMaker (startup check): %v", err)
	}
	defer s.Close()

	if err := s.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	cest := time.FixedZone("CEST", 2*60*60)
	entries := []types.UsageLog{
		{Status: 200, Method: "GET", Endpoint: "/in-1", CreatedAt: day.Add(10 * time.Hour), Request: "{}", Response: `{"result":"a"}`},
		{Status: 404, Method: "GET", Error: "Nope", Endpoint: "/in-2", CreatedAt: time.Date(2026, 10, 1, 0, 30, 0, 0, cest), Request: "{}", Response: "Not Found"}, // 22:30Z on the 30th
		{Status: 200, Method: "GET", Endpoint: "/out", CreatedAt: day.AddDate(0, 0, 1), Request: "{}", Response: "{}"},
	}
	if err := s.LogRequests(ctx, entries); err != nil {
		t.Fatalf("LogRequests ($batch): %v", err)
	}

	logs, err := s.GetLogs(ctx, day, day.AddDate(0, 0, 1), Page{})
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	if len(logs) != 2 || logs[0].Endpoint != "/in-1" || logs[1].Endpoint != "/in-2" {
		t.Fatalf("expected /in-1 and /in-2 for the UTC day, got %+v", logs)
	}
	if want := time.Date(2026, 9, 30, 22, 30, 0, 0, time.UTC); !logs[1].CreatedAt.Equal(want) {
		t.Errorf("UTC round-trip: expected %v, got %v", want, logs[1].CreatedAt)
	}
	if logs[1].Status != 404 || logs[1].Error != "Nope" || string(logs[0].Response) != `{"result":"a"}` {
		t.Errorf("fields did not round-trip: %+v", logs)
	}
}

// TestFileMakerIntegration_CreatesMissingTable checks FMS_CREATE_LOG_TABLE
// (#113): startup creates a missing log table, and leaves it alone after.
func TestFileMakerIntegration_CreatesMissingTable(t *testing.T) {
	cfg := types.FileMakerSettings{
		Host:           os.Getenv("FMS_TEST_HOST"),
		Database:       os.Getenv("FMS_TEST_DATABASE"),
		Username:       os.Getenv("FMS_TEST_USERNAME"),
		Password:       os.Getenv("FMS_TEST_PASSWORD"),
		CAFile:         os.Getenv("FMS_TEST_CA_FILE"),
		Timeout:        30 * time.Second,
		LogTable:       fmt.Sprintf("LogsCreate_%d", time.Now().Unix()),
		CreateLogTable: true,
	}
	if cfg.Host == "" || cfg.Database == "" || cfg.Username == "" {
		t.Skip("set FMS_TEST_HOST, FMS_TEST_DATABASE, FMS_TEST_USERNAME and FMS_TEST_PASSWORD to run")
	}
	ctx := context.Background()

	// Off, a missing table still stops startup.
	off := cfg
	off.CreateLogTable = false
	if _, err := NewFileMaker(ctx, off); err == nil || !isMissingTable(err) {
		t.Fatalf("without CreateLogTable: want the missing-table error, got %v", err)
	}

	s, err := NewFileMaker(ctx, cfg)
	if err != nil {
		t.Fatalf("NewFileMaker (create): %v", err)
	}
	t.Cleanup(func() {
		if err := s.client.DeleteTable(context.Background(), cfg.LogTable); err != nil {
			t.Errorf("delete table %s: %v", cfg.LogTable, err)
		}
		s.Close()
	})
	if !s.CreatedLogTable() {
		t.Fatal("CreatedLogTable() = false on a new table")
	}
	if err := s.LogRequests(ctx, []types.UsageLog{{Status: 200, Method: "GET", Endpoint: "/created", CreatedAt: time.Now(), Request: "{}", Response: "{}"}}); err != nil {
		t.Fatalf("LogRequests into the created table: %v", err)
	}

	again, err := NewFileMaker(ctx, cfg)
	if err != nil {
		t.Fatalf("NewFileMaker (existing): %v", err)
	}
	defer again.Close()
	if again.CreatedLogTable() {
		t.Error("created the table a second time")
	}
}
