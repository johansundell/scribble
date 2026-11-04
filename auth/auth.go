// Package auth turns an authenticator app's TOTP code into a signed session
// cookie for the web app (ADR 5, ADR 13).
//
// Sessions are stateless: the cookie carries its expiry, signed with
// HMAC-SHA256, so nothing is stored server side and changing the secret logs
// everyone out. Wrong codes are rate limited per client address, and many
// wrong codes from any addresses raise the Login Alert and lock web login
// for everyone. The JSON API never accepts a code.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/johansundell/scribble/logging"
)

// CookieName is the name of the session cookie.
const CookieName = "scribble_session"

// cookiePath keeps the session cookie to the web app; the API never sees it.
const cookiePath = "/"

// Guessing limits (ADR 5). After DefaultMaxFailures wrong codes (or setup
// tokens) from one address within DefaultFailureWindow, that address is
// refused until the window has passed. After DefaultAlertFailures checked
// wrong codes from any addresses within DefaultAlertWindow, the Login Alert
// triggers and web login is locked for DefaultAlertWindow.
const (
	DefaultMaxFailures   = 5
	DefaultFailureWindow = 15 * time.Minute
	DefaultAlertFailures = 20
	DefaultAlertWindow   = 24 * time.Hour
)

var (
	// ErrInvalidCode means the code was checked and is wrong, or was
	// already used.
	ErrInvalidCode = errors.New("invalid code")
	// ErrInvalidToken means the setup token was checked and is wrong.
	ErrInvalidToken = errors.New("invalid token")
	// ErrRateLimited means the address has too many recent wrong codes or
	// tokens; the code or token was not checked.
	ErrRateLimited = errors.New("too many wrong attempts from this address")
	// ErrLocked means web login is locked for everyone after too many wrong
	// codes; the code was not checked.
	ErrLocked = errors.New("web login is locked")
)

// Config configures an Authenticator.
type Config struct {
	// TOTPKey is the decoded WEB_TOTP_SECRET; at least MinSecretBytes.
	TOTPKey []byte
	// SetupToken (AUTH_TOKEN) opens the setup page, which shows the TOTP
	// secret; "" keeps it closed.
	SetupToken string
	// Secret signs session cookies; at least 32 bytes.
	Secret []byte
	// SessionTTL is how long a session lasts. The expiry is fixed at login
	// and never extended.
	SessionTTL time.Duration
	// SecureCookie marks the cookie Secure (HTTPS only).
	SecureCookie bool
	// The guessing limits default to the Default* constants when zero.
	MaxFailures   int
	FailureWindow time.Duration
	AlertFailures int
	AlertWindow   time.Duration
	// Now defaults to time.Now.
	Now func() time.Time
	// Logger receives the warning when the Login Alert triggers; the
	// standard logger when nil.
	Logger logging.Logger
}

// Authenticator checks login codes and the setup token, and issues and
// verifies session cookies. It is safe for concurrent use.
type Authenticator struct {
	totp    *totp
	token   []byte
	secret  []byte
	ttl     time.Duration
	secure  bool
	now     func() time.Time
	limiter *limiter
	alerter *loginAlerter
}

// New validates cfg and returns an Authenticator.
func New(cfg Config) (*Authenticator, error) {
	if len(cfg.TOTPKey) < MinSecretBytes {
		return nil, fmt.Errorf("TOTP key must be at least %d bytes", MinSecretBytes)
	}
	if len(cfg.Secret) < 32 {
		return nil, errors.New("session secret must be at least 32 bytes")
	}
	if cfg.SessionTTL <= 0 {
		return nil, errors.New("session TTL must be positive")
	}
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = DefaultMaxFailures
	}
	if cfg.FailureWindow <= 0 {
		cfg.FailureWindow = DefaultFailureWindow
	}
	if cfg.AlertFailures <= 0 {
		cfg.AlertFailures = DefaultAlertFailures
	}
	if cfg.AlertWindow <= 0 {
		cfg.AlertWindow = DefaultAlertWindow
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Authenticator{
		totp:    newTOTP(cfg.TOTPKey),
		token:   []byte(cfg.SetupToken),
		secret:  append([]byte(nil), cfg.Secret...),
		ttl:     cfg.SessionTTL,
		secure:  cfg.SecureCookie,
		now:     cfg.Now,
		limiter: newLimiter(cfg.MaxFailures, cfg.FailureWindow),
		alerter: newLoginAlerter(cfg.AlertFailures, cfg.AlertWindow, cfg.Logger),
	}, nil
}

// Login checks code, a TOTP code, for a request from clientAddr. While web
// login is locked it returns ErrLocked, and while clientAddr is blocked
// ErrRateLimited, both without checking the code. A wrong code, or one
// whose step a login already used, returns ErrInvalidCode and counts
// towards the block, the Login Alert and the lock.
func (a *Authenticator) Login(clientAddr, code string) error {
	now := a.now()
	if a.alerter.locked(now) {
		return ErrLocked
	}
	if !a.limiter.reserve(clientAddr, now) {
		return ErrRateLimited
	}
	if a.totp.use(code, now) {
		a.limiter.reset(clientAddr)
		return nil
	}
	a.alerter.record(now)
	return ErrInvalidCode
}

// CheckCode reports whether code is right now, without logging in or using
// it up, so the setup page can show that an app makes the right codes.
// Only call it behind Setup.
func (a *Authenticator) CheckCode(code string) bool {
	return a.totp.check(code, a.now())
}

// Setup checks token, which opens the setup page, for a request from
// clientAddr. While clientAddr is blocked it returns ErrRateLimited without
// checking the token. A wrong token returns ErrInvalidToken and counts
// towards the block, but not the Login Alert.
func (a *Authenticator) Setup(clientAddr, token string) error {
	now := a.now()
	if !a.limiter.reserve(clientAddr, now) {
		return ErrRateLimited
	}
	if len(a.token) > 0 && subtle.ConstantTimeCompare([]byte(token), a.token) == 1 {
		a.limiter.reset(clientAddr)
		return nil
	}
	return ErrInvalidToken
}

// Secret is the TOTP secret in base32, for an authenticator app.
func (a *Authenticator) Secret() string {
	return EncodeSecret(a.totp.key)
}

// KeyURI is the otpauth:// URI an authenticator app reads from a QR code,
// with host naming this server in the app.
func (a *Authenticator) KeyURI(host string) string {
	return "otpauth://totp/" + url.PathEscape("Scribble:"+host) +
		"?secret=" + a.Secret() + "&issuer=Scribble"
}

// Locked reports whether web login is locked after too many wrong codes.
func (a *Authenticator) Locked() bool {
	return a.alerter.locked(a.now())
}

// LoginAlert returns the Login Alert, once it has triggered. It stays until
// the service restarts.
func (a *Authenticator) LoginAlert() (LoginAlert, bool) {
	return a.alerter.current()
}

// StartSession sets a session cookie on w that lasts the session TTL.
func (a *Authenticator) StartSession(w http.ResponseWriter) {
	expires := a.now().Add(a.ttl)
	c := a.cookie(a.sign(expires), int(a.ttl/time.Second))
	c.Expires = expires
	http.SetCookie(w, c)
}

// EndSession tells the browser on w to delete the session cookie. A copy of
// the cookie kept elsewhere stays valid until it expires.
func (a *Authenticator) EndSession(w http.ResponseWriter) {
	http.SetCookie(w, a.cookie("", -1))
}

// cookie builds the session cookie. Start and end share it because a browser
// only deletes a cookie whose name, path and domain match the one it holds.
func (a *Authenticator) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     cookiePath,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// Session reports whether r carries a valid, unexpired session cookie.
func (a *Authenticator) Session(r *http.Request) bool {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return false
	}
	return a.verify(c.Value)
}

// sessionPrefix is signed with the expiry, so a value signed with the same
// secret for another purpose can't pass as a session.
const sessionPrefix = "session|"

// sign encodes "session|unix-expiry" and its HMAC as base64url, joined by ".".
func (a *Authenticator) sign(expires time.Time) string {
	payload := []byte(sessionPrefix + strconv.FormatInt(expires.Unix(), 10))
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(a.mac(payload))
}

func (a *Authenticator) verify(value string) bool {
	encPayload, encSig, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(encPayload)
	if err != nil {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(encSig)
	if err != nil || !hmac.Equal(sig, a.mac(payload)) {
		return false
	}
	expStr, ok := strings.CutPrefix(string(payload), sessionPrefix)
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	return err == nil && a.now().Before(time.Unix(exp, 0))
}

func (a *Authenticator) mac(payload []byte) []byte {
	m := hmac.New(sha256.New, a.secret)
	m.Write(payload)
	return m.Sum(nil)
}
