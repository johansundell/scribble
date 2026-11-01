package gui_test

import (
	"context"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/gui"
	"github.com/johansundell/scribble/notes"
)

// shareEnabled reports whether the Share button and File > Share note… are
// enabled, failing if they disagree.
func shareEnabled(t *testing.T, ui *gui.UI, w fyne.Window) bool {
	t.Helper()
	item := menuItem(t, w, "File", "Share note…")
	if item.Disabled != ui.Share.Disabled() {
		t.Fatalf("menu item disabled=%v, button disabled=%v", item.Disabled, ui.Share.Disabled())
	}
	return !ui.Share.Disabled()
}

// dialogLink is the share link's selectable label in the dialog on top of
// w, nil if it shows none.
func dialogLink(w fyne.Window) *widget.Label {
	for _, o := range test.LaidOutObjects(w.Canvas().Overlays().Top()) {
		if l, ok := o.(*widget.Label); ok && l.Visible() && l.Selectable {
			return l
		}
	}
	return nil
}

// dialogHasQR reports whether the dialog on top of w shows an image.
func dialogHasQR(w fyne.Window) bool {
	for _, o := range test.LaidOutObjects(w.Canvas().Overlays().Top()) {
		if img, ok := o.(*canvas.Image); ok && img.Visible() && img.Resource != nil {
			return true
		}
	}
	return false
}

// hasDialogButton reports whether the dialog on top of w shows a button
// labelled text.
func hasDialogButton(w fyne.Window, text string) bool {
	for _, o := range test.LaidOutObjects(w.Canvas().Overlays().Top()) {
		if b, ok := o.(*widget.Button); ok && b.Visible() && b.Text == text {
			return true
		}
	}
	return false
}

func TestShareNeedsASavedNoteAndTheServer(t *testing.T) {
	c := newSharingServer(t)
	cb := plug(c)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)

	if shareEnabled(t, ui, w) {
		t.Error("Share enabled with no note open")
	}
	ui.List.Select(0)
	if !shareEnabled(t, ui, w) {
		t.Fatal("Share disabled on a saved note")
	}
	test.Tap(ui.EditToggle)
	test.Type(ui.Editor, "x")
	if shareEnabled(t, ui, w) {
		t.Error("Share enabled with unsaved edits")
	}
	test.Tap(ui.Save)
	if !shareEnabled(t, ui, w) {
		t.Fatal("Share disabled once the edits are saved")
	}
	// Opening the dialog while it is disabled does nothing.
	test.Tap(ui.NewNote)
	if shareEnabled(t, ui, w) {
		t.Error("Share enabled on an unsaved draft")
	}
	menuItem(t, w, "File", "Share note…").Action()
	if w.Canvas().Overlays().Top() != nil {
		t.Fatal("Share note… opened a dialog for a draft")
	}

	ui.List.UnselectAll()
	ui.List.Select(0)
	tapDialogButton(t, w, "Discard") // leaving the draft asks first
	if !shareEnabled(t, ui, w) {
		t.Fatal("Share disabled on a saved note")
	}
	cb.unplugged.Store(true)
	ui.Sync()
	if shareEnabled(t, ui, w) {
		t.Error("Share enabled while offline")
	}
	cb.unplugged.Store(false)
	ui.Sync()
	if !shareEnabled(t, ui, w) {
		t.Error("Share still disabled once the server is back")
	}
}

func TestShareDialogCreatesCopiesAndRevokesALink(t *testing.T) {
	c := newSharingServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	ui.List.Select(0)

	test.Tap(ui.Share)
	if !strings.Contains(dialogText(w), "Anyone with this public link can read the note without credentials.") {
		t.Fatalf("private note's dialog says %q", dialogText(w))
	}
	if dialogLink(w) != nil || dialogHasQR(w) || hasDialogButton(w, "Revoke link") {
		t.Fatal("a private note's dialog shows a link")
	}

	tapDialogButton(t, w, "Create public link")
	sh := serverShare(t, c, "Ideas")
	if sh.Token == "" {
		t.Fatal("Create public link did not share the note")
	}
	link := dialogLink(w)
	if link == nil || link.Text != sh.URL || !strings.HasSuffix(link.Text, "/s/"+sh.Token) {
		t.Fatalf("dialog shows link %v, want %q", link, sh.URL)
	}
	if !dialogHasQR(w) {
		t.Error("the dialog shows no QR code")
	}
	if hasDialogButton(w, "Create public link") {
		t.Error("a shared note's dialog still offers to create a link")
	}

	tapDialogButton(t, w, "Copy link")
	if got := fyne.CurrentApp().Clipboard().Content(); got != sh.URL {
		t.Errorf("clipboard holds %q, want %q", got, sh.URL)
	}

	tapDialogButton(t, w, "Revoke link")
	if got := serverShare(t, c, "Ideas"); got.Token != "" {
		t.Fatalf("Revoke link left the note shared: %+v", got)
	}
	if dialogLink(w) != nil || dialogHasQR(w) || !hasDialogButton(w, "Create public link") {
		t.Fatal("the dialog did not go back to the private state")
	}

	tapDialogButton(t, w, "Done")
	if w.Canvas().Overlays().Top() != nil {
		t.Error("Done left the dialog open")
	}
}

func TestShareMenuShowsAnExistingLink(t *testing.T) {
	c := newSharingServer(t)
	create(t, c, "# Ideas\nfly")
	n := byTitle(t, c, "Ideas")
	sh, err := c.ShareNote(context.Background(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	ui, w := open(t, c)
	ui.List.Select(0)

	menuItem(t, w, "File", "Share note…").Action()
	if link := dialogLink(w); link == nil || link.Text != sh.URL {
		t.Fatalf("dialog shows link %v, want %q", link, sh.URL)
	}
	if !dialogHasQR(w) || !hasDialogButton(w, "Revoke link") {
		t.Error("the dialog lacks the QR code or Revoke link")
	}
}

func TestShareRefusedWhenTheServerHasNoWebApp(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	ui.List.Select(0)

	test.Tap(ui.Share)
	tapDialogButton(t, w, "Create public link")
	if !strings.Contains(dialogText(w), "WEB_PIN") {
		t.Fatalf("dialog says %q, want why sharing is off", dialogText(w))
	}
	if dialogLink(w) != nil {
		t.Error("the dialog shows a link")
	}
}

// serverShare is the share link of the note titled title, as the server
// has it.
func serverShare(t *testing.T, c *client.Client, title string) notes.Share {
	t.Helper()
	sh, err := c.GetShare(context.Background(), byTitle(t, c, title).ID)
	if err != nil {
		t.Fatal(err)
	}
	return sh
}

func TestShareFailuresKeepTheDialogAsItWas(t *testing.T) {
	c := newSharingServer(t)
	cb := plug(c)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	ui.List.Select(0)

	// The server drops before the dialog opens: no dialog, the reason in
	// the status line, and Share greys out.
	cb.unplugged.Store(true)
	test.Tap(ui.Share)
	if w.Canvas().Overlays().Top() != nil {
		t.Fatal("a dialog opened without the server")
	}
	if !strings.Contains(ui.Status.Text, "unreachable") || shareEnabled(t, ui, w) {
		t.Fatalf("status %q, Share enabled %v", ui.Status.Text, !ui.Share.Disabled())
	}

	cb.unplugged.Store(false)
	ui.Sync()
	test.Tap(ui.Share)
	tapDialogButton(t, w, "Create public link")
	sh := serverShare(t, c, "Ideas")

	// The server drops before Revoke: the link stays, with the reason.
	cb.unplugged.Store(true)
	tapDialogButton(t, w, "Revoke link")
	if link := dialogLink(w); link == nil || link.Text != sh.URL {
		t.Fatalf("a failed revoke changed the link to %v", link)
	}
	if !strings.Contains(dialogText(w), "unreachable") {
		t.Errorf("dialog says %q", dialogText(w))
	}
	cb.unplugged.Store(false)
	if got := serverShare(t, c, "Ideas"); got.Token != sh.Token {
		t.Errorf("the server's link changed: %+v", got)
	}
	if shareEnabled(t, ui, w) {
		t.Error("Share enabled after the server was found unreachable")
	}
}

// A server from before share links (API level 3) has no endpoints for them.
func TestShareNeedsAServerWithShareLinks(t *testing.T) {
	c := startServer(t, nil, 3)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	ui.List.Select(0)
	if shareEnabled(t, ui, w) {
		t.Error("Share enabled against an API level 3 server")
	}
}
