package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

// OAuth login (#184) is on only with the web app's authenticator code and
// a SQLite database to keep the tokens in.
func TestNewAPIOAuth(t *testing.T) {
	original := settings
	defer func() { settings = original }()

	webAuth, err := newWebAuth(types.WebSettings{TOTPSecret: "gezd gnbv gy3t qojq gezd gnbv gy3t qojq", SessionSecret: "0123456789abcdef0123456789abcdef", SessionTimeout: time.Hour}, "token")
	if err != nil {
		t.Fatal(err)
	}
	sqlite, err := store.NewSQLite(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlite.Close()

	settings.Storage = types.StorageSQLite
	if o, err := newAPIOAuth(nil, sqlite); err != nil || o != nil {
		t.Errorf("web app off: %v, %v; want nil", o, err)
	}
	if o, err := newAPIOAuth(webAuth, sqlite); err != nil || o == nil {
		t.Errorf("sqlite: %v, %v; want a server", o, err)
	}
	settings.Storage = types.StorageMySQL
	if o, err := newAPIOAuth(webAuth, sqlite); err != nil || o != nil {
		t.Errorf("mysql: %v, %v; want nil", o, err)
	}
}
