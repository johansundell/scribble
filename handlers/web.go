package handlers

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/httperror"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/store"
)

// The web app at /app (ADR 4, ADR 5). The router only registers these
// routes when WEB_PIN is set, and checks the session before App.

// AppHome is where a login lands, and LoginPath the PIN screen.
const (
	AppHome   = "/app"
	LoginPath = "/app/login"
)

// NotePath is the web app page of the note with id.
func NotePath(id int64) string {
	return AppHome + "/notes/" + strconv.FormatInt(id, 10)
}

// TitlePath is the web app page that opens the note titled title. Dots are
// escaped too, so a browser never reads "." or ".." as a path step.
func TitlePath(title string) string {
	return AppHome + "/title/" + strings.ReplaceAll(url.PathEscape(title), ".", "%2E")
}

const (
	msgWrongPIN    = "Wrong PIN"
	msgRateLimited = "Too many wrong PINs from this address. Try again later."
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
	// Notes pages only. Note is the open one, if any; Message replaces it
	// when there is nothing to show.
	Notes     []notes.Note
	Note      *notes.Note
	NoteHTML  template.HTML
	Backlinks []notes.Note
	Message   string
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
	c.Redirect(http.StatusSeeOther, LoginPath)
	return nil
}

// App lists the notes, with none open.
func (h *Handler) App(c *gin.Context) error {
	p, _, err := h.notesPage(c)
	if err != nil {
		return err
	}
	return h.renderNotes(c, http.StatusOK, p)
}

// AppNote shows the list and the note with id :id, rendered as Markdown,
// with the notes that link to it.
func (h *Handler) AppNote(c *gin.Context) error {
	p, ns, err := h.notesPage(c)
	if err != nil {
		return err
	}
	ctx := c.Request.Context()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return h.notesMessage(c, p, "Note not found")
	}
	n, err := ns.GetNote(ctx, id)
	if errors.Is(err, store.ErrNoteNotFound) {
		return h.notesMessage(c, p, "Note not found")
	}
	if err != nil {
		return noteError(err)
	}
	if p.Backlinks, err = ns.NoteBacklinks(ctx, id); err != nil {
		return noteError(err)
	}
	var lookupErr error
	exists := func(title string) bool {
		_, err := ns.GetNoteByTitle(ctx, title)
		if err != nil && !errors.Is(err, store.ErrNoteNotFound) {
			lookupErr = err
		}
		return err == nil
	}
	if p.NoteHTML, err = renderNote(n.Body, exists); err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}
	if lookupErr != nil {
		return noteError(lookupErr)
	}
	p.Title, p.Note = n.Title, n
	return h.renderNotes(c, http.StatusOK, p)
}

// AppTitle opens the note titled :title (ignoring case), which is where
// [[links]] go. #50 will create the note when there is none.
func (h *Handler) AppTitle(c *gin.Context) error {
	p, ns, err := h.notesPage(c)
	if err != nil {
		return err
	}
	title := strings.TrimPrefix(c.Param("title"), "/")
	n, err := ns.GetNoteByTitle(c.Request.Context(), title)
	if errors.Is(err, store.ErrNoteNotFound) {
		return h.notesMessage(c, p, "No note titled "+title)
	}
	if err != nil {
		return noteError(err)
	}
	c.Redirect(http.StatusSeeOther, NotePath(n.ID))
	return nil
}

// notesPage is the data for a page with the note list, and the store.
func (h *Handler) notesPage(c *gin.Context) (webPage, store.NoteStore, error) {
	if h.auth == nil {
		return webPage{}, nil, httperror.ReturnWithHTTPStatus(errWebNotConfigured, http.StatusInternalServerError)
	}
	ns, err := h.noteStore()
	if err != nil {
		return webPage{}, nil, err
	}
	p := h.appPage("Notes")
	if p.Notes, err = ns.ListNotes(c.Request.Context(), "", ""); err != nil {
		return webPage{}, nil, noteError(err)
	}
	return p, ns, nil
}

// notesMessage shows msg, with 404, where the note would be.
func (h *Handler) notesMessage(c *gin.Context, p webPage, msg string) error {
	p.Message = msg
	return h.renderNotes(c, http.StatusNotFound, p)
}

func (h *Handler) renderNotes(c *gin.Context, status int, p webPage) error {
	return h.renderWeb(c, status, "app_home.html", p)
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

// webFuncs are the functions web templates may call.
var webFuncs = template.FuncMap{"notePath": NotePath}

// renderWeb renders tmplFile inside the app layout. Web pages use
// html/template, so note text and form values are escaped.
func (h *Handler) renderWeb(c *gin.Context, status int, tmplFile string, data webPage) error {
	tmpl, err := template.New(tmplFile).Funcs(webFuncs).ParseFS(h.templates, "tmpl/"+tmplFile, "tmpl/app.html")
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
