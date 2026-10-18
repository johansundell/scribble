// Package gui is the Fyne desktop client for a scribble server. It holds all
// the windows and widgets; cmd/scribble-gui only opens the app and calls New.
package gui

import (
	"context"
	"slices"
	"strings"
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

// allNotes is the project choice that turns the project filter off.
const allNotes = "All notes"

// UI is the main window's content. The widgets are exported so tests can
// drive the window the way a user does.
type UI struct {
	Project *widget.Select // allNotes or a #project the list is narrowed to
	Search  *widget.Entry
	List    *widget.List
	Viewer  *widget.RichText // view mode: the open note rendered as Markdown
	Editor  *widget.Entry    // edit mode: the open note's raw Markdown

	EditToggle *widget.Button // switches between view and edit mode
	Save       *widget.Button
	NewNote    *widget.Button
	Rename     *widget.Button // retitles the open note and the [[links]] to it
	Delete     *widget.Button

	// Backlinks is the "Linked from" panel: a button per note that links to
	// the open one, hidden when there are none.
	Backlinks *fyne.Container

	Status *widget.Label // the last error, empty when all is well

	backlinkButtons *fyne.Container // inside Backlinks

	w        fyne.Window
	c        *client.Client
	notes    []notes.Note
	projects []string // the server's #projects, as of the last reload
	// current is the open note, nil when none; ID 0 for a new, unsaved
	// note. Its working text, saved or not, is always Editor.Text.
	current *notes.Note
	// draftTag is the "#project" New pre-filled into the open draft, "" if
	// none. The draft cannot be saved while that tag is still its title.
	draftTag string
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
		id := ui.notes[i].ID
		if ui.current == nil || ui.current.ID != id {
			ui.unlessUnsaved(func() { ui.load(id) })
		}
	}
	ui.Project = widget.NewSelect([]string{allNotes}, func(string) { ui.refresh() })
	ui.Project.Selected = allNotes
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
	ui.Rename = widget.NewButton("Rename", ui.askRename)
	ui.Delete = widget.NewButton("Delete", ui.confirmDelete)
	ui.Status = widget.NewLabel("")
	ui.Status.Wrapping = fyne.TextWrapWord
	ui.backlinkButtons = container.NewHBox()
	ui.Backlinks = container.NewVBox(
		widget.NewLabelWithStyle("Linked from", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHScroll(ui.backlinkButtons))
	ui.Backlinks.Hide()

	toolbar := container.NewHBox(ui.NewNote, ui.EditToggle, ui.Save, layout.NewSpacer(), ui.Rename, ui.Delete)
	split := container.NewHSplit(
		container.NewBorder(container.NewVBox(ui.Project, ui.Search), nil, nil, nil, ui.List),
		container.NewBorder(toolbar, ui.Backlinks, nil, nil,
			container.NewStack(container.NewVScroll(ui.Viewer), ui.Editor)),
	)
	split.Offset = 0.3
	w.SetContent(container.NewBorder(nil, ui.Status, nil, nil, split))
	w.SetCloseIntercept(ui.Close)
	ui.showView()
	ui.reload()
	return ui
}

// Close closes the window, first asking to discard any unsaved changes. The
// window's close button calls it.
func (ui *UI) Close() { ui.unlessUnsaved(ui.w.Close) }

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

// load fetches the latest version of note id from the server and opens it.
// If that fails, whatever was open stays open.
func (ui *UI) load(id int64) {
	ctx, cancel := requestContext()
	defer cancel()
	n, err := ui.c.Get(ctx, id)
	if ui.report(err) {
		ui.selectCurrent()
		return
	}
	ui.open(*n)
}

// open shows note n in view mode.
func (ui *UI) open(n notes.Note) {
	ui.current = &n
	ui.Editor.SetText(n.Body)
	ui.showView()
	ui.selectCurrent()
	ui.loadBacklinks()
}

// loadBacklinks fills the "Linked from" panel for the open note.
func (ui *UI) loadBacklinks() {
	var list []notes.Note
	if ui.isSaved() {
		ctx, cancel := requestContext()
		defer cancel()
		var err error
		if list, err = ui.c.Backlinks(ctx, ui.current.ID); ui.report(err) {
			list = nil
		}
	}
	slices.SortFunc(list, func(a, b notes.Note) int { return strings.Compare(a.Title, b.Title) })
	ui.backlinkButtons.RemoveAll()
	for _, n := range list {
		b := widget.NewButton(n.Title, func() { ui.unlessUnsaved(func() { ui.load(n.ID) }) })
		b.Importance = widget.LowImportance
		ui.backlinkButtons.Add(b)
	}
	if len(list) == 0 {
		ui.Backlinks.Hide()
	} else {
		ui.Backlinks.Show()
	}
}

// follow opens the note titled title, the target of a [[link]]. If there is
// none yet it creates it and opens it for editing below the title.
func (ui *UI) follow(title string) {
	ctx, cancel := requestContext()
	defer cancel()
	n, err := ui.c.ByTitle(ctx, title)
	if client.IsNotFound(err) {
		if n, err = ui.c.Create(ctx, "# "+title+"\n\n"); ui.report(err) {
			return
		}
		ui.Status.SetText("")
		ui.reload()
		ui.open(*n)
		ui.Editor.CursorRow, ui.Editor.CursorColumn = 2, 0
		ui.Editor.Refresh()
		ui.showEdit()
		return
	}
	if ui.report(err) {
		return
	}
	ui.open(*n)
}

// startNote opens an editor for a new note. Inside a project it starts with
// the #project tag below an empty title line, so the note stays in view.
func (ui *UI) startNote() {
	ui.current = &notes.Note{}
	ui.draftTag = ""
	text := ""
	if p := ui.activeProject(); p != "" {
		ui.draftTag = "#" + p
		text = "\n\n" + ui.draftTag + "\n"
	}
	ui.Editor.SetText(text)
	// SetText only clamps the cursor; put it on the title line.
	ui.Editor.CursorRow, ui.Editor.CursorColumn = 0, 0
	ui.Editor.Refresh()
	ui.showEdit()
	ui.selectCurrent()
	ui.loadBacklinks()
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

// showView switches to view mode, rendering the editor's text with its
// [[links]] as links that open the note and its #project tags as links that
// switch the project filter.
func (ui *UI) showView() {
	// [[links]] first, or "[[Plan #2]]" would lose its tag to a project
	// link. Parse off-screen and hook the links before the viewer sees the
	// segments: hyperlink and table visuals copy OnTapped when created.
	md := notes.LinkifyProjectsForView(notes.LinkifyForView(ui.Editor.Text))
	segs := widget.NewRichTextFromMarkdown(md).Segments
	ui.hookLinks(segs)
	ui.Viewer.Segments = segs
	ui.Viewer.Refresh()
	ui.Editor.Hide()
	ui.Viewer.Show()
	ui.EditToggle.SetText("Edit")
	ui.updateButtons()
}

// hookLinks makes note:/// links open their note and project:// links
// select their project, instead of asking the OS to open the URL. Lists and
// tables nest their segments, so it recurses.
func (ui *UI) hookLinks(segs []widget.RichTextSegment) {
	for _, s := range segs {
		switch s := s.(type) {
		case *widget.HyperlinkSegment:
			if title, ok := notes.LinkTitle(s.URL); ok {
				s.OnTapped = func() { ui.unlessUnsaved(func() { ui.follow(title) }) }
			} else if s.URL != nil && s.URL.Scheme == "project" {
				name := s.URL.Host
				s.OnTapped = func() { ui.selectProject(name) }
			}
		case *widget.TableSegment:
			for _, cell := range s.Headers {
				ui.hookLinks(cell)
			}
			for _, row := range s.Rows {
				for _, cell := range row {
					ui.hookLinks(cell)
				}
			}
		case interface {
			Segments() []widget.RichTextSegment
		}:
			ui.hookLinks(s.Segments())
		}
	}
}

// activeProject returns the project the list is narrowed to, "" for all.
func (ui *UI) activeProject() string {
	if ui.Project.Selected == allNotes {
		return ""
	}
	return ui.Project.Selected
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
	// Rename rewrites the note on the server, which would make unsaved
	// edits stale.
	setEnabled(ui.Rename, ui.isSaved() && !ui.hasUnsavedChanges())
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
	if !ui.isSaved() && ui.draftTag != "" && strings.EqualFold(notes.TitleLine(ui.Editor.Text), ui.draftTag) &&
		!notes.HasProject(ui.Editor.Text, ui.draftTag) {
		// No title typed above the pre-filled tag: saved like this, the
		// tag would become the title and the note would not be in the
		// project.
		ui.Status.SetText("Type a title on the first line, above the #tag.")
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
	ui.reload()
	ui.open(*n)
}

// askRename asks for a new title for the open note. The server rewrites
// [[links]] to it in other notes as well.
func (ui *UI) askRename() {
	if !ui.isSaved() || ui.hasUnsavedChanges() {
		return
	}
	id := ui.current.ID
	title := widget.NewEntry()
	title.SetText(ui.current.Title)
	d := dialog.NewForm("Rename note", "Rename", "Cancel",
		[]*widget.FormItem{widget.NewFormItem("Title", title)},
		func(ok bool) {
			if ok {
				ui.rename(id, title.Text)
			}
		}, ui.w)
	d.Resize(fyne.NewSize(400, d.MinSize().Height))
	d.Show()
	ui.w.Canvas().Focus(title)
}

func (ui *UI) rename(id int64, title string) {
	ctx, cancel := requestContext()
	defer cancel()
	n, err := ui.c.Rename(ctx, id, title)
	if ui.report(err) {
		return
	}
	ui.Status.SetText("")
	ui.reload()
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
		ui.loadBacklinks()
	}
	ui.reload()
}

// reload refreshes the project choices and the note list. Called after the
// notes change; typing in the search box only needs refresh.
func (ui *UI) reload() {
	ctx, cancel := requestContext()
	defer cancel()
	projects, err := ui.c.Projects(ctx)
	if !ui.report(err) {
		ui.projects = projects
		ui.Project.SetOptions(ui.projectOptions(ui.activeProject()))
	}
	ui.refresh()
}

// projectOptions is the selector's choices: allNotes, then the server's
// projects plus extra. extra keeps a project selectable that no saved note
// has (its last note deleted, or a tag only in an unsaved draft).
func (ui *UI) projectOptions(extra string) []string {
	projects := slices.Clone(ui.projects)
	if extra != "" && !slices.Contains(projects, extra) {
		projects = append(projects, extra)
		slices.Sort(projects)
	}
	return append([]string{allNotes}, projects...)
}

// selectProject narrows the list to project name.
func (ui *UI) selectProject(name string) {
	ui.Project.SetOptions(ui.projectOptions(name))
	ui.Project.SetSelected(name)
}

// refresh reloads the note list from the server, filtered by the project
// and the search box.
func (ui *UI) refresh() {
	ctx, cancel := requestContext()
	defer cancel()
	list, err := ui.c.ListInProject(ctx, ui.activeProject(), ui.Search.Text)
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
