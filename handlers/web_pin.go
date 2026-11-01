package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// PinPath is where the web app posts a note's Pin note or Unpin note (#140).
func PinPath(id int64) string {
	return NotePath(id) + "/pin"
}

// AppPin pins the note, or unpins it with pinned=false, and shows it.
func (h *Handler) AppPin(c *gin.Context) error {
	p, ns, n, err := h.webNote(c)
	if err != nil || n == nil {
		return err
	}
	if _, err := ns.PinNote(c.Request.Context(), n.ID, c.PostForm("pinned") == "true"); err != nil {
		return noteError(err)
	}
	c.Redirect(http.StatusSeeOther, NotePath(n.ID)+p.Filter())
	return nil
}
