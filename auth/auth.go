// Package auth turns the web app's PIN into a signed session cookie
// (ADR 5).
//
// Sessions are stateless: the cookie carries its expiry, signed with
// HMAC-SHA256, so nothing is stored server side and changing the secret logs
// everyone out. Wrong PINs are rate limited per client address, and many
// wrong PINs from any addresses raise the PIN Alert and lock PIN login for
// everyone. The JSON API never accepts the PIN.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/johansundell/scribble/logging"
)

// CookieName is the name of the session cookie.
const CookieName = "scribble_session"

// cookiePath keeps the session cookie to the web app; the API never sees it.
const cookiePath = "/app"

// Guessing limits (ADR 5). After DefaultMaxFailures wrong PINs from one
// address within DefaultFailureWindow, that address is refused until the
// window has passed. After DefaultAlertFailures checked wrong PINs from any
// addresses within DefaultAlertWindow, the PIN Alert triggers and PIN login
// is locked for DefaultAlertWindow.
const (
	DefaultMaxFailures   = 5
	DefaultFailureWindow = 15 * time.Minute
	DefaultAlertFailures = 20
	DefaultAlertWindow   = 24 * time.Hour
)

var (
	// ErrInvalidPIN means the PIN was checked and is wrong.
	ErrInvalidPIN = errors.New("invalid PIN")
	// ErrRateLimited means the address has too many recent wrong PINs; the
	// PIN was not checked.
	ErrRateLimited = errors.New("too many wrong PINs from this address")
	// ErrLocked means PIN login is locked for everyone after too many wrong
	// PINs; the PIN was not checked.
	ErrLocked = errors.New("PIN login is locked")
)

// Config configures an Authenticator.
type Config struct {
	PIN string
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
	// Logger receives the warning when the PIN Alert triggers; the standard
	// logger when nil.
	Logger logging.Logger
}

// Authenticator checks the PIN and issues and verifies session cookies. It
// is safe for concurrent use.
type Authenticator struct {
	pin     []byte
	secret  []byte
	ttl     time.Duration
	secure  bool
	now     func() time.Time
	limiter *limiter
	alerter *pinAlerter
}

// New validates cfg and returns an Authenticator.
func New(cfg Config) (*Authenticator, error) {
	if cfg.PIN == "" {
		return nil, errors.New("PIN must be set")
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
		pin:     []byte(cfg.PIN),
		secret:  append([]byte(nil), cfg.Secret...),
		ttl:     cfg.SessionTTL,
		secure:  cfg.SecureCookie,
		now:     cfg.Now,
		limiter: newLimiter(cfg.MaxFailures, cfg.FailureWindow),
		alerter: newPINAlerter(cfg.AlertFailures, cfg.AlertWindow, cfg.Logger),
	}, nil
}

// Login checks pin for a request from clientAddr. While PIN login is locked
// it returns ErrLocked, and while clientAddr is blocked ErrRateLimited, both
// without checking the PIN. A wrong PIN returns ErrInvalidPIN and counts
// towards the block, the PIN Alert and the lock.
func (a *Authenticator) Login(clientAddr, pin string) error {
	now := a.now()
	if a.alerter.locked(now) {
		return ErrLocked
	}
	if !a.limiter.reserve(clientAddr, now) {
		return ErrRateLimited
	}
	if subtle.ConstantTimeCompare([]byte(pin), a.pin) == 1 {
		a.limiter.reset(clientAddr)
		return nil
	}
	a.alerter.record(now)
	return ErrInvalidPIN
}

// Locked reports whether PIN login is locked after too many wrong PINs.
func (a *Authenticator) Locked() bool {
	return a.alerter.locked(a.now())
}

// PINAlert returns the PIN Alert, once it has triggered. It stays until the
// service restarts.
func (a *Authenticator) PINAlert() (PINAlert, bool) {
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
