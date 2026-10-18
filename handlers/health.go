package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
)

// HealthCheck reports the service and storage state. It answers 503 when
// the storage ping fails, so health checks see the service as unhealthy.
func (h *Handler) HealthCheck(c *gin.Context) error {
	status := http.StatusOK
	dbStatus := "OK"
	if err := h.store.Ping(c.Request.Context()); err != nil {
		status = http.StatusServiceUnavailable
		dbStatus = err.Error()
	}

	data := map[string]interface{}{
		"title":    "Health Check",
		"name":     h.nameOfService,
		"version":  h.versionOfService,
		"dbStatus": dbStatus,
	}

	// JSON when the client prefers it (Accept), HTML otherwise, including
	// for browsers and requests without an Accept header.
	if c.NegotiateFormat(gin.MIMEHTML, gin.MIMEJSON) == gin.MIMEJSON {
		c.JSON(status, data)
		return nil
	}

	const tmplFile = "health.html"

	tmpl, err := h.getTemplate(true, tmplFile)
	if err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}

	c.Status(status)
	if err := tmpl.ExecuteTemplate(c.Writer, "base", data); err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}
	return nil
}
