package handlers

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

const (
	defaultLogsLimit = 1000
	maxLogsLimit     = 10000
)

// logsPage is the GET /logs response. Next is the URL of the following page,
// or null on the last page.
type logsPage struct {
	Entries []types.UsageLog `json:"entries"`
	Next    *string          `json:"next"`
}

func (h *Handler) GetLogs(c *gin.Context) error {
	// Dates are whole UTC days: from 00:00Z up to, not including, the day after to.
	fromStr := c.Param("from")
	toStr := c.Param("to")

	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		return httperror.ReturnWithHTTPStatus(errors.New("wrong date format in from"), http.StatusBadRequest)
	}

	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		return httperror.ReturnWithHTTPStatus(errors.New("wrong date format in to"), http.StatusBadRequest)
	}

	limit, err := queryInt(c, "limit", defaultLogsLimit, 1, maxLogsLimit)
	if err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusBadRequest)
	}
	offset, err := queryInt(c, "offset", 0, 0, math.MaxInt)
	if err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusBadRequest)
	}

	// One entry more than asked tells whether there is a next page.
	logs, err := h.store.GetLogs(c.Request.Context(), from, to.AddDate(0, 0, 1), store.Page{Limit: limit + 1, Offset: offset})
	if err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}

	page := logsPage{Entries: logs}
	if len(logs) > limit {
		page.Entries = logs[:limit]
		q := url.Values{}
		q.Set("limit", strconv.Itoa(limit))
		q.Set("offset", strconv.Itoa(offset+limit))
		next := c.Request.URL.Path + "?" + q.Encode()
		page.Next = &next
	}
	if page.Entries == nil {
		page.Entries = []types.UsageLog{} // an empty page is [], not null
	}

	c.JSON(http.StatusOK, page)
	return nil
}

// queryInt reads an integer query parameter in [min, max], returning def
// when it is absent.
func queryInt(c *gin.Context, name string, def, min, max int) (int, error) {
	raw, ok := c.GetQuery(name)
	if !ok {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("%s must be an integer from %d to %d", name, min, max)
	}
	return n, nil
}
