package router_test

import (
	"bytes"
	"encoding/base64"
	"html"
	"image/png"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/johansundell/scribble/auth"
)

// The authenticator setup page (#172, ADR 13) shows the TOTP secret, so it
// needs AUTH_TOKEN.

func postSetup(form url.Values) *http.Request {
	req := postForm("/totp-setup", form)
	req.RemoteAddr = "10.9.0.1:12345"
	return req
}

func TestTOTPSetup_AsksForTheToken(t *testing.T) {
	e := newWebEnv(t)
	secret := auth.EncodeSecret(webKey)
	w := e.do(get("/totp-setup"))
	wantBody(t, w, http.StatusOK, `action="/totp-setup"`, `method="post"`, `name="token"`, "AUTH_TOKEN")
	if strings.Contains(w.Body.String(), secret) || strings.Contains(w.Body.String(), "otpauth:") {
		t.Errorf("the secret is shown without the token:\n%s", w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}

	for _, req := range []*http.Request{
		get("/totp-setup?token=wrong"),
		postSetup(url.Values{"token": {"wrong"}}),
	} {
		w := e.do(req)
		wantBody(t, w, http.StatusUnauthorized, "Wrong token", `name="token"`)
		if strings.Contains(w.Body.String(), secret) {
			t.Errorf("%s %s: the secret is shown with a wrong token", req.Method, req.URL)
		}
	}
}

func TestTOTPSetup_ShowsTheSecretAndQRCode(t *testing.T) {
	e := newWebEnv(t)
	secret := auth.EncodeSecret(webKey)
	uri := "otpauth://totp/Scribble:example.com?secret=" + secret + "&issuer=Scribble"

	for _, req := range []*http.Request{
		get("/totp-setup?token=secret-token"),
		postSetup(url.Values{"token": {"secret-token"}}),
	} {
		w := e.do(req)
		wantBody(t, w, http.StatusOK,
			secret, "data-copy", "/assets/js/copy.js",
			// Phones open the app from the link, where scanning is awkward.
			`href="`+strings.ReplaceAll(uri, "&", "&amp;")+`"`,
			// The test form keeps the token, so it needs no second entry.
			`name="code"`, `type="hidden" name="token" value="secret-token"`,
			`href="/login"`,
		)
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q", got)
		}
		// The token may be in the URL: don't pass it on to other sites.
		if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("Referrer-Policy = %q", got)
		}

		// The QR code is an inline PNG.
		m := regexp.MustCompile(`<img [^>]*src="data:image/png;base64,([^"]+)"`).FindStringSubmatch(w.Body.String())
		if m == nil {
			t.Fatalf("no inline QR code:\n%s", w.Body.String())
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

func TestTOTPSetup_TestsACodeWithoutUsingItUp(t *testing.T) {
	e := newWebEnv(t)
	code := e.code()

	w := e.do(postSetup(url.Values{"token": {"secret-token"}, "code": {code}}))
	wantBody(t, w, http.StatusOK, "The code is right", auth.EncodeSecret(webKey))
	if len(w.Result().Cookies()) != 0 {
		t.Errorf("testing a code started a session: %v", w.Result().Cookies())
	}
	// The same code still logs in.
	wantRedirect(t, e.do(postLogin("10.0.0.1", code, "")), "/")

	w = e.do(postSetup(url.Values{"token": {"secret-token"}, "code": {webWrongCode}}))
	wantBody(t, w, http.StatusOK, "The code is wrong", auth.EncodeSecret(webKey))
	// A wrong test code is no wrong login: no alert, no limit.
	for i := 0; i < auth.DefaultMaxFailures; i++ {
		e.do(postSetup(url.Values{"token": {"secret-token"}, "code": {webWrongCode}}))
	}
	if w := e.do(postSetup(url.Values{"token": {"secret-token"}})); w.Code != http.StatusOK {
		t.Errorf("after wrong test codes: %d, want 200", w.Code)
	}
}

func TestTOTPSetup_WrongTokensAreRateLimited(t *testing.T) {
	e := newWebEnv(t)
	for i := 0; i < auth.DefaultMaxFailures; i++ {
		if w := e.do(postSetup(url.Values{"token": {"wrong"}})); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i+1, w.Code)
		}
	}
	w := e.do(postSetup(url.Values{"token": {"secret-token"}}))
	wantBody(t, w, http.StatusTooManyRequests, "Too many wrong")
	if strings.Contains(w.Body.String(), auth.EncodeSecret(webKey)) {
		t.Error("the secret is shown to a blocked address")
	}
}

func TestTOTPSetup_RefusesCrossSitePosts(t *testing.T) {
	e := newWebEnv(t)
	req := postSetup(url.Values{"token": {"secret-token"}})
	req.Header.Set("Origin", "https://evil.test")
	if w := e.do(req); w.Code != http.StatusForbidden {
		t.Errorf("cross-site POST = %d, want 403", w.Code)
	}
}
