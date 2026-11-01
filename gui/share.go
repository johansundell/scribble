package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/client"
	"rsc.io/qr"
)

// Share links (#142, ADR 11): the Share dialog gives the open note a public,
// read-only link, shows it as text and a QR code, and revokes it. It works
// the same as the web app's share pane, through the same server.

const shareExplanation = "Anyone with this public link can read the note without credentials."

// shareDialog is the open Share dialog of note id.
type shareDialog struct {
	ui   *UI
	id   int64
	body *fyne.Container // what the dialog shows; rebuilt by show
	d    dialog.Dialog
}

// showShare opens the Share dialog of the open note, as the server has it.
func (ui *UI) showShare() {
	if ui.current == nil || ui.current.ID <= 0 {
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
func (s *shareDialog) show(sh client.Share, problem string) {
	var objs []fyne.CanvasObject
	if sh.Token == "" {
		objs = append(objs, widget.NewLabel(shareExplanation),
			widget.NewButton("Create public link", s.create))
	} else {
		link := widget.NewEntry()
		link.SetText(sh.URL)
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
	sh, err := s.ui.c.ShareNote(ctx, s.id, "")
	if err != nil {
		s.show(client.Share{}, s.failed(err))
		return
	}
	s.show(sh, "")
}

// revoke clears link sh, so it stops working.
func (s *shareDialog) revoke(sh client.Share) {
	ctx, cancel := requestContext()
	defer cancel()
	if err := s.ui.c.UnshareNote(ctx, s.id); err != nil {
		s.show(sh, s.failed(err))
		return
	}
	s.show(client.Share{}, "")
}

// failed says why a share request failed, for the dialog.
func (s *shareDialog) failed(err error) string {
	switch {
	case client.IsUnreachable(err):
		s.ui.offline = true
		s.ui.setOffline()
		return "The server is unreachable. Sharing needs it; try again when it is back."
	case client.IsSharingOff(err):
		return "The server has share links turned off. They are part of its web app, which runs once WEB_PIN is set on the server."
	case client.IsNotFound(err):
		return "The note is no longer on the server."
	case client.IsUpgradeRequired(err):
		s.ui.setOutdated()
	}
	return explain(err)
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
