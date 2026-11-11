package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// OAuthToken is an OAuth2 access token and its refresh token as stored for
// API clients (#184, ADR 14).
type OAuthToken struct {
	Access  string
	Refresh string // "" when there is none
	// Data is the token's details, as the oauth package encodes them.
	Data []byte
	// ExpiresAt is when neither token is of any use any more.
	ExpiresAt time.Time
}

// OAuthTokenStore keeps OAuth2 tokens, so API clients stay logged in when
// the server restarts. Only SQLite has one.
type OAuthTokenStore interface {
	// SaveOAuthToken stores t, replacing a token with the same access
	// token, and drops the tokens that have expired.
	SaveOAuthToken(ctx context.Context, t OAuthToken) error
	// OAuthTokenByAccess returns the Data of the token with this access
	// token, and false when there is none.
	OAuthTokenByAccess(ctx context.Context, access string) ([]byte, bool, error)
	// OAuthTokenByRefresh is OAuthTokenByAccess by refresh token.
	OAuthTokenByRefresh(ctx context.Context, refresh string) ([]byte, bool, error)
	// DeleteOAuthTokenByAccess deletes the token with this access token,
	// if there is one.
	DeleteOAuthTokenByAccess(ctx context.Context, access string) error
	// DeleteOAuthTokenByRefresh deletes the token with this refresh
	// token, if there is one.
	DeleteOAuthTokenByRefresh(ctx context.Context, refresh string) error
}

var errNoOAuthTokens = errors.New("OAuth tokens need STORAGE=sqlite")

func (s *SQLStore) SaveOAuthToken(ctx context.Context, t OAuthToken) error {
	if !s.notesEnabled {
		return errNoOAuthTokens
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE expires_at < ?`, s.timeArg(time.Now())); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO oauth_tokens (access, refresh, data, expires_at) VALUES (?, ?, ?, ?)`,
		t.Access, t.Refresh, string(t.Data), s.timeArg(t.ExpiresAt)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStore) OAuthTokenByAccess(ctx context.Context, access string) ([]byte, bool, error) {
	return s.oauthToken(ctx, `SELECT data FROM oauth_tokens WHERE access = ?`, access)
}

func (s *SQLStore) OAuthTokenByRefresh(ctx context.Context, refresh string) ([]byte, bool, error) {
	if refresh == "" {
		return nil, false, nil
	}
	return s.oauthToken(ctx, `SELECT data FROM oauth_tokens WHERE refresh = ?`, refresh)
}

func (s *SQLStore) oauthToken(ctx context.Context, query, key string) ([]byte, bool, error) {
	if !s.notesEnabled {
		return nil, false, errNoOAuthTokens
	}
	var data string
	err := s.db.QueryRowContext(ctx, query, key).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return []byte(data), true, nil
}

func (s *SQLStore) DeleteOAuthTokenByAccess(ctx context.Context, access string) error {
	return s.deleteOAuthToken(ctx, `DELETE FROM oauth_tokens WHERE access = ?`, access)
}

func (s *SQLStore) DeleteOAuthTokenByRefresh(ctx context.Context, refresh string) error {
	if refresh == "" {
		return nil
	}
	return s.deleteOAuthToken(ctx, `DELETE FROM oauth_tokens WHERE refresh = ?`, refresh)
}

func (s *SQLStore) deleteOAuthToken(ctx context.Context, query, key string) error {
	if !s.notesEnabled {
		return errNoOAuthTokens
	}
	_, err := s.db.ExecContext(ctx, query, key)
	return err
}
