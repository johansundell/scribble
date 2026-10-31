package router_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

const webPIN = "246813"

type webClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *webClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *webClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type webEnv struct {
	r     *gin.Engine
	auth  *auth.Authenticator
	clock *webClock
	log   *testLogger
	store *store.SQLStore
}

// newWebEnv builds a router with the web app on, a real SQLite store, the
// real templates and assets, and an injectable clock.
func newWebEnv(t *testing.T, trustedProxies ...string) *webEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	clk := &webClock{t: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
	tl := &testLogger{}
	a, err := auth.New(auth.Config{
		PIN:          webPIN,
		Secret:       []byte("0123456789abcdef0123456789abcdef"),
		SessionTTL:   168 * time.Hour,
		SecureCookie: true,
		Now:          clk.Now,
		Logger:       tl,
	})
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &webEnv{r: newWebRouter(t, s, a, tl, trustedProxies), auth: a, clock: clk, log: tl, store: s}
}

func newWebRouter(t *testing.T, s store.Store, a *auth.Authenticator, l router.Logger, trustedProxies []string) *gin.Engine {
	t.Helper()
	repo := os.DirFS("..")
	h, err := handlers.NewHandler(s, false, repo, "test", "v9.8.7", handlers.WithAuth(a))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	r, err := router.NewRouter(router.Config{
		Handler:  h,
		Auth:     a,
		LogSink:  &recordingSink{},
		Settings: types.AppSettings{AuthToken: "secret-token", TrustedProxies: trustedProxies},
		Assets:   repo,
		Version:  "v9.8.7",
		Logger:   l,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return r
}

func (e *webEnv) do(req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func get(path string, cookies ...*http.Cookie) *http.Request {
	req := httptest.NewRequest("GET", path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return req
}

// postLogin posts pin (and next, when set) from remoteAddr.
func postLogin(remoteAddr, pin, next string) *http.Request {
	form := url.Values{"pin": {pin}}
	if next != "" {
		form.Set("next", next)
	}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remoteAddr + ":12345"
	return req
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	t.Fatalf("no session cookie in %v", w.Result().Header["Set-Cookie"])
	return nil
}

func (e *webEnv) login(t *testing.T) *http.Cookie {
	t.Helper()
	w := e.do(postLogin("10.0.0.1", webPIN, ""))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	return sessionCookie(t, w)
}

func wantRedirect(t *testing.T, w *httptest.ResponseRecorder, location string) {
	t.Helper()
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != location {
		t.Errorf("got %d to %q, want 303 to %q", w.Code, w.Header().Get("Location"), location)
	}
}

func wantBody(t *testing.T, w *httptest.ResponseRecorder, status int, parts ...string) {
	t.Helper()
	if w.Code != status {
		t.Errorf("status = %d, want %d: %s", w.Code, status, w.Body.String())
	}
	for _, p := range parts {
		if !strings.Contains(w.Body.String(), p) {
			t.Errorf("body lacks %q:\n%s", p, w.Body.String())
		}
	}
}

func TestWeb_OffWithoutPIN(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := mustNewHandler(t, nopStore{}, false, os.DirFS(".."), "test", "dev")
	r, err := router.NewRouter(router.Config{
		Handler:  h,
		LogSink:  &recordingSink{},
		Settings: types.AppSettings{AuthToken: "secret-token"},
		Assets:   os.DirFS(".."),
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	// The root is the server's own: it sends browsers to the health check.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, get("/"))
	wantRedirect(t, w, "/healthz")
	for _, req := range []*http.Request{
		get("/login"), get("/note/1"), get("/new"), get("/manifest.webmanifest"),
		postLogin("10.0.0.1", webPIN, ""),
		httptest.NewRequest("POST", "/logout", nil),
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", req.Method, req.URL, w.Code)
		}
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, get("/healthz"))
	if w.Code != http.StatusOK {
		t.Errorf("/healthz = %d", w.Code)
	}
	// This store has no schema version, so the page leaves it out.
	if body := w.Body.String(); !strings.Contains(body, "API level 3\n") || strings.Contains(body, "schema") {
		t.Errorf("/healthz wants the API level without a schema version:\n%s", body)
	}
}

func TestWeb_RequiresSession(t *testing.T) {
	e := newWebEnv(t)
	wantRedirect(t, e.do(get("/")), "/login?next=/")
	if got := e.do(get("/")).Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
}

func TestWeb_LoginPage(t *testing.T) {
	e := newWebEnv(t)
	w := e.do(get("/login?next=/note/1"))
	wantBody(t, w, http.StatusOK,
		`action="/login"`, `method="post"`, `name="pin"`,
		`inputmode="numeric"`, `autocomplete="current-password"`,
		`name="next" value="/note/1"`,
		`data-digit="0"`, `data-digit="9"`, // the on-screen keypad
		"Scribble v9.8.7",
	)
	if strings.Contains(w.Body.String(), "Log out") {
		t.Error("the login page shows a Log out button")
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type = %q", w.Header().Get("Content-Type"))
	}

	// next is escaped, not trusted.
	w = e.do(get(`/login?next="><script>`))
	if strings.Contains(w.Body.String(), `"><script>`) {
		t.Error("next is not escaped")
	}

	// With a session, the login page sends you on, to next when it is safe.
	c := e.login(t)
	wantRedirect(t, e.do(get("/login", c)), "/")
	wantRedirect(t, e.do(get("/login?next=/note/1", c)), "/note/1")
	wantRedirect(t, e.do(get("/login?next=/healthz", c)), "/")
}

func TestWeb_Login(t *testing.T) {
	e := newWebEnv(t)

	w := e.do(postLogin("10.0.0.1", webPIN, ""))
	wantRedirect(t, w, "/")
	c := sessionCookie(t, w)
	if c.Path != "/" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie = %+v", c)
	}

	for next, want := range map[string]string{
		"/note/1?q=x":         "/note/1?q=x",
		"/?project=work":      "/?project=work",
		"/":                   "/",
		"https://evil.test/":  "/",
		"//evil.test/":        "/",
		"/\\evil.test":        "/",
		"/note/../../healthz": "/",
		"/note/./1":           "/",
		"note/1":              "/",
		// Only web app pages: not the API, health check, shares or assets.
		"/healthz":            "/",
		"/notes/1":            "/",
		"/s/abc":              "/",
		"/assets/css/app.css": "/",
		"/login":              "/",
		"/note":               "/",
		"/note/":              "/",
		"/title/":             "/",
		"/new/":               "/",
		"/newx":               "/",
		"?x":                  "/",
		"/new":                "/new",
		"/note/1/edit":        "/note/1/edit",
		"/title/x":            "/title/x",
	} {
		w := e.do(postLogin("10.0.0.2", webPIN, next))
		if got := w.Header().Get("Location"); got != want {
			t.Errorf("next %q: redirected to %q, want %q", next, got, want)
		}
	}
}

func TestWeb_WrongPIN(t *testing.T) {
	e := newWebEnv(t)
	w := e.do(postLogin("10.0.0.1", "000000", "/note/1"))
	wantBody(t, w, http.StatusUnauthorized, "Wrong PIN", `name="next" value="/note/1"`)
	if len(w.Result().Cookies()) != 0 {
		t.Errorf("wrong PIN set cookies: %v", w.Result().Cookies())
	}
}

func TestWeb_AppPage(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)

	w := e.do(get("/", c))
	wantBody(t, w, http.StatusOK,
		"Scribble v9.8.7", `action="/logout"`, "Log out",
		`<meta name="viewport"`, `/assets/css/app.css`,
	)
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if strings.Contains(w.Body.String(), "wrong PINs") {
		t.Error("PIN Alert shown without wrong PINs")
	}

	// No external fonts or scripts.
	for _, external := range []string{`src="http`, `href="http`, `src="//`, `href="//`} {
		if strings.Contains(w.Body.String(), external) {
			t.Errorf("page loads something external: %q", external)
		}
	}
}

func TestWeb_SessionExpiresAndIsNotExtended(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)

	e.clock.Add(167 * time.Hour)
	w := e.do(get("/", c))
	if w.Code != http.StatusOK {
		t.Fatalf("before expiry: %d", w.Code)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Errorf("a page view set cookies (extending the session?): %v", w.Result().Cookies())
	}
	e.clock.Add(time.Hour)
	wantRedirect(t, e.do(get("/", c)), "/login?next=/")
}

func TestWeb_ForgedCookies(t *testing.T) {
	e := newWebEnv(t)
	good := e.login(t)

	other, err := auth.New(auth.Config{PIN: webPIN, Secret: []byte(strings.Repeat("z", 32)), SessionTTL: time.Hour, Now: e.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	other.StartSession(rec)
	otherSecret := rec.Result().Cookies()[0]

	edited := *good
	edited.Value = "A" + good.Value[1:]
	for name, c := range map[string]*http.Cookie{"other secret": otherSecret, "edited": &edited} {
		if w := e.do(get("/", c)); w.Code != http.StatusSeeOther {
			t.Errorf("%s: %d, want a redirect to login", name, w.Code)
		}
	}
}

func TestWeb_Logout(t *testing.T) {
	e := newWebEnv(t)

	w := e.do(httptest.NewRequest("POST", "/logout", nil))
	wantRedirect(t, w, "/login")
	c := sessionCookie(t, w)
	if c.MaxAge >= 0 || c.Value != "" || c.Path != "/" {
		t.Errorf("logout cookie = %+v", c)
	}

	if w := e.do(get("/logout")); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /logout = %d, want 405", w.Code)
	}
}

func TestWeb_PerAddressLimit(t *testing.T) {
	e := newWebEnv(t)
	for i := 0; i < auth.DefaultMaxFailures; i++ {
		if w := e.do(postLogin("10.0.0.1", "000000", "")); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i+1, w.Code)
		}
	}
	w := e.do(postLogin("10.0.0.1", webPIN, ""))
	wantBody(t, w, http.StatusTooManyRequests, "Too many wrong PINs")
	if len(w.Result().Cookies()) != 0 {
		t.Error("a refused attempt started a session")
	}
	wantRedirect(t, e.do(postLogin("10.0.0.2", webPIN, "")), "/")

	e.clock.Add(auth.DefaultFailureWindow)
	wantRedirect(t, e.do(postLogin("10.0.0.1", webPIN, "")), "/")
}

func burnAddress(e *webEnv, remoteAddr, forwardedFor string) {
	for i := 0; i < auth.DefaultMaxFailures; i++ {
		req := postLogin(remoteAddr, "000000", "")
		if forwardedFor != "" {
			req.Header.Set("X-Forwarded-For", forwardedFor)
		}
		e.do(req)
	}
}

func TestWeb_TrustedProxies(t *testing.T) {
	loginVia := func(e *webEnv, remoteAddr, forwardedFor string) int {
		req := postLogin(remoteAddr, webPIN, "")
		req.Header.Set("X-Forwarded-For", forwardedFor)
		return e.do(req).Code
	}

	t.Run("trusted proxy", func(t *testing.T) {
		e := newWebEnv(t, "127.0.0.1")
		burnAddress(e, "127.0.0.1", "203.0.113.1")
		if got := loginVia(e, "127.0.0.1", "203.0.113.1"); got != http.StatusTooManyRequests {
			t.Errorf("same forwarded address: %d, want 429", got)
		}
		if got := loginVia(e, "127.0.0.1", "203.0.113.2"); got != http.StatusSeeOther {
			t.Errorf("another forwarded address: %d, want 303", got)
		}
	})

	t.Run("forged header from elsewhere", func(t *testing.T) {
		e := newWebEnv(t, "127.0.0.1")
		burnAddress(e, "198.51.100.7", "203.0.113.1")
		if got := loginVia(e, "198.51.100.7", "203.0.113.99"); got != http.StatusTooManyRequests {
			t.Errorf("forged X-Forwarded-For changed the address: %d, want 429", got)
		}
	})

	t.Run("no proxies trusted by default", func(t *testing.T) {
		e := newWebEnv(t)
		burnAddress(e, "127.0.0.1", "203.0.113.1")
		if got := loginVia(e, "127.0.0.1", "203.0.113.2"); got != http.StatusTooManyRequests {
			t.Errorf("X-Forwarded-For believed without TRUSTED_PROXIES: %d, want 429", got)
		}
	})
}

func TestNewRouter_InvalidTrustedProxies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, err := router.NewRouter(router.Config{
		Handler:  mustNewHandler(t, nopStore{}, false, os.DirFS(".."), "test", "dev"),
		LogSink:  &recordingSink{},
		Settings: types.AppSettings{AuthToken: "secret-token", TrustedProxies: []string{"not-an-ip"}},
		Assets:   os.DirFS(".."),
	})
	if err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXIES") {
		t.Errorf("err = %v, want one naming TRUSTED_PROXIES", err)
	}
}

func TestWeb_AlertAndLockout(t *testing.T) {
	e := newWebEnv(t)
	session := e.login(t)
	first := e.clock.Now()

	for i := 0; i < auth.DefaultAlertFailures/auth.DefaultMaxFailures; i++ {
		burnAddress(e, fmt.Sprintf("10.1.0.%d", i), "")
		e.clock.Add(time.Hour)
	}

	var warned bool
	for _, entry := range e.log.entries {
		if entry.level == "WARN" && strings.Contains(entry.message, "20 wrong PINs") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("no PIN Alert warning in %v", e.log.messages)
	}

	// Locked for everyone, with the right PIN too.
	wantBody(t, e.do(postLogin("10.2.0.1", webPIN, "")), http.StatusTooManyRequests,
		"Web login is locked after too many wrong PINs. Restart the service (or wait 24 hours) to unlock it.")

	// Existing sessions keep working, and see the alert.
	wantBody(t, e.do(get("/", session)), http.StatusOK,
		"20 wrong PINs since "+first.Local().Format("2006-01-02 15:04"), "change WEB_PIN")

	// A restart (a new Authenticator) clears the lock and the alert.
	restarted := newWebEnv(t)
	wantRedirect(t, restarted.do(postLogin("10.2.0.1", webPIN, "")), "/")
}

func TestWeb_NotRequestLogged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	a, err := auth.New(auth.Config{PIN: webPIN, Secret: []byte("0123456789abcdef0123456789abcdef"), SessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	h, err := handlers.NewHandler(nopStore{}, false, os.DirFS(".."), "test", "dev", handlers.WithAuth(a))
	if err != nil {
		t.Fatal(err)
	}
	r, err := router.NewRouter(router.Config{
		Handler: h, Auth: a, LogSink: sink, Assets: os.DirFS(".."),
		Settings: types.AppSettings{AuthToken: "secret-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []*http.Request{get("/login"), postLogin("10.0.0.1", "000000", ""), postLogin("10.0.0.1", webPIN, ""), get("/"), httptest.NewRequest("POST", "/logout", nil)} {
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	if len(sink.entries) != 0 {
		t.Errorf("web requests were request-logged: %+v", sink.entries)
	}

	for _, route := range router.GetRoutes(h) {
		if route.Web && (route.UseLogger || route.UseAuth) {
			t.Errorf("web route %q: UseLogger=%v UseAuth=%v", route.Name, route.UseLogger, route.UseAuth)
		}
	}
}

// TestNewRouter_WebOriginCheck checks the wiring: every web POST gets the
// same-origin check, with or without a session, unless it allows cross-origin
// requests (only login and logout do).
func TestNewRouter_WebOriginCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	a, err := auth.New(auth.Config{PIN: webPIN, Secret: []byte("0123456789abcdef0123456789abcdef"), SessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ok := func(c *gin.Context) error { c.String(http.StatusOK, "ok"); return nil }
	routes := router.Routes{
		{Name: "Open", Method: "POST", Pattern: "/open", HandlerFunc: ok, Web: true},
		{Name: "Session", Method: "POST", Pattern: "/session", HandlerFunc: ok, Web: true, UseSession: true},
		{Name: "Cross", Method: "POST", Pattern: "/cross", HandlerFunc: ok, Web: true, AllowCrossOrigin: true},
	}
	newRouter := func(a *auth.Authenticator) *gin.Engine {
		r, err := router.NewRouter(router.Config{
			Handler: mustNewHandler(t, nopStore{}, false, os.DirFS(".."), "test", "dev"),
			Auth:    a, LogSink: &recordingSink{}, Assets: os.DirFS(".."), Routes: routes,
		})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	post := func(r *gin.Engine, path, origin string) int {
		req := httptest.NewRequest("POST", "http://notes.example"+path, nil)
		req.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	r := newRouter(a)
	for path, want := range map[string]int{
		"/open": http.StatusForbidden, "/session": http.StatusForbidden, "/cross": http.StatusOK,
	} {
		if got := post(r, path, "https://evil.example"); got != want {
			t.Errorf("foreign POST %s = %d, want %d", path, got, want)
		}
	}
	if got := post(r, "/open", "http://notes.example"); got != http.StatusOK {
		t.Errorf("same-origin POST /open = %d, want 200", got)
	}

	// With the web app off, web routes aren't registered at all.
	if got := post(newRouter(nil), "/cross", "http://notes.example"); got != http.StatusNotFound {
		t.Errorf("web off: POST /cross = %d, want 404", got)
	}
}

func TestSameOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/x", router.WrapHandler(router.SameOrigin(func(c *gin.Context) error {
		c.String(http.StatusOK, "ok")
		return nil
	})))

	for _, tc := range []struct {
		name            string
		origin, referer string
		want            int
	}{
		{"same origin", "https://notes.example", "", http.StatusOK},
		{"same origin over http", "http://notes.example", "", http.StatusOK},
		{"foreign origin", "https://evil.example", "", http.StatusForbidden},
		{"null origin", "null", "", http.StatusForbidden},
		{"foreign origin beats same referer", "https://evil.example", "https://notes.example/", http.StatusForbidden},
		{"referer when no origin", "", "https://notes.example/x", http.StatusOK},
		{"foreign referer", "", "https://evil.example/", http.StatusForbidden},
		{"neither", "", "", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "https://notes.example/x", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}
}
