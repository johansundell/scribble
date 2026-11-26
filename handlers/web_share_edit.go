package handlers

import (
	"errors"
	"html/template"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/store"
)

// Edit links (#195, ADR 15): like a share link, but whoever has one can also
// edit the note, through the web app's editor, and see the versions made
// since the link was. They can't rename it (the title line stays), restore,
// delete, pin or share it. Like /s/, these pages need no session.

// EditSharedPath is the public page of the note shared for editing with
// token.
func EditSharedPath(token string) string {
	return "/e/" + token
}

// EditSharedEditPath is the editor behind edit link token.
func EditSharedEditPath(token string) string {
	return EditSharedPath(token) + "/edit"
}

// EditSharedHistoryPath is the history behind edit link token.
func EditSharedHistoryPath(token string) string {
	return EditSharedPath(token) + "/history"
}

const (
	msgTitleChanged = "The title can't be changed through a share link."
	msgLinkGone     = "This link no longer works. Copy your text before you leave."
	msgNoChanges    = "No changes since this link was made."
)

// editLinkNote is the note behind the edit link in :token and the link, or
// a 404 when the token is unknown, revoked or expired.
func (h *Handler) editLinkNote(c *gin.Context) (store.NoteStore, *notes.Note, store.Share, error) {
	ns, err := h.noteStore()
	if err != nil {
		return nil, nil, store.Share{}, err
	}
	n, sh, err := ns.GetEditSharedNote(c.Request.Context(), c.Param("token"))
	if err != nil {
		return nil, nil, store.Share{}, noteError(err)
	}
	return ns, n, sh, nil
}

// EditShared shows the note behind an edit link, rendered as /s/ shows it,
// with Edit and History.
func (h *Handler) EditShared(c *gin.Context) error {
	_, n, sh, err := h.editLinkNote(c)
	if err != nil {
		return err
	}
	out, err := renderSharedNote(n.Body)
	if err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}
	expires := expiresText(sh.ExpiresAt)
	if expires == "" {
		expires = "Never expires"
	}
	return h.renderShared(c, http.StatusOK, sharedPage{
		Title:    n.Title,
		NoteHTML: out,
		Updated:  n.UpdatedAt.UTC().Format("2006-01-02 15:04 UTC"),
		Expires:  expires,
		EditHref:    EditSharedEditPath(sh.Token),
		HistoryHref: EditSharedHistoryPath(sh.Token),
	})
}

// sharedEditor is the editor behind edit link token.
func sharedEditor(n *notes.Note, token string) *noteEditor {
	return &noteEditor{ID: n.ID, Action: EditSharedEditPath(token), Cancel: EditSharedPath(token)}
}

// EditSharedForm shows the editor behind an edit link.
func (h *Handler) EditSharedForm(c *gin.Context) error {
	_, n, sh, err := h.editLinkNote(c)
	if err != nil {
		return err
	}
	ed := sharedEditor(n, sh.Token)
	ed.Body, ed.Revision = n.Body, n.Revision
	return h.renderShared(c, http.StatusOK, sharedPage{Title: n.Title, Editor: ed})
}

// EditSharedSave saves the editor behind an edit link, as AppSave does:
// Save stays in the editor, Done shows the note, and a stale revision offers
// Reload and Overwrite. A save that changes the title is refused.
func (h *Handler) EditSharedSave(c *gin.Context) error {
	ns, n, sh, err := h.editLinkNote(c)
	if err != nil {
		return err
	}
	body, err := formBody(c)
	if err != nil {
		return err
	}
	rev, err := strconv.ParseInt(c.PostForm("revision"), 10, 64)
	if err != nil || rev < 1 {
		return httperror.ReturnWithHTTPStatus(errors.New("invalid revision"), http.StatusBadRequest)
	}
	ed := sharedEditor(n, sh.Token)
	ed.Body, ed.Revision, ed.Dirty = body, rev, true
	action := c.PostForm("action")
	if action == actionReload {
		ed.Body, ed.Revision, ed.Draft, ed.Dirty = n.Body, n.Revision, body, false
		return h.renderShared(c, http.StatusOK, sharedPage{Title: n.Title, Editor: ed})
	}
	base := rev
	if action == actionOverwrite {
		base = 0
	}
	_, err = ns.UpdateEditSharedNote(c.Request.Context(), sh.Token, body, base)
	status := http.StatusBadRequest
	switch {
	case err == nil:
		to := EditSharedPath(sh.Token)
		if action == actionSave {
			to = EditSharedEditPath(sh.Token)
		}
		c.Redirect(http.StatusSeeOther, to)
		return nil
	case errors.Is(err, store.ErrTitleChanged):
		ed.Error = msgTitleChanged
	case errors.Is(err, store.ErrStaleRevision):
		status, ed.Error, ed.Conflict = http.StatusPreconditionFailed, msgStale, true
	case errors.Is(err, store.ErrNoteNotFound):
		// Revoked, expired or deleted since the page was loaded.
		status, ed.Error = http.StatusNotFound, msgLinkGone
	default:
		return noteError(err)
	}
	return h.renderShared(c, status, sharedPage{Title: n.Title, Editor: ed})
}

// EditSharedHistory shows the versions of the note made since its edit link
// was, and one of them: ?rev=, or by default the one before the newest.
// Older versions stay hidden, so text removed before sharing is not shown.
func (h *Handler) EditSharedHistory(c *gin.Context) error {
	ns, n, sh, err := h.editLinkNote(c)
	if err != nil {
		return err
	}
	ctx := c.Request.Context()
	all, err := ns.NoteVersions(ctx, n.ID)
	if err != nil {
		return noteError(err)
	}
	var versions []notes.Version
	for _, v := range all {
		// A merged save is as new as its last save.
		if !v.UpdatedAt.Before(sh.CreatedAt) {
			versions = append(versions, v)
		}
	}
	back := EditSharedPath(sh.Token)
	hv := &historyView{ID: n.ID, Title: n.Title, Back: back}
	page := sharedPage{Title: "History of " + n.Title, History: hv}
	if len(versions) == 0 {
		if _, ok := c.GetQuery("rev"); ok {
			return noteError(store.ErrVersionNotFound)
		}
		hv.Error = msgNoChanges
		return h.renderShared(c, http.StatusOK, page)
	}
	rev := versions[min(1, len(versions)-1)].Revision
	if s, ok := c.GetQuery("rev"); ok {
		if rev, err = strconv.ParseInt(s, 10, 64); err != nil {
			return noteError(store.ErrVersionNotFound)
		}
	}
	for _, v := range versions {
		it := historyItem{
			Revision: v.Revision,
			Reason:   reasonLabel(v.Reason),
			At:       v.UpdatedAt,
			Href:     EditSharedHistoryPath(sh.Token) + "?rev=" + strconv.FormatInt(v.Revision, 10),
			Current:  v.Revision == n.Revision,
			Selected: v.Revision == rev,
		}
		if it.Selected {
			hv.Shown = it
		}
		hv.Versions = append(hv.Versions, it)
	}
	if hv.Shown.Revision == 0 {
		return noteError(store.ErrVersionNotFound) // not one made since sharing
	}
	v, err := ns.NoteVersion(ctx, n.ID, rev)
	if err != nil {
		return noteError(err)
	}
	if hv.ShownHTML, err = renderSharedNote(v.Body); err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}
	return h.renderShared(c, http.StatusOK, page)
}

// sharedPage is the data of a share link's public page: the note on its
// own, or, behind an edit link, the note with Edit and History, its editor
// or its history.
type sharedPage struct {
	Title    string
	NoteHTML template.HTML
	Updated  string
	Expires  string
	// EditHref and HistoryHref are the edit link's editor and history
	// pages, which fill in Editor and History.
	EditHref    string
	HistoryHref string
	Editor      *noteEditor
	History     *historyView
}

// renderShared renders a share link's public page with p. Its URL holds
// the token, so it is never sent to the sites the note links to, and the
// page is kept out of search engines.
func (h *Handler) renderShared(c *gin.Context, status int, p sharedPage) error {
	tmpl, err := template.New("shared.html").Funcs(webFuncs).ParseFS(h.templates, "tmpl/shared.html", "tmpl/editor.html")
	if err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Robots-Tag", "noindex")
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := tmpl.Execute(c.Writer, p); err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}
	return nil
}
