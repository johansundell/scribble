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
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
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
	Delete     *widget.Button

	Status *widget.Label // the last error, empty when all is well

	w        fyne.Window
	c        *client.Client
	hasTray  bool
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
	ui.Delete = widget.NewButton("Delete", ui.confirmDelete)
	ui.Status = widget.NewLabel("")
	ui.Status.Wrapping = fyne.TextWrapWord

	toolbar := container.NewHBox(ui.NewNote, ui.EditToggle, ui.Save, layout.NewSpacer(), ui.Delete)
	split := container.NewHSplit(
		container.NewBorder(container.NewVBox(ui.Project, ui.Search), nil, nil, nil, ui.List),
		container.NewBorder(toolbar, nil, nil, nil,
			container.NewStack(container.NewVScroll(ui.Viewer), ui.Editor)),
	)
	split.Offset = 0.3
	w.SetContent(container.NewBorder(nil, ui.Status, nil, nil, split))
	w.SetCloseIntercept(ui.Close)
	ui.showView()
	ui.reload()
	return ui
}

// SetupTray registers the system tray icon and menu ("New Note", "Open
// Scribble", "Quit") on desk and makes closing the main window hide it to
// the tray instead of exiting.
func (ui *UI) SetupTray(desk desktop.App) {
	if desk == nil {
		return
	}
	ui.hasTray = true
	quit := fyne.NewMenuItem("Quit", ui.quit)
	quit.IsQuit = true
	menu := fyne.NewMenu("Scribble",
		fyne.NewMenuItem("New Note", ui.OpenNewNote),
		fyne.NewMenuItem("Open Scribble", ui.showWindow),
		fyne.NewMenuItemSeparator(),
		quit,
	)
	desk.SetSystemTrayIcon(theme.DocumentIcon())
	desk.SetSystemTrayMenu(menu)
	desk.SetSystemTrayWindow(ui.w)
	// SetSystemTrayWindow replaces the window's close intercept with w.Hide;
	// restore ui.Close so ui.Close() and the window's close button share one path.
	ui.w.SetCloseIntercept(ui.Close)
}

func (ui *UI) showWindow() {
	ui.w.Show()
	ui.w.RequestFocus()
}

// OpenNewNote shows and focuses the main window and opens a new-note editor
// ready for typing, asking first if there are unsaved changes.
func (ui *UI) OpenNewNote() {
	ui.showWindow()
	if ui.w.Canvas().Overlays().Top() != nil {
		return
	}
	ui.unlessUnsaved(ui.startNote)
}

func (ui *UI) quit() {
	if ui.w.Canvas().Overlays().Top() != nil {
		ui.showWindow()
		return
	}
	if ui.hasUnsavedChanges() {
		ui.showWindow()
	}
	ui.unlessUnsaved(func() {
		ui.w.Close()
		fyne.CurrentApp().Quit()
	})
}

// Close closes the window, or hides it to the system tray when a tray is
// active. Without a tray, it first asks to discard any unsaved changes.
func (ui *UI) Close() {
	if ui.hasTray {
		ui.w.Hide()
		return
	}
	ui.unlessUnsaved(ui.w.Close)
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
	ui.w.Canvas().Focus(ui.Editor)
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

// showView switches to view mode, rendering the editor's text with its
// #project tags as links that switch the project filter.
func (ui *UI) showView() {
	// Parse off-screen and hook the links before the viewer sees the
	// segments: hyperlink and table visuals copy OnTapped when created.
	segs := widget.NewRichTextFromMarkdown(notes.LinkifyProjectsForView(ui.Editor.Text)).Segments
	ui.hookProjectLinks(segs)
	ui.Viewer.Segments = segs
	ui.Viewer.Refresh()
	ui.Editor.Hide()
	ui.Viewer.Show()
	ui.EditToggle.SetText("Edit")
	ui.updateButtons()
}

// hookProjectLinks makes project:// links select their project instead of
// asking the OS to open the URL. Lists and tables nest their segments, so
// it recurses.
func (ui *UI) hookProjectLinks(segs []widget.RichTextSegment) {
	for _, s := range segs {
		switch s := s.(type) {
		case *widget.HyperlinkSegment:
			if s.URL != nil && s.URL.Scheme == "project" {
				name := s.URL.Host
				s.OnTapped = func() { ui.selectProject(name) }
			}
		case *widget.TableSegment:
			for _, cell := range s.Headers {
				ui.hookProjectLinks(cell)
			}
			for _, row := range s.Rows {
				for _, cell := range row {
					ui.hookProjectLinks(cell)
				}
			}
		case interface {
			Segments() []widget.RichTextSegment
		}:
			ui.hookProjectLinks(s.Segments())
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
