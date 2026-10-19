package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/store"
)

// Rename and delete in the web app (#51). Each has a GET page with a form
// and a POST that does it, so a GET never changes anything and the POST is
// origin-checked like every web form.

// RenamePath is the rename form of the note with id.
func RenamePath(id int64) string {
	return NotePath(id) + "/rename"
}

// DeletePath is the delete confirmation of the note with id.
func DeletePath(id int64) string {
	return NotePath(id) + "/delete"
}

const (
	msgEmptyTitle   = "Type a title."
	msgTitleBracket = "A title can't contain [ or ]."
)

// noteForm is the rename form or the delete confirmation of one note.
type noteForm struct {
	ID     int64
	Action string
	Cancel string
	// Title is the typed title for rename, the note's title for delete.
	Title string
	Error string
}

// deleteQuestion is what Delete asks, on the page and in the confirm()
// dialog.
func deleteQuestion(title string) string {
	return `Delete "` + title + `"? This cannot be undone.`
}

// AppRenameForm shows the rename form with the note's title.
func (h *Handler) AppRenameForm(c *gin.Context) error {
	p, n, err := h.webNote(c)
	if err != nil || n == nil {
		return err
	}
	return h.renderRename(c, http.StatusOK, p, n, n.Title, "")
}

// AppRename renames the note, rewriting [[links]] to it in other notes,
// and shows it.
func (h *Handler) AppRename(c *gin.Context) error {
	p, n, err := h.webNote(c)
	if err != nil || n == nil {
		return err
	}
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	title := strings.TrimSpace(c.PostForm("title"))
	_, err = ns.RenameNote(c.Request.Context(), n.ID, title)
	switch {
	case err == nil:
		c.Redirect(http.StatusSeeOther, NotePath(n.ID)+p.Filter())
		return nil
	case errors.Is(err, store.ErrTitleTaken):
		return h.renderRename(c, http.StatusConflict, p, n, title, "A note titled "+title+" already exists.")
	case errors.Is(err, store.ErrNoTitle) && title == "":
		return h.renderRename(c, http.StatusBadRequest, p, n, title, msgEmptyTitle)
	case errors.Is(err, store.ErrNoTitle):
		return h.renderRename(c, http.StatusBadRequest, p, n, title, msgTitleBracket)
	case errors.Is(err, store.ErrNoteNotFound):
		return h.notesMessage(c, p, "Note not found")
	}
	return noteError(err)
}

// AppDeleteForm asks before deleting. With JavaScript, note.js asks with
// confirm() instead and posts straight away.
func (h *Handler) AppDeleteForm(c *gin.Context) error {
	p, n, err := h.webNote(c)
	if err != nil || n == nil {
		return err
	}
	p.Title = "Delete " + n.Title
	p.Delete = &noteForm{ID: n.ID, Action: DeletePath(n.ID) + p.Filter(), Cancel: NotePath(n.ID) + p.Filter(), Title: n.Title}
	return h.renderNotes(c, http.StatusOK, p)
}

// AppDelete deletes the note, leaving a tombstone for sync, and goes back
// to the list with its filter.
func (h *Handler) AppDelete(c *gin.Context) error {
	p, ns, err := h.notesPage(c)
	if err != nil {
		return err
	}
	id, ok := webNoteID(c)
	if !ok {
		return h.notesMessage(c, p, "Note not found")
	}
	err = ns.DeleteNote(c.Request.Context(), id)
	if errors.Is(err, store.ErrNoteNotFound) {
		return h.notesMessage(c, p, "Note not found")
	}
	if err != nil {
		return noteError(err)
	}
	c.Redirect(http.StatusSeeOther, AppHome+p.Filter())
	return nil
}

// webNote is the page data and the note :id. With no such note it has
// already shown "Note not found" and returns a nil note.
func (h *Handler) webNote(c *gin.Context) (webPage, *notes.Note, error) {
	p, ns, err := h.notesPage(c)
	if err != nil {
		return p, nil, err
	}
	id, ok := webNoteID(c)
	if !ok {
		return p, nil, h.notesMessage(c, p, "Note not found")
	}
	n, err := ns.GetNote(c.Request.Context(), id)
	if errors.Is(err, store.ErrNoteNotFound) {
		return p, nil, h.notesMessage(c, p, "Note not found")
	}
	if err != nil {
		return p, nil, noteError(err)
	}
	return p, n, nil
}

func (h *Handler) renderRename(c *gin.Context, status int, p webPage, n *notes.Note, title, msg string) error {
	p.Title = "Rename " + n.Title
	p.Rename = &noteForm{ID: n.ID, Action: RenamePath(n.ID) + p.Filter(), Cancel: NotePath(n.ID) + p.Filter(), Title: title, Error: msg}
	return h.renderNotes(c, status, p)
}
