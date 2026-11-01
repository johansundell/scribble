package handlers

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
	"github.com/johansundell/scribble/store"
	"rsc.io/qr"
)

// Share links (#83): a note can be given a public, read-only link, so it
// can be read on a phone without the PIN. The share pane works like rename:
// a GET shows it and a POST changes it.

// SharePath is the share pane of the note with id.
func SharePath(id int64) string {
	return NotePath(id) + "/share"
}

// SharedPath is the public page of the note shared with token.
func SharedPath(token string) string {
	return "/s/" + token
}

// What the share pane's buttons send as "action".
const (
	actionShare   = "share"
	actionUnshare = "unshare"
)

var (
	errUnknownAction = errors.New("unknown action")
	errUnknownTTL    = errors.New("unknown ttl")
)

// shareTTLs are how long a new share link can work (#145), by the "ttl"
// the share form sends; "never" (or no ttl) is a permanent link.
var shareTTLs = map[string]time.Duration{
	"1h":  time.Hour,
	"1d":  24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// shareExpiry is when a link shared now with ttl stops working, nil for never.
func shareExpiry(ttl string) (*time.Time, error) {
	if ttl == "" || ttl == "never" {
		return nil, nil
	}
	d, ok := shareTTLs[ttl]
	if !ok {
		return nil, errUnknownTTL
	}
	t := time.Now().Add(d)
	return &t, nil
}

// expiresText says when a share link expires, "" for never.
func expiresText(t *time.Time) string {
	if t == nil {
		return ""
	}
	return "Expires on " + t.UTC().Format("2006-01-02 15:04 UTC")
}

// shareForm is the share pane of one note. URL is "" while it isn't shared;
// Expires is "" for a link that never expires.
type shareForm struct {
	ID      int64
	Action  string
	Cancel  string
	URL     string
	QR      template.HTML
	Expires string
}

// AppShareForm shows the share pane: the link, its QR code and Revoke when
// the note is shared, and the button that shares it when it isn't.
func (h *Handler) AppShareForm(c *gin.Context) error {
	p, ns, n, err := h.webNote(c)
	if err != nil || n == nil {
		return err
	}
	sh, err := ns.NoteShare(c.Request.Context(), n.ID)
	if err != nil {
		return noteError(err)
	}
	p.Title = "Share " + n.Title
	p.Share = &shareForm{ID: n.ID, Action: SharePath(n.ID) + p.Filter(), Cancel: NotePath(n.ID) + p.Filter(),
		Expires: expiresText(sh.ExpiresAt)}
	if sh.Token != "" {
		p.Share.URL = siteURL(c.Request) + SharedPath(sh.Token)
		if p.Share.QR, err = qrSVG(p.Share.URL); err != nil {
			return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
		}
	}
	return h.renderNotes(c, http.StatusOK, p)
}

// AppShare shares the note for the chosen ttl (keeping its link if it has
// a live one) or revokes its link, and goes back to the share pane.
func (h *Handler) AppShare(c *gin.Context) error {
	p, ns, n, err := h.webNote(c)
	if err != nil || n == nil {
		return err
	}
	ctx := c.Request.Context()
	switch c.PostForm("action") {
	case actionShare:
		expires, terr := shareExpiry(c.PostForm("ttl"))
		if terr != nil {
			return httperror.ReturnWithHTTPStatus(terr, http.StatusBadRequest)
		}
		_, err = ns.ShareNote(ctx, n.ID, expires)
	case actionUnshare:
		err = ns.UnshareNote(ctx, n.ID)
	default:
		return httperror.ReturnWithHTTPStatus(errUnknownAction, http.StatusBadRequest)
	}
	if errors.Is(err, store.ErrNoteNotFound) {
		return h.notesMessage(c, p, msgNoteNotFound)
	}
	if err != nil {
		return noteError(err)
	}
	c.Redirect(http.StatusSeeOther, SharePath(n.ID)+p.Filter())
	return nil
}

// sharedPage is the data of a shared note's public page.
type sharedPage struct {
	Title    string
	NoteHTML template.HTML
	Updated  string
	Expires  string
}

// Shared shows the note shared with :token to anyone, with no session and
// none of the app around it. An unknown, revoked or expired token is a
// plain 404.
func (h *Handler) Shared(c *gin.Context) error {
	ns, err := h.noteStore()
	if err != nil {
		return err
	}
	n, expires, err := ns.GetSharedNote(c.Request.Context(), c.Param("token"))
	if err != nil {
		return noteError(err)
	}
	out, err := renderSharedNote(n.Body)
	if err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}
	tmpl, err := template.ParseFS(h.templates, "tmpl/shared.html")
	if err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}
	// The token is in the URL: never send it to the sites the note links to.
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Robots-Tag", "noindex")
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	if err := tmpl.Execute(c.Writer, sharedPage{
		Title:    n.Title,
		NoteHTML: out,
		Updated:  n.UpdatedAt.UTC().Format("2006-01-02 15:04 UTC"),
		Expires:  expiresText(expires),
	}); err != nil {
		return httperror.ReturnWithHTTPStatus(err, http.StatusInternalServerError)
	}
	return nil
}

// siteURL is the scheme and host r was sent to, as the browser sees them.
// Behind a TLS proxy the scheme comes from X-Forwarded-Proto; it only
// shapes the link shown to the logged-in user, so it is not checked.
func siteURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// qrSVG draws text as a QR code in inline SVG: one path of unit squares on
// a white background with the standard four-module quiet zone, so phones can
// scan it in dark mode too. Nothing is fetched from anywhere.
func qrSVG(text string) (template.HTML, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 4
	size := code.Size + 2*quiet
	var d strings.Builder
	for y := range code.Size {
		for x := range code.Size {
			if code.Black(x, y) {
				fmt.Fprintf(&d, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	return template.HTML(fmt.Sprintf(
		`<svg class="qr" viewBox="0 0 %d %d" shape-rendering="crispEdges" role="img" aria-label="QR code of the link"><rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="%s"/></svg>`,
		size, size, size, size, d.String())), nil
}
