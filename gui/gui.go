// Package gui is the Fyne desktop client for a scribble server. It holds all
// the windows and widgets; cmd/scribble-gui only opens the app and calls New.
package gui

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/offline"
)

// requestTimeout bounds each call to the server. Saves, renames and deletes
// run on the UI thread, so this is also the longest the window can stall.
const requestTimeout = 10 * time.Second

// unreachable is the status line while the server cannot be reached.
const unreachable = "The server is unreachable. Showing the notes saved on this computer; edits are saved here and sent when it is back."

// allNotes is the project choice that turns the project filter off.
const allNotes = "All notes"

// UI is the main window's content. The widgets are exported so tests can
// drive the window the way a user does.
type UI struct {
	Project *widget.Select // allNotes or a #project the list is narrowed to
	Search  *TextField
	List    *NoteList
	Viewer  *widget.RichText // view mode: the open note rendered as Markdown
	Editor  *TextField       // edit mode: the open note's raw Markdown

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

	menu                                         *fyne.MainMenu
	saveItem, renameItem, deleteItem, toggleItem *fyne.MenuItem // greyed out like their toolbar buttons
	lastField                                    *TextField     // the text field that had focus last, nil if another widget did

	w        fyne.Window
	c        *client.Client
	store    *offline.Store // the local copy all reads come from
	hasTray  bool
	offline  bool // the last sync could not reach the server
	notes    []notes.Note
	projects []string // the #projects in use, as of the last reload
	// current is the open note, nil when none; ID 0 for a new, unsaved
	// note, negative for one saved only on this computer so far. Its
	// working text, saved or not, is always Editor.Text.
	current *notes.Note
	// draftTag is the "#project" New pre-filled into the open draft, "" if
	// none. The draft cannot be saved while that tag is still its title.
	draftTag string
}

// New builds the main window's content for server c and shows the notes in
// store, its local copy. It does not wait for the server: call Sync or
// StartSync to bring the copy up to date.
func New(w fyne.Window, c *client.Client, store *offline.Store) *UI {
	ui := &UI{w: w, c: c, store: store}
	ui.List = &NoteList{ui: ui, List: widget.List{
		Length:     func() int { return len(ui.notes) },
		CreateItem: func() fyne.CanvasObject { return widget.NewLabel("") },
		UpdateItem: func(i widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(ui.notes[i].Title) },
	}}
	ui.List.ExtendBaseWidget(ui.List)
	ui.List.OnSelected = func(i widget.ListItemID) {
		id := ui.notes[i].ID
		if ui.current == nil || ui.current.ID != id {
			ui.unlessUnsaved(func() { ui.load(id) })
		}
	}
	ui.Project = widget.NewSelect([]string{allNotes}, func(string) { ui.refresh() })
	ui.Project.Selected = allNotes
	ui.Search = newTextField(ui)
	ui.Search.SetPlaceHolder("Search")
	ui.Search.OnChanged = func(string) { ui.refresh() }
	ui.Viewer = widget.NewRichText()
	ui.Viewer.Wrapping = fyne.TextWrapWord
	ui.Editor = newTextField(ui)
	ui.Editor.MultiLine = true
	ui.Editor.Wrapping = fyne.TextWrapWord
	ui.Editor.OnChanged = func(string) { ui.updateButtons() }
	ui.EditToggle = widget.NewButton("Edit", ui.toggleMode)
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
	ui.setupMenu()
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
	if nw, ok := ui.w.(driver.NativeWindow); ok {
		nw.RunNative(restoreNativeWindow)
	}
	ui.w.RequestFocus()
}

// OpenNewNote shows and focuses the main window and opens a new-note editor
// ready for typing, asking first if there are unsaved changes.
func (ui *UI) OpenNewNote() {
	ui.showWindow()
	if ui.dialogOpen() {
		return
	}
	ui.unlessUnsaved(ui.startNote)
}

func (ui *UI) quit() {
	if ui.dialogOpen() {
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

// CloseStore releases the lock on the underlying offline cache.
func (ui *UI) CloseStore() error {
	if ui.store != nil {
		return ui.store.Close()
	}
	return nil
}

func requestContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), requestTimeout)
}

// isSaved reports whether the open note has been saved, on the server or
// only on this computer so far.
func (ui *UI) isSaved() bool { return ui.current != nil && ui.current.ID != 0 }

// onServer reports whether the open note is on the server with no edit
// waiting to be sent.
func (ui *UI) onServer() bool {
	if ui.current == nil || ui.current.ID <= 0 {
		return false
	}
	_, queued := ui.store.Pending(ui.current.ID)
	return !queued
}

// hasUnsavedChanges reports whether the editor holds text that is not
// saved.
func (ui *UI) hasUnsavedChanges() bool {
	return ui.current != nil && (ui.current.ID == 0 || ui.Editor.Text != ui.current.Body)
}

// load opens note id from the local copy. If the server refused the note's
// queued edit, the edit comes back as unsaved text in the editor, over the
// version it was based on, with the reason in the status line, just as if
// the save had just been refused.
func (ui *UI) load(id int64) {
	n, ok := ui.store.Get(id)
	if !ok {
		ui.Status.SetText("That note is gone.")
		ui.reload()
		return
	}
	e, queued := ui.store.Pending(id)
	if !queued || e.Err == nil {
		ui.open(n)
		return
	}
	base, ok := ui.store.Server(id)
	if ok {
		base.Revision = e.BaseRevision
	} else {
		base = notes.Note{} // never reached the server, or deleted there: save it anew
	}
	ui.restore(base, e)
}

// restore opens refused edit e as unsaved text over note base. The editor
// now holds the edit, so it leaves the queue.
func (ui *UI) restore(base notes.Note, e offline.Edit) {
	ui.open(base)
	ui.Editor.SetText(e.Body)
	ui.showEdit()
	if ui.report(ui.store.Drop(e.ID)) {
		return
	}
	ui.reload()
	ui.report(e.Err)
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
		list = ui.store.Backlinks(ui.current.ID)
	}
	slices.SortFunc(list, func(a, b notes.Note) int {
		return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	})
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
// none yet it creates it and opens it for editing below the title; inside a
// project it is tagged #project, so it stays in view like a New note.
func (ui *UI) follow(title string) {
	if n, ok := ui.store.ByTitle(title); ok {
		ui.Status.SetText("")
		ui.load(n.ID)
		return
	}
	body := "# " + title + "\n\n"
	if p := ui.activeProject(); p != "" {
		body += "#" + p + "\n"
	}
	n, err := ui.write(0, body, 0)
	if ui.report(err) || ui.hasUnsavedChanges() {
		// Or the sync brought a refused edit of the open note back into
		// the editor: that stays, and the new note waits in the list.
		return
	}
	ui.open(n)
	ui.Editor.CursorRow, ui.Editor.CursorColumn = 1, 0
	ui.Editor.Refresh()
	ui.showEdit()
	ui.w.Canvas().Focus(ui.Editor)
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
	if ui.w.Canvas().Focused() == ui.Editor {
		// A shortcut leaves the focus where it was, and the editor
		// would go on taking keys while hidden.
		ui.w.Canvas().Unfocus()
	}
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

// toggleMode switches between view and edit mode.
func (ui *UI) toggleMode() {
	if ui.Editor.Visible() {
		ui.showView()
	} else {
		ui.showEdit()
	}
}

func (ui *UI) updateButtons() {
	setEnabled(ui.EditToggle, ui.current != nil)
	setEnabled(ui.Save, ui.hasUnsavedChanges())
	// Rename rewrites the note on the server, which would make unsaved
	// or queued edits stale.
	setEnabled(ui.Rename, ui.onServer() && !ui.hasUnsavedChanges())
	setEnabled(ui.Delete, ui.isSaved())
	ui.updateMenu()
}

func setEnabled(b *widget.Button, on bool) {
	if on {
		b.Enable()
	} else {
		b.Disable()
	}
}

// save writes the editor to the server and returns to view mode.
func (ui *UI) save() { ui.saveThen(ui.open) }

// saveAndKeepEditing writes the editor to the server and stays in edit
// mode, with the cursor and the focus where they were.
func (ui *UI) saveAndKeepEditing() {
	// From a view-mode preview too, and if the save is refused, the user
	// ends up in the editor.
	ui.showEdit()
	ui.saveThen(func(n notes.Note) {
		// The editor already holds the saved text; leave it, and its
		// cursor, selection and scroll position, alone.
		ui.current = &n
		ui.updateButtons()
		ui.selectCurrent()
		ui.loadBacklinks()
		if f := ui.editTarget(); f != nil {
			// Clicking the menu bar took the focus away.
			ui.w.Canvas().Focus(f)
		}
	})
}

// saveThen writes the editor to the server and passes the saved note to
// then. The server takes the title from the first non-empty line. An update
// carries the revision the edit started from, so the server refuses it if
// someone else saved in between; the edit then stays open and askConflict
// asks what to do about it.
func (ui *UI) saveThen(then func(notes.Note)) {
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
	n, err := ui.write(ui.current.ID, ui.Editor.Text, ui.current.Revision)
	switch {
	case client.IsStale(err):
		ui.report(err)
		ui.askConflict()
	case !ui.report(err):
		then(n)
	}
}

// askConflict asks what to do with an edit the server refused because the
// note was changed elsewhere: overwrite that version, reload it, or keep
// editing.
func (ui *UI) askConflict() {
	var d *dialog.CustomDialog
	button := func(label string, then func()) *widget.Button {
		return widget.NewButton(label, func() {
			d.Hide()
			then()
		})
	}
	overwrite := button("Overwrite", ui.overwrite)
	overwrite.Importance = widget.DangerImportance
	msg := widget.NewLabel("This note was changed elsewhere since you started editing.\n\n" +
		"Overwrite replaces that version with yours. Reload shows that version instead and copies your edit to the clipboard.")
	msg.Wrapping = fyne.TextWrapWord
	d = dialog.NewCustomWithoutButtons("Note changed elsewhere", msg, ui.w)
	d.SetButtons([]fyne.CanvasObject{button("Keep editing", func() {}), button("Reload", ui.reloadLatest), overwrite})
	d.Resize(fyne.NewSize(460, d.MinSize().Height))
	d.Show()
}

// overwrite saves the editor over whatever the server now holds. It needs
// the server: queued, revision 0 would stay the open note's revision and
// make the next ordinary save overwrite too.
func (ui *UI) overwrite() {
	if ui.current == nil {
		return
	}
	ctx, cancel := requestContext()
	defer cancel()
	n, err := ui.c.Update(ctx, ui.store.ServerID(ui.current.ID), ui.Editor.Text, 0)
	if ui.reportOnline(err) {
		return
	}
	ui.openFromServer(*n)
}

// reloadLatest fetches the open note's latest version and shows it in
// place of the user's edit, which it leaves on the clipboard. If the sync
// fails the edit stays open instead: the local copy may be the version it
// conflicted with.
func (ui *UI) reloadLatest() {
	if ui.current == nil {
		return
	}
	draft := ui.Editor.Text
	id := ui.store.ServerID(ui.current.ID)
	ui.Status.SetText("")
	ctx, cancel := requestContext()
	defer cancel()
	r, err := ui.store.Sync(ctx, ui.c)
	ui.synced(r, err)
	if err != nil {
		ui.Status.SetText(ui.Status.Text + " Your edit is still open.")
		return
	}
	fyne.CurrentApp().Clipboard().SetContent(draft)
	n, ok := ui.store.Get(id)
	if !ok {
		ui.closeNote()
		ui.Status.SetText("The note was deleted elsewhere. Your edit is on the clipboard.")
		return
	}
	ui.open(n)
	ui.Status.SetText("Loaded the latest version. Your edit is on the clipboard.")
}

// write saves body as note id's text (id 0 for a new note), edited from
// revision base, and syncs. It returns the note as saved: on the server,
// or only on this computer while the server is unreachable. If the server
// refuses the edit, it returns why; the text then stays only in the
// editor.
func (ui *UI) write(id int64, body string, base int64) (notes.Note, error) {
	n, err := ui.store.Queue(id, body, base)
	if err != nil {
		return n, err
	}
	ctx, cancel := requestContext()
	defer cancel()
	r, err := ui.store.Sync(ctx, ui.c)
	key := ui.store.ServerID(n.ID)
	if e, ok := ui.store.Pending(key); ok && e.Err != nil {
		ui.report(ui.store.Drop(key))
		return n, e.Err
	}
	r.Failed = slices.DeleteFunc(r.Failed, func(id int64) bool { return id == key })
	ui.Status.SetText("")
	ui.synced(r, err)
	n, _ = ui.store.Get(key)
	return n, nil
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
	if ui.reportOnline(err) {
		return
	}
	// The sync picks up the links the rename rewrote in other notes.
	ui.openFromServer(*n)
}

// openFromServer records note n as the server just returned it, syncs and
// opens it. n is kept and shown even if the sync fails.
func (ui *UI) openFromServer(n notes.Note) {
	ui.Status.SetText("")
	if ui.report(ui.store.Put(n)) {
		return
	}
	ui.Sync()
	if cached, ok := ui.store.Get(n.ID); ok {
		n = cached
	}
	ui.open(n)
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
	// A note created offline may have reached the server while the
	// dialog was open. If it is being sent right now, Sync deletes it
	// once it lands.
	id := ui.store.ServerID(n.ID)
	if id > 0 {
		ctx, cancel := requestContext()
		defer cancel()
		if ui.reportOnline(ui.c.Delete(ctx, id)) {
			return
		}
	}
	// Gone from the copy even if the sync below fails. A queued edit of
	// the note has nothing left to change.
	forget := ui.store.Drop
	if id > 0 {
		forget = ui.store.Remove
	}
	if ui.report(forget(id)) {
		return
	}
	ui.Status.SetText("")
	if ui.current != nil && (ui.current.ID == n.ID || ui.current.ID == id) {
		ui.closeNote()
	}
	if id > 0 {
		ui.Sync()
	} else {
		ui.reload()
	}
}

// closeNote leaves no note open.
func (ui *UI) closeNote() {
	ui.current = nil
	ui.Editor.SetText("")
	ui.showView()
	ui.loadBacklinks()
}

// Sync brings the local copy up to date with the server and shows the
// result. It runs on the UI thread; StartSync runs it in the background.
func (ui *UI) Sync() {
	ctx, cancel := requestContext()
	defer cancel()
	r, err := ui.store.Sync(ctx, ui.c)
	ui.synced(r, err)
}

// StartSync syncs now and then every interval in the background, until
// the app quits.
func (ui *UI) StartSync(interval time.Duration) {
	go func() {
		for {
			ctx, cancel := requestContext()
			r, err := ui.store.Sync(ctx, ui.c)
			cancel()
			fyne.Do(func() { ui.synced(r, err) })
			time.Sleep(interval)
		}
	}()
}

// synced shows the outcome of a sync: the refreshed list, the open note's
// latest version, and any trouble in the status line.
func (ui *UI) synced(r offline.Report, err error) {
	wasOffline := ui.offline
	ui.offline = client.IsUnreachable(err)
	switch {
	case ui.offline:
		if !wasOffline || ui.Status.Text == "" {
			ui.Status.SetText(unreachable)
		}
	case err != nil:
		ui.report(err)
	case wasOffline && ui.Status.Text == unreachable:
		ui.Status.SetText("")
	}
	// Opening a note may have taken its refused edit back into the
	// editor since the sync ran, so look again.
	var refused []offline.Edit
	for _, id := range r.Failed {
		if e, ok := ui.store.Pending(id); ok && e.Err != nil {
			refused = append(refused, e)
		}
	}
	if len(refused) > 0 {
		e := refused[0]
		msg := "\"" + notes.TitleOf(e.Body) + "\" could not be synced. " + explain(e.Err) + " Open the note to sort it out."
		if more := len(refused) - 1; more > 0 {
			msg += " " + strconv.Itoa(more) + " more edits could not be synced either."
		}
		ui.Status.SetText(msg)
	}
	ui.reload()
	ui.followOpenNote()
}

// followOpenNote keeps the open note in step with the local copy after a
// sync: it reopens a refused edit in the editor, and shows the latest
// version (under its server ID, for a note created offline) unless the
// user is editing.
func (ui *UI) followOpenNote() {
	if !ui.isSaved() {
		return
	}
	id := ui.store.ServerID(ui.current.ID)
	n, ok := ui.store.Get(id)
	e, queued := ui.store.Pending(id)
	switch {
	case queued && e.Err != nil && !ui.hasUnsavedChanges():
		ui.load(id) // refused: back to the editor
	case !ok:
		if !ui.hasUnsavedChanges() {
			ui.closeNote() // deleted elsewhere
		}
	case ui.Editor.Visible() || ui.hasUnsavedChanges():
		// Keep the text being edited or previewed, and the revision it
		// started from: a save goes on from there. A note created
		// offline keeps its local ID too; Queue maps it to the server ID
		// and the revision it was created at, not to a newer one
		// downloaded since.
	case n.Revision != ui.current.Revision || n.Body != ui.current.Body || n.ID != ui.current.ID:
		ui.open(n)
	default:
		ui.loadBacklinks()
	}
}

// reload refreshes the project choices and the note list from the local
// copy. Called after the notes change; typing in the search box only needs
// refresh.
func (ui *UI) reload() {
	ui.projects = ui.store.Projects()
	ui.Project.SetOptions(ui.projectOptions(ui.activeProject()))
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

// refresh reloads the note list from the local copy, filtered by the
// project and the search box.
func (ui *UI) refresh() {
	ui.notes = ui.store.List(ui.activeProject(), ui.Search.Text)
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

// reportOnline is report for an action only the server can do.
func (ui *UI) reportOnline(err error) bool {
	if client.IsUnreachable(err) {
		ui.Status.SetText("The server is unreachable. Renaming, deleting and overwriting a note need it; try again when it is back.")
		return true
	}
	return ui.report(err)
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
		return "The note was changed elsewhere since you started editing. Your edit is still open: save it again to choose between overwriting that version and reloading it."
	case client.IsTitleTaken(err):
		return "Another note already has that title."
	case client.IsUnauthorized(err):
		return "The server rejected SCRIBBLE_TOKEN."
	}
	return err.Error()
}
