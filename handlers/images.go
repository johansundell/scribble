package handlers

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
	"github.com/johansundell/scribble/images"
	"github.com/johansundell/scribble/store"
)

// Images in notes (#201, ADR 16). The web app and an edit link upload them;
// anyone with an image's URL can fetch it, since its name is the SHA-256 of
// its bytes and can't be guessed.

// ImagesUploadPath is where the web app's editor uploads an image.
const ImagesUploadPath = "/app/images"

// ImagePath is the pattern of an image's URL.
const ImagePath = images.PathPrefix + ":file"

// EditSharedImagesPath is where the editor behind edit link token uploads
// an image.
func EditSharedImagesPath(token string) string {
	return EditSharedPath(token) + "/images"
}

// maxImageRequestBytes caps an upload request: one image and the multipart
// wrapping around it.
const maxImageRequestBytes = images.MaxBytes + 64<<10

// imageStore is the store as an ImageStore, or 501.
func (h *Handler) imageStore() (store.ImageStore, error) {
	is, ok := h.store.(store.ImageStore)
	if !ok {
		return nil, httperror.ReturnWithHTTPStatus(store.ErrNotesUnsupported, http.StatusNotImplemented)
	}
	return is, nil
}

// ServeImage handles GET /images/<sha256>.<ext>, with no login. The image
// is sent as never changing, and in a sandbox, so a browser never runs it as
// a page.
func (h *Handler) ServeImage(c *gin.Context) error {
	c.Header("X-Robots-Tag", "noindex")
	c.Header("X-Content-Type-Options", "nosniff")
	notFound := httperror.ReturnWithHTTPStatus(store.ErrImageNotFound, http.StatusNotFound)
	id, mime, ok := images.ParseFileName(c.Param("file"))
	if !ok {
		return notFound
	}
	is, err := h.imageStore()
	if err != nil {
		return err
	}
	img, err := is.GetImage(c.Request.Context(), id)
	if errors.Is(err, store.ErrImageNotFound) || (err == nil && img.MIME != mime) {
		return notFound
	}
	if err != nil {
		return err
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("Content-Security-Policy", "sandbox")
	c.Data(http.StatusOK, img.MIME, img.Data)
	return nil
}

// AppUploadImage handles the web app editor's upload.
func (h *Handler) AppUploadImage(c *gin.Context) error {
	return h.uploadImage(c)
}

// EditSharedUploadImage handles an upload from the editor behind an edit
// link, while the link works.
func (h *Handler) EditSharedUploadImage(c *gin.Context) error {
	if _, _, _, err := h.editLinkNote(c); err != nil {
		return err
	}
	return h.uploadImage(c)
}

// uploadImage stores the multipart upload "file" and answers with its URL
// as {"url": "/images/<sha256>.<ext>"}. A refused upload is answered with
// {"error": why}, for the editor to show.
func (h *Handler) uploadImage(c *gin.Context) error {
	is, err := h.imageStore()
	if err != nil {
		return err
	}
	refuse := func(status int, err error) error {
		c.JSON(status, gin.H{"error": err.Error()})
		return nil
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxImageRequestBytes)
	fh, err := c.FormFile("file")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return refuse(http.StatusRequestEntityTooLarge, images.ErrTooLarge)
		}
		return refuse(http.StatusBadRequest, errors.New("no file uploaded"))
	}
	f, err := fh.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, images.MaxBytes+1))
	if err != nil {
		return err
	}
	img, err := images.Prepare(data)
	if err != nil {
		return refuse(imageStatus(err), err)
	}
	if err := is.PutImage(c.Request.Context(), img); err != nil {
		return err
	}
	c.JSON(http.StatusOK, gin.H{"url": img.Path()})
	return nil
}

// imageStatus is the HTTP status of an images.Prepare error.
func imageStatus(err error) int {
	if errors.Is(err, images.ErrTooLarge) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusUnsupportedMediaType
}
