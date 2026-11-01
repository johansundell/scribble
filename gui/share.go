package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/notes"
	"rsc.io/qr"
)

// Share links (#142, ADR 11): the Share dialog gives the open note a public,
// read-only link, shows it as text and a QR code, and revokes it. It works
// the same as the web app's share pane, through the same server.

const shareExplanation = "Anyone with this public link can read the note without credentials."

// shareAPILevel is the server API level that added share links.
const shareAPILevel = 4

// canShare reports whether the open note can be shared now: it is on the
// server, saved, and the server is reachable and new enough.
func (ui *UI) canShare() bool {
	if ui.outdated.Load() || ui.offline || ui.current == nil || ui.current.ID <= 0 || ui.hasUnsavedChanges() {
		return false
	}
	_, api := ui.c.ServerVersion()
	return api == 0 || api >= shareAPILevel
}

// shareDialog is the open Share dialog of note id.
type shareDialog struct {
	ui   *UI
	id   int64
	body *fyne.Container // what the dialog shows; rebuilt by show
	d    dialog.Dialog
}

// showShare opens the Share dialog of the open note, as the server has it.
func (ui *UI) showShare() {
	if !ui.canShare() {
		return
	}
	id := ui.current.ID
	ctx, cancel := requestContext()
	sh, err := ui.c.GetShare(ctx, id)
	cancel()
	if ui.reportOnline(err) {
		return
	}
	s := &shareDialog{ui: ui, id: id, body: container.NewVBox()}
	s.d = dialog.NewCustom("Share "+ui.current.Title, "Done", s.body, ui.w)
	s.show(sh, "")
	s.d.Show()
}

// show fills the dialog for link sh (private when it has no token), with
// problem, if any, below.
func (s *shareDialog) show(sh notes.Share, problem string) {
	var objs []fyne.CanvasObject
	if sh.Token == "" {
		objs = append(objs, widget.NewLabel(shareExplanation),
			widget.NewButton("Create public link", s.create))
	} else {
		// Selectable, not editable: the link is the server's.
		link := widget.NewLabel(sh.URL)
		link.Selectable = true
		objs = append(objs, link)
		if img := qrImage(sh.URL); img != nil {
			objs = append(objs, img)
		}
		if sh.ExpiresAt != nil {
			objs = append(objs, widget.NewLabel("Expires "+sh.ExpiresAt.Local().Format("2006-01-02 15:04")))
		}
		var copyButton *widget.Button
		copyButton = widget.NewButton("Copy link", func() {
			fyne.CurrentApp().Clipboard().SetContent(sh.URL)
			copyButton.SetText("Copied")
		})
		objs = append(objs, container.NewHBox(copyButton, widget.NewButton("Revoke link", func() { s.revoke(sh) })))
	}
	if problem != "" {
		l := widget.NewLabel(problem)
		l.Wrapping = fyne.TextWrapWord
		objs = append(objs, l)
	}
	s.body.Objects = objs
	s.body.Refresh()
	s.d.Resize(fyne.NewSize(max(440, s.d.MinSize().Width), s.d.MinSize().Height))
}

// create shares the note with a link that never expires.
func (s *shareDialog) create() {
	ctx, cancel := requestContext()
	defer cancel()
	sh, err := s.ui.c.ShareNote(ctx, s.id)
	if err != nil {
		s.show(notes.Share{}, s.failed(err))
		return
	}
	s.show(sh, "")
}

// revoke clears link sh, so it stops working.
func (s *shareDialog) revoke(sh notes.Share) {
	ctx, cancel := requestContext()
	defer cancel()
	if err := s.ui.c.UnshareNote(ctx, s.id); err != nil {
		s.show(sh, s.failed(err))
		return
	}
	s.show(notes.Share{}, "")
}

// failed says why a share request failed, for the dialog, after noting
// what err says about the server (see serverTrouble).
func (s *shareDialog) failed(err error) string {
	switch {
	case client.IsSharingOff(err):
		return "The server has share links turned off. They are part of its web app, which runs once WEB_PIN is set on the server."
	case client.IsNotFound(err):
		return "The note is no longer on the server."
	}
	return s.ui.serverTrouble(err, "The server is unreachable. Sharing needs it; try again when it is back.")
}

// qrImage is text as a QR code for a phone to scan, nil if it is too long
// for one. The PNG has a white quiet zone, so it scans in dark mode too.
func qrImage(text string) *canvas.Image {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil
	}
	img := canvas.NewImageFromResource(fyne.NewStaticResource("share-qr.png", code.PNG()))
	img.FillMode = canvas.ImageFillContain
	img.ScaleMode = canvas.ImageScalePixels
	img.SetMinSize(fyne.NewSquareSize(200))
	return img
}
