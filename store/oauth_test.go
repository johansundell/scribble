package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/johansundell/scribble/store"
)

func newOAuthStore(t *testing.T) *store.SQLStore {
	t.Helper()
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "oauth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOAuthTokenSaveAndLoad(t *testing.T) {
	s := newOAuthStore(t)
	ctx := context.Background()
	exp := time.Now().Add(time.Hour)
	if err := s.SaveOAuthToken(ctx, store.OAuthToken{Access: "a1", Refresh: "r1", Data: []byte(`{"x":1}`), ExpiresAt: exp}); err != nil {
		t.Fatal(err)
	}

	data, ok, err := s.OAuthTokenByAccess(ctx, "a1")
	if err != nil || !ok || string(data) != `{"x":1}` {
		t.Fatalf("by access = %q, %v, %v", data, ok, err)
	}
	data, ok, err = s.OAuthTokenByRefresh(ctx, "r1")
	if err != nil || !ok || string(data) != `{"x":1}` {
		t.Fatalf("by refresh = %q, %v, %v", data, ok, err)
	}
	if _, ok, err := s.OAuthTokenByAccess(ctx, "r1"); err != nil || ok {
		t.Fatalf("a refresh token found as an access token: %v, %v", ok, err)
	}
	if _, ok, err := s.OAuthTokenByRefresh(ctx, ""); err != nil || ok {
		t.Fatalf("empty refresh token found: %v, %v", ok, err)
	}
}

func TestOAuthTokenDelete(t *testing.T) {
	s := newOAuthStore(t)
	ctx := context.Background()
	exp := time.Now().Add(time.Hour)
	for _, tok := range []store.OAuthToken{
		{Access: "a1", Refresh: "r1", Data: []byte("1"), ExpiresAt: exp},
		{Access: "a2", Refresh: "r2", Data: []byte("2"), ExpiresAt: exp},
	} {
		if err := s.SaveOAuthToken(ctx, tok); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteOAuthTokenByAccess(ctx, "a1"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteOAuthTokenByRefresh(ctx, "r2"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteOAuthTokenByRefresh(ctx, "missing"); err != nil {
		t.Fatalf("deleting a missing token: %v", err)
	}
	for _, access := range []string{"a1", "a2"} {
		if _, ok, err := s.OAuthTokenByAccess(ctx, access); err != nil || ok {
			t.Errorf("%s still there: %v, %v", access, ok, err)
		}
	}
}

// Saving a token drops the rows that have expired, so the table doesn't
// grow with every login.
func TestOAuthTokenSaveDropsExpired(t *testing.T) {
	s := newOAuthStore(t)
	ctx := context.Background()
	if err := s.SaveOAuthToken(ctx, store.OAuthToken{Access: "old", Refresh: "r-old", Data: []byte("1"), ExpiresAt: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOAuthToken(ctx, store.OAuthToken{Access: "new", Data: []byte("2"), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.OAuthTokenByAccess(ctx, "old"); ok {
		t.Error("expired token kept")
	}
	if _, ok, _ := s.OAuthTokenByAccess(ctx, "new"); !ok {
		t.Error("new token missing")
	}
}

// Two tokens without a refresh token can both be saved: the empty refresh
// token is not unique.
func TestOAuthTokenWithoutRefresh(t *testing.T) {
	s := newOAuthStore(t)
	ctx := context.Background()
	exp := time.Now().Add(time.Hour)
	for _, access := range []string{"a1", "a2"} {
		if err := s.SaveOAuthToken(ctx, store.OAuthToken{Access: access, Data: []byte("x"), ExpiresAt: exp}); err != nil {
			t.Fatalf("save %s: %v", access, err)
		}
	}
}
