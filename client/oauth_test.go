package client_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
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
)

var oauthKey = []byte("abcdefghij0123456789")

// startOAuthServer starts a server with OAuth2 login on (#184) and no
// AUTH_TOKEN, and returns its URL.
func startOAuthServer(t *testing.T, cfg oauth.Config) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a, err := auth.New(auth.Config{TOTPKey: oauthKey, Secret: []byte(strings.Repeat("k", 32)), SessionTTL: time.Hour})
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

func oauthCode() string { return auth.Code(oauthKey, time.Now()) }

// The client's copies of the server's constants must match.
func TestOAuthConstantsMatchServer(t *testing.T) {
	if client.TokenPath != oauth.TokenPath || client.ClientCLI != oauth.ClientCLI || client.ClientGUI != oauth.ClientGUI {
		t.Fatal("client and oauth constants differ")
	}
}

func TestLoginThenUseTheAPI(t *testing.T) {
	ctx := context.Background()
	c := client.New(startOAuthServer(t, oauth.Config{}), "")
	c.ClientID = client.ClientCLI
	tok, err := c.Login(ctx, oauthCode())
	if err != nil {
		t.Fatal(err)
	}
	if tok.Access == "" || tok.Refresh == "" || time.Until(tok.Expiry) < 23*time.Hour {
		t.Fatalf("tokens = %+v", tok)
	}
	if got := c.Tokens(); got != tok {
		t.Errorf("client holds %+v, want %+v", got, tok)
	}
	if _, err := c.List(ctx, ""); err != nil {
		t.Fatalf("list after login: %v", err)
	}
}

func TestLoginWrongCode(t *testing.T) {
	c := client.New(startOAuthServer(t, oauth.Config{}), "")
	c.ClientID = client.ClientCLI
	wrong := "000000"
	if wrong == oauthCode() {
		wrong = "111111"
	}
	_, err := c.Login(context.Background(), wrong)
	if !client.IsWrongCode(err) {
		t.Fatalf("err = %v, want a wrong code", err)
	}
}

func TestLoginWithoutOAuthOnServer(t *testing.T) {
	c := newClient(t) // AUTH_TOKEN only
	c.ClientID = client.ClientCLI
	_, err := c.Login(context.Background(), "123456")
	if err == nil || !strings.Contains(err.Error(), "WEB_TOTP_SECRET") {
		t.Fatalf("err = %v, want a hint that the server has no OAuth login", err)
	}
}

// An expired access token is refreshed before the request, and the new
// tokens are handed to TokensChanged.
func TestExpiredAccessTokenIsRefreshed(t *testing.T) {
	ctx := context.Background()
	// Within refreshMargin of expiring from the start, so it is renewed
	// before the first request.
	c := client.New(startOAuthServer(t, oauth.Config{AccessTTL: time.Second}), "")
	c.ClientID = client.ClientCLI
	first, err := c.Login(ctx, oauthCode())
	if err != nil {
		t.Fatal(err)
	}
	var saved []client.Tokens
	c.TokensChanged = func(tok client.Tokens) { saved = append(saved, tok) }
	if _, err := c.List(ctx, ""); err != nil {
		t.Fatalf("list with an expired access token: %v", err)
	}
	if len(saved) != 1 || saved[0].Access == first.Access || saved[0].Refresh == first.Refresh {
		t.Fatalf("saved = %+v, want one new pair", saved)
	}
}

// A 401 for an access token the client thought was good (revoked, or the
// clocks disagree) is refreshed once and the request sent again.
func TestUnauthorizedIsRefreshedAndRetried(t *testing.T) {
	ctx := context.Background()
	url := startOAuthServer(t, oauth.Config{})
	c := client.New(url, "")
	c.ClientID = client.ClientCLI
	tok, err := c.Login(ctx, oauthCode())
	if err != nil {
		t.Fatal(err)
	}
	// A client with a made-up access token but the real refresh token.
	c2 := client.New(url, "")
	c2.ClientID = client.ClientCLI
	c2.SetTokens(client.Tokens{Access: "stale", Refresh: tok.Refresh, Expiry: time.Now().Add(time.Hour)})
	changed := 0
	c2.TokensChanged = func(client.Tokens) { changed++ }
	if _, err := c2.Create(ctx, "# Retried\nbody"); err != nil {
		t.Fatalf("create with a stale access token: %v", err)
	}
	if changed != 1 {
		t.Errorf("TokensChanged called %d times, want 1", changed)
	}
}

// When the refresh token is no good either, the caller gets the 401.
func TestRefreshFailsGivesUnauthorized(t *testing.T) {
	c := client.New(startOAuthServer(t, oauth.Config{}), "")
	c.ClientID = client.ClientCLI
	c.SetTokens(client.Tokens{Access: "stale", Refresh: "also-stale"})
	_, err := c.List(context.Background(), "")
	if !client.IsUnauthorized(err) {
		t.Fatalf("err = %v, want 401", err)
	}
}

// Requests in parallel with an expired token refresh it once: a second
// refresh with the used-up refresh token would fail.
func TestParallelRequestsRefreshOnce(t *testing.T) {
	ctx := context.Background()
	c := client.New(startOAuthServer(t, oauth.Config{}), "")
	c.ClientID = client.ClientGUI
	tok, err := c.Login(ctx, oauthCode())
	if err != nil {
		t.Fatal(err)
	}
	tok.Expiry = time.Now().Add(-time.Minute) // make the client think it expired
	c.SetTokens(tok)
	var mu sync.Mutex
	changed := 0
	c.TokensChanged = func(client.Tokens) { mu.Lock(); changed++; mu.Unlock() }
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.List(ctx, "")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if changed != 1 {
		t.Errorf("refreshed %d times, want 1", changed)
	}
}

// A static AUTH_TOKEN client never tries to refresh.
func TestStaticTokenNoRefresh(t *testing.T) {
	c := newClient(t)
	c.Token = "wrong"
	_, err := c.List(context.Background(), "")
	if !client.IsUnauthorized(err) {
		t.Fatalf("err = %v, want 401", err)
	}
}
