//go:build filemaker

// Integration test against a real FileMaker Server. It runs only with
//
//	FMS_TEST_HOST=https://fms.example.com FMS_TEST_DATABASE=... \
//	FMS_TEST_USERNAME=... FMS_TEST_PASSWORD=... go test -tags filemaker ./replica
//
// Like store's test, it uses its own FMS_TEST_* variables, and creates and
// deletes temporary tables, so the account needs schema privileges. OData
// cannot create an auto-enter serial, so the test sets PendingEdits::ID
// itself; check the auto-enter fields once by hand on tables set up in
// FileMaker from docs/filemaker/notes.md.
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
	defs := []fmsodata.TableDefinition{
		{TableName: tables.Notes, Fields: []fmsodata.FieldDefinition{
			{Name: "NoteID", Type: "NUMERIC", Primary: true},
			{Name: "Title", Type: "VARCHAR"},
			{Name: "Body", Type: "VARCHAR"},
			{Name: "Revision", Type: "NUMERIC"},
			{Name: "Seq", Type: "NUMERIC"},
			{Name: "Deleted", Type: "NUMERIC"},
			{Name: "CreatedAt", Type: "TIMESTAMP"},
			{Name: "UpdatedAt", Type: "TIMESTAMP"},
			{Name: "SyncedAt", Type: "TIMESTAMP"},
		}},
		{TableName: tables.Pending, Fields: []fmsodata.FieldDefinition{
			{Name: "ID", Type: "NUMERIC", Primary: true},
			{Name: "Kind", Type: "VARCHAR"},
			{Name: "NoteID", Type: "NUMERIC"},
			{Name: "Body", Type: "VARCHAR"},
			{Name: "BaseRevision", Type: "NUMERIC"},
			{Name: "CreatedAt", Type: "TIMESTAMP"},
			{Name: "CreatedBy", Type: "VARCHAR"},
			{Name: "Status", Type: "VARCHAR"},
			{Name: "Message", Type: "VARCHAR"},
		}},
		{TableName: tables.Info, Fields: []fmsodata.FieldDefinition{
			{Name: "SchemaVersion", Type: "NUMERIC"},
		}},
	}
	for _, d := range defs {
		if err := client.CreateTable(ctx, d); err != nil {
			t.Fatalf("create table %s: %v", d.TableName, err)
		}
		t.Cleanup(func() {
			if err := client.DeleteTable(context.Background(), d.TableName); err != nil {
				t.Errorf("delete table %s: %v", d.TableName, err)
			}
		})
	}

	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := New(client, db, Config{Tables: tables, Interval: time.Hour})

	if err := s.Check(ctx); err == nil || !isSetupError(err) {
		t.Fatalf("Check without a ScribbleInfo record = %v, want a setup error", err)
	}
	if _, err := client.CreateRecord(ctx, tables.Info, map[string]any{"SchemaVersion": SchemaVersion}); err != nil {
		t.Fatal(err)
	}
	if err := s.Check(ctx); err != nil {
		t.Fatalf("Check: %v", err)
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
