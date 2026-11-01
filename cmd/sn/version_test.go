package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/client"
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

// installLine is what version prints under "update available" on goos.
func installLine(goos string) string {
	if h := installHint(goos, "run: "); h != "" {
		return h + "\n"
	}
	return ""
}

func setVersion(t *testing.T, v string) {
	old := Version
	Version = v
	t.Cleanup(func() { Version = old })
}

func TestTooOldCLIPrintsTheUpgradeMessage(t *testing.T) {
	serveVersion(t, "v0.1.0", client.APILevel+1)
	setVersion(t, "v0.0.9")

	err := runWith(&bytes.Buffer{}, []string{"ls"})
	want := "sn: this version (v0.0.9) is too old for the server; download the latest from https://github.com/johansundell/scribble/releases/latest" +
		installHint(runtime.GOOS, " or run: ")
	if got := errorLine(err); got != want {
		t.Errorf("printed %q, want %q", got, want)
	}
}

// The one-line update command is only offered where the installer runs.
func TestInstallHint(t *testing.T) {
	cmd := "curl -fsSL https://raw.githubusercontent.com/johansundell/scribble/master/scripts/install-sn.sh | sh"
	for goos, want := range map[string]string{"linux": "run: " + cmd, "darwin": "run: " + cmd, "windows": "", "freebsd": ""} {
		if got := installHint(goos, "run: "); got != want {
			t.Errorf("%s: got %q, want %q", goos, got, want)
		}
	}
}

func TestCmdVersion(t *testing.T) {
	cases := []struct {
		name, client, server string
		want                 string
	}{
		{"update available", "v0.0.9", "v0.0.12",
			"client v0.0.9 (API level 4)\nserver v0.0.12 (API level 2, schema 5)\nupdate available: https://github.com/johansundell/scribble/releases/latest\n" +
				installLine(runtime.GOOS)},
		{"up to date", "v0.0.12", "v0.0.12",
			"client v0.0.12 (API level 4)\nserver v0.0.12 (API level 2, schema 5)\n"},
		{"dev build never compares", "dev", "v0.0.12",
			"client dev (API level 4)\nserver v0.0.12 (API level 2, schema 5)\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Too old as well: version still answers.
			serveVersion(t, tc.server, client.APILevel+1)
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
	if want := "client v0.0.12 (API level 4)\nserver v0.0.12 (API level 2)\n"; out.String() != want {
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
	if want := "client v0.0.9 (API level 4)\n"; out.String() != want {
		t.Errorf("printed %q, want %q", out.String(), want)
	}
}

func TestHelpAndHintsNameTheSnCommand(t *testing.T) {
	// An unknown command is only reported once the settings load, so the
	// test can't depend on the caller's environment.
	t.Setenv("SCRIBBLE_URL", "http://127.0.0.1:1")
	t.Setenv("SCRIBBLE_TOKEN", "tok")
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

func TestHelpNamesTheDefaultServer(t *testing.T) {
	var out bytes.Buffer
	if err := runWith(&out, []string{"help"}); err != nil {
		t.Fatal(err)
	}
	if want := "SCRIBBLE_URL    server address (default https://notes.sudde.net)"; !strings.Contains(out.String(), want) {
		t.Errorf("help does not contain %q:\n%s", want, out.String())
	}
}
