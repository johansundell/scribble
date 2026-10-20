package auth_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/johansundell/scribble/auth"
)

const (
	testPIN    = "123456"
	testSecret = "0123456789abcdef0123456789abcdef"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type recLogger struct {
	mu       sync.Mutex
	warnings []string
}

func (l *recLogger) Infof(string, ...interface{}) {}
func (l *recLogger) Warningf(format string, v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warnings = append(l.warnings, fmt.Sprintf(format, v...))
}
func (l *recLogger) Errorf(string, ...interface{}) {}

func newAuth(t *testing.T, mod func(*auth.Config)) (*auth.Authenticator, *clock) {
	t.Helper()
	clk := &clock{t: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
	cfg := auth.Config{
		PIN:          testPIN,
		Secret:       []byte(testSecret),
		SessionTTL:   time.Hour,
		SecureCookie: true,
		Now:          clk.Now,
		Logger:       &recLogger{},
	}
	if mod != nil {
		mod(&cfg)
	}
	a, err := auth.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a, clk
}

// login starts a session and returns the cookie it set.
func login(t *testing.T, a *auth.Authenticator) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	a.StartSession(w)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %v", cookies)
	}
	return cookies[0]
}

func hasSession(a *auth.Authenticator, c *http.Cookie) bool {
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(c)
	return a.Session(r)
}

func TestNew_Validates(t *testing.T) {
	base := auth.Config{PIN: testPIN, Secret: []byte(testSecret), SessionTTL: time.Hour}
	for name, mod := range map[string]func(*auth.Config){
		"no PIN":       func(c *auth.Config) { c.PIN = "" },
		"short secret": func(c *auth.Config) { c.Secret = []byte("short") },
		"no TTL":       func(c *auth.Config) { c.SessionTTL = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			mod(&cfg)
			if _, err := auth.New(cfg); err == nil {
				t.Error("expected an error")
			}
		})
	}
	if _, err := auth.New(base); err != nil {
		t.Errorf("valid config: %v", err)
	}
}

func TestLogin(t *testing.T) {
	a, _ := newAuth(t, nil)
	if err := a.Login("1.1.1.1", testPIN); err != nil {
		t.Errorf("right PIN: %v", err)
	}
	if err := a.Login("1.1.1.1", "654321"); !errors.Is(err, auth.ErrInvalidPIN) {
		t.Errorf("wrong PIN: got %v, want ErrInvalidPIN", err)
	}
	if err := a.Login("1.1.1.1", testPIN+"0"); !errors.Is(err, auth.ErrInvalidPIN) {
		t.Errorf("PIN with an extra digit: got %v, want ErrInvalidPIN", err)
	}
}

func TestSessionCookie(t *testing.T) {
	a, clk := newAuth(t, nil)
	c := login(t, a)

	if c.Name != auth.CookieName || c.Path != "/" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie attributes: %+v", c)
	}
	if c.MaxAge != 3600 {
		t.Errorf("MaxAge = %d, want 3600", c.MaxAge)
	}
	if !hasSession(a, c) {
		t.Fatal("fresh cookie rejected")
	}

	// The expiry is fixed at login: using the session doesn't extend it.
	clk.Add(59 * time.Minute)
	if !hasSession(a, c) {
		t.Fatal("cookie rejected before expiry")
	}
	clk.Add(time.Minute)
	if hasSession(a, c) {
		t.Error("cookie accepted at its expiry")
	}
}

func TestSessionCookie_InsecureForLocalTesting(t *testing.T) {
	a, _ := newAuth(t, func(c *auth.Config) { c.SecureCookie = false })
	if c := login(t, a); c.Secure {
		t.Error("cookie is Secure with SecureCookie false")
	}
}

func TestSession_RejectsForgedCookies(t *testing.T) {
	a, clk := newAuth(t, nil)
	other, _ := newAuth(t, func(c *auth.Config) {
		c.Secret = []byte(strings.Repeat("x", 32))
		c.Now = clk.Now
	})

	if hasSession(a, login(t, other)) {
		t.Error("cookie signed with another secret accepted")
	}

	good := login(t, a)
	payload, sig, _ := strings.Cut(good.Value, ".")
	for name, value := range map[string]string{
		"empty":          "",
		"no signature":   payload,
		"edited payload": "x" + payload + "." + sig,
		"edited sig":     payload + "." + sig[:len(sig)-2] + "AA",
	} {
		c := *good
		c.Value = value
		if hasSession(a, &c) {
			t.Errorf("%s: cookie accepted", name)
		}
	}
	if hasSession(a, &http.Cookie{Name: "other", Value: good.Value}) {
		t.Error("cookie with another name accepted")
	}
}

func TestEndSession(t *testing.T) {
	a, _ := newAuth(t, nil)
	w := httptest.NewRecorder()
	a.EndSession(w)
	c := w.Result().Cookies()
	if len(c) != 1 || c[0].Name != auth.CookieName || c[0].Path != "/" || c[0].MaxAge >= 0 || c[0].Value != "" {
		t.Errorf("EndSession cookie: %+v", c)
	}
}

func TestLogin_PerAddressLimit(t *testing.T) {
	a, clk := newAuth(t, nil)

	for i := 0; i < auth.DefaultMaxFailures; i++ {
		if err := a.Login("1.1.1.1", "000000"); !errors.Is(err, auth.ErrInvalidPIN) {
			t.Fatalf("attempt %d: got %v, want ErrInvalidPIN", i+1, err)
		}
	}
	// The 6th attempt is refused without the PIN being checked, so even the
	// right PIN gets nowhere.
	if err := a.Login("1.1.1.1", testPIN); !errors.Is(err, auth.ErrRateLimited) {
		t.Errorf("6th attempt: got %v, want ErrRateLimited", err)
	}
	if err := a.Login("2.2.2.2", testPIN); err != nil {
		t.Errorf("another address: %v", err)
	}

	clk.Add(auth.DefaultFailureWindow - time.Second)
	if err := a.Login("1.1.1.1", testPIN); !errors.Is(err, auth.ErrRateLimited) {
		t.Errorf("before the window ends: got %v, want ErrRateLimited", err)
	}
	clk.Add(time.Second)
	if err := a.Login("1.1.1.1", testPIN); err != nil {
		t.Errorf("after the window: %v", err)
	}
}

func TestLogin_RightPINResetsAddress(t *testing.T) {
	a, _ := newAuth(t, nil)
	for i := 0; i < auth.DefaultMaxFailures-1; i++ {
		_ = a.Login("1.1.1.1", "000000")
	}
	if err := a.Login("1.1.1.1", testPIN); err != nil {
		t.Fatalf("right PIN: %v", err)
	}
	for i := 0; i < auth.DefaultMaxFailures; i++ {
		if err := a.Login("1.1.1.1", "000000"); !errors.Is(err, auth.ErrInvalidPIN) {
			t.Fatalf("attempt %d after reset: got %v", i+1, err)
		}
	}
}

func TestLogin_AlertAndLockout(t *testing.T) {
	l := &recLogger{}
	a, clk := newAuth(t, func(c *auth.Config) {
		c.MaxFailures = 3
		c.AlertFailures = 7
		c.SessionTTL = 7 * 24 * time.Hour
		c.Logger = l
	})
	session := login(t, a)
	first := clk.Now()

	// Attempts refused by the per-address limit don't count.
	for i := 0; i < 10; i++ {
		_ = a.Login("1.1.1.1", "000000")
	}
	clk.Add(time.Hour)
	for i := 0; i < 3; i++ {
		_ = a.Login("2.2.2.2", "000000")
	}
	if _, ok := a.PINAlert(); ok {
		t.Fatal("alert after 6 checked wrong PINs")
	}
	if a.Locked() {
		t.Fatal("locked after 6 checked wrong PINs")
	}

	clk.Add(time.Hour)
	if err := a.Login("3.3.3.3", "000000"); !errors.Is(err, auth.ErrInvalidPIN) {
		t.Fatalf("7th wrong PIN: got %v, want ErrInvalidPIN", err)
	}
	alert, ok := a.PINAlert()
	if !ok || alert.Count != 7 || !alert.First.Equal(first) || !alert.Last.Equal(clk.Now()) {
		t.Errorf("alert = %+v, %v; want 7 from %v to %v", alert, ok, first, clk.Now())
	}
	if len(l.warnings) != 1 || !strings.Contains(l.warnings[0], "7 wrong PINs") {
		t.Errorf("warnings = %q", l.warnings)
	}

	// Locked for everyone, even with the right PIN, but existing sessions work.
	if err := a.Login("4.4.4.4", testPIN); !errors.Is(err, auth.ErrLocked) {
		t.Errorf("right PIN while locked: got %v, want ErrLocked", err)
	}
	if !a.Locked() {
		t.Error("Locked() = false")
	}
	if !hasSession(a, session) {
		t.Error("existing session rejected while locked")
	}

	// The lock lifts after the alert window; the alert stays until restart.
	clk.Add(auth.DefaultAlertWindow - time.Second)
	if err := a.Login("4.4.4.4", testPIN); !errors.Is(err, auth.ErrLocked) {
		t.Errorf("just before unlock: got %v, want ErrLocked", err)
	}
	clk.Add(time.Second)
	if err := a.Login("4.4.4.4", testPIN); err != nil {
		t.Errorf("after unlock: %v", err)
	}
	if _, ok := a.PINAlert(); !ok {
		t.Error("alert cleared by unlock")
	}

	// Later wrong PINs add to the alert, and enough of them lock again.
	for i := 0; i < 7; i++ {
		_ = a.Login(fmt.Sprintf("5.5.5.%d", i), "000000")
	}
	if alert, _ := a.PINAlert(); alert.Count != 14 || !alert.First.Equal(first) {
		t.Errorf("alert after more wrong PINs = %+v", alert)
	}
	if !a.Locked() {
		t.Error("not locked again after 7 more wrong PINs")
	}
}

func TestLogin_AlertWindowSlides(t *testing.T) {
	a, clk := newAuth(t, func(c *auth.Config) {
		c.MaxFailures = 100
		c.AlertFailures = 3
	})
	_ = a.Login("1.1.1.1", "000000")
	clk.Add(auth.DefaultAlertWindow / 2)
	_ = a.Login("1.1.1.1", "000000")
	clk.Add(auth.DefaultAlertWindow / 2)
	// The first wrong PIN is now 24 hours old and no longer counts.
	_ = a.Login("1.1.1.1", "000000")
	if _, ok := a.PINAlert(); ok {
		t.Error("alert counted a wrong PIN older than the window")
	}
	_ = a.Login("1.1.1.1", "000000")
	if _, ok := a.PINAlert(); !ok {
		t.Error("no alert after 3 wrong PINs within the window")
	}
}

func TestLogin_ConcurrentAttemptsRespectLimit(t *testing.T) {
	a, _ := newAuth(t, nil)
	var wg sync.WaitGroup
	var mu sync.Mutex
	checked := 0
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.Login("1.1.1.1", "000000"); errors.Is(err, auth.ErrInvalidPIN) {
				mu.Lock()
				checked++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if checked != auth.DefaultMaxFailures {
		t.Errorf("%d PINs checked, want %d", checked, auth.DefaultMaxFailures)
	}
}
