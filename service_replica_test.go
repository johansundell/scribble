package main

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johansundell/scribble/fmsodata"
	"github.com/johansundell/scribble/replica"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

// noTables is a FileMaker file without the replica's tables.
type noTables struct{ replica.Client }

func (noTables) GetRecords(context.Context, string, url.Values) ([]map[string]any, error) {
	return nil, &fmsodata.StatusError{StatusCode: 404, Body: "table not found"}
}

func replicaSettings(t *testing.T) {
	t.Helper()
	orig := settings
	t.Cleanup(func() { settings = orig })
	settings = types.AppSettings{Storage: types.StorageSQLite,
		Replica: types.ReplicaSettings{Mode: types.ReplicaFileMaker, NotesTable: "Notes", PendingTable: "PendingEdits", InfoTable: "ScribbleInfo", Interval: time.Hour}}
}

func TestStartReplica_OffWhenNotConfigured(t *testing.T) {
	orig := settings
	defer func() { settings = orig }()
	settings = types.AppSettings{Storage: types.StorageSQLite}
	syncer, stop := startReplica(nil)
	stop()
	if syncer != nil {
		t.Errorf("got a syncer without NOTES_REPLICA")
	}
}

func TestStartReplica_ABrokenSetupTurnsItOff(t *testing.T) {
	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for name, client := range map[string]func(types.FileMakerSettings) (replica.Client, error){
		"missing tables": func(types.FileMakerSettings) (replica.Client, error) { return noTables{}, nil },
		"bad CA file": func(types.FileMakerSettings) (replica.Client, error) {
			return nil, errors.New("read FMS_CA_FILE: no such file")
		},
	} {
		t.Run(name, func(t *testing.T) {
			replicaSettings(t)
			orig := newReplicaClient
			newReplicaClient = client
			defer func() { newReplicaClient = orig }()

			syncer, stop := startReplica(db)
			defer stop()
			deadline := time.Now().Add(2 * time.Second)
			for syncer.Status(context.Background()).Status != "off" && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			if st := syncer.Status(context.Background()); st.Status != "off" || st.Reason == "" {
				t.Errorf("status: %+v", st)
			}
		})
	}
}

func TestRunReplicaResync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.db")
	db, err := store.NewSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := db.CreateNote(context.Background(), "Note")
	db.AdvancePush(context.Background(), 0, n.Seq)
	db.ApplyPendingEdit(context.Background(), store.PendingEdit{ID: 41, Kind: store.PendingCreate, Body: "From FileMaker"})
	db.Close()

	s := types.AppSettings{Storage: types.StorageSQLite, SqlitePath: path}
	var out, errOut strings.Builder
	if code := runReplica([]string{"resync"}, s, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "41") {
		t.Errorf("output does not name the last applied PendingEdits ID: %q", out.String())
	}
	db, _ = store.NewSQLite(path)
	defer db.Close()
	if st, _ := db.ReplicaState(context.Background()); st.LastPushedSeq != 0 {
		t.Errorf("last pushed seq = %d, want 0", st.LastPushedSeq)
	}

	for _, args := range [][]string{nil, {"bogus"}} {
		if code := runReplica(args, s, &out, &errOut); code != 2 {
			t.Errorf("replica %q: exit %d, want 2", args, code)
		}
	}
	if code := runReplica([]string{"resync"}, types.AppSettings{Storage: types.StorageMySQL}, &out, &errOut); code != 1 {
		t.Errorf("resync with STORAGE=mysql: exit %d, want 1", code)
	}
}
