package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/store"
)

// Share links over the API (#142, ADR 11), for the desktop app. They are
// served by the web app, so while it is off (WEB_PIN unset) no note reads as
// shared and none can be.

// errShareOff is why the API won't share a note while the web app is off:
// the link would answer 404.
var errShareOff = errors.New("share links need the web app (WEB_PIN)")

// webOn reports whether the web app, which serves share links, is on.
func (h *Handler) webOn() bool { return h.auth != nil }

// shareJSON answers with link sh, adding its URL as the client reached us.
func shareJSON(c *gin.Context, sh store.Share) {
	resp := notes.Share{Token: sh.Token, ExpiresAt: sh.ExpiresAt}
	if sh.Token != "" {
		resp.URL = siteURL(c.Request) + SharedPath(sh.Token)
	}
	c.JSON(http.StatusOK, resp)
}

// GetShare handles GET /notes/:id/share: the note's live link, or an empty
// one when it is private or the web app is off.
func (h *Handler) GetShare(c *gin.Context) error {
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	id, err := noteID(c)
	if err != nil {
		return err
	}
	sh, err := ns.NoteShare(c.Request.Context(), id)
	if err != nil {
		return noteError(err)
	}
	if !h.webOn() {
		sh = store.Share{} // a link made earlier answers 404 now
	}
	shareJSON(c, sh)
	return nil
}

// ShareNote handles POST /notes/:id/share: it shares the note with a link
// that never expires, keeping a live link's token, and answers with it.
func (h *Handler) ShareNote(c *gin.Context) error {
	if !h.webOn() {
		return httperror.ReturnWithHTTPStatus(errShareOff, http.StatusNotImplemented)
	}
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	id, err := noteID(c)
	if err != nil {
		return err
	}
	tok, err := ns.ShareNote(c.Request.Context(), id, nil)
	if err != nil {
		return noteError(err)
	}
	shareJSON(c, store.Share{Token: tok})
	return nil
}

// UnshareNote handles DELETE /notes/:id/share: it revokes the note's link.
func (h *Handler) UnshareNote(c *gin.Context) error {
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	id, err := noteID(c)
	if err != nil {
		return err
	}
	if err := ns.UnshareNote(c.Request.Context(), id); err != nil {
		return noteError(err)
	}
	c.Status(http.StatusNoContent)
	return nil
}
