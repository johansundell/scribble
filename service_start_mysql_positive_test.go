package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

func TestStart_WithMySQLStorage_UsesMockedConstructor(t *testing.T) {
	originalSettings := settings
	settings = types.AppSettings{Port: freeAddr(t), Timeout: 15 * time.Second, Storage: types.StorageMySQL, AuthToken: "test-token"}
	settings.MySQL.Username = "user"
	settings.MySQL.Host = "localhost"
	settings.MySQL.Port = "3306"
	settings.MySQL.Database = "db"
	defer func() { settings = originalSettings }()

	// Return a SQLite store instead of connecting to MySQL, and capture the config
	var captured types.MySQLSettings
	orig := newMySQLStore
	newMySQLStore = func(cfg types.MySQLSettings) (store.Store, error) {
		captured = cfg
		return store.NewSQLite(filepath.Join(t.TempDir(), "test_mysql_positive.db"))
	}
	defer func() { newMySQLStore = orig }()

	p := newProgram()
	if err := p.startWorker(); err != nil {
		t.Fatalf("expected startup to succeed, got error: %v", err)
	}
	if err := p.Stop(nil); err != nil {
		t.Fatalf("expected Stop to succeed, got error: %v", err)
	}

	if captured != settings.MySQL {
		t.Errorf("expected the MYSQL_* settings to be passed on, got %+v", captured)
	}
}
