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
	testSecret = "0123456789abcdef0123456789abcdef"
	testToken  = "the-auth-token"
)

// testKey is the TOTP key the tests' authenticators use.
var testKey = []byte("0123456789abcdefghij")

// code is the right code at clk's time.
func code(clk *clock) string { return auth.Code(testKey, clk.Now()) }

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
		TOTPKey:      testKey,
		SetupToken:   testToken,
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
	base := auth.Config{TOTPKey: testKey, Secret: []byte(testSecret), SessionTTL: time.Hour}
	for name, mod := range map[string]func(*auth.Config){
		"no TOTP key":    func(c *auth.Config) { c.TOTPKey = nil },
		"short TOTP key": func(c *auth.Config) { c.TOTPKey = testKey[:auth.MinSecretBytes-1] },
		"short secret":   func(c *auth.Config) { c.Secret = []byte("short") },
		"no TTL":         func(c *auth.Config) { c.SessionTTL = 0 },
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
	a, clk := newAuth(t, nil)
	if err := a.Login("1.1.1.1", code(clk)); err != nil {
		t.Errorf("right code: %v", err)
	}
	if err := a.Login("1.1.1.1", "654321"); !errors.Is(err, auth.ErrInvalidCode) {
		t.Errorf("wrong code: got %v, want ErrInvalidCode", err)
	}
	if err := a.Login("1.1.1.1", code(clk)+"0"); !errors.Is(err, auth.ErrInvalidCode) {
		t.Errorf("code with an extra digit: got %v, want ErrInvalidCode", err)
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
		if err := a.Login("1.1.1.1", "000000"); !errors.Is(err, auth.ErrInvalidCode) {
			t.Fatalf("attempt %d: got %v, want ErrInvalidCode", i+1, err)
		}
	}
	// The 6th attempt is refused without the code being checked, so even the
	// right code gets nowhere.
	if err := a.Login("1.1.1.1", code(clk)); !errors.Is(err, auth.ErrRateLimited) {
		t.Errorf("6th attempt: got %v, want ErrRateLimited", err)
	}
	if err := a.Login("2.2.2.2", code(clk)); err != nil {
		t.Errorf("another address: %v", err)
	}

	clk.Add(auth.DefaultFailureWindow - time.Second)
	if err := a.Login("1.1.1.1", code(clk)); !errors.Is(err, auth.ErrRateLimited) {
		t.Errorf("before the window ends: got %v, want ErrRateLimited", err)
	}
	clk.Add(time.Second)
	if err := a.Login("1.1.1.1", code(clk)); err != nil {
		t.Errorf("after the window: %v", err)
	}
}

func TestLogin_RightCodeResetsAddress(t *testing.T) {
	a, clk := newAuth(t, nil)
	for i := 0; i < auth.DefaultMaxFailures-1; i++ {
		_ = a.Login("1.1.1.1", "000000")
	}
	if err := a.Login("1.1.1.1", code(clk)); err != nil {
		t.Fatalf("right code: %v", err)
	}
	for i := 0; i < auth.DefaultMaxFailures; i++ {
		if err := a.Login("1.1.1.1", "000000"); !errors.Is(err, auth.ErrInvalidCode) {
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
	if _, ok := a.LoginAlert(); ok {
		t.Fatal("alert after 6 checked wrong codes")
	}
	if a.Locked() {
		t.Fatal("locked after 6 checked wrong codes")
	}

	clk.Add(time.Hour)
	if err := a.Login("3.3.3.3", "000000"); !errors.Is(err, auth.ErrInvalidCode) {
		t.Fatalf("7th wrong code: got %v, want ErrInvalidCode", err)
	}
	alert, ok := a.LoginAlert()
	if !ok || alert.Count != 7 || !alert.First.Equal(first) || !alert.Last.Equal(clk.Now()) {
		t.Errorf("alert = %+v, %v; want 7 from %v to %v", alert, ok, first, clk.Now())
	}
	if len(l.warnings) != 1 || !strings.Contains(l.warnings[0], "7 wrong codes") {
		t.Errorf("warnings = %q", l.warnings)
	}

	// Locked for everyone, even with the right code, but existing sessions work.
	if err := a.Login("4.4.4.4", code(clk)); !errors.Is(err, auth.ErrLocked) {
		t.Errorf("right code while locked: got %v, want ErrLocked", err)
	}
	if !a.Locked() {
		t.Error("Locked() = false")
	}
	if !hasSession(a, session) {
		t.Error("existing session rejected while locked")
	}

	// The lock lifts after the alert window; the alert stays until restart.
	clk.Add(auth.DefaultAlertWindow - time.Second)
	if err := a.Login("4.4.4.4", code(clk)); !errors.Is(err, auth.ErrLocked) {
		t.Errorf("just before unlock: got %v, want ErrLocked", err)
	}
	clk.Add(time.Second)
	if err := a.Login("4.4.4.4", code(clk)); err != nil {
		t.Errorf("after unlock: %v", err)
	}
	if _, ok := a.LoginAlert(); !ok {
		t.Error("alert cleared by unlock")
	}

	// Later wrong codes add to the alert, and enough of them lock again.
	for i := 0; i < 7; i++ {
		_ = a.Login(fmt.Sprintf("5.5.5.%d", i), "000000")
	}
	if alert, _ := a.LoginAlert(); alert.Count != 14 || !alert.First.Equal(first) {
		t.Errorf("alert after more wrong codes = %+v", alert)
	}
	if !a.Locked() {
		t.Error("not locked again after 7 more wrong codes")
	}
}

// A public demo (#175) keeps the per-address limit but has no global lock,
// so one visitor can't lock everyone out, and no Login Alert.
func TestLogin_NoLockout(t *testing.T) {
	l := &recLogger{}
	a, clk := newAuth(t, func(c *auth.Config) {
		c.MaxFailures = 3
		c.AlertFailures = 4
		c.NoLockout = true
		c.Logger = l
	})
	for i := 0; i < 20; i++ {
		if err := a.Login(fmt.Sprintf("6.6.6.%d", i), "000000"); !errors.Is(err, auth.ErrInvalidCode) {
			t.Fatalf("wrong code %d: got %v, want ErrInvalidCode", i+1, err)
		}
	}
	if a.Locked() {
		t.Error("locked without the global lockout")
	}
	if _, ok := a.LoginAlert(); ok {
		t.Error("Login Alert without the global lockout")
	}
	if len(l.warnings) != 0 {
		t.Errorf("warnings = %q", l.warnings)
	}
	if err := a.Login("7.7.7.7", code(clk)); err != nil {
		t.Errorf("right code after many wrong ones: %v", err)
	}
	// The per-address limit still holds.
	for i := 0; i < 3; i++ {
		_ = a.Login("8.8.8.8", "000000")
	}
	if err := a.Login("8.8.8.8", "000000"); !errors.Is(err, auth.ErrRateLimited) {
		t.Errorf("4th wrong code from one address: got %v, want ErrRateLimited", err)
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
	// The first wrong code is now 24 hours old and no longer counts.
	_ = a.Login("1.1.1.1", "000000")
	if _, ok := a.LoginAlert(); ok {
		t.Error("alert counted a wrong code older than the window")
	}
	_ = a.Login("1.1.1.1", "000000")
	if _, ok := a.LoginAlert(); !ok {
		t.Error("no alert after 3 wrong codes within the window")
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
			if err := a.Login("1.1.1.1", "000000"); errors.Is(err, auth.ErrInvalidCode) {
				mu.Lock()
				checked++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if checked != auth.DefaultMaxFailures {
		t.Errorf("%d codes checked, want %d", checked, auth.DefaultMaxFailures)
	}
}

func TestLogin_AcceptsOneStepOfClockDrift(t *testing.T) {
	a, clk := newAuth(t, nil)
	now := clk.Now()
	for _, d := range []time.Duration{-30 * time.Second, 30 * time.Second} {
		clk.Add(5 * time.Minute) // a fresh step each time, so none is a replay
		now = clk.Now()
		if err := a.Login("1.1.1.1", auth.Code(testKey, now.Add(d))); err != nil {
			t.Errorf("code from %v away: %v", d, err)
		}
	}
	clk.Add(5 * time.Minute)
	now = clk.Now()
	for _, d := range []time.Duration{-60 * time.Second, 60 * time.Second} {
		if err := a.Login("1.1.1.1", auth.Code(testKey, now.Add(d))); !errors.Is(err, auth.ErrInvalidCode) {
			t.Errorf("code from %v away: got %v, want ErrInvalidCode", d, err)
		}
	}
}

func TestLogin_RefusesReplayedCode(t *testing.T) {
	a, clk := newAuth(t, nil)
	used := code(clk)
	if err := a.Login("1.1.1.1", used); err != nil {
		t.Fatalf("first use: %v", err)
	}
	// The same code again, from anywhere, while it is still in the window.
	for _, d := range []time.Duration{0, 10 * time.Second, 40 * time.Second} {
		clk.Add(d)
		if err := a.Login("2.2.2.2", used); !errors.Is(err, auth.ErrInvalidCode) {
			t.Errorf("replay after %v more: got %v, want ErrInvalidCode", d, err)
		}
	}
	// The previous step's code is older than the used one: refused too.
	if err := a.Login("2.2.2.2", auth.Code(testKey, clk.Now().Add(-30*time.Second))); !errors.Is(err, auth.ErrInvalidCode) {
		t.Errorf("older code after a login: got %v, want ErrInvalidCode", err)
	}
	// The next step's code works.
	clk.Add(30 * time.Second)
	if err := a.Login("2.2.2.2", code(clk)); err != nil {
		t.Errorf("next code: %v", err)
	}
}

func TestCheckCode_DoesNotUseTheCodeUp(t *testing.T) {
	a, clk := newAuth(t, nil)
	if !a.CheckCode(code(clk)) || !a.CheckCode(code(clk)) {
		t.Error("right code not accepted twice by CheckCode")
	}
	if a.CheckCode("000000") {
		t.Error("wrong code accepted")
	}
	if err := a.Login("1.1.1.1", code(clk)); err != nil {
		t.Errorf("login after CheckCode: %v", err)
	}
	if !a.CheckCode(code(clk)) {
		t.Error("CheckCode refuses a code a login used")
	}
}

func TestSetup_NeedsTheToken(t *testing.T) {
	a, _ := newAuth(t, func(c *auth.Config) { c.MaxFailures = 3 })
	if err := a.Setup("1.1.1.1", testToken); err != nil {
		t.Errorf("right token: %v", err)
	}
	for _, tok := range []string{"", "wrong", testToken + "x"} {
		if err := a.Setup("1.1.1.1", tok); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("token %q: got %v, want ErrInvalidToken", tok, err)
		}
	}
	// Wrong tokens count towards the address's limit, as wrong codes do.
	if err := a.Setup("1.1.1.1", testToken); !errors.Is(err, auth.ErrRateLimited) {
		t.Errorf("after 3 wrong tokens: got %v, want ErrRateLimited", err)
	}
	if _, ok := a.LoginAlert(); ok {
		t.Error("wrong tokens raised the Login Alert")
	}

	// Without a token, setup is off.
	off, _ := newAuth(t, func(c *auth.Config) { c.SetupToken = "" })
	if err := off.Setup("1.1.1.1", ""); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("no token configured: got %v, want ErrInvalidToken", err)
	}
}

func TestKeyURI(t *testing.T) {
	a, _ := newAuth(t, nil)
	if got := a.Secret(); got != auth.EncodeSecret(testKey) {
		t.Errorf("Secret = %s", got)
	}
	want := "otpauth://totp/Scribble:notes.example.com?secret=" + auth.EncodeSecret(testKey) + "&issuer=Scribble"
	if got := a.KeyURI("notes.example.com"); got != want {
		t.Errorf("KeyURI = %s, want %s", got, want)
	}
	// A port is kept; anything odd in the host is escaped.
	if got := a.KeyURI("localhost:8080"); got != "otpauth://totp/Scribble:localhost:8080?secret="+auth.EncodeSecret(testKey)+"&issuer=Scribble" {
		t.Errorf("KeyURI with a port = %s", got)
	}
	if got := a.KeyURI("a b?c"); got != "otpauth://totp/Scribble:a%20b%3Fc?secret="+auth.EncodeSecret(testKey)+"&issuer=Scribble" {
		t.Errorf("KeyURI with an odd host = %s", got)
	}
}
