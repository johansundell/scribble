package main

import (
	"context"
	"maps"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/gui"
	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/oauth"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
	"github.com/joho/godotenv"
)

var loginKey = []byte("abcdefghij0123456789")

func loginCode() string { return auth.Code(loginKey, time.Now()) }

// discardSink drops request log entries.
type discardSink struct{}

func (discardSink) Enqueue(types.UsageLog) {}

// newOAuthServerURL starts a server with OAuth2 login (#184) and returns
// its address.
func newOAuthServerURL(t *testing.T) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "gui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a, err := auth.New(auth.Config{TOTPKey: loginKey, Secret: []byte(strings.Repeat("k", 32)), SessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	o, err := oauth.New(oauth.Config{Auth: a, Tokens: s})
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

func readEnvFile(t *testing.T, path string) map[string]string {
	t.Helper()
	env, err := godotenv.Read(path)
	if err != nil {
		t.Fatalf("the written .env does not parse: %v", err)
	}
	return env
}

func TestSaveLoginWritesDotEnvAndSetsTheEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("SCRIBBLE_HOTKEY=none\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{envURL, envToken, envRefresh, envExpiry} {
		t.Setenv(k, "")
	}
	exp := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

	if err := saveLogin(path, "https://notes.example", client.Tokens{Access: "a", Refresh: "r", Expiry: exp}); err != nil {
		t.Fatal(err)
	}

	env := readEnvFile(t, path)
	want := map[string]string{envURL: "https://notes.example", envToken: "a", envRefresh: "r", envExpiry: "2030-01-02T03:04:05Z", "SCRIBBLE_HOTKEY": "none"}
	if !maps.Equal(env, want) {
		t.Errorf(".env holds %v, want %v", env, want)
	}
	if got := savedLogin(gui.Overridden{}); got != (client.Tokens{Access: "a", Refresh: "r", Expiry: exp}) {
		t.Errorf("the saved login in the environment for this run is %+v", got)
	}
}

// A SCRIBBLE_TOKEN from the environment is static; the saved refresh
// token is not used with it.
func TestSavedLoginWithAnOverriddenToken(t *testing.T) {
	t.Setenv(envToken, "static")
	t.Setenv(envRefresh, "r")
	t.Setenv(envExpiry, "2030-01-02T03:04:05Z")
	if got := savedLogin(gui.Overridden{Token: true}); got != (client.Tokens{Access: "static"}) {
		t.Errorf("savedLogin = %+v, want the static token only", got)
	}
}

func TestLoginWithACode(t *testing.T) {
	for _, k := range []string{envURL, envToken, envRefresh, envExpiry} {
		t.Setenv(k, "")
	}
	srvURL := newOAuthServerURL(t)
	path := filepath.Join(t.TempDir(), ".env")

	wrong := "000000"
	if wrong == loginCode() {
		wrong = "111111"
	}
	if _, err := login(path, "dev", gui.Settings{URL: srvURL, Code: wrong}); err == nil || !strings.Contains(err.Error(), "wrong") {
		t.Fatalf("wrong code: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a failed login wrote the .env file")
	}

	tok, err := login(path, "dev", gui.Settings{URL: srvURL + "/", Code: loginCode()})
	if err != nil {
		t.Fatal(err)
	}
	env := readEnvFile(t, path)
	if env[envURL] != srvURL || env[envToken] != tok.Access || env[envRefresh] != tok.Refresh || tok.Refresh == "" {
		t.Fatalf(".env holds %v after logging in with %+v", env, tok)
	}

	// The client renews the token and saves the new one.
	tok.Expiry = time.Now().Add(-time.Minute)
	c := newClient(srvURL, "dev", tok, func() (string, error) { return path, nil })
	if _, err := c.List(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if env := readEnvFile(t, path); env[envToken] == tok.Access || env[envToken] != c.Tokens().Access {
		t.Errorf("the renewed token was not saved: %v", env)
	}
}

func TestSetupScreenOffersSettings(t *testing.T) {
	test.NewTempApp(t)
	w := test.NewTempWindow(t, nil)
	var saved gui.Settings
	started := false
	w.SetContent(setupScreen(w, "Not logged in.", gui.Settings{URL: "https://notes.sudde.net"}, gui.Overridden{},
		func(s gui.Settings) error { saved = s; return nil },
		func() { started = true }))
	w.Resize(fyne.NewSize(600, 400))

	var button *widget.Button
	for _, o := range test.LaidOutObjects(w.Content()) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Settings…" {
			button = b
		}
	}
	if button == nil {
		t.Fatal("the setup screen has no Settings… button")
	}
	test.Tap(button)

	var entries []*widget.Entry
	for _, o := range test.LaidOutObjects(w.Canvas().Overlays().Top()) {
		if e, ok := o.(*widget.Entry); ok {
			entries = append(entries, e)
		}
	}
	if len(entries) != 2 || entries[0].Text != "https://notes.sudde.net" {
		t.Fatalf("the Settings dialog is not filled in with the default URL")
	}
	entries[1].SetText("123456")
	for _, o := range test.LaidOutObjects(w.Canvas().Overlays().Top()) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Save" {
			test.Tap(b)
		}
	}

	if saved != (gui.Settings{URL: "https://notes.sudde.net", Code: "123456"}) || !started {
		t.Errorf("saved %v, started %v; want the settings saved and the app started", saved, started)
	}
}

func TestEnvOverridesSeesVariablesSetInTheEnvironment(t *testing.T) {
	t.Setenv("SCRIBBLE_URL", "https://notes.example")
	t.Setenv("SCRIBBLE_TOKEN", "")
	os.Unsetenv("SCRIBBLE_TOKEN")

	if got := envOverrides(); got != (gui.Overridden{URL: true}) {
		t.Errorf("envOverrides() = %+v, want only the URL", got)
	}

	t.Setenv("SCRIBBLE_TOKEN", "") // set, even if empty: .env cannot change it
	if got := envOverrides(); got != (gui.Overridden{URL: true, Token: true}) {
		t.Errorf("envOverrides() = %+v, want both", got)
	}
}
