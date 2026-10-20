package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
)

// PreviewPath renders the editor's text for its Preview tab (#53).
const PreviewPath = "/preview"

// AppPreview answers with the posted text rendered as the note view renders
// it, as an HTML fragment. It saves nothing.
func (h *Handler) AppPreview(c *gin.Context) error {
	if h.auth == nil {
		return httperror.ReturnWithHTTPStatus(errWebNotConfigured, http.StatusInternalServerError)
	}
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	body, err := formBody(c)
	if err != nil {
		return err
	}
	out, err := renderNoteBody(c.Request.Context(), ns, body)
	if err != nil {
		return err
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(out))
	return nil
}
