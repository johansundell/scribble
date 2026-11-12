package oauth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/oauth"
	"github.com/johansundell/scribble/store"
)

var testKey = []byte("0123456789abcdef0123")

type fixture struct {
	srv    *oauth.Server
	tokens *store.SQLStore
	cfg    oauth.Config
}

func newFixture(t *testing.T, mod func(*oauth.Config)) *fixture {
	t.Helper()
	st, err := store.NewSQLite(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a, err := auth.New(auth.Config{TOTPKey: testKey, Secret: []byte(strings.Repeat("s", 32)), SessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	cfg := oauth.Config{Auth: a, Tokens: st}
	if mod != nil {
		mod(&cfg)
	}
	srv, err := oauth.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{srv: srv, tokens: st, cfg: cfg}
}

type tokenAnswer struct {
	Status    int
	Access    string  `json:"access_token"`
	Refresh   string  `json:"refresh_token"`
	Type      string  `json:"token_type"`
	ExpiresIn float64 `json:"expires_in"`
	// RefreshIn is refresh_token_expires_in (#193).
	RefreshIn   float64 `json:"refresh_token_expires_in"`
	Error       string  `json:"error"`
	Description string  `json:"error_description"`
}

func (f *fixture) post(t *testing.T, addr string, form url.Values) tokenAnswer {
	t.Helper()
	r := httptest.NewRequest("POST", oauth.TokenPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.srv.HandleToken(w, r, addr)
	var a tokenAnswer
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatalf("answer %d %q is not JSON: %v", w.Code, w.Body, err)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	a.Status = w.Code
	return a
}

func passwordForm(client, code string) url.Values {
	return url.Values{"grant_type": {"password"}, "client_id": {client}, "username": {"me"}, "password": {code}}
}

func refreshForm(client, refresh string) url.Values {
	return url.Values{"grant_type": {"refresh_token"}, "client_id": {client}, "refresh_token": {refresh}}
}

func code() string { return auth.Code(testKey, time.Now()) }

func (f *fixture) valid(token string) bool {
	return f.srv.ValidAccessToken(context.Background(), token)
}

func TestPasswordGrantIssuesTokens(t *testing.T) {
	f := newFixture(t, nil)
	a := f.post(t, "1.2.3.4", passwordForm(oauth.ClientCLI, code()))
	if a.Status != http.StatusOK || a.Access == "" || a.Refresh == "" {
		t.Fatalf("answer = %+v", a)
	}
	if a.Type != "Bearer" || a.ExpiresIn != (24*time.Hour).Seconds() {
		t.Errorf("type %q expires_in %v, want Bearer and 24h", a.Type, a.ExpiresIn)
	}
	if a.RefreshIn != (365 * 24 * time.Hour).Seconds() {
		t.Errorf("refresh_token_expires_in %v, want 365 days", a.RefreshIn)
	}
	if !f.valid(a.Access) {
		t.Error("issued access token is not valid")
	}
	if f.valid(a.Refresh) || f.valid("") || f.valid("nonsense") {
		t.Error("a refresh token, empty or unknown token passes as an access token")
	}
}

func TestPasswordGrantGUIClient(t *testing.T) {
	f := newFixture(t, nil)
	if a := f.post(t, "1.2.3.4", passwordForm(oauth.ClientGUI, code())); a.Status != http.StatusOK {
		t.Fatalf("answer = %+v", a)
	}
}

func TestPasswordGrantWrongCode(t *testing.T) {
	f := newFixture(t, nil)
	wrong := "000000"
	if wrong == code() {
		wrong = "111111"
	}
	a := f.post(t, "1.2.3.4", passwordForm(oauth.ClientCLI, wrong))
	if a.Status != http.StatusBadRequest || a.Error != "invalid_grant" || a.Access != "" {
		t.Fatalf("answer = %+v", a)
	}
}

// A code logs in once: the replay protection the web login has.
func TestPasswordGrantCodeUsedOnce(t *testing.T) {
	f := newFixture(t, nil)
	c := code()
	if a := f.post(t, "1.2.3.4", passwordForm(oauth.ClientCLI, c)); a.Status != http.StatusOK {
		t.Fatalf("first login: %+v", a)
	}
	if a := f.post(t, "1.2.3.4", passwordForm(oauth.ClientCLI, c)); a.Status != http.StatusBadRequest {
		t.Fatalf("second login with the same code: %+v", a)
	}
}

func TestPasswordGrantRateLimited(t *testing.T) {
	f := newFixture(t, nil)
	for i := 0; i < auth.DefaultMaxFailures; i++ {
		if a := f.post(t, "1.2.3.4", passwordForm(oauth.ClientCLI, "abcdef")); a.Status != http.StatusBadRequest {
			t.Fatalf("wrong code %d: %+v", i+1, a)
		}
	}
	a := f.post(t, "1.2.3.4", passwordForm(oauth.ClientCLI, code()))
	if a.Status != http.StatusTooManyRequests || a.Access != "" || a.Description == "" {
		t.Fatalf("right code while blocked: %+v", a)
	}
	// Another address is not blocked.
	if a := f.post(t, "5.6.7.8", passwordForm(oauth.ClientCLI, code())); a.Status != http.StatusOK {
		t.Fatalf("other address: %+v", a)
	}
}

func TestUnknownClient(t *testing.T) {
	f := newFixture(t, nil)
	for _, client := range []string{"", "someone-else"} {
		a := f.post(t, "1.2.3.4", passwordForm(client, code()))
		if a.Error != "invalid_client" || a.Access != "" {
			t.Errorf("client %q: %+v", client, a)
		}
	}
}

func TestOtherGrantTypesRefused(t *testing.T) {
	f := newFixture(t, nil)
	a := f.post(t, "1.2.3.4", url.Values{"grant_type": {"client_credentials"}, "client_id": {oauth.ClientCLI}})
	if a.Status == http.StatusOK || a.Access != "" {
		t.Fatalf("client_credentials: %+v", a)
	}
}

func TestRefreshGrantRollsTokens(t *testing.T) {
	f := newFixture(t, nil)
	first := f.post(t, "1.2.3.4", passwordForm(oauth.ClientCLI, code()))
	second := f.post(t, "1.2.3.4", refreshForm(oauth.ClientCLI, first.Refresh))
	if second.Status != http.StatusOK || second.Access == "" || second.Refresh == "" {
		t.Fatalf("refresh: %+v", second)
	}
	if second.RefreshIn != (365 * 24 * time.Hour).Seconds() {
		t.Errorf("refresh_token_expires_in %v after a refresh, want 365 days", second.RefreshIn)
	}
	if second.Access == first.Access || second.Refresh == first.Refresh {
		t.Error("refresh did not issue new tokens")
	}
	if !f.valid(second.Access) || f.valid(first.Access) {
		t.Error("want only the new access token valid")
	}
	// The old refresh token is used up.
	if a := f.post(t, "1.2.3.4", refreshForm(oauth.ClientCLI, first.Refresh)); a.Status != http.StatusBadRequest || a.Error != "invalid_grant" {
		t.Fatalf("reused refresh token: %+v", a)
	}
	if a := f.post(t, "1.2.3.4", refreshForm(oauth.ClientCLI, "nonsense")); a.Status != http.StatusBadRequest {
		t.Fatalf("unknown refresh token: %+v", a)
	}
}

func TestExpiredAccessTokenRefreshes(t *testing.T) {
	f := newFixture(t, func(c *oauth.Config) { c.AccessTTL = time.Nanosecond })
	first := f.post(t, "1.2.3.4", passwordForm(oauth.ClientCLI, code()))
	if first.Status != http.StatusOK {
		t.Fatalf("login: %+v", first)
	}
	if f.valid(first.Access) {
		t.Fatal("expired access token is valid")
	}
	if a := f.post(t, "1.2.3.4", refreshForm(oauth.ClientCLI, first.Refresh)); a.Status != http.StatusOK {
		t.Fatalf("refresh after the access token expired: %+v", a)
	}
}

func TestExpiredRefreshToken(t *testing.T) {
	f := newFixture(t, func(c *oauth.Config) { c.RefreshTTL = time.Nanosecond })
	first := f.post(t, "1.2.3.4", passwordForm(oauth.ClientCLI, code()))
	if a := f.post(t, "1.2.3.4", refreshForm(oauth.ClientCLI, first.Refresh)); a.Status != http.StatusBadRequest || a.Error != "invalid_grant" {
		t.Fatalf("expired refresh token: %+v", a)
	}
}

// Tokens are in the database, so a restarted server still knows them.
func TestTokensSurviveRestart(t *testing.T) {
	f := newFixture(t, nil)
	a := f.post(t, "1.2.3.4", passwordForm(oauth.ClientCLI, code()))
	again, err := oauth.New(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !again.ValidAccessToken(context.Background(), a.Access) {
		t.Fatal("token lost on restart")
	}
}

func TestNewNeedsAuthAndTokens(t *testing.T) {
	f := newFixture(t, nil)
	if _, err := oauth.New(oauth.Config{Tokens: f.tokens}); err == nil {
		t.Error("no Auth accepted")
	}
	if _, err := oauth.New(oauth.Config{Auth: f.cfg.Auth}); err == nil {
		t.Error("no Tokens accepted")
	}
}
