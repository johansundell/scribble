package router_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/oauth"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

// newOAuthRouter is the API with OAuth2 login on (#184) and authToken as
// AUTH_TOKEN ("" for none).
func newOAuthRouter(t *testing.T, authToken string) *gin.Engine {
	t.Helper()
	r, _ := newOAuthRouterWithSink(t, authToken)
	return r
}

func newOAuthRouterWithSink(t *testing.T, authToken string) (*gin.Engine, *recordingSink) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "oauth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a, err := auth.New(auth.Config{TOTPKey: webKey, Secret: []byte(strings.Repeat("k", 32)), SessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	o, err := oauth.New(oauth.Config{Auth: a, Tokens: s})
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	r, err := router.NewRouter(router.Config{
		Handler:  mustNewHandler(t, s, false, fstest.MapFS{}, "test", "dev"),
		LogSink:  sink,
		OAuth:    o,
		Settings: types.AppSettings{AuthToken: authToken},
		Assets:   fstest.MapFS{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, sink
}

func getNotes(r http.Handler, token string) int {
	req := httptest.NewRequest("GET", "/notes", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func oauthLogin(t *testing.T, r http.Handler) (access, refresh string) {
	t.Helper()
	form := url.Values{"grant_type": {"password"}, "client_id": {oauth.ClientCLI}, "username": {"me"}, "password": {auth.Code(webKey, time.Now())}}
	req := httptest.NewRequest("POST", oauth.TokenPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	var tok struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &tok); err != nil {
		t.Fatal(err)
	}
	return tok.Access, tok.Refresh
}

func TestOAuthTokenOpensTheAPI(t *testing.T) {
	r := newOAuthRouter(t, "")
	if got := getNotes(r, ""); got != http.StatusUnauthorized {
		t.Fatalf("no token: %d", got)
	}
	if got := getNotes(r, "made-up"); got != http.StatusUnauthorized {
		t.Fatalf("made-up token: %d", got)
	}
	access, refresh := oauthLogin(t, r)
	if got := getNotes(r, access); got != http.StatusOK {
		t.Fatalf("access token: %d", got)
	}
	if got := getNotes(r, refresh); got != http.StatusUnauthorized {
		t.Fatalf("refresh token used as an access token: %d", got)
	}
}

// AUTH_TOKEN still works beside OAuth, until it is removed (ADR 14).
func TestOAuthBesideAuthToken(t *testing.T) {
	r := newOAuthRouter(t, "static")
	if got := getNotes(r, "static"); got != http.StatusOK {
		t.Fatalf("AUTH_TOKEN: %d", got)
	}
	access, _ := oauthLogin(t, r)
	if got := getNotes(r, access); got != http.StatusOK {
		t.Fatalf("access token: %d", got)
	}
}

func TestOAuthTokenEndpointOffWithoutOAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r, err := router.NewRouter(router.Config{
		Handler:  mustNewHandler(t, nopStore{}, false, fstest.MapFS{}, "test", "dev"),
		LogSink:  &recordingSink{},
		Settings: types.AppSettings{AuthToken: "tok"},
		Assets:   fstest.MapFS{},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", oauth.TokenPath, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("token endpoint without OAuth: %d", w.Code)
	}
}

// The token endpoint is never request-logged: the log would keep codes
// and tokens.
func TestOAuthTokenEndpointNotLogged(t *testing.T) {
	r, sink := newOAuthRouterWithSink(t, "")
	oauthLogin(t, r)
	if n := len(sink.entries); n != 0 {
		t.Fatalf("%d request log entries for the token endpoint", n)
	}
}
