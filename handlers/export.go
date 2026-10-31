package handlers

import (
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/store"
)

// Export and import of every note as a ZIP archive of Markdown files
// (#117). The API has GET /export and POST /import with the token; the web
// app has the same at ExportPath and ImportPath with its session.

// ExportPath and ImportPath are the web app's export download and import
// upload. They differ from the API's /export and /import, which take the
// token.
const (
	ExportPath = "/export-notes"
	ImportPath = "/import-notes"
)

// maxImportBytes caps an uploaded archive.
const maxImportBytes = 64 << 20

// ImportResult is what an import did: Imported notes were created or
// overwritten, Skipped ones already existed (or were unchanged).
type ImportResult struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
}

// Summary is the result as a sentence, for people.
func (r ImportResult) Summary() string {
	noun := "notes"
	if r.Imported == 1 {
		noun = "note"
	}
	return fmt.Sprintf("Imported %d %s, skipped %d.", r.Imported, noun, r.Skipped)
}

// ExportNotes handles GET /export: every live note as a ZIP download.
func (h *Handler) ExportNotes(c *gin.Context) error {
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	list, err := ns.ListNotes(c.Request.Context(), "", "")
	if err != nil {
		return noteError(err)
	}
	// Oldest first, so the oldest of two notes whose titles make the same
	// file name keeps the plain one.
	slices.SortFunc(list, func(a, b notes.Note) int { return int(a.ID - b.ID) })
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", `attachment; filename="`+notes.ZipFileName(time.Now())+`"`)
	c.Status(http.StatusOK)
	// Headers are sent by now, so a failure can only cut the download short.
	return notes.WriteZip(c.Writer, list)
}

// ImportNotes handles POST /import: a multipart upload of a ZIP archive as
// "file", with "overwrite" true to replace notes that already exist.
func (h *Handler) ImportNotes(c *gin.Context) error {
	res, err := h.importUpload(c)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, res)
	return nil
}

// AppExport is the web app's export download.
func (h *Handler) AppExport(c *gin.Context) error {
	return h.ExportNotes(c)
}

// AppImport imports the web app's upload and shows the list with what it
// did, or why it failed, where the note would be.
func (h *Handler) AppImport(c *gin.Context) error {
	res, err := h.importUpload(c)
	status := http.StatusOK
	if err != nil {
		if status = httperror.HTTPStatus(err); status >= 500 {
			return err
		}
	}
	p, _, perr := h.notesPage(c)
	if perr != nil {
		return perr
	}
	if err != nil {
		p.Message = "Import failed: " + errors.Unwrap(err).Error()
	} else {
		p.Message = res.Summary()
	}
	return h.renderNotes(c, status, p)
}

// importUpload reads the uploaded archive and imports its notes.
func (h *Handler) importUpload(c *gin.Context) (ImportResult, error) {
	ns, err := h.noteStore()
	if err != nil {
		return ImportResult{}, err
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxImportBytes)
	fh, err := c.FormFile("file")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return ImportResult{}, httperror.ReturnWithHTTPStatus(err, http.StatusRequestEntityTooLarge)
		}
		return ImportResult{}, httperror.ReturnWithHTTPStatus(errors.New("no file uploaded"), http.StatusBadRequest)
	}
	list, err := readUpload(fh)
	if err != nil {
		return ImportResult{}, httperror.ReturnWithHTTPStatus(err, http.StatusBadRequest)
	}
	overwrite, _ := strconv.ParseBool(c.Request.FormValue("overwrite"))
	if c.Request.FormValue("overwrite") == "on" { // an HTML checkbox
		overwrite = true
	}
	return importNotes(c.Request.Context(), ns, list, overwrite)
}

func readUpload(fh *multipart.FileHeader) ([]notes.Note, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return notes.ReadZip(f, fh.Size)
}

// importNotes creates each note. One whose title is taken is skipped, or
// with overwrite gets the imported text, unless it has it already.
func importNotes(ctx context.Context, ns store.NoteStore, list []notes.Note, overwrite bool) (ImportResult, error) {
	var res ImportResult
	for _, n := range list {
		_, err := ns.CreateNote(ctx, n.Body)
		if err == nil {
			res.Imported++
			continue
		}
		if !errors.Is(err, store.ErrTitleTaken) {
			return res, noteError(fmt.Errorf("%q: %w", n.Title, err))
		}
		if !overwrite {
			res.Skipped++
			continue
		}
		cur, err := ns.GetNoteByTitle(ctx, n.Title)
		if err != nil {
			return res, noteError(fmt.Errorf("%q: %w", n.Title, err))
		}
		if cur.Body == n.Body {
			res.Skipped++
			continue
		}
		if _, err := ns.UpdateNote(ctx, cur.ID, n.Body, cur.Revision); err != nil {
			return res, noteError(fmt.Errorf("%q: %w", n.Title, err))
		}
		res.Imported++
	}
	return res, nil
}
