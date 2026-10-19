package handlers

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/httperror"
)

// The web app at /app (ADR 4, ADR 5). The router only registers these
// routes when WEB_PIN is set, and checks the session before AppHome.

// AppHome is where a login lands.
const AppHome = "/app"

const (
	msgWrongPIN    = "Wrong PIN"
	msgRateLimited = "Too many wrong PINs. Try again in 15 minutes."
	msgLocked      = "Web login is locked after too many wrong PINs. Restart the service (or wait 24 hours) to unlock it."
)

var errWebNotConfigured = errors.New("web login is not configured")

// webPage is the data every web app page gets.
type webPage struct {
	Title    string
	Version  string
	LoggedIn bool
	Alert    *auth.PINAlert
	// Login page only.
	Error string
	Next  string
}

// LoginPage shows the PIN entry screen, or sends someone with a valid
// session on to the app.
func (h *Handler) LoginPage(c *gin.Context) error {
	if h.auth == nil {
		return httperror.ReturnWithHTTPStatus(errWebNotConfigured, http.StatusInternalServerError)
	}
	if h.auth.Session(c.Request) {
		c.Redirect(http.StatusSeeOther, AppHome)
		return nil
	}
	msg := ""
	if h.auth.Locked() {
		msg = msgLocked
	}
	return h.renderLogin(c, http.StatusOK, msg)
}

// Login checks the posted PIN. The right one starts a session and goes to
// next (a path under /app) or AppHome; a wrong one shows the screen again.
func (h *Handler) Login(c *gin.Context) error {
	if h.auth == nil {
		return httperror.ReturnWithHTTPStatus(errWebNotConfigured, http.StatusInternalServerError)
	}
	err := h.auth.Login(c.ClientIP(), c.PostForm("pin"))
	switch {
	case errors.Is(err, auth.ErrLocked):
		return h.renderLogin(c, http.StatusTooManyRequests, msgLocked)
	case errors.Is(err, auth.ErrRateLimited):
		return h.renderLogin(c, http.StatusTooManyRequests, msgRateLimited)
	case errors.Is(err, auth.ErrInvalidPIN):
		return h.renderLogin(c, http.StatusUnauthorized, msgWrongPIN)
	case err != nil:
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}
	h.auth.StartSession(c.Writer)
	c.Redirect(http.StatusSeeOther, SafeNext(c.PostForm("next")))
	return nil
}

// Logout ends the session on this device. It needs no session: logging out
// twice, or after expiry, does no harm.
func (h *Handler) Logout(c *gin.Context) error {
	if h.auth == nil {
		return httperror.ReturnWithHTTPStatus(errWebNotConfigured, http.StatusInternalServerError)
	}
	h.auth.EndSession(c.Writer)
	c.Redirect(http.StatusSeeOther, "/app/login")
	return nil
}

// App shows the app's layout. The note list and the note come later (#48).
func (h *Handler) App(c *gin.Context) error {
	if h.auth == nil {
		return httperror.ReturnWithHTTPStatus(errWebNotConfigured, http.StatusInternalServerError)
	}
	return h.renderWeb(c, http.StatusOK, "app_home.html", h.appPage("Notes"))
}

// appPage is the data for a page behind the login, with the PIN Alert when
// it has triggered.
func (h *Handler) appPage(title string) webPage {
	p := webPage{Title: title, Version: h.versionOfService, LoggedIn: true}
	if alert, ok := h.auth.PINAlert(); ok {
		p.Alert = &alert
	}
	return p
}

func (h *Handler) renderLogin(c *gin.Context, status int, msg string) error {
	next := c.PostForm("next")
	if next == "" {
		next = c.Query("next")
	}
	return h.renderWeb(c, status, "login.html", webPage{
		Title:   "Log in",
		Version: h.versionOfService,
		Error:   msg,
		Next:    SafeNext(next),
	})
}

// renderWeb renders tmplFile inside the app layout. Web pages use
// html/template, so note text and form values are escaped.
func (h *Handler) renderWeb(c *gin.Context, status int, tmplFile string, data webPage) error {
	tmpl, err := template.ParseFS(h.templates, "tmpl/"+tmplFile, "tmpl/app.html")
	if err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := tmpl.ExecuteTemplate(c.Writer, "app", data); err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}
	return nil
}

// SafeNext returns next when it is a plain path under /app, and AppHome
// otherwise, so the login can't send anyone to another site.
func SafeNext(next string) string {
	if strings.Contains(next, `\`) {
		return AppHome
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Opaque != "" {
		return AppHome
	}
	if u.Path != AppHome && !strings.HasPrefix(u.Path, AppHome+"/") {
		return AppHome
	}
	// "/app/../x" would leave /app once the browser resolves it.
	if path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") {
		return AppHome
	}
	return next
}
