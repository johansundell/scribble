package router_test

import (
	"bytes"
	"encoding/base64"
	"html"
	"image/png"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/handlers"
)

// A public demo (PUBLIC_DEMO=true, #175) shows the authenticator secret on
// the login screen, so visitors need no AUTH_TOKEN to set up an app.

// newPublicDemoEnv is newWebEnv with PUBLIC_DEMO set to on.
func newPublicDemoEnv(t *testing.T, on bool) *webEnv {
	t.Helper()
	e := newWebEnv(t)
	e.r = newWebRouter(t, e.store, e.auth, e.log, nil, handlers.WithPublicDemo(on))
	return e
}

func TestPublicDemo_LoginShowsTheSecretAndQRCode(t *testing.T) {
	e := newPublicDemoEnv(t, true)
	secret := auth.EncodeSecret(webKey)
	uri := "otpauth://totp/Scribble:example.com?secret=" + secret + "&issuer=Scribble"

	for _, req := range []*http.Request{get("/login"), postLogin("10.0.0.1", webWrongCode, "")} {
		w := e.do(req)
		status := http.StatusOK
		if req.Method == http.MethodPost {
			status = http.StatusUnauthorized
		}
		wantBody(t, w, status,
			`class="demo-setup`, secret, "data-copy", "/assets/js/copy.js",
			`href="`+strings.ReplaceAll(uri, "&", "&amp;")+`"`,
			// The login form is still there.
			`action="/login"`, `name="code"`,
		)
		body := w.Body.String()
		if strings.Contains(body, `href="/totp-setup"`) {
			t.Errorf("%s /login: links to /totp-setup, which the demo doesn't need", req.Method)
		}
		// The setup comes after the login form, so phones show it below.
		if form, setup := strings.Index(body, `class="login-form"`), strings.Index(body, `class="demo-setup`); setup < form {
			t.Errorf("%s /login: the setup comes before the login form", req.Method)
		}
		m := regexp.MustCompile(`<img [^>]*class="totp-qr"[^>]*src="data:image/png;base64,([^"]+)"`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("%s /login: no inline QR code:\n%s", req.Method, body)
		}
		raw, err := base64.StdEncoding.DecodeString(html.UnescapeString(m[1]))
		if err != nil {
			t.Fatalf("QR code isn't base64: %v", err)
		}
		if _, err := png.Decode(bytes.NewReader(raw)); err != nil {
			t.Errorf("QR code isn't a PNG: %v", err)
		}
	}
}

// The per-address limit still holds: 5 wrong codes from one address, then
// 429, with the secret still shown. (No Lockout is auth.Config.NoLockout,
// which service.go sets from PUBLIC_DEMO: TestNewWebAuth_PublicDemoNeverLocks.)
func TestPublicDemo_KeepsThePerAddressLimit(t *testing.T) {
	e := newPublicDemoEnv(t, true)
	for i := 0; i < auth.DefaultMaxFailures; i++ {
		if w := e.do(postLogin("10.0.0.1", webWrongCode, "")); w.Code != http.StatusUnauthorized {
			t.Fatalf("wrong code %d: %d, want 401", i+1, w.Code)
		}
	}
	wantBody(t, e.do(postLogin("10.0.0.1", e.code(), "")), http.StatusTooManyRequests, "Too many wrong codes", `class="demo-setup`)
}

func TestPublicDemo_OffByDefault(t *testing.T) {
	for _, e := range []*webEnv{newWebEnv(t), newPublicDemoEnv(t, false)} {
		w := e.do(get("/login"))
		body := w.Body.String()
		if strings.Contains(body, auth.EncodeSecret(webKey)) || strings.Contains(body, "otpauth:") || strings.Contains(body, "demo-setup") {
			t.Errorf("the secret is on the login screen without PUBLIC_DEMO:\n%s", body)
		}
		wantBody(t, w, http.StatusOK, `href="/totp-setup"`)
	}
}
