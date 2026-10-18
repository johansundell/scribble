package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
)

func (h *Handler) Pong(c *gin.Context) error {
	var input map[string]interface{}
	if err := c.ShouldBindJSON(&input); err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusBadRequest)
	}
	// A JSON null decodes into a nil map without an error; only objects are accepted.
	if input == nil {
		return httperror.ReturnWithHTTPStatus(errors.New("body must be a JSON object"), http.StatusBadRequest)
	}

	c.JSON(http.StatusOK, gin.H{"message": input})
	return nil
}
