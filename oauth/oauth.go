// Package oauth is the OAuth2 server API clients log in with (#184,
// ADR 14): the sn CLI and the desktop GUI trade an authenticator code for
// an access token and a refresh token at TokenPath, with the Resource Owner
// Password Credentials Grant, and later trade the refresh token for new
// ones. The tokens are kept in the database, so a restart logs nobody out.
//
// The code is checked by the web app's auth.Authenticator, so the same
// per-address limit, Login Alert and replay protection hold.
package oauth

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-oauth2/oauth2/v4"
	"github.com/go-oauth2/oauth2/v4/errors"
	"github.com/go-oauth2/oauth2/v4/manage"
	"github.com/go-oauth2/oauth2/v4/models"
	"github.com/go-oauth2/oauth2/v4/server"
	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/store"
)

// TokenPath is where clients get tokens.
const TokenPath = "/oauth/token"

// The public clients, which need no secret.
const (
	ClientCLI = "scribble-cli"
	ClientGUI = "scribble-gui"
)

// Default token lifetimes. A refresh renews both.
const (
	DefaultAccessTTL  = 24 * time.Hour
	DefaultRefreshTTL = 365 * 24 * time.Hour
)

// userID is the one user tokens are issued to.
const userID = "owner"

// Config configures a Server.
type Config struct {
	// Auth checks the authenticator codes.
	Auth *auth.Authenticator
	// Tokens keeps the issued tokens.
	Tokens store.OAuthTokenStore
	// AccessTTL and RefreshTTL default to DefaultAccessTTL and
	// DefaultRefreshTTL when zero.
	AccessTTL, RefreshTTL time.Duration
}

// Server issues and checks tokens. It is safe for concurrent use.
type Server struct {
	srv     *server.Server
	manager *manage.Manager
	// mu runs one token request at a time, so two refreshes with the same
	// refresh token can't both succeed.
	mu sync.Mutex
}

// clientAddrKey carries the client's address to the password handler.
type clientAddrKey struct{}

// New returns a Server for cfg.
func New(cfg Config) (*Server, error) {
	if cfg.Auth == nil {
		return nil, stderrors.New("oauth: Auth is required")
	}
	if cfg.Tokens == nil {
		return nil, stderrors.New("oauth: Tokens is required")
	}
	if cfg.AccessTTL <= 0 {
		cfg.AccessTTL = DefaultAccessTTL
	}
	if cfg.RefreshTTL <= 0 {
		cfg.RefreshTTL = DefaultRefreshTTL
	}

	m := manage.NewDefaultManager()
	m.SetPasswordTokenCfg(&manage.Config{AccessTokenExp: cfg.AccessTTL, RefreshTokenExp: cfg.RefreshTTL, IsGenerateRefresh: true})
	m.SetRefreshTokenCfg(&manage.RefreshingConfig{
		AccessTokenExp:     cfg.AccessTTL,
		RefreshTokenExp:    cfg.RefreshTTL,
		IsGenerateRefresh:  true,
		IsResetRefreshTime: true,
		IsRemoveAccess:     true,
		IsRemoveRefreshing: true,
	})
	m.MapTokenStorage(tokenStore{cfg.Tokens})
	m.MapClientStorage(clientStore{})

	sc := server.NewConfig()
	sc.AllowedGrantTypes = []oauth2.GrantType{oauth2.PasswordCredentials, oauth2.Refreshing}
	sc.AllowedResponseTypes = nil
	srv := server.NewServer(sc, m)
	// A public client sends its ID in the form. An unknown one is refused
	// here, before the password handler uses up the code.
	srv.SetClientInfoHandler(func(r *http.Request) (string, string, error) {
		id, secret, err := server.ClientFormHandler(r)
		if err != nil {
			return "", "", err
		}
		if !knownClient(id) {
			return "", "", errors.ErrInvalidClient
		}
		return id, secret, nil
	})
	srv.SetPasswordAuthorizationHandler(func(ctx context.Context, _, _, password string) (string, error) {
		addr, _ := ctx.Value(clientAddrKey{}).(string)
		if err := cfg.Auth.Login(addr, password); err != nil {
			if stderrors.Is(err, auth.ErrInvalidCode) {
				return "", errors.ErrInvalidGrant
			}
			return "", err
		}
		return userID, nil
	})
	srv.SetInternalErrorHandler(func(err error) *errors.Response {
		switch {
		case stderrors.Is(err, auth.ErrRateLimited):
			return &errors.Response{Error: errors.ErrInvalidGrant, Description: "too many wrong codes from this address; try again later", StatusCode: http.StatusTooManyRequests}
		case stderrors.Is(err, auth.ErrLocked):
			return &errors.Response{Error: errors.ErrInvalidGrant, Description: "login is locked after too many wrong codes", StatusCode: http.StatusTooManyRequests}
		}
		return nil
	})
	// RFC 6749 §5.2 answers invalid_grant with 400; the library uses 401,
	// which a client takes for a rejected access token.
	srv.SetResponseErrorHandler(func(re *errors.Response) {
		if re.Error == errors.ErrInvalidGrant && re.StatusCode == http.StatusUnauthorized {
			re.StatusCode = http.StatusBadRequest
		}
	})
	return &Server{srv: srv, manager: m}, nil
}

// HandleToken answers a token request, r, from clientAddr, which keys the
// limit on wrong codes.
func (s *Server) HandleToken(w http.ResponseWriter, r *http.Request, clientAddr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r = r.WithContext(context.WithValue(r.Context(), clientAddrKey{}, clientAddr))
	// It writes its own error answers; what it returns is only a failure
	// to write them.
	_ = s.srv.HandleTokenRequest(w, r)
}

// ValidAccessToken reports whether access is an access token this server
// issued that has not expired or been replaced.
func (s *Server) ValidAccessToken(ctx context.Context, access string) bool {
	if access == "" {
		return false
	}
	ti, err := s.manager.LoadAccessToken(ctx, access)
	return err == nil && ti != nil
}

func knownClient(id string) bool {
	return id == ClientCLI || id == ClientGUI
}

// clientStore knows the two public clients.
type clientStore struct{}

func (clientStore) GetByID(_ context.Context, id string) (oauth2.ClientInfo, error) {
	if !knownClient(id) {
		return nil, errors.ErrInvalidClient
	}
	return &models.Client{ID: id, Public: true}, nil
}

// tokenStore keeps tokens in a store.OAuthTokenStore as JSON.
type tokenStore struct{ s store.OAuthTokenStore }

func (t tokenStore) Create(ctx context.Context, info oauth2.TokenInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	expires := info.GetAccessCreateAt().Add(info.GetAccessExpiresIn())
	if info.GetRefresh() != "" {
		expires = info.GetRefreshCreateAt().Add(info.GetRefreshExpiresIn())
	}
	return t.s.SaveOAuthToken(ctx, store.OAuthToken{
		Access:    info.GetAccess(),
		Refresh:   info.GetRefresh(),
		Data:      data,
		ExpiresAt: expires,
	})
}

func (t tokenStore) RemoveByAccess(ctx context.Context, access string) error {
	return t.s.DeleteOAuthTokenByAccess(ctx, access)
}

func (t tokenStore) RemoveByRefresh(ctx context.Context, refresh string) error {
	return t.s.DeleteOAuthTokenByRefresh(ctx, refresh)
}

func (t tokenStore) GetByAccess(ctx context.Context, access string) (oauth2.TokenInfo, error) {
	return decode(t.s.OAuthTokenByAccess(ctx, access))
}

func (t tokenStore) GetByRefresh(ctx context.Context, refresh string) (oauth2.TokenInfo, error) {
	return decode(t.s.OAuthTokenByRefresh(ctx, refresh))
}

// Authorization codes are not used.
func (tokenStore) RemoveByCode(context.Context, string) error { return nil }
func (tokenStore) GetByCode(context.Context, string) (oauth2.TokenInfo, error) {
	return nil, nil
}

func decode(data []byte, ok bool, err error) (oauth2.TokenInfo, error) {
	if err != nil || !ok {
		return nil, err
	}
	tok := models.NewToken()
	if err := json.Unmarshal(data, tok); err != nil {
		return nil, err
	}
	return tok, nil
}
