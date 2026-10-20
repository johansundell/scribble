package handlers

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/httperror"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/store"
)

// The web app at the root (ADR 4, ADR 5, #92). The router only registers
// these routes when WEB_PIN is set, and checks the session before App. Its
// note pages are under /note, as /notes is the JSON API.

// AppHome is where a login lands, LoginPath the PIN screen and LogoutPath
// where Log out posts.
const (
	AppHome    = "/"
	LoginPath  = "/login"
	LogoutPath = "/logout"
	// NotePrefix starts a note's pages and TitlePrefix the open-by-title
	// page.
	NotePrefix  = "/note/"
	TitlePrefix = "/title/"
)

// LoginTo is the PIN screen, going on to next after login. The slashes in
// next are left as they are, for readability.
func LoginTo(next string) string {
	return LoginPath + "?next=" + strings.ReplaceAll(url.QueryEscape(next), "%2F", "/")
}

// NotePath is the web app page of the note with id.
func NotePath(id int64) string {
	return NotePrefix + strconv.FormatInt(id, 10)
}

// TitlePath is the web app page that opens the note titled title. Dots are
// escaped too, so a browser never reads "." or ".." as a path step.
func TitlePath(title string) string {
	return TitlePrefix + strings.ReplaceAll(url.PathEscape(title), ".", "%2E")
}

// ProjectPath is the list of the notes tagged #project.
func ProjectPath(project string) string {
	return AppHome + "?project=" + url.QueryEscape(notes.NormalizeProject(project))
}

const (
	msgWrongPIN     = "Wrong PIN"
	msgRateLimited  = "Too many wrong PINs from this address. Try again later."
	msgLocked       = "Web login is locked after too many wrong PINs. Restart the service (or wait 24 hours) to unlock it."
	msgNoteNotFound = "Note not found"
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
	// Edit is the editor, Rename and Delete their forms and Share the
	// share pane, shown in place of the note.
	Edit   *noteEditor
	Rename *noteForm
	Delete *noteForm
	Share  *shareForm
	// The list's filter: Query searches titles and bodies, and Project (a
	// name, without '#') keeps the notes tagged with it. Projects are the
	// ones to choose from.
	Query    string
	Project  string
	Projects []string
}

// Filter is the list's filter as a query string ("?project=…&q=…"), or ""
// with none. Links and redirects carry it, so the filter stays as the user
// moves around.
func (p webPage) Filter() string {
	v := url.Values{}
	if p.Query != "" {
		v.Set("q", p.Query)
	}
	if p.Project != "" {
		v.Set("project", p.Project)
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

// NotePane reports whether the page is about a note (or says why there is
// none), so that phones show it in place of the list (#65).
func (p webPage) NotePane() bool {
	return p.Note != nil || p.Edit != nil || p.Rename != nil || p.Delete != nil || p.Share != nil || p.Message != ""
}

// Filtered reports whether the list is filtered.
func (p webPage) Filtered() bool {
	return p.Query != "" || p.Project != ""
}

// LoginPage shows the PIN entry screen, or sends someone with a valid
// session on to next or the app.
func (h *Handler) LoginPage(c *gin.Context) error {
	if h.auth == nil {
		return httperror.ReturnWithHTTPStatus(errWebNotConfigured, http.StatusInternalServerError)
	}
	if h.auth.Session(c.Request) {
		c.Redirect(http.StatusSeeOther, SafeNext(c.Query("next")))
		return nil
	}
	msg := ""
	if h.auth.Locked() {
		msg = msgLocked
	}
	return h.renderLogin(c, http.StatusOK, msg)
}

// Login checks the posted PIN. The right one starts a session and goes to
// next (a web app page) or AppHome; a wrong one shows the screen again.
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
	p, ns, n, err := h.webNote(c)
	if err != nil || n == nil {
		return err
	}
	ctx := c.Request.Context()
	if p.Backlinks, err = ns.NoteBacklinks(ctx, n.ID); err != nil {
		return noteError(err)
	}
	if p.NoteHTML, err = renderNoteBody(ctx, ns, n.Body); err != nil {
		return err
	}
	p.Title, p.Note = n.Title, n
	return h.renderNotes(c, http.StatusOK, p)
}

// renderNoteBody renders body as the note view shows it, looking up in ns
// which [[links]] go to a note.
func renderNoteBody(ctx context.Context, ns store.NoteStore, body string) (template.HTML, error) {
	var lookupErr error
	exists := func(title string) bool {
		_, err := ns.GetNoteByTitle(ctx, title)
		if err != nil && !errors.Is(err, store.ErrNoteNotFound) {
			lookupErr = err
		}
		return err == nil
	}
	out, err := renderNote(body, exists)
	if err != nil {
		return "", httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}
	if lookupErr != nil {
		return "", noteError(lookupErr)
	}
	return out, nil
}

// AppTitle opens the note titled :title (ignoring case), which is where
// [[links]] go. When there is none it opens the new-note editor with that
// title; the note is only created when it is saved.
func (h *Handler) AppTitle(c *gin.Context) error {
	p, ns, err := h.notesPage(c)
	if err != nil {
		return err
	}
	title := strings.TrimPrefix(c.Param("title"), "/")
	n, err := ns.GetNoteByTitle(c.Request.Context(), title)
	if errors.Is(err, store.ErrNoteNotFound) {
		ed := newNoteEditor()
		ed.Body = "# " + title + "\n"
		return h.renderEditor(c, http.StatusOK, p, "New note", ed)
	}
	if err != nil {
		return noteError(err)
	}
	c.Redirect(http.StatusSeeOther, NotePath(n.ID))
	return nil
}

// notesPage is the data for a page with the note list, filtered by ?q= and
// ?project=, and the store. A project that isn't a #project name is
// ignored.
func (h *Handler) notesPage(c *gin.Context) (webPage, store.NoteStore, error) {
	if h.auth == nil {
		return webPage{}, nil, httperror.ReturnWithHTTPStatus(errWebNotConfigured, http.StatusInternalServerError)
	}
	ns, err := h.noteStore()
	if err != nil {
		return webPage{}, nil, err
	}
	ctx := c.Request.Context()
	p := h.appPage("Notes")
	p.Query = strings.TrimSpace(c.Query("q"))
	if projectTag(c.Query("project")) != "" {
		p.Project = notes.NormalizeProject(c.Query("project"))
	}
	if p.Notes, err = ns.ListNotes(ctx, p.Query, p.Project); err != nil {
		return webPage{}, nil, noteError(err)
	}
	if p.Projects, err = ns.Projects(ctx); err != nil {
		return webPage{}, nil, noteError(err)
	}
	if p.Project != "" && !slices.Contains(p.Projects, p.Project) {
		// No note has it any more; still show it as the one chosen.
		p.Projects = append(p.Projects, p.Project)
		slices.Sort(p.Projects)
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
var webFuncs = template.FuncMap{
	"notePath":       NotePath,
	"editPath":       EditPath,
	"renamePath":     RenamePath,
	"deletePath":     DeletePath,
	"sharePath":      SharePath,
	"deleteQuestion": deleteQuestion,
}

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

// SafeNext returns next when it is a plain path to a web app page, and
// AppHome otherwise, so the login can't send anyone to another site, or to
// the API, health check, shares or assets on this one (#95).
func SafeNext(next string) string {
	if strings.Contains(next, `\`) {
		return AppHome
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Opaque != "" {
		return AppHome
	}
	if !strings.HasPrefix(u.Path, "/") {
		return AppHome
	}
	// Only a clean path: "/note/../x" is not where it seems to go.
	if u.Path != "/" && path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") {
		return AppHome
	}
	if !isAppPage(u.Path) {
		return AppHome
	}
	return next
}

// isAppPage reports whether p is a web app page: the list, the new-note
// editor, or a page under a note or title.
func isAppPage(p string) bool {
	switch p {
	case AppHome, NewPath:
		return true
	}
	for _, prefix := range []string{NotePrefix, TitlePrefix} {
		if len(p) > len(prefix) && strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}
