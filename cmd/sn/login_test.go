package main

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/oauth"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/store"
	"github.com/joho/godotenv"
)

var loginKey = []byte("abcdefghij0123456789")

// newOAuthServerURL starts a server with OAuth2 login (#184) and no
// AUTH_TOKEN, and returns its address.
func newOAuthServerURL(t *testing.T, cfg oauth.Config) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "cli.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a, err := auth.New(auth.Config{TOTPKey: loginKey, Secret: []byte(strings.Repeat("k", 32)), SessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Auth, cfg.Tokens = a, s
	o, err := oauth.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h, err := handlers.NewHandler(s, false, fstest.MapFS{}, "test", "dev")
	if err != nil {
		t.Fatal(err)
	}
	r, err := router.NewRouter(router.Config{Handler: h, OAuth: o, LogSink: discardSink{}, Assets: fstest.MapFS{}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv.URL
}

func loginCode() string { return auth.Code(loginKey, time.Now()) }

// withStdin makes text what sn reads from standard input.
func withStdin(t *testing.T, text string) {
	t.Helper()
	old := stdin
	stdin = strings.NewReader(text)
	t.Cleanup(func() { stdin = old })
}

// dotfile gives the test a home folder of its own, a new temporary one,
// and returns the path of ~/.scribble in it.
func dotfile(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, k := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "AppData"} {
		t.Setenv(k, home)
	}
	return filepath.Join(home, ".scribble")
}

func readDotfile(t *testing.T, path string) map[string]string {
	t.Helper()
	env, err := godotenv.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func clearEnv(t *testing.T) {
	t.Setenv("SCRIBBLE_URL", "")
	t.Setenv("SCRIBBLE_TOKEN", "")
}

func TestLoginSavesTokens(t *testing.T) {
	clearEnv(t)
	srvURL := newOAuthServerURL(t, oauth.Config{})
	path := dotfile(t)
	withStdin(t, srvURL+"\n"+loginCode()+"\n")

	var out bytes.Buffer
	if err := runWith(&out, []string{"login"}); err != nil {
		t.Fatal(err)
	}
	env := readDotfile(t, path)
	if env["SCRIBBLE_URL"] != srvURL || env["SCRIBBLE_TOKEN"] == "" || env["SCRIBBLE_REFRESH_TOKEN"] == "" {
		t.Fatalf("~/.scribble = %v", env)
	}
	if exp, err := time.Parse(time.RFC3339, env["SCRIBBLE_TOKEN_EXPIRY"]); err != nil || time.Until(exp) < 23*time.Hour {
		t.Errorf("SCRIBBLE_TOKEN_EXPIRY = %q", env["SCRIBBLE_TOKEN_EXPIRY"])
	}
	if exp, err := time.Parse(time.RFC3339, env["SCRIBBLE_REFRESH_EXPIRY"]); err != nil || time.Until(exp) < 364*24*time.Hour {
		t.Errorf("SCRIBBLE_REFRESH_EXPIRY = %q", env["SCRIBBLE_REFRESH_EXPIRY"])
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	if !strings.Contains(out.String(), "Logged in") {
		t.Errorf("output %q", out.String())
	}
	if err := runWith(&bytes.Buffer{}, []string{"ls"}); err != nil {
		t.Fatalf("ls after login: %v", err)
	}
}

// An empty answer keeps the URL in the file, a static token there is
// replaced, and the file's other lines stay. A file another tool made
// readable to others is made private.
func TestLoginKeepsURLAndReplacesStaticToken(t *testing.T) {
	clearEnv(t)
	srvURL := newOAuthServerURL(t, oauth.Config{})
	path := dotfile(t)
	writeFile(t, path, "# mine\nurl="+srvURL+"\ntoken=static\nSCRIBBLE_HOTKEY=none\n")
	os.Chmod(path, 0o644)
	withStdin(t, "\n"+loginCode()+"\n")

	if err := runWith(&bytes.Buffer{}, []string{"login"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	text := string(raw)
	for _, gone := range []string{"\nurl=", "token=static"} {
		if strings.Contains(text, gone) {
			t.Errorf("file still has %q:\n%s", gone, text)
		}
	}
	for _, kept := range []string{"# mine\n", "SCRIBBLE_HOTKEY=none\n", `SCRIBBLE_URL="` + srvURL + `"`} {
		if !strings.Contains(text, kept) {
			t.Errorf("file lost %q:\n%s", kept, text)
		}
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

// The URL can be given as an argument instead of answered.
func TestLoginURLArgument(t *testing.T) {
	clearEnv(t)
	srvURL := newOAuthServerURL(t, oauth.Config{})
	path := dotfile(t)
	withStdin(t, loginCode()+"\n")
	if err := runWith(&bytes.Buffer{}, []string{"login", srvURL}); err != nil {
		t.Fatal(err)
	}
	if got := readDotfile(t, path)["SCRIBBLE_URL"]; got != srvURL {
		t.Errorf("SCRIBBLE_URL = %q", got)
	}
}

func TestLoginWrongCode(t *testing.T) {
	clearEnv(t)
	srvURL := newOAuthServerURL(t, oauth.Config{})
	path := dotfile(t)
	wrong := "000000"
	if wrong == loginCode() {
		wrong = "111111"
	}
	withStdin(t, srvURL+"\n"+wrong+"\n")
	err := runWith(&bytes.Buffer{}, []string{"login"})
	if err == nil || !strings.Contains(err.Error(), "wrong") {
		t.Fatalf("err = %v, want a wrong code", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a failed login wrote ~/.scribble")
	}
}

func TestLogoutRemovesTokens(t *testing.T) {
	clearEnv(t)
	srvURL := newOAuthServerURL(t, oauth.Config{})
	path := dotfile(t)
	withStdin(t, srvURL+"\n"+loginCode()+"\n")
	if err := runWith(&bytes.Buffer{}, []string{"login"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runWith(&out, []string{"logout"}); err != nil {
		t.Fatal(err)
	}
	env := readDotfile(t, path)
	if env["SCRIBBLE_URL"] != srvURL {
		t.Errorf("logout dropped the URL: %v", env)
	}
	for _, k := range []string{"SCRIBBLE_TOKEN", "SCRIBBLE_REFRESH_TOKEN", "SCRIBBLE_TOKEN_EXPIRY", "SCRIBBLE_REFRESH_EXPIRY"} {
		if _, ok := env[k]; ok {
			t.Errorf("logout kept %s", k)
		}
	}
	err := runWith(&bytes.Buffer{}, []string{"ls"})
	if err == nil || !strings.Contains(err.Error(), "sn login") {
		t.Errorf("ls after logout: %v, want a hint to log in", err)
	}
}

// An expired access token is renewed with the refresh token, and the new
// tokens are saved for the next run.
func TestRefreshedTokensAreSaved(t *testing.T) {
	clearEnv(t)
	srvURL := newOAuthServerURL(t, oauth.Config{})
	path := dotfile(t)
	withStdin(t, srvURL+"\n"+loginCode()+"\n")
	if err := runWith(&bytes.Buffer{}, []string{"login"}); err != nil {
		t.Fatal(err)
	}
	before := readDotfile(t, path)
	// Pretend the access token has expired.
	raw, _ := os.ReadFile(path)
	text := strings.Replace(string(raw), before["SCRIBBLE_TOKEN_EXPIRY"], time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), 1)
	writeFile(t, path, text)

	if err := runWith(&bytes.Buffer{}, []string{"ls"}); err != nil {
		t.Fatal(err)
	}
	after := readDotfile(t, path)
	if after["SCRIBBLE_TOKEN"] == before["SCRIBBLE_TOKEN"] || after["SCRIBBLE_REFRESH_TOKEN"] == before["SCRIBBLE_REFRESH_TOKEN"] {
		t.Fatal("refreshed tokens were not saved")
	}
	if err := runWith(&bytes.Buffer{}, []string{"ls"}); err != nil {
		t.Fatalf("ls with the saved tokens: %v", err)
	}
}

// SCRIBBLE_TOKEN in the environment is a static token: the refresh token
// in the file is not used with it.
func TestEnvTokenIgnoresSavedRefreshToken(t *testing.T) {
	cfg, err := resolveConfig(envOf(map[string]string{"SCRIBBLE_TOKEN": "static"}), []string{
		writeFile(t, filepath.Join(t.TempDir(), "dot"), "SCRIBBLE_TOKEN=a\nSCRIBBLE_REFRESH_TOKEN=r\nSCRIBBLE_TOKEN_EXPIRY=2030-01-01T00:00:00Z\nSCRIBBLE_REFRESH_EXPIRY=2031-01-01T00:00:00Z\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "static" || cfg.Refresh != "" || !cfg.Expiry.IsZero() || !cfg.RefreshExpiry.IsZero() {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestResolveConfigReadsOAuthTokens(t *testing.T) {
	cfg, err := resolveConfig(envOf(nil), []string{
		writeFile(t, filepath.Join(t.TempDir(), "dot"), "SCRIBBLE_TOKEN=a\nSCRIBBLE_REFRESH_TOKEN=r\nSCRIBBLE_TOKEN_EXPIRY=2030-01-01T00:00:00Z\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "a" || cfg.Refresh != "r" || !cfg.Expiry.Equal(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("cfg = %+v", cfg)
	}
}

// The refresh token's expiry is read under its canonical name and the
// lower-case ones (#193).
func TestResolveConfigReadsRefreshExpiry(t *testing.T) {
	want := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, key := range []string{"SCRIBBLE_REFRESH_EXPIRY", "scribble_refresh_expiry", "refresh_expiry"} {
		cfg, err := resolveConfig(envOf(nil), []string{
			writeFile(t, filepath.Join(t.TempDir(), "dot"), "SCRIBBLE_TOKEN=a\nSCRIBBLE_REFRESH_TOKEN=r\n"+key+"=2031-01-01T00:00:00Z\n"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.RefreshExpiry.Equal(want) {
			t.Errorf("%s: RefreshExpiry = %v, want %v", key, cfg.RefreshExpiry, want)
		}
	}
}

// A login replaces a refresh expiry saved under another spelling.
func TestLoginReplacesOtherRefreshExpirySpellings(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "dot"), "refresh_expiry=2020-01-01T00:00:00Z\n")
	exp := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := saveLogin(path, "https://a", client.Tokens{Access: "a", Refresh: "r", RefreshExpiry: exp}); err != nil {
		t.Fatal(err)
	}
	env := readDotfile(t, path)
	if _, ok := env["refresh_expiry"]; ok {
		t.Errorf("old spelling kept: %v", env)
	}
	if env["SCRIBBLE_REFRESH_EXPIRY"] != "2031-01-01T00:00:00Z" {
		t.Errorf("SCRIBBLE_REFRESH_EXPIRY = %q", env["SCRIBBLE_REFRESH_EXPIRY"])
	}
}
