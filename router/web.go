package router

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/httperror"
)

// RequireSession lets the request through only with a valid web app
// session. Anyone else is sent to the PIN screen, which returns them to the
// page they asked for. NewRouter only adds web routes when a is set.
func RequireSession(a *auth.Authenticator) func(HandlerFuncWithError) HandlerFuncWithError {
	return func(inner HandlerFuncWithError) HandlerFuncWithError {
		return func(c *gin.Context) error {
			if !a.Session(c.Request) {
				c.Redirect(http.StatusSeeOther, loginURL(c.Request))
				return nil
			}
			return inner(c)
		}
	}
}

// loginURL is the PIN screen, with next set to r's page when r is a page
// view. The slashes in next are left as they are, for readability.
func loginURL(r *http.Request) string {
	if !isSafeMethod(r.Method) {
		return handlers.LoginPath
	}
	return handlers.LoginPath + "?next=" + strings.ReplaceAll(url.QueryEscape(r.URL.RequestURI()), "%2F", "/")
}

// isSafeMethod reports whether method only reads (GET or HEAD), so it needs
// no same-origin check and can be returned to after login.
func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

// SameOrigin refuses, with 403, a request whose Origin header (or, without
// one, Referer) is not on the request's own host. With SameSite=Lax cookies
// this stops cross-site form posts without CSRF tokens (#47).
func SameOrigin(inner HandlerFuncWithError) HandlerFuncWithError {
	return func(c *gin.Context) error {
		src := c.GetHeader("Origin")
		if src == "" {
			src = c.GetHeader("Referer")
		}
		u, err := url.Parse(src)
		if src == "" || err != nil || u.Host == "" || u.Host != c.Request.Host {
			return httperror.ReturnWithHTTPStatus(errors.New("cross-origin request refused"), http.StatusForbidden)
		}
		return inner(c)
	}
}

// noStore keeps web app pages out of shared caches and the back/forward
// cache, so notes don't linger after logout.
func noStore(inner HandlerFuncWithError) HandlerFuncWithError {
	return func(c *gin.Context) error {
		c.Header("Cache-Control", "no-store")
		return inner(c)
	}
}
