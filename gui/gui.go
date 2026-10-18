// Package gui is the Fyne desktop client for a scribble server. It holds all
// the windows and widgets; cmd/scribble-gui only opens the app and calls New.
package gui

import (
	"context"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/notes"
)

// requestTimeout bounds each call to the server. Calls run on the UI thread,
// so this is also the longest the window can stall.
const requestTimeout = 10 * time.Second

// UI is the main window's content. The widgets are exported so tests can
// drive the window the way a user does.
type UI struct {
	Search *widget.Entry
	List   *widget.List
	Viewer *widget.RichText // view mode: the open note rendered as Markdown
	Editor *widget.Entry    // edit mode: the open note's raw Markdown

	EditToggle *widget.Button // switches between view and edit mode
	Save       *widget.Button
	NewNote    *widget.Button
	Delete     *widget.Button

	Status *widget.Label // the last error, empty when all is well

	w     fyne.Window
	c     *client.Client
	notes []notes.Note
	// current is the open note, nil when none; ID 0 for a new, unsaved
	// note. Its working text, saved or not, is always Editor.Text.
	current *notes.Note
}

// New builds the main window's content for server c and loads the notes.
func New(w fyne.Window, c *client.Client) *UI {
	ui := &UI{w: w, c: c}
	ui.List = widget.NewList(
		func() int { return len(ui.notes) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(ui.notes[i].Title) },
	)
	ui.List.OnSelected = func(i widget.ListItemID) {
		n := ui.notes[i]
		if ui.current == nil || ui.current.ID != n.ID {
			ui.unlessUnsaved(func() { ui.open(n) })
		}
	}
	ui.Search = widget.NewEntry()
	ui.Search.SetPlaceHolder("Search")
	ui.Search.OnChanged = func(string) { ui.refresh() }
	ui.Viewer = widget.NewRichText()
	ui.Viewer.Wrapping = fyne.TextWrapWord
	ui.Editor = widget.NewMultiLineEntry()
	ui.Editor.Wrapping = fyne.TextWrapWord
	ui.Editor.OnChanged = func(string) { ui.updateButtons() }
	ui.EditToggle = widget.NewButton("Edit", func() {
		if ui.Editor.Visible() {
			ui.showView()
		} else {
			ui.showEdit()
		}
	})
	ui.Save = widget.NewButton("Save", ui.save)
	ui.NewNote = widget.NewButton("New", func() { ui.unlessUnsaved(ui.startNote) })
	ui.Delete = widget.NewButton("Delete", ui.confirmDelete)
	ui.Status = widget.NewLabel("")
	ui.Status.Wrapping = fyne.TextWrapWord

	toolbar := container.NewHBox(ui.NewNote, ui.EditToggle, ui.Save, layout.NewSpacer(), ui.Delete)
	split := container.NewHSplit(
		container.NewBorder(ui.Search, nil, nil, nil, ui.List),
		container.NewBorder(toolbar, nil, nil, nil,
			container.NewStack(container.NewVScroll(ui.Viewer), ui.Editor)),
	)
	split.Offset = 0.3
	w.SetContent(container.NewBorder(nil, ui.Status, nil, nil, split))
	ui.showView()
	ui.refresh()
	return ui
}

func requestContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), requestTimeout)
}

// isSaved reports whether the open note exists on the server.
func (ui *UI) isSaved() bool { return ui.current != nil && ui.current.ID != 0 }

// hasUnsavedChanges reports whether the editor holds text that is not on
// the server.
func (ui *UI) hasUnsavedChanges() bool {
	return ui.current != nil && (ui.current.ID == 0 || ui.Editor.Text != ui.current.Body)
}

// open shows note n in view mode.
func (ui *UI) open(n notes.Note) {
	ui.current = &n
	ui.Editor.SetText(n.Body)
	ui.showView()
	ui.selectCurrent()
}

// startNote opens an empty editor for a new note.
func (ui *UI) startNote() {
	ui.current = &notes.Note{}
	ui.Editor.SetText("")
	ui.showEdit()
	ui.selectCurrent()
}

// unlessUnsaved runs then, first asking to discard any unsaved changes.
func (ui *UI) unlessUnsaved(then func()) {
	if !ui.hasUnsavedChanges() {
		then()
		return
	}
	d := dialog.NewConfirm("Unsaved changes", "Discard your unsaved changes?",
		func(discard bool) {
			if discard {
				then()
			} else {
				ui.selectCurrent()
			}
		}, ui.w)
	d.SetConfirmText("Discard")
	d.SetDismissText("Cancel")
	d.Show()
}

// showView switches to view mode, rendering the editor's text.
func (ui *UI) showView() {
	ui.Viewer.ParseMarkdown(ui.Editor.Text)
	ui.Editor.Hide()
	ui.Viewer.Show()
	ui.EditToggle.SetText("Edit")
	ui.updateButtons()
}

// showEdit switches to edit mode on the open note.
func (ui *UI) showEdit() {
	ui.Viewer.Hide()
	ui.Editor.Show()
	ui.EditToggle.SetText("View")
	ui.updateButtons()
}

func (ui *UI) updateButtons() {
	setEnabled(ui.EditToggle, ui.current != nil)
	setEnabled(ui.Save, ui.hasUnsavedChanges())
	setEnabled(ui.Delete, ui.isSaved())
}

func setEnabled(b *widget.Button, on bool) {
	if on {
		b.Enable()
	} else {
		b.Disable()
	}
}

// save writes the editor to the server and returns to view mode. The server
// takes the title from the first non-empty line. An update carries the
// revision the edit started from, so the server refuses it if someone else
// saved in between; the edit then stays open.
func (ui *UI) save() {
	if ui.current == nil {
		return
	}
	ctx, cancel := requestContext()
	defer cancel()
	var n *notes.Note
	var err error
	if ui.isSaved() {
		n, err = ui.c.Update(ctx, ui.current.ID, ui.Editor.Text, ui.current.Revision)
	} else {
		n, err = ui.c.Create(ctx, ui.Editor.Text)
	}
	if ui.report(err) {
		return
	}
	ui.Status.SetText("")
	ui.refresh()
	ui.open(*n)
}

// confirmDelete asks before deleting the open note.
func (ui *UI) confirmDelete() {
	if !ui.isSaved() {
		return
	}
	n := *ui.current
	d := dialog.NewConfirm("Delete note", "Delete \""+n.Title+"\"? This cannot be undone.",
		func(ok bool) {
			if ok {
				ui.delete(n)
			}
		}, ui.w)
	d.SetConfirmText("Delete")
	d.SetDismissText("Cancel")
	d.Show()
}

func (ui *UI) delete(n notes.Note) {
	ctx, cancel := requestContext()
	defer cancel()
	if ui.report(ui.c.Delete(ctx, n.ID)) {
		return
	}
	ui.Status.SetText("")
	if ui.current != nil && ui.current.ID == n.ID {
		ui.current = nil
		ui.Editor.SetText("")
		ui.showView()
	}
	ui.refresh()
}

// refresh reloads the note list from the server, filtered by the search box.
func (ui *UI) refresh() {
	ctx, cancel := requestContext()
	defer cancel()
	list, err := ui.c.List(ctx, ui.Search.Text)
	if ui.report(err) {
		return
	}
	ui.notes = list
	ui.List.Refresh()
	ui.selectCurrent()
}

// selectCurrent highlights the open note in the list, if it is there.
func (ui *UI) selectCurrent() {
	for i, n := range ui.notes {
		if ui.isSaved() && n.ID == ui.current.ID {
			ui.List.Select(i)
			return
		}
	}
	ui.List.UnselectAll()
}

// report shows err in the status line and reports whether there was one.
func (ui *UI) report(err error) bool {
	if err != nil {
		ui.Status.SetText(explain(err))
	}
	return err != nil
}

// explain turns a server error into a sentence for the status line.
func explain(err error) string {
	switch {
	case client.IsStale(err):
		return "The note was changed elsewhere since you started editing. Your edit is still open: copy it, then open the note again to load the latest version."
	case client.IsTitleTaken(err):
		return "Another note already has that title."
	case client.IsUnauthorized(err):
		return "The server rejected SCRIBBLE_TOKEN."
	}
	return err.Error()
}
