package handlers

import (
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/store"
)

// A note's history in the web app (#110, ADR 7): its kept versions, one of
// them rendered, and Restore. It works like the share pane: a GET shows it
// and a POST restores.

// HistoryPath is the history pane of the note with id.
func HistoryPath(id int64) string {
	return NotePath(id) + "/history"
}

// What the history pane's restore form sends as "action".
const actionRestore = "restore"

const (
	msgVersionNotFound = "That version of the note was not kept."
	msgRestoreStale    = "The note has changed since this page was loaded. Look at its history again before restoring."
)

// historyView is the history pane of one note.
type historyView struct {
	ID    int64
	Title string
	// Back is the note's page, Action where Restore posts.
	Back   string
	Action string
	// Revision is the note's current revision, sent back with a restore.
	Revision int64
	Versions []historyItem
	// Shown is the version on show, rendered as ShownHTML.
	Shown     historyItem
	ShownHTML template.HTML
	Error     string
}

// historyItem is one version in the history pane's list.
type historyItem struct {
	Revision int64
	Event    string
	At       time.Time
	Href     string
	Current  bool
	Selected bool
}

// noteMeta is the note view's line about its history.
type noteMeta struct {
	Revision int64
	// Edits is how many changes the history keeps since the note was
	// created; merged saves count once.
	Edits   int
	Updated time.Time
}

// historyEvents are the words for a version's reason. A reason not listed
// is shown as it is.
var historyEvents = map[string]string{
	"create":             "Created",
	"edit":               "Edited",
	"rename":             "Renamed",
	"link-rewrite":       "Links updated",
	"restore":            "Restored",
	"delete":             "Deleted",
	"undelete":           "Undeleted",
	"filemaker-conflict": "Sync conflict",
	"conflict-kept-app":  "Sync conflict",
}

// historyEvent is the badge text for reason.
func historyEvent(reason string) string {
	if e, ok := historyEvents[reason]; ok {
		return e
	}
	return reason
}

// restoreQuestion is what Restore asks, in the confirm() dialog.
func restoreQuestion(rev int64, title string) string {
	return "Restore revision r" + strconv.FormatInt(rev, 10) + ` of "` + title + `"?`
}

// AppHistory shows the note's versions and one of them: ?rev=, or by
// default the one before the current, so a change shows straight away.
func (h *Handler) AppHistory(c *gin.Context) error {
	p, ns, n, err := h.webNote(c)
	if err != nil || n == nil {
		return err
	}
	rev := int64(0)
	if s, ok := c.GetQuery("rev"); ok {
		if rev, err = strconv.ParseInt(s, 10, 64); err != nil || rev <= 0 {
			return h.notesMessage(c, p, msgVersionNotFound)
		}
	}
	return h.renderHistory(c, http.StatusOK, p, ns, n, rev, "")
}

// AppRestore makes the posted revision the note's text again, as a new
// revision, and shows the note. base_revision, when sent, must still be the
// note's revision, as with a save.
func (h *Handler) AppRestore(c *gin.Context) error {
	p, ns, n, err := h.webNote(c)
	if err != nil || n == nil {
		return err
	}
	if c.PostForm("action") != actionRestore {
		return httperror.ReturnWithHTTPStatus(errUnknownAction, http.StatusBadRequest)
	}
	rev, err := strconv.ParseInt(c.PostForm("revision"), 10, 64)
	if err != nil || rev <= 0 {
		return h.notesMessage(c, p, msgVersionNotFound)
	}
	base, _ := strconv.ParseInt(c.PostForm("base_revision"), 10, 64)
	_, err = ns.RestoreVersion(c.Request.Context(), n.ID, rev, base)
	switch {
	case err == nil:
		c.Redirect(http.StatusSeeOther, NotePath(n.ID)+p.Filter())
		return nil
	case errors.Is(err, store.ErrVersionNotFound):
		return h.notesMessage(c, p, msgVersionNotFound)
	case errors.Is(err, store.ErrStaleRevision):
		return h.renderHistory(c, http.StatusConflict, p, ns, n, rev, msgRestoreStale)
	case errors.Is(err, store.ErrNoteNotFound), errors.Is(err, store.ErrNoteDeleted):
		return h.notesMessage(c, p, msgNoteNotFound)
	}
	return noteError(err)
}

// renderHistory shows n's history with version rev, or the default one
// when rev is 0.
func (h *Handler) renderHistory(c *gin.Context, status int, p webPage, ns store.NoteStore, n *notes.Note, rev int64, msg string) error {
	ctx := c.Request.Context()
	versions, err := ns.NoteVersions(ctx, n.ID)
	if err != nil {
		return noteError(err)
	}
	if len(versions) == 0 {
		return h.notesMessage(c, p, msgVersionNotFound)
	}
	if rev == 0 {
		rev = versions[min(1, len(versions)-1)].Revision
	}
	v, err := ns.NoteVersion(ctx, n.ID, rev)
	if errors.Is(err, store.ErrVersionNotFound) {
		return h.notesMessage(c, p, msgVersionNotFound)
	}
	if err != nil {
		return noteError(err)
	}
	hv := &historyView{
		ID:       n.ID,
		Title:    n.Title,
		Back:     NotePath(n.ID) + p.Filter(),
		Action:   HistoryPath(n.ID) + p.Filter(),
		Revision: n.Revision,
		Error:    msg,
	}
	for _, ver := range versions {
		it := historyItem{
			Revision: ver.Revision,
			Event:    historyEvent(ver.Reason),
			At:       ver.UpdatedAt,
			Href:     historyRevPath(n.ID, ver.Revision, p),
			Current:  ver.Revision == n.Revision,
			Selected: ver.Revision == rev,
		}
		if it.Selected {
			hv.Shown = it
		}
		hv.Versions = append(hv.Versions, it)
	}
	if hv.ShownHTML, err = renderNoteBody(ctx, ns, v.Body); err != nil {
		return err
	}
	p.Title = "History of " + n.Title
	p.History = hv
	return h.renderNotes(c, status, p)
}

// historyRevPath is the history pane of note id showing revision rev,
// keeping p's filter.
func historyRevPath(id, rev int64, p webPage) string {
	v := p.filterValues()
	v.Set("rev", strconv.FormatInt(rev, 10))
	return HistoryPath(id) + "?" + v.Encode()
}

// noteMetaOf is the history line of n, whose versions are versions.
func noteMetaOf(n *notes.Note, versions []notes.Version) *noteMeta {
	return &noteMeta{Revision: n.Revision, Edits: max(len(versions)-1, 0), Updated: n.UpdatedAt}
}
