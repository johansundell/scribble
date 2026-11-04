package router_test

import (
	"os"
	"testing"
	"time"

	"github.com/johansundell/scribble/auth"
)

// TestTOTPSetupBrowser checks the setup page's forms in Chrome (#172): a
// browser, not the test, sets the Origin header that the same-origin check
// reads, and the page's Referrer-Policy changes what it sends. It needs a
// WebDriver server, as TestWebPhoneBrowser does.
func TestTOTPSetupBrowser(t *testing.T) {
	wd := os.Getenv("SCRIBBLE_WEBDRIVER")
	if wd == "" {
		t.Skip("SCRIBBLE_WEBDRIVER is not set")
	}
	srv, _ := newBrowserServer(t)
	d := newDriver(t, wd, 1024, 768, 1, false)

	// The token form, as someone types it.
	d.open(srv + "/totp-setup")
	d.exec(`document.getElementById("token").value = "secret-token"; document.querySelector(".login-form").requestSubmit()`)
	d.waitFor(`return document.querySelector(".totp-qr") !== null`)
	d.wantText(".totp-secret", auth.EncodeSecret(webKey))

	// The test form on the page that the token form opened.
	d.exec(`document.getElementById("code").value = arguments[0]; document.querySelector(".login-form").requestSubmit()`, auth.Code(webKey, time.Now()))
	d.waitFor(`return document.querySelector(".code-ok") !== null`)

	// And from the page that ?token= opened.
	d.open(srv + "/totp-setup?token=secret-token")
	d.exec(`document.getElementById("code").value = "000000"; document.querySelector(".login-form").requestSubmit()`)
	d.waitFor(`return document.querySelector(".login-error") !== null`)
}
