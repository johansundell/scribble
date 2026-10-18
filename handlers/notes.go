package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/store"
)

// maxNoteRequestBytes caps a note upload (body of POST/PUT).
const maxNoteRequestBytes = 4 << 20

type noteRequest struct {
	Body string `json:"body"`
	// BaseRevision is the revision the client edited from. When > 0 and the
	// stored revision differs, the update is refused with 412.
	BaseRevision int64 `json:"base_revision"`
}

type renameRequest struct {
	Title string `json:"title"`
}

// changesResponse is the GET /changes body. LatestSeq is the cursor to pass
// as ?since= next time.
type changesResponse struct {
	Notes     []notes.Note `json:"notes"`
	LatestSeq int64        `json:"latest_seq"`
}

// noteStore returns the notes side of the store, or 501 when the configured
// backend has none.
func (h *Handler) noteStore() (store.NoteStore, error) {
	ns, ok := h.store.(store.NoteStore)
	if !ok {
		return nil, httperror.ReturnWithHTTPStatus(store.ErrNotesUnsupported, http.StatusNotImplemented)
	}
	return ns, nil
}

// noteError maps store errors onto HTTP statuses.
func noteError(err error) error {
	switch {
	case errors.Is(err, store.ErrNoteNotFound):
		return httperror.ReturnWithHTTPStatus(err, http.StatusNotFound)
	case errors.Is(err, store.ErrStaleRevision):
		return httperror.ReturnWithHTTPStatus(err, http.StatusPreconditionFailed)
	case errors.Is(err, store.ErrTitleTaken):
		return httperror.ReturnWithHTTPStatus(err, http.StatusConflict)
	case errors.Is(err, store.ErrNoTitle):
		return httperror.ReturnWithHTTPStatus(err, http.StatusBadRequest)
	case errors.Is(err, store.ErrNotesUnsupported):
		return httperror.ReturnWithHTTPStatus(err, http.StatusNotImplemented)
	}
	return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
}

func noteID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, httperror.ReturnWithHTTPStatus(errors.New("invalid note id"), http.StatusBadRequest)
	}
	return id, nil
}

func bindJSON(c *gin.Context, v any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxNoteRequestBytes)
	if err := c.ShouldBindJSON(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return httperror.ReturnWithHTTPStatus(err, http.StatusRequestEntityTooLarge)
		}
		return httperror.ReturnWithHTTPStatus(err, http.StatusBadRequest)
	}
	return nil
}

// ListNotes handles GET /notes. ?title= is an exact (case-insensitive)
// lookup returning zero or one note; ?q= searches titles and bodies.
func (h *Handler) ListNotes(c *gin.Context) error {
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	ctx := c.Request.Context()
	if title := c.Query("title"); title != "" {
		n, err := ns.GetNoteByTitle(ctx, title)
		if errors.Is(err, store.ErrNoteNotFound) {
			c.JSON(http.StatusOK, []notes.Note{})
			return nil
		}
		if err != nil {
			return noteError(err)
		}
		c.JSON(http.StatusOK, []notes.Note{*n})
		return nil
	}
	list, err := ns.ListNotes(ctx, c.Query("q"))
	if err != nil {
		return noteError(err)
	}
	c.JSON(http.StatusOK, list)
	return nil
}

func (h *Handler) CreateNote(c *gin.Context) error {
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	var req noteRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	n, err := ns.CreateNote(c.Request.Context(), req.Body)
	if err != nil {
		return noteError(err)
	}
	c.JSON(http.StatusCreated, n)
	return nil
}

func (h *Handler) GetNote(c *gin.Context) error {
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	id, err := noteID(c)
	if err != nil {
		return err
	}
	n, err := ns.GetNote(c.Request.Context(), id)
	if err != nil {
		return noteError(err)
	}
	c.JSON(http.StatusOK, n)
	return nil
}

func (h *Handler) UpdateNote(c *gin.Context) error {
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	id, err := noteID(c)
	if err != nil {
		return err
	}
	var req noteRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	n, err := ns.UpdateNote(c.Request.Context(), id, req.Body, req.BaseRevision)
	if err != nil {
		return noteError(err)
	}
	c.JSON(http.StatusOK, n)
	return nil
}

func (h *Handler) DeleteNote(c *gin.Context) error {
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	id, err := noteID(c)
	if err != nil {
		return err
	}
	if err := ns.DeleteNote(c.Request.Context(), id); err != nil {
		return noteError(err)
	}
	c.Status(http.StatusNoContent)
	return nil
}

func (h *Handler) NoteBacklinks(c *gin.Context) error {
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	id, err := noteID(c)
	if err != nil {
		return err
	}
	list, err := ns.NoteBacklinks(c.Request.Context(), id)
	if err != nil {
		return noteError(err)
	}
	c.JSON(http.StatusOK, list)
	return nil
}

func (h *Handler) RenameNote(c *gin.Context) error {
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	id, err := noteID(c)
	if err != nil {
		return err
	}
	var req renameRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	n, err := ns.RenameNote(c.Request.Context(), id, req.Title)
	if err != nil {
		return noteError(err)
	}
	c.JSON(http.StatusOK, n)
	return nil
}

// NoteChanges handles GET /changes?since=N for sync: every note (deleted
// ones flagged) that changed after sequence N.
func (h *Handler) NoteChanges(c *gin.Context) error {
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	since, err := queryInt(c, "since", 0, 0, 1<<62)
	if err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusBadRequest)
	}
	list, err := ns.NoteChanges(c.Request.Context(), int64(since))
	if err != nil {
		return noteError(err)
	}
	resp := changesResponse{Notes: list, LatestSeq: int64(since)}
	for _, n := range list {
		resp.LatestSeq = max(resp.LatestSeq, n.Seq)
	}
	c.JSON(http.StatusOK, resp)
	return nil
}
