package handlers

import (
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/httperror"
	qrcode "github.com/skip2/go-qrcode"
)

// SetupPath is the authenticator setup page (#172, ADR 13). It shows the
// TOTP secret, as a QR code and as text, to whoever gives AUTH_TOKEN.
const SetupPath = "/totp-setup"

// qrSize is the QR code's width and height in pixels.
const qrSize = 256

const (
	msgWrongToken       = "Wrong token"
	msgSetupRateLimited = "Too many wrong tokens or codes from this address. Try again later."
)

// totpSetup is the setup page's data once the token is right.
type totpSetup struct {
	Secret string
	// KeyURI opens an authenticator app on a phone; QR is the same URI as
	// an inline PNG.
	KeyURI template.URL
	QR     template.URL
	// Token goes back in the test form, so testing a code needs no second
	// entry of it.
	Token string
	// Tested is set when a code was tested, and CodeOK tells how it went.
	Tested bool
	CodeOK bool
}

// TOTPSetup shows the setup page: a form for AUTH_TOKEN, or with the right
// token (posted, or as ?token=) the secret, its QR code and a form to test
// a code. Testing a code doesn't use it up, so it can log in after.
func (h *Handler) TOTPSetup(c *gin.Context) error {
	if h.auth == nil {
		return httperror.ReturnWithHTTPStatus(errWebNotConfigured, http.StatusInternalServerError)
	}
	// The token may be in the URL; links on the page mustn't pass it on to
	// other sites. Not no-referrer: with it, browsers send "Origin: null" on
	// the page's own forms, which the same-origin check refuses.
	c.Header("Referrer-Policy", "same-origin")
	token := c.Query("token")
	if c.Request.Method == http.MethodPost {
		token = c.PostForm("token")
	}
	if token == "" {
		return h.renderSetup(c, http.StatusOK, "", nil)
	}
	switch err := h.auth.Setup(c.ClientIP(), token); {
	case errors.Is(err, auth.ErrRateLimited):
		return h.renderSetup(c, http.StatusTooManyRequests, msgSetupRateLimited, nil)
	case errors.Is(err, auth.ErrInvalidToken):
		return h.renderSetup(c, http.StatusUnauthorized, msgWrongToken, nil)
	case err != nil:
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}
	uri := h.auth.KeyURI(c.Request.Host)
	png, err := qrcode.Encode(uri, qrcode.Medium, qrSize)
	if err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}
	s := &totpSetup{
		Secret: h.auth.Secret(),
		KeyURI: template.URL(uri),
		QR:     template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png)),
		Token:  token,
	}
	if code := c.PostForm("code"); code != "" {
		s.Tested, s.CodeOK = true, h.auth.CheckCode(code)
	}
	return h.renderSetup(c, http.StatusOK, "", s)
}

func (h *Handler) renderSetup(c *gin.Context, status int, msg string, s *totpSetup) error {
	return h.renderWeb(c, status, "totp_setup.html", webPage{
		Title:   "Set up authenticator",
		Version: h.versionOfService,
		Error:   msg,
		Setup:   s,
	})
}
