package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/oauth"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

// serveVersion starts a scribble server at version, refusing clients below
// minClient, and points the CLI at it.
func serveVersion(t *testing.T, version string, minClient int) string {
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
	return srv.URL
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
		extra                string
	}{
		{"update available", "v0.0.9", "v0.0.12",
			"update available: https://github.com/johansundell/scribble/releases/latest\n" +
				installLine(runtime.GOOS)},
		{"up to date", "v0.0.12", "v0.0.12", ""},
		{"dev build never compares", "dev", "v0.0.12", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Too old as well: version still answers.
			srvURL := serveVersion(t, tc.server, client.APILevel+1)
			setVersion(t, tc.client)
			var out bytes.Buffer
			if err := runWith(&out, []string{"version"}); err != nil {
				t.Fatal(err)
			}
			want := "client " + tc.client + " (API level 7)\n" +
				"server " + tc.server + " (API level 2, schema 8)\n" +
				"url: " + srvURL + "\n" +
				"token: static (never expires)\n" +
				"config: none (environment / defaults)\n" +
				tc.extra
			if out.String() != want {
				t.Errorf("printed\n%s\nwant\n%s", out.String(), want)
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
	if want := "client v0.0.12 (API level 7)\nserver v0.0.12 (API level 2)\nurl: " + srv.URL + "\ntoken: (not logged in)\nconfig: none (environment / defaults)\n"; out.String() != want {
		t.Errorf("printed %q, want %q", out.String(), want)
	}
}

// TestCmdVersionWithoutServer prints the client's own version, URL and token, then fails.
func TestCmdVersionWithoutServer(t *testing.T) {
	t.Setenv("SCRIBBLE_URL", "http://127.0.0.1:1")
	t.Setenv("SCRIBBLE_TOKEN", "")
	setVersion(t, "v0.0.9")
	var out bytes.Buffer
	if err := runWith(&out, []string{"version"}); err == nil {
		t.Error("want an error when the server cannot be reached")
	}
	if want := "client v0.0.9 (API level 7)\nurl: http://127.0.0.1:1\ntoken: (not logged in)\nconfig: none (environment / defaults)\n"; out.String() != want {
		t.Errorf("printed %q, want %q", out.String(), want)
	}
}

func TestCmdVersionWithoutServerCustomToken(t *testing.T) {
	t.Setenv("SCRIBBLE_URL", "http://127.0.0.1:1")
	t.Setenv("SCRIBBLE_TOKEN", "secret-token")
	setVersion(t, "v0.0.9")
	var out bytes.Buffer
	if err := runWith(&out, []string{"version"}); err == nil {
		t.Error("want an error when the server cannot be reached")
	}
	// The token itself is never printed (#193).
	if want := "client v0.0.9 (API level 7)\nurl: http://127.0.0.1:1\ntoken: static (never expires)\nconfig: none (environment / defaults)\n"; out.String() != want {
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

func TestTokenStatus(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	oauth := client.Tokens{Access: "a", Refresh: "r",
		Expiry:        now.Add(23*time.Hour + 30*time.Minute),
		RefreshExpiry: now.Add(364*24*time.Hour + time.Hour)}
	expired := oauth
	expired.Expiry = now.Add(-time.Minute)
	cases := []struct {
		name    string
		tok     client.Tokens
		refused bool
		want    string
	}{
		{"not logged in", client.Tokens{}, false, "token: (not logged in)\n"},
		{"static", client.Tokens{Access: "secret"}, false, "token: static (never expires)\n"},
		{"oauth", oauth, false,
			"access token expires: 2026-11-13 09:30:00 UTC (in 23 hours)\n" +
				"refresh token expires: 2027-11-11 11:00:00 UTC (in 364 days)\n"},
		{"refresh refused", expired, true,
			"access token: expired\nrefresh token: expired (run sn login to re-authenticate)\n"},
		{"refresh token past its expiry", client.Tokens{Access: "a", Refresh: "r", Expiry: now.Add(-time.Hour), RefreshExpiry: now.Add(-time.Minute)}, false,
			"access token: expired\nrefresh token: expired (run sn login to re-authenticate)\n"},
		{"access expired, not renewed (server down)", expired, false,
			"access token: expired\nrefresh token expires: 2027-11-11 11:00:00 UTC (in 364 days)\n"},
		{"unknown expiries (saved before #193)", client.Tokens{Access: "a", Refresh: "r"}, false,
			"access token expires: unknown\nrefresh token expires: unknown\n"},
		{"minutes and singulars", client.Tokens{Access: "a", Refresh: "r", Expiry: now.Add(90 * time.Second), RefreshExpiry: now.Add(25 * time.Hour)}, false,
			"access token expires: 2026-11-12 10:01:30 UTC (in 1 minute)\n" +
				"refresh token expires: 2026-11-13 11:00:00 UTC (in 1 day)\n"},
		{"under a minute", client.Tokens{Access: "a", Refresh: "r", Expiry: now.Add(30 * time.Second), RefreshExpiry: now.Add(3 * time.Hour)}, false,
			"access token expires: 2026-11-12 10:00:30 UTC (in less than a minute)\n" +
				"refresh token expires: 2026-11-12 13:00:00 UTC (in 3 hours)\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tokenStatus(tc.tok, now, tc.refused); got != tc.want {
				t.Errorf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// loggedIn logs sn in to a new OAuth2 server and returns ~/.scribble.
func loggedIn(t *testing.T) string {
	t.Helper()
	clearEnv(t)
	srvURL := newOAuthServerURL(t, oauth.Config{})
	path := dotfile(t)
	withStdin(t, srvURL+"\n"+loginCode()+"\n")
	if err := runWith(&bytes.Buffer{}, []string{"login"}); err != nil {
		t.Fatal(err)
	}
	return path
}

// setInDotfile replaces key's value in the file at path.
func setInDotfile(t *testing.T, path, key, value string) {
	t.Helper()
	old := readDotfile(t, path)[key]
	raw, _ := os.ReadFile(path)
	writeFile(t, path, strings.Replace(string(raw), old, value, 1))
}

func TestVersionShowsLoginExpiry(t *testing.T) {
	loggedIn(t)
	var out bytes.Buffer
	if err := runWith(&out, []string{"version"}); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile("\naccess token expires: \\d{4}-\\d\\d-\\d\\d \\d\\d:\\d\\d:\\d\\d UTC \\(in 23 hours\\)\n" +
		"refresh token expires: \\d{4}-\\d\\d-\\d\\d \\d\\d:\\d\\d:\\d\\d UTC \\(in 364 days\\)\nconfig: ")
	if !re.MatchString(out.String()) {
		t.Errorf("printed\n%s", out.String())
	}
	if strings.Contains(out.String(), "token: ") {
		t.Errorf("printed a token line:\n%s", out.String())
	}
}

// An expired access token is renewed before the expiry is shown, and the
// renewed tokens are saved.
func TestVersionRenewsAnExpiredAccessToken(t *testing.T) {
	path := loggedIn(t)
	before := readDotfile(t, path)
	setInDotfile(t, path, "SCRIBBLE_TOKEN_EXPIRY", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
	var out bytes.Buffer
	if err := runWith(&out, []string{"version"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "(in 23 hours)\n") {
		t.Errorf("printed\n%s", out.String())
	}
	if after := readDotfile(t, path); after["SCRIBBLE_TOKEN"] == before["SCRIBBLE_TOKEN"] {
		t.Error("renewed tokens were not saved")
	}
}

func TestVersionRefusedRefreshToken(t *testing.T) {
	path := loggedIn(t)
	setInDotfile(t, path, "SCRIBBLE_TOKEN_EXPIRY", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
	setInDotfile(t, path, "SCRIBBLE_REFRESH_TOKEN", "not-a-refresh-token")
	var out bytes.Buffer
	if err := runWith(&out, []string{"version"}); err != nil {
		t.Fatal(err)
	}
	if want := "\naccess token: expired\nrefresh token: expired (run sn login to re-authenticate)\nconfig: "; !strings.Contains(out.String(), want) {
		t.Errorf("printed\n%s\nwant it to contain %q", out.String(), want)
	}
}
