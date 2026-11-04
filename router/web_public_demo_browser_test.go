package router_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/store"
)

// TestPublicDemoBrowser checks the public demo's login screen in Chrome
// (#175): the setup is beside the form on a desktop and below it on a
// phone, neither scrolls sideways, and the form still logs in. It needs a
// WebDriver server, as TestWebPhoneBrowser does.
func TestPublicDemoBrowser(t *testing.T) {
	wd := os.Getenv("SCRIBBLE_WEBDRIVER")
	if wd == "" {
		t.Skip("SCRIBBLE_WEBDRIVER is not set")
	}
	a, err := auth.New(auth.Config{
		TOTPKey:      webKey,
		Secret:       []byte("0123456789abcdef0123456789abcdef"),
		SessionTTL:   time.Hour,
		SecureCookie: false, // plain HTTP, as in newBrowserServer
		NoLockout:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "demo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	srv := serveToBrowser(t, newWebRouter(t, s, a, &testLogger{}, nil, handlers.WithPublicDemo(true)))

	type box struct{ Left, Right, Top, Bottom float64 }
	type layout struct {
		Form, Setup     box
		ScrollW, Client float64
	}
	measure := func(d *webDriver) layout {
		var l layout
		d.execInto(&l, `function box(sel) {
				var r = document.querySelector(sel).getBoundingClientRect();
				return {Left: r.left, Right: r.right, Top: r.top, Bottom: r.bottom};
			}
			return {Form: box(".login-form"), Setup: box(".demo-setup"),
				ScrollW: document.documentElement.scrollWidth, Client: document.documentElement.clientWidth};`)
		return l
	}

	t.Run("desktop", func(t *testing.T) {
		d := newDriver(t, wd, 1024, 768, 1, false)
		d.open(srv + "/login")
		d.wantText(".totp-secret", auth.EncodeSecret(webKey))
		l := measure(d)
		if l.Setup.Left < l.Form.Right || l.Setup.Top >= l.Form.Bottom {
			t.Errorf("the setup isn't beside the form: form %+v, setup %+v", l.Form, l.Setup)
		}
		if l.ScrollW > l.Client {
			t.Errorf("the page scrolls sideways: %v > %v", l.ScrollW, l.Client)
		}
		if !d.visible(".totp-qr") || !d.visible(".code-copy") {
			t.Error("the QR code or the Copy button is hidden")
		}

		// The form still logs in.
		d.exec(`document.getElementById("code").value = arguments[0]; document.querySelector(".login-form").requestSubmit()`, auth.Code(webKey, time.Now()))
		d.waitPath("/")
	})

	t.Run("phone", func(t *testing.T) {
		d := newDriver(t, wd, 375, 740, 2, false)
		d.open(srv + "/login")
		d.wantText(".totp-secret", auth.EncodeSecret(webKey))
		l := measure(d)
		if l.Setup.Top < l.Form.Bottom {
			t.Errorf("the setup isn't below the form: form %+v, setup %+v", l.Form, l.Setup)
		}
		if l.ScrollW > l.Client {
			t.Errorf("the page scrolls sideways: %v > %v", l.ScrollW, l.Client)
		}
	})
}
