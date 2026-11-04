package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

func TestStart_InvalidTimeoutReturnsError(t *testing.T) {
	settings := types.AppSettings{Port: ":8080", Timeout: 0, Storage: types.StorageSQLite}
	if err := settings.Validate(); err == nil {
		t.Fatal("expected validation to fail with an invalid timeout")
	}
}

func TestStart_DatabaseInitializationErrorReturnsError(t *testing.T) {
	originalConstructor := newSQLiteStore
	newSQLiteStore = func(string) (store.Store, error) {
		return nil, errors.New("database unavailable")
	}
	defer func() { newSQLiteStore = originalConstructor }()

	originalSettings := settings
	settings = types.AppSettings{Port: ":8080", Timeout: 15 * time.Second, Storage: types.StorageSQLite}
	defer func() { settings = originalSettings }()

	p := &program{}
	if err := p.run(make(chan error, 1)); err == nil {
		t.Fatal("expected run to return the database initialization error")
	}
}

func TestNewWebAuth(t *testing.T) {
	a, err := newWebAuth(types.WebSettings{}, "token")
	if err != nil || a != nil {
		t.Errorf("web off: got %v, %v; want nil, nil", a, err)
	}

	// Spaces and lower case, as some apps show a secret, are fine.
	a, err = newWebAuth(types.WebSettings{TOTPSecret: "gezd gnbv gy3t qojq gezd gnbv gy3t qojq", SessionSecret: "0123456789abcdef0123456789abcdef", SessionTimeout: time.Hour}, "token")
	if err != nil || a == nil {
		t.Fatalf("web on: got %v, %v", a, err)
	}
	if err := a.Login("127.0.0.1", auth.Code([]byte("12345678901234567890"), time.Now())); err != nil {
		t.Errorf("the configured secret's code doesn't log in: %v", err)
	}
	if err := a.Setup("127.0.0.1", "token"); err != nil {
		t.Errorf("AUTH_TOKEN doesn't open the setup page: %v", err)
	}
}

// PUBLIC_DEMO turns off the lock for everyone (#175): many wrong codes, each
// from its own address, never lock web login.
func TestNewWebAuth_PublicDemoNeverLocks(t *testing.T) {
	w := types.WebSettings{TOTPSecret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", SessionSecret: "0123456789abcdef0123456789abcdef", SessionTimeout: time.Hour}
	for _, demo := range []bool{false, true} {
		w.PublicDemo = demo
		a, err := newWebAuth(w, "token")
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < auth.DefaultAlertFailures; i++ {
			_ = a.Login(fmt.Sprintf("10.1.0.%d", i), "000000")
		}
		if a.Locked() == demo {
			t.Errorf("PUBLIC_DEMO=%v: Locked() = %v after %d wrong codes", demo, a.Locked(), auth.DefaultAlertFailures)
		}
	}
}

func TestEnsureAuthToken(t *testing.T) {
	orig := settings.AuthToken
	defer func() { settings.AuthToken = orig }()

	settings.AuthToken = ""
	ensureAuthToken()
	first := settings.AuthToken
	if first == "" {
		t.Fatal("expected a generated token, got empty string")
	}

	settings.AuthToken = ""
	ensureAuthToken()
	if settings.AuthToken == first {
		t.Errorf("expected a new random token on each call, got %q twice", first)
	}

	settings.AuthToken = "configured"
	ensureAuthToken()
	if settings.AuthToken != "configured" {
		t.Errorf("expected configured token to be kept, got %q", settings.AuthToken)
	}
}

func TestStart_UsesConfiguredSqlitePath(t *testing.T) {
	var capturedPath string
	originalConstructor := newSQLiteStore
	newSQLiteStore = func(path string) (store.Store, error) {
		capturedPath = path
		return nil, errors.New("stop here")
	}
	defer func() { newSQLiteStore = originalConstructor }()

	originalSettings := settings
	settings = types.AppSettings{Port: ":8080", Timeout: 15 * time.Second, Storage: types.StorageSQLite, SqlitePath: "/custom/data/my.db"}
	defer func() { settings = originalSettings }()

	p := &program{}
	_ = p.run(make(chan error, 1))

	if capturedPath != "/custom/data/my.db" {
		t.Errorf("expected sqlite path /custom/data/my.db, got %q", capturedPath)
	}
}

func TestOpenStore_FileMakerMapsSettings(t *testing.T) {
	originalSettings := settings
	settings = types.AppSettings{Storage: types.StorageFileMaker, FileMaker: types.FileMakerSettings{
		Host: "https://fms.example.com", Database: "Logging", Username: "u", Password: "p",
		Timeout: 3 * time.Second, LogTable: "ServiceLogs", CAFile: "/etc/ca.pem",
	}}
	defer func() { settings = originalSettings }()

	var got types.FileMakerSettings
	var deadline time.Time
	orig := newFileMakerStore
	newFileMakerStore = func(ctx context.Context, cfg types.FileMakerSettings) (store.Store, error) {
		got = cfg
		deadline, _ = ctx.Deadline()
		return nil, errors.New("stop here")
	}
	defer func() { newFileMakerStore = orig }()

	if _, _, err := openStore(); err == nil {
		t.Fatal("expected the constructor's error to be returned")
	}
	want := settings.FileMaker
	if got != want {
		t.Errorf("expected %+v, got %+v", want, got)
	}
	if deadline.IsZero() || time.Until(deadline) > 3*time.Second {
		t.Errorf("expected the startup check to run within FMS_TIMEOUT, got deadline %v", deadline)
	}
}

// deadlineStore records the deadline of the startup Ping.
type deadlineStore struct {
	store.Store
	pingDeadline time.Time
}

func (d *deadlineStore) Ping(ctx context.Context) error {
	d.pingDeadline, _ = ctx.Deadline()
	return nil
}
func (d *deadlineStore) Close() error { return nil }

func TestStart_FileMakerPingUsesFMSTimeout(t *testing.T) {
	useTestSettings(t, freeAddr(t))
	settings.Storage = types.StorageFileMaker
	settings.FileMaker.Timeout = 7 * time.Second

	ds := &deadlineStore{}
	orig := newFileMakerStore
	newFileMakerStore = func(context.Context, types.FileMakerSettings) (store.Store, error) { return ds, nil }
	defer func() { newFileMakerStore = orig }()

	p := newProgram()
	start := time.Now()
	if err := p.startWorker(); err != nil {
		t.Fatalf("expected startup to succeed, got %v", err)
	}
	p.Stop(nil)

	if got := ds.pingDeadline.Sub(start); got < 6*time.Second || got > 8*time.Second {
		t.Errorf("expected the startup ping deadline to follow FMS_TIMEOUT (7s), got %v", got)
	}
}

func TestStoreOpenersMatchStorageBackends(t *testing.T) {
	if len(storeOpeners) != len(types.StorageBackends) {
		t.Errorf("storeOpeners has %d backends, types.StorageBackends lists %d", len(storeOpeners), len(types.StorageBackends))
	}
	for _, name := range types.StorageBackends {
		if _, ok := storeOpeners[name]; !ok {
			t.Errorf("STORAGE=%s passes validation but has no store constructor", name)
		}
	}
}
