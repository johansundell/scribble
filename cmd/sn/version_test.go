package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

// serveVersion starts a scribble server at version, refusing clients below
// minClient, and points the CLI at it.
func serveVersion(t *testing.T, version string, minClient int) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "cli.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h, err := handlers.NewHandler(s, false, fstest.MapFS{}, "test", version)
	if err != nil {
		t.Fatal(err)
	}
	r, err := router.NewRouter(router.Config{
		Handler: h, LogSink: discardSink{}, Settings: types.AppSettings{AuthToken: "tok"},
		Assets: fstest.MapFS{}, Version: version, APILevel: 2, MinClientAPI: minClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	t.Setenv("SCRIBBLE_URL", srv.URL)
	t.Setenv("SCRIBBLE_TOKEN", "tok")
}

func setVersion(t *testing.T, v string) {
	old := Version
	Version = v
	t.Cleanup(func() { Version = old })
}

func TestTooOldCLIPrintsTheUpgradeMessage(t *testing.T) {
	serveVersion(t, "v0.1.0", 2)
	setVersion(t, "v0.0.9")

	err := runWith(&bytes.Buffer{}, []string{"ls"})
	want := "sn: this version (v0.0.9) is too old for the server; download the latest from https://github.com/johansundell/scribble/releases/latest"
	if got := errorLine(err); got != want {
		t.Errorf("printed %q, want %q", got, want)
	}
}

func TestCmdVersion(t *testing.T) {
	cases := []struct {
		name, client, server string
		want                 string
	}{
		{"update available", "v0.0.9", "v0.0.12",
			"client v0.0.9 (API level 1)\nserver v0.0.12 (API level 2, schema 4)\nupdate available: https://github.com/johansundell/scribble/releases/latest\n"},
		{"up to date", "v0.0.12", "v0.0.12",
			"client v0.0.12 (API level 1)\nserver v0.0.12 (API level 2, schema 4)\n"},
		{"dev build never compares", "dev", "v0.0.12",
			"client dev (API level 1)\nserver v0.0.12 (API level 2, schema 4)\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Too old as well: version still answers.
			serveVersion(t, tc.server, 2)
			setVersion(t, tc.client)
			var out bytes.Buffer
			if err := runWith(&out, []string{"version"}); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.want {
				t.Errorf("printed\n%s\nwant\n%s", out.String(), tc.want)
			}
		})
	}
}

// TestCmdVersionWithoutSchema reads as before when the server sends no
// schema version (MySQL, FileMaker, or a server from before #103).
func TestCmdVersionWithoutSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"version":"v0.0.12","api":2}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SCRIBBLE_URL", srv.URL)
	t.Setenv("SCRIBBLE_TOKEN", "")
	setVersion(t, "v0.0.12")
	var out bytes.Buffer
	if err := runWith(&out, []string{"version"}); err != nil {
		t.Fatal(err)
	}
	if want := "client v0.0.12 (API level 1)\nserver v0.0.12 (API level 2)\n"; out.String() != want {
		t.Errorf("printed %q, want %q", out.String(), want)
	}
}

// TestCmdVersionWithoutServer prints the client's own version, then fails.
func TestCmdVersionWithoutServer(t *testing.T) {
	t.Setenv("SCRIBBLE_URL", "http://127.0.0.1:1")
	t.Setenv("SCRIBBLE_TOKEN", "")
	setVersion(t, "v0.0.9")
	var out bytes.Buffer
	if err := runWith(&out, []string{"version"}); err == nil {
		t.Error("want an error when the server cannot be reached")
	}
	if want := "client v0.0.9 (API level 1)\n"; out.String() != want {
		t.Errorf("printed %q, want %q", out.String(), want)
	}
}

func TestHelpAndHintsNameTheSnCommand(t *testing.T) {
	var out bytes.Buffer
	if err := runWith(&out, []string{"help"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "sn - notes from the command line") {
		t.Errorf("help starts %q", strings.SplitN(out.String(), "\n", 2)[0])
	}
	if strings.Contains(out.String(), "scribble ") {
		t.Errorf("help still mentions the old command name:\n%s", out.String())
	}
	err := runWith(&out, []string{"frobnicate"})
	if want := `unknown command "frobnicate" (try: sn help)`; err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}
