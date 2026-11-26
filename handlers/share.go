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
// served by the web app, so while it is off (WEB_TOTP_SECRET unset) no note reads as
// shared and none can be.

// errShareOff is why the API won't share a note while the web app is off:
// the link would answer 404.
var errShareOff = errors.New("share links need the web app (WEB_TOTP_SECRET)")

// webOn reports whether the web app, which serves share links, is on.
func (h *Handler) webOn() bool { return h.auth != nil }

// shareJSON answers with note id's live links, with their URLs as the
// client reached us, or empty ones while the web app is off: a link made
// earlier answers 404 now.
func (h *Handler) shareJSON(c *gin.Context, ns store.NoteStore, id int64) error {
	ctx := c.Request.Context()
	view, err := ns.NoteShare(ctx, id)
	if err != nil {
		return noteError(err)
	}
	edit, err := ns.NoteEditShare(ctx, id)
	if err != nil {
		return noteError(err)
	}
	var resp notes.Share
	if h.webOn() {
		resp = notes.Share{Token: view.Token, ExpiresAt: view.ExpiresAt, EditExpiresAt: edit.ExpiresAt}
		if view.Token != "" {
			resp.URL = siteURL(c.Request) + SharedPath(view.Token)
		}
		if edit.Token != "" {
			resp.EditURL = siteURL(c.Request) + EditSharedPath(edit.Token)
		}
	}
	c.JSON(http.StatusOK, resp)
	return nil
}

// GetShare handles GET /notes/:id/share: the note's live links, empty when
// it is private or the web app is off.
func (h *Handler) GetShare(c *gin.Context) error {
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	id, err := noteID(c)
	if err != nil {
		return err
	}
	return h.shareJSON(c, ns, id)
}

// ShareNote handles POST /notes/:id/share: it shares the note with a view
// link that never expires, keeping a live link's token, and answers with
// the note's links as GET does.
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
	if _, err := ns.ShareNote(c.Request.Context(), id, nil); err != nil {
		return noteError(err)
	}
	return h.shareJSON(c, ns, id)
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
