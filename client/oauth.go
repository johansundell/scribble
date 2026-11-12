package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuth2 login (#184, ADR 14). These match the server's oauth package.
const (
	// TokenPath is the server's token endpoint.
	TokenPath = "/oauth/token"
	// ClientCLI and ClientGUI are the OAuth2 client IDs of sn and
	// scribble-gui.
	ClientCLI = "scribble-cli"
	ClientGUI = "scribble-gui"
)

// refreshMargin is how long before it expires an access token is renewed,
// so it doesn't expire on the way to the server.
const refreshMargin = time.Minute

// defaultRefreshTTL is how long a refresh token is assumed to last when
// the server doesn't say (servers before #193): the server's default.
const defaultRefreshTTL = 365 * 24 * time.Hour

// Tokens is an OAuth2 login: the access token sent with each request, the
// refresh token that renews it, when the access token expires and when the
// refresh token does (zero when unknown).
type Tokens struct {
	Access        string
	Refresh       string
	Expiry        time.Time
	RefreshExpiry time.Time
}

// LoginError is the token endpoint refusing a login or a refresh.
type LoginError struct {
	Status      int
	Code        string // the OAuth2 error, e.g. "invalid_grant"
	Description string
}

func (e *LoginError) Error() string {
	if e.Description != "" {
		return "login refused: " + e.Description
	}
	if e.Code != "" {
		return "login refused: " + e.Code
	}
	return fmt.Sprintf("login refused: %d %s", e.Status, http.StatusText(e.Status))
}

// IsWrongCode reports whether a Login failed because the authenticator
// code is wrong or was already used.
func IsWrongCode(err error) bool {
	var e *LoginError
	return errors.As(err, &e) && e.Status == http.StatusBadRequest && e.Code == "invalid_grant"
}

// noOAuth is the error for a server without the token endpoint.
const noOAuth = "this server has no authenticator code login for apps; it needs scribble-server v2.3 or later, WEB_TOTP_SECRET and STORAGE=sqlite"

// Login trades an authenticator code for tokens, as ClientID, and starts
// using them. A wrong or used code gives an error IsWrongCode accepts.
func (c *Client) Login(ctx context.Context, code string) (Tokens, error) {
	tok, err := c.tokenRequest(ctx, url.Values{
		"grant_type": {"password"},
		// The server has one user and ignores the name.
		"username": {"scribble"},
		"password": {strings.TrimSpace(code)},
	})
	if err != nil {
		return Tokens{}, err
	}
	c.SetTokens(tok)
	return tok, nil
}

// SetTokens makes the client send tok.Access, renewed with tok.Refresh.
func (c *Client) SetTokens(tok Tokens) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	c.Token, c.RefreshToken, c.Expiry, c.RefreshExpiry = tok.Access, tok.Refresh, tok.Expiry, tok.RefreshExpiry
}

// Tokens returns the tokens the client is using.
func (c *Client) Tokens() Tokens {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	return Tokens{Access: c.Token, Refresh: c.RefreshToken, Expiry: c.Expiry, RefreshExpiry: c.RefreshExpiry}
}

// tokenRequest posts form to the token endpoint and returns the tokens it
// answers with.
func (c *Client) tokenRequest(ctx context.Context, form url.Values) (Tokens, error) {
	form.Set("client_id", c.ClientID)
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+TokenPath, strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	start := time.Now()
	resp, err := c.send(req)
	if err != nil {
		return Tokens{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Tokens{}, &LoginError{Status: resp.StatusCode, Description: noOAuth}
	}
	var body struct {
		Access      string `json:"access_token"`
		Refresh     string `json:"refresh_token"`
		ExpiresIn   int64  `json:"expires_in"`
		RefreshIn   int64  `json:"refresh_token_expires_in"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
	if resp.StatusCode != http.StatusOK {
		return Tokens{}, &LoginError{Status: resp.StatusCode, Code: body.Error, Description: body.Description}
	}
	if decodeErr != nil {
		return Tokens{}, fmt.Errorf("unreadable answer from the token endpoint: %w", decodeErr)
	}
	if body.Access == "" {
		return Tokens{}, errors.New("the token endpoint answered without an access token")
	}
	tok := Tokens{Access: body.Access, Refresh: body.Refresh}
	if body.ExpiresIn > 0 {
		tok.Expiry = start.Add(time.Duration(body.ExpiresIn) * time.Second)
	}
	if body.RefreshIn > 0 {
		tok.RefreshExpiry = start.Add(time.Duration(body.RefreshIn) * time.Second)
	} else {
		tok.RefreshExpiry = start.Add(defaultRefreshTTL)
	}
	return tok, nil
}

// bearer is the token to send, renewed first when it is about to expire.
func (c *Client) bearer(ctx context.Context) (string, error) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if c.RefreshToken != "" && !c.Expiry.IsZero() && time.Now().Add(refreshMargin).After(c.Expiry) {
		if err := c.refreshLocked(ctx); err != nil {
			return "", err
		}
	}
	return c.Token, nil
}

// Refresh trades the refresh token for new tokens now, whether or not the
// access token has expired, and hands them to TokensChanged. Without a
// refresh token, or when the server refuses it, the error is a 401 *Error,
// which IsUnauthorized accepts.
func (c *Client) Refresh(ctx context.Context) error {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if c.RefreshToken == "" {
		return &Error{Status: http.StatusUnauthorized, Message: "no refresh token; log in again"}
	}
	return c.refreshLocked(ctx)
}

// renew gets a new access token after the server refused used, and
// reports whether there is one to try. If another request renewed it
// meanwhile, that one is used.
func (c *Client) renew(ctx context.Context, used string) (string, bool, error) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if c.RefreshToken == "" {
		return "", false, nil
	}
	if c.Token != used {
		return c.Token, true, nil
	}
	if err := c.refreshLocked(ctx); err != nil {
		return "", false, err
	}
	return c.Token, true, nil
}

// refreshLocked trades the refresh token for new tokens and hands them to
// TokensChanged. A refused refresh token is a 401 *Error, so
// IsUnauthorized tells the caller to log in again. Call with authMu held.
func (c *Client) refreshLocked(ctx context.Context) error {
	tok, err := c.tokenRequest(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c.RefreshToken}})
	var le *LoginError
	if errors.As(err, &le) && le.Status >= 400 && le.Status < 500 {
		return &Error{Status: http.StatusUnauthorized, Message: "login expired: " + le.Error()}
	}
	if err != nil {
		return err
	}
	if tok.Refresh == "" {
		tok.Refresh, tok.RefreshExpiry = c.RefreshToken, c.RefreshExpiry
	}
	c.Token, c.RefreshToken, c.Expiry, c.RefreshExpiry = tok.Access, tok.Refresh, tok.Expiry, tok.RefreshExpiry
	if c.TokensChanged != nil {
		c.TokensChanged(tok)
	}
	return nil
}
