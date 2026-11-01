package router_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/types"
)

// newLevelRouter is a router whose API level and minimum client level are
// set by the test.
func newLevelRouter(t *testing.T, level, minClient int, l router.Logger) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := mustNewHandler(t, nopStore{}, false, fstest.MapFS{}, "test", "v1.2.3")
	r, err := router.NewRouter(router.Config{
		Handler:      h,
		LogSink:      &recordingSink{},
		Settings:     types.AppSettings{AuthToken: "tok"},
		Assets:       fstest.MapFS{},
		Version:      "v1.2.3",
		Logger:       l,
		APILevel:     level,
		MinClientAPI: minClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAPILevelHeaderOnEveryResponse(t *testing.T) {
	r := newLevelRouter(t, 0, 0, &testLogger{})
	for _, path := range []string{"/", "/healthz", "/notes", "/nope", "/assets/x"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if got := w.Header().Get("X-Scribble-API"); got != "4" {
			t.Errorf("%s: X-Scribble-API = %q, want the default level 4", path, got)
		}
	}

	r = newLevelRouter(t, 7, 0, &testLogger{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/healthz", nil)
	req.Header.Set("Accept", "application/json")
	r.ServeHTTP(w, req)
	if got := w.Header().Get("X-Scribble-API"); got != "7" {
		t.Errorf("X-Scribble-API = %q, want 7", got)
	}
	if !strings.Contains(w.Body.String(), `"api":7`) {
		t.Errorf("/healthz JSON lacks the API level: %s", w.Body.String())
	}
}

func TestTooOldClientGets426(t *testing.T) {
	r := newLevelRouter(t, 3, 2, &testLogger{})
	cases := []struct {
		name, method, path, client string
		want                       int
	}{
		{"no header counts as level 1", "GET", "/notes", "", http.StatusUpgradeRequired},
		{"level below the minimum", "GET", "/changes?since=0", "cli/1 v0.0.9", http.StatusUpgradeRequired},
		{"projects too", "GET", "/projects", "gui/1 v0.0.9", http.StatusUpgradeRequired},
		{"writes too", "PUT", "/notes/1", "", http.StatusUpgradeRequired},
		{"garbage counts as level 1", "GET", "/notes", "who knows", http.StatusUpgradeRequired},
		{"at the minimum", "GET", "/notes", "cli/2 v0.1.0", http.StatusNotImplemented},
		{"above the minimum", "GET", "/notes", "gui/3 v0.2.0", http.StatusNotImplemented},
		{"healthz is never refused", "GET", "/healthz", "", http.StatusOK},
		{"root is never refused", "GET", "/", "", http.StatusSeeOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Accept", "application/json") // the health page template is not in the test FS
			req.Header.Set("Authorization", "Bearer tok")
			if tc.client != "" {
				req.Header.Set("X-Scribble-Client", tc.client)
			}
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
			if tc.want == http.StatusUpgradeRequired {
				want := "This Scribble client is too old for this server. Download the latest release: https://github.com/johansundell/scribble/releases/latest"
				if w.Body.String() != want {
					t.Errorf("body = %q, want %q", w.Body.String(), want)
				}
			}
		})
	}
}

// TestTooOldClientNeedsTokenFirst checks that a wrong token is still a 401,
// so the refusal is only logged for real clients.
func TestTooOldClientNeedsTokenFirst(t *testing.T) {
	r := newLevelRouter(t, 2, 2, &testLogger{})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/notes", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestClientCheckLogsOncePerClientPerHour(t *testing.T) {
	gin.SetMode(gin.TestMode)
	l := &testLogger{}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	check := router.ClientCheck(2, l, func() time.Time { return now })
	handler := router.WrapHandler(check(func(*gin.Context) error { return nil }))

	refuse := func(client string) {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/notes", nil)
		c.Request.RemoteAddr = "192.0.2.7:1234"
		if client != "" {
			c.Request.Header.Set("X-Scribble-Client", client)
		}
		handler(c)
		if w.Code != http.StatusUpgradeRequired {
			t.Fatalf("status = %d, want 426", w.Code)
		}
	}
	warnings := func() []string {
		var out []string
		for _, e := range l.entries {
			if e.level == "WARN" {
				out = append(out, e.message)
			}
		}
		return out
	}

	refuse("cli/1 v0.0.9")
	refuse("cli/1 v0.0.9")
	now = now.Add(59 * time.Minute)
	refuse("cli/1 v0.0.9")
	if got := warnings(); len(got) != 1 {
		t.Fatalf("want one warning within the hour, got %q", got)
	}
	if msg := warnings()[0]; !strings.Contains(msg, "cli/1 v0.0.9") || !strings.Contains(msg, "192.0.2.7") {
		t.Errorf("warning lacks the client string or IP: %q", msg)
	}

	refuse("gui/1 v0.0.8")
	refuse("")
	if got := warnings(); len(got) != 3 {
		t.Fatalf("each client string is logged on its own, got %q", got)
	}

	now = now.Add(2 * time.Minute)
	refuse("cli/1 v0.0.9")
	if got := warnings(); len(got) != 4 {
		t.Errorf("want the client logged again after an hour, got %q", got)
	}
}
