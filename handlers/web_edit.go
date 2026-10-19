package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/store"
)

// The web app's note editor (#50): a plain form that posts the note's
// Markdown, so it works without JavaScript. editor.js adds the unsaved
// changes check and saving with fetch.

// NewPath is the new-note editor.
const NewPath = AppHome + "/new"

// EditPath is the editor of the note with id.
func EditPath(id int64) string {
	return NotePath(id) + "/edit"
}

const (
	msgNoTitle      = "Type a title on the first line."
	msgTagOnly      = "Type a title on the first line, above the #tag."
	msgStale        = "This note was changed elsewhere since you started editing."
	msgDeletedSince = "This note was deleted elsewhere."
)

// What the editor's submit buttons send as "action". Anything else counts
// as Done.
const (
	actionSave      = "save"
	actionReload    = "reload"
	actionOverwrite = "overwrite"
)

// noteEditor is the editor's form.
type noteEditor struct {
	ID     int64 // 0 for a new note
	Action string
	Cancel string
	Body   string
	// Revision is the one the edit started from, posted as "revision" and
	// passed to the store as the base revision.
	Revision int64
	// Tag is the #project a new note was prefilled with, "" if none.
	Tag   string
	Error string
	// Conflict offers Keep editing, Reload and Overwrite.
	Conflict bool
	// Draft is the user's text, shown read-only below the latest version
	// after Reload.
	Draft string
	// Dirty says Body is not what is saved, so leaving asks first. A Draft
	// counts too: leaving would lose it.
	Dirty bool
}

func newNoteEditor() *noteEditor {
	return &noteEditor{Action: NewPath, Cancel: AppHome}
}

// AppNew shows an empty editor. With ?project= it starts with the #project
// tag below an empty title line, like the desktop app.
func (h *Handler) AppNew(c *gin.Context) error {
	p, _, err := h.notesPage(c)
	if err != nil {
		return err
	}
	ed := newNoteEditor()
	if ed.Tag = projectTag(p.Project); ed.Tag != "" {
		ed.Body = "\n\n" + ed.Tag + "\n"
	}
	return h.renderEditor(c, http.StatusOK, p, "New note", ed)
}

// AppCreate creates a note from the new-note editor.
func (h *Handler) AppCreate(c *gin.Context) error {
	p, ns, err := h.notesPage(c)
	if err != nil {
		return err
	}
	body, err := formBody(c)
	if err != nil {
		return err
	}
	ed := newNoteEditor()
	ed.Body, ed.Tag, ed.Dirty = body, projectTag(c.PostForm("tag")), true
	if ed.Tag != "" && strings.EqualFold(notes.TitleLine(body), ed.Tag) && !notes.HasProject(body, ed.Tag) {
		// Saved like this, the tag would become the title and the note
		// would not be in the project.
		ed.Error = msgTagOnly
		return h.renderEditor(c, http.StatusBadRequest, p, "New note", ed)
	}
	n, err := ns.CreateNote(c.Request.Context(), body)
	if err != nil {
		return h.editFailed(c, p, "New note", ed, err)
	}
	savedRedirect(c, p, n.ID)
	return nil
}

// AppEdit shows the editor with the note's Markdown.
func (h *Handler) AppEdit(c *gin.Context) error {
	p, ns, err := h.notesPage(c)
	if err != nil {
		return err
	}
	id, ok := webNoteID(c)
	if !ok {
		return h.notesMessage(c, p, "Note not found")
	}
	n, err := ns.GetNote(c.Request.Context(), id)
	if errors.Is(err, store.ErrNoteNotFound) {
		return h.notesMessage(c, p, "Note not found")
	}
	if err != nil {
		return noteError(err)
	}
	ed := editorFor(id)
	ed.Body, ed.Revision = n.Body, n.Revision
	return h.renderEditor(c, http.StatusOK, p, n.Title, ed)
}

// AppSave saves the editor of note :id: Save stays in the editor and Done
// shows the note. After a conflict, Reload shows the latest version with
// the user's text below it, and Overwrite saves over whatever is there.
func (h *Handler) AppSave(c *gin.Context) error {
	p, ns, err := h.notesPage(c)
	if err != nil {
		return err
	}
	id, ok := webNoteID(c)
	if !ok {
		return h.notesMessage(c, p, "Note not found")
	}
	body, err := formBody(c)
	if err != nil {
		return err
	}
	rev, err := strconv.ParseInt(c.PostForm("revision"), 10, 64)
	if err != nil || rev < 1 {
		// Revision 0 would overwrite without asking.
		return httperror.ReturnWithHTTPStatus(errors.New("invalid revision"), http.StatusBadRequest)
	}
	ctx := c.Request.Context()
	ed := editorFor(id)
	ed.Body, ed.Revision, ed.Dirty = body, rev, true
	title := notes.TitleOf(body)
	action := c.PostForm("action")
	if action == actionReload {
		n, err := ns.GetNote(ctx, id)
		if err != nil {
			return h.editFailed(c, p, title, ed, err)
		}
		ed.Body, ed.Revision, ed.Draft, ed.Dirty = n.Body, n.Revision, body, false
		return h.renderEditor(c, http.StatusOK, p, n.Title, ed)
	}
	base := rev
	if action == actionOverwrite {
		base = 0
	}
	if _, err := ns.UpdateNote(ctx, id, body, base); err != nil {
		return h.editFailed(c, p, title, ed, err)
	}
	savedRedirect(c, p, id)
	return nil
}

// editFailed shows the editor again, with the user's text and why the save
// was refused.
func (h *Handler) editFailed(c *gin.Context, p webPage, title string, ed *noteEditor, err error) error {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, store.ErrTitleTaken):
		status, ed.Error = http.StatusConflict, "A note titled "+notes.TitleOf(ed.Body)+" already exists."
	case errors.Is(err, store.ErrNoTitle):
		ed.Error = msgNoTitle
	case errors.Is(err, store.ErrStaleRevision):
		status, ed.Error, ed.Conflict = http.StatusPreconditionFailed, msgStale, true
	case errors.Is(err, store.ErrNoteNotFound):
		status, ed.Error = http.StatusNotFound, msgDeletedSince
	default:
		return noteError(err)
	}
	if title == "" {
		title = "New note"
	}
	return h.renderEditor(c, status, p, title, ed)
}

// savedRedirect goes on from a saved note, keeping the list's filter: its
// editor for Save, its page for Done and Overwrite.
func savedRedirect(c *gin.Context, p webPage, id int64) {
	to := NotePath(id)
	if c.PostForm("action") == actionSave {
		to = EditPath(id)
	}
	c.Redirect(http.StatusSeeOther, to+p.Filter())
}

// renderEditor shows ed in place of the note. Its form and Cancel keep the
// list's filter.
func (h *Handler) renderEditor(c *gin.Context, status int, p webPage, title string, ed *noteEditor) error {
	ed.Action += p.Filter()
	ed.Cancel += p.Filter()
	p.Title, p.Edit = title, ed
	return h.renderNotes(c, status, p)
}

func editorFor(id int64) *noteEditor {
	return &noteEditor{ID: id, Action: EditPath(id), Cancel: NotePath(id)}
}

// webNoteID is the :id of a web app note page, if it is one.
func webNoteID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	return id, err == nil && id > 0
}

// formBody is the posted note text, with the browser's CRLF line ends
// turned into LF.
func formBody(c *gin.Context) (string, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxNoteRequestBytes)
	if err := c.Request.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return "", httperror.ReturnWithHTTPStatus(err, http.StatusRequestEntityTooLarge)
		}
		return "", httperror.ReturnWithHTTPStatus(err, http.StatusBadRequest)
	}
	return strings.ReplaceAll(c.PostForm("body"), "\r\n", "\n"), nil
}

// projectTag is "#name" when project is a name notes reads as a #project
// tag, and "" otherwise.
func projectTag(project string) string {
	p := notes.NormalizeProject(project)
	if p == "" {
		return ""
	}
	// The first line is the title, which is never read for tags.
	if ps := notes.Projects("title\n#" + p); len(ps) != 1 || ps[0] != p {
		return ""
	}
	return "#" + p
}
