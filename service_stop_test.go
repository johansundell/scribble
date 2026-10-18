package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

// freeAddr returns a loopback address with a port that is free right now.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find a free port: %v", err)
	}
	defer l.Close()
	return l.Addr().String()
}

func useTestSettings(t *testing.T, port string) {
	t.Helper()
	originalSettings := settings
	settings = types.AppSettings{
		Port:       port,
		Timeout:    15 * time.Second,
		Storage:    types.StorageSQLite,
		SqlitePath: filepath.Join(t.TempDir(), "test_stop.db"),
		AuthToken:  "test-token",
	}
	t.Cleanup(func() { settings = originalSettings })
}

func TestStop_WaitsForShutdown(t *testing.T) {
	addr := freeAddr(t)
	useTestSettings(t, addr)

	p := newProgram()
	if err := p.startWorker(); err != nil {
		t.Fatalf("expected startup to succeed, got %v", err)
	}

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("expected the server to answer before Stop, got %v", err)
	}
	resp.Body.Close()

	if err := p.Stop(nil); err != nil {
		t.Fatalf("expected Stop to succeed, got %v", err)
	}

	// Stop must not return before the server has shut down.
	if conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
		conn.Close()
		t.Fatal("expected the port to refuse connections once Stop returned")
	}

	// A second Stop must not panic on closing p.exit again.
	if err := p.Stop(nil); err != nil {
		t.Errorf("expected a second Stop to return nil, got %v", err)
	}
}

type failingListener struct{}

func (failingListener) Accept() (net.Conn, error) { return nil, errors.New("accept failed") }
func (failingListener) Close() error              { return nil }
func (failingListener) Addr() net.Addr            { return &net.TCPAddr{} }

func TestRun_ServeFailureIsReported(t *testing.T) {
	useTestSettings(t, "127.0.0.1:0")

	originalListen := netListen
	netListen = func(string, string) (net.Listener, error) { return failingListener{}, nil }
	defer func() { netListen = originalListen }()

	p := newProgram()
	if err := p.startWorker(); err != nil {
		t.Fatalf("expected startup to succeed, got %v", err)
	}

	select {
	case err := <-p.failed:
		if !strings.Contains(err.Error(), "accept failed") {
			t.Errorf("expected the serve error to be reported, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected the serve failure to be reported on p.failed")
	}

	// run has already returned, so Stop must return its error without blocking.
	if err := p.Stop(nil); err == nil || !strings.Contains(err.Error(), "accept failed") {
		t.Errorf("expected Stop to return the serve error, got %v", err)
	}
}

func TestStop_DrainsRequestLogs(t *testing.T) {
	addr := freeAddr(t)
	useTestSettings(t, addr)

	p := newProgram()
	if err := p.startWorker(); err != nil {
		t.Fatalf("expected startup to succeed, got %v", err)
	}

	// Logged route; the entry waits in the queue (flush interval 1s).
	resp, err := http.Get("http://" + addr + "/ping/drained")
	if err != nil {
		t.Fatalf("expected the request to succeed, got %v", err)
	}
	resp.Body.Close()

	// Stop right away: draining must write the entry before the store closes.
	if err := p.Stop(nil); err != nil {
		t.Fatalf("expected Stop to succeed, got %v", err)
	}

	st, err := store.NewSQLite(settings.SqlitePath)
	if err != nil {
		t.Fatalf("failed to reopen the database: %v", err)
	}
	defer st.Close()
	now := time.Now().UTC()
	logs, err := st.GetLogs(context.Background(), now.Add(-time.Hour), now.Add(time.Hour), store.Page{})
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if len(logs) != 1 || !strings.HasSuffix(logs[0].Endpoint, "/ping/drained") {
		t.Errorf("expected the /ping/drained entry to be persisted on Stop, got %+v", logs)
	}
}
