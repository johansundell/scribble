//go:build filemaker

// Integration test against a real FileMaker Server. It runs only with
//
//	FMS_TEST_HOST=https://fms.example.com FMS_TEST_DATABASE=... \
//	FMS_TEST_USERNAME=... FMS_TEST_PASSWORD=... go test -tags filemaker ./replica
//
// Like store's test, it uses its own FMS_TEST_* variables, and lets the sync
// create temporary tables (FMS_CREATE_REPLICA_TABLES) and deletes them after,
// so the account needs schema privileges. OData cannot create an auto-enter
// serial, so the test sets PendingEdits::ID itself; check the auto-enter
// fields once by hand on tables set up in FileMaker from
// docs/filemaker/notes.md.
package replica

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/johansundell/scribble/fmsodata"
	"github.com/johansundell/scribble/store"
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
	}
	if cfg.Host == "" || cfg.Database == "" || cfg.Username == "" {
		t.Skip("set FMS_TEST_HOST, FMS_TEST_DATABASE, FMS_TEST_USERNAME and FMS_TEST_PASSWORD to run")
	}
	ctx := context.Background()
	client, err := store.NewFileMakerClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Unix()
	tables := Tables{
		Notes:   fmt.Sprintf("NotesTest_%d", stamp),
		Pending: fmt.Sprintf("PendingTest_%d", stamp),
		Info:    fmt.Sprintf("InfoTest_%d", stamp),
	}
	// Registered before the create, so a half-done create is cleaned up too.
	for _, table := range []string{tables.Notes, tables.Pending, tables.Info} {
		t.Cleanup(func() {
			if err := client.DeleteTable(context.Background(), table); err != nil && !fmsodata.IsMissingTable(err) {
				t.Errorf("delete table %s: %v", table, err)
			}
		})
	}

	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Off, missing tables turn the replica off and nothing is created.
	off := New(client, db, Config{Tables: tables, Interval: time.Hour})
	if err := off.Check(ctx); !isSetupError(err) || !fmsodata.IsMissingTable(err) {
		t.Fatalf("Check without CreateTables = %v, want the missing-table setup error", err)
	}

	// FMS_CREATE_REPLICA_TABLES (#185) creates them, with the ScribbleInfo record.
	s := New(client, db, Config{Tables: tables, Interval: time.Hour, CreateTables: true})
	if err := s.Check(ctx); err != nil {
		t.Fatalf("Check creating the tables: %v", err)
	}
	if info, err := client.GetRecords(ctx, tables.Info, nil); err != nil || len(info) != 1 || number(info[0]["SchemaVersion"]) != SchemaVersion {
		t.Fatalf("%s after the create: %v, %v", tables.Info, info, err)
	}
	// A second start finds them and changes nothing.
	if err := New(client, db, Config{Tables: tables, Interval: time.Hour, CreateTables: true}).Check(ctx); err != nil {
		t.Fatalf("second Check: %v", err)
	}
	if info, err := client.GetRecords(ctx, tables.Info, nil); err != nil || len(info) != 1 {
		t.Fatalf("%s after the second check: %d records, %v", tables.Info, len(info), err)
	}

	bodies := []string{
		"Plain\nline two",
		"Trailing\n\ntext\n",
		"Blank lines\n\n\n\nafter\n\n",
		"Code\n\n```go\nfunc main() {\n\tprintln(\"x\")\n}\n```\n",
	}
	var ids []int64
	for _, b := range bodies {
		n, err := db.CreateNote(ctx, b)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	gone, _ := db.CreateNote(ctx, "Deleted")
	db.DeleteNote(ctx, gone.ID)
	if err := s.Round(ctx); err != nil {
		t.Fatalf("first Round: %v", err)
	}
	for i, id := range ids {
		rs, err := client.GetRecords(ctx, tables.Notes, url.Values{"$filter": {fmt.Sprintf("NoteID eq %d", id)}})
		if err != nil || len(rs) != 1 {
			t.Fatalf("read note %d: %v, %d records", id, err, len(rs))
		}
		if got := normalizeBody(text(rs[0]["Body"])); got != bodies[i] {
			t.Errorf("note %d body round-trips as %q, want %q", id, got, bodies[i])
		}
	}

	// An edit made in FileMaker, as #67 would queue it.
	first, _ := db.GetNote(ctx, ids[0])
	if _, err := client.CreateRecord(ctx, tables.Pending, map[string]any{
		"ID": 1, "Kind": "update", "NoteID": first.ID, "BaseRevision": first.Revision, "Body": "Plain\r\nedited in FileMaker",
	}); err != nil {
		t.Fatal(err)
	}
	// Pushing everything again must be harmless.
	db.ResyncReplica(ctx)
	if err := s.Round(ctx); err != nil {
		t.Fatalf("second Round: %v", err)
	}
	if got, _ := db.GetNote(ctx, first.ID); got.Body != "Plain\nedited in FileMaker" {
		t.Errorf("FileMaker edit saved as %q", got.Body)
	}
	if rows, err := client.GetRecords(ctx, tables.Pending, nil); err != nil || len(rows) != 0 {
		t.Errorf("PendingEdits after the round: %v, %v; want empty", rows, err)
	}
	if rows, err := client.GetRecords(ctx, tables.Notes, nil); err != nil || len(rows) != len(bodies)+1 {
		t.Errorf("Notes has %d records (%v), want %d", len(rows), err, len(bodies)+1)
	}
}
