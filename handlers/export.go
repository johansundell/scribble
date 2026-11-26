package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
	"github.com/johansundell/scribble/images"
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

// maxImportBytes caps an uploaded archive, which may hold images (#201).
const maxImportBytes = 512 << 20

// ImportResult is what an import did: Imported notes were created or
// overwritten, Skipped ones already existed, were unchanged or had no
// title. client.ImportResult is its JSON twin.
type ImportResult struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
}

// ExportNotes handles GET /export, and the web app's ExportPath: every
// live note as a ZIP download.
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
	imgs, err := h.zipImages(c.Request.Context())
	if err != nil {
		return err
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", `attachment; filename="`+notes.ZipFileName(time.Now())+`"`)
	c.Status(http.StatusOK)
	// Headers are sent by now, so a failure can only cut the download short.
	return notes.WriteZip(c.Writer, list, imgs...)
}

// zipImages is every stored image, each read as the archive is written.
func (h *Handler) zipImages(ctx context.Context) ([]notes.ZipImage, error) {
	is, err := h.imageStore()
	if err != nil {
		return nil, err
	}
	list, err := is.ListImages(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]notes.ZipImage, len(list))
	for i, img := range list {
		out[i] = notes.ZipImage{FileName: img.FileName(), Data: func() ([]byte, error) {
			got, err := is.GetImage(ctx, img.ID)
			return got.Data, err
		}}
	}
	return out, nil
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
		cause := err
		if inner := errors.Unwrap(err); inner != nil {
			cause = inner
		}
		p.Message = "Import failed: " + cause.Error()
	} else {
		p.Message = notes.ImportSummary(res.Imported, res.Skipped)
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
	is, err := h.imageStore()
	if err != nil {
		return ImportResult{}, err
	}
	f, err := fh.Open()
	if err != nil {
		return ImportResult{}, err
	}
	defer f.Close()
	list, imgs, err := notes.ReadArchive(f, fh.Size)
	if err != nil {
		return ImportResult{}, httperror.ReturnWithHTTPStatus(err, http.StatusBadRequest)
	}
	// Images first, so a bad one stops the import before any note.
	if err := importImages(c.Request.Context(), is, imgs); err != nil {
		return ImportResult{}, err
	}
	overwrite, _ := strconv.ParseBool(c.Request.FormValue("overwrite"))
	if c.Request.FormValue("overwrite") == "on" { // an HTML checkbox
		overwrite = true
	}
	return importNotes(c.Request.Context(), ns, list, overwrite)
}

// importImages stores each image, checked as an upload is, and refuses one
// whose name isn't the one its bytes give it.
func importImages(ctx context.Context, is store.ImageStore, imgs []notes.ZipImage) error {
	for _, zi := range imgs {
		data, err := zi.Data()
		if err != nil {
			return httperror.ReturnWithHTTPStatus(err, http.StatusBadRequest)
		}
		img, err := images.Prepare(data)
		if err == nil && img.FileName() != zi.FileName {
			err = errors.New("the name doesn't match the image")
		}
		if err != nil {
			return httperror.ReturnWithHTTPStatus(fmt.Errorf("%s%s: %w", notes.ImageDir, zi.FileName, err), http.StatusBadRequest)
		}
		if err := is.PutImage(ctx, img); err != nil {
			return err
		}
	}
	return nil
}

// importNotes creates each note. One whose title is taken is skipped, or
// with overwrite gets the imported text, unless it has it already. One the
// store refuses for having no title is skipped; other errors stop it.
func importNotes(ctx context.Context, ns store.NoteStore, list []notes.Note, overwrite bool) (ImportResult, error) {
	var res ImportResult
	for _, n := range list {
		_, err := ns.CreateNote(ctx, n.Body)
		if err == nil {
			res.Imported++
			continue
		}
		if errors.Is(err, store.ErrNoTitle) {
			res.Skipped++
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
