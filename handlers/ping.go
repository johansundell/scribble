package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
)

func (h *Handler) Ping(c *gin.Context) error {
	argument := c.Param("argument")

	payload := struct {
		Result string `json:"result"`
	}{Result: argument}

	if payload.Result == "notfound" {
		return httperror.ReturnWithHTTPStatus(errors.New("Nope"), http.StatusNotFound)
	}

	c.JSON(http.StatusOK, payload)
	return nil
}
