package handlers

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"text/template"

	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/replica"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/utils"
)

type Handler struct {
	store            store.Store
	templates        fs.FS // holds tmpl/*.html
	nameOfService    string
	versionOfService string
	auth             *auth.Authenticator // nil: the web app pages fail closed
	replica          ReplicaReporter     // nil: no replica, so /healthz omits it
	loginHint        string              // WEB_LOGIN_HINT, shown on the login screen
	publicDemo       bool                // PUBLIC_DEMO: the login screen shows the TOTP secret
}

// ReplicaReporter reports the FileMaker replica's state (#66);
// *replica.Syncer is one.
type ReplicaReporter interface {
	Status(ctx context.Context) replica.Status
}

// Option configures optional Handler dependencies.
type Option func(*Handler)

// WithAuth turns on the web app's login with authenticator codes.
func WithAuth(a *auth.Authenticator) Option {
	return func(h *Handler) { h.auth = a }
}

// WithLoginHint shows hint, as plain text, under the code field (#168).
func WithLoginHint(hint string) Option {
	return func(h *Handler) { h.loginHint = hint }
}

// WithPublicDemo shows the TOTP secret, as a QR code and as text, on the
// login screen, so visitors of a public demo need no AUTH_TOKEN (#175).
func WithPublicDemo(on bool) Option {
	return func(h *Handler) { h.publicDemo = on }
}

// WithReplica adds the FileMaker replica's state to /healthz.
func WithReplica(r ReplicaReporter) Option {
	return func(h *Handler) { h.replica = r }
}

// NewHandler creates the handlers. With useFileSystem, templates are read
// from the tmpl folder next to the binary on every request (edit without
// rebuilding) and embedded is ignored; otherwise the embedded filesystem is
// required.
func NewHandler(s store.Store, useFileSystem bool, embedded fs.FS, name, version string, opts ...Option) (*Handler, error) {
	templates := embedded
	if useFileSystem {
		templates = os.DirFS(utils.GetBinaryBasePath())
	} else if embedded == nil {
		return nil, errors.New("embedded templates filesystem is nil")
	}
	h := &Handler{
		store:            s,
		templates:        templates,
		nameOfService:    name,
		versionOfService: version,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h, nil
}

func (h *Handler) getTemplate(withBase bool, tmplFile ...string) (*template.Template, error) {
	files := make([]string, 0, len(tmplFile)+1)
	for _, t := range tmplFile {
		files = append(files, "tmpl/"+t)
	}
	if withBase {
		files = append(files, "tmpl/base.html")
	}
	return template.ParseFS(h.templates, files...)
}
