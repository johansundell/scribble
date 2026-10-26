package gui

import (
	"net/url"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// Version is the application version shown in Help > About Scribble. When it
// is "dev" or empty, the dialog falls back to the Fyne app metadata version.
var Version = "dev"

const repoURL = "https://github.com/johansundell/scribble"

// setupMenu gives the window its File, Edit, View and Help menus. The window
// runs a menu item's shortcut before the focused widget sees the key, and even
// while a dialog is open, so each action checks for that itself.
func (ui *UI) setupMenu() {
	ui.saveItem = menuItem("Save", ctrl(fyne.KeyS), ui.whenEnabled(ui.Save, ui.saveAndKeepEditing))
	ui.renameItem = menuItem("Rename…", &desktop.CustomShortcut{KeyName: fyne.KeyF2}, ui.whenEnabled(ui.Rename, ui.askRename))
	ui.deleteItem = menuItem("Delete…", nil, ui.whenEnabled(ui.Delete, ui.confirmDelete))
	ui.toggleItem = menuItem("Edit note", ctrl(fyne.KeyE), ui.whenEnabled(ui.EditToggle, ui.toggleMode))
	// Our own Quit asks about unsaved changes; with it last, Fyne adds none.
	quit := menuItem("Quit", ctrl(fyne.KeyQ), ui.quit)
	quit.IsQuit = true

	clip := fyne.CurrentApp().Clipboard()
	redo := ui.forwardingItem("Redo", &fyne.ShortcutRedo{})
	if runtime.GOOS == "darwin" {
		redo.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyZ, Modifier: fyne.KeyModifierSuper | fyne.KeyModifierShift}
	}
	ui.menu = fyne.NewMainMenu(
		fyne.NewMenu("File",
			menuItem("New note", ctrl(fyne.KeyN), ui.OpenNewNote),
			ui.saveItem,
			fyne.NewMenuItemSeparator(),
			ui.renameItem,
			ui.deleteItem,
			fyne.NewMenuItemSeparator(),
			quit,
		),
		fyne.NewMenu("Edit",
			ui.forwardingItem("Undo", &fyne.ShortcutUndo{}),
			redo,
			fyne.NewMenuItemSeparator(),
			ui.forwardingItem("Cut", &fyne.ShortcutCut{Clipboard: clip}),
			ui.forwardingItem("Copy", &fyne.ShortcutCopy{Clipboard: clip}),
			ui.forwardingItem("Paste", &fyne.ShortcutPaste{Clipboard: clip}),
			ui.forwardingItem("Select all", &fyne.ShortcutSelectAll{}),
			fyne.NewMenuItemSeparator(),
			menuItem("Find", ctrl(fyne.KeyF), ui.unlessDialog(func() { ui.w.Canvas().Focus(ui.Search) })),
		),
		fyne.NewMenu("View", ui.toggleItem),
		fyne.NewMenu("Help",
			menuItem("About Scribble", nil, ui.unlessDialog(ui.showAbout)),
		),
	)
	ui.w.SetMainMenu(ui.menu)
	// The window only runs menu shortcuts with a modifier, so F2 is caught
	// here with nothing focused and by the note list after a click on it.
	ui.w.Canvas().SetOnTypedKey(func(e *fyne.KeyEvent) { ui.typedKey(e) })
}

func menuItem(label string, shortcut fyne.Shortcut, action func()) *fyne.MenuItem {
	item := fyne.NewMenuItem(label, action)
	item.Shortcut = shortcut
	return item
}

func ctrl(key fyne.KeyName) fyne.Shortcut {
	return &desktop.CustomShortcut{KeyName: key, Modifier: fyne.KeyModifierShortcutDefault}
}

// forwardingItem is an Edit menu item that passes shortcut on to the
// focused text field, which no longer gets it once it is a menu shortcut.
// A field in an open dialog has the focus, so it also works there.
func (ui *UI) forwardingItem(label string, shortcut fyne.Shortcut) *fyne.MenuItem {
	return menuItem(label, shortcut, func() {
		if f, ok := ui.editTarget().(fyne.Shortcutable); ok {
			f.TypedShortcut(shortcut)
		}
	})
}

// editTarget is the widget an Edit menu item acts on: the focused one, or
// else the text field or viewer that had focus last. Clicking the menu bar
// takes the focus away before the item runs. One that is hidden or behind
// a dialog is not one the user meant.
func (ui *UI) editTarget() fyne.Focusable {
	if f := ui.w.Canvas().Focused(); f != nil || ui.dialogOpen() {
		return f
	}
	if ui.lastFocus != nil && ui.lastFocus.Visible() {
		return ui.lastFocus
	}
	return nil
}

// dialogOpen reports whether a dialog is showing over the window.
func (ui *UI) dialogOpen() bool { return ui.w.Canvas().Overlays().Top() != nil }

// unlessDialog wraps action to do nothing while a dialog is open.
func (ui *UI) unlessDialog(action func()) func() {
	return func() {
		if !ui.dialogOpen() {
			action()
		}
	}
}

// whenEnabled wraps action, the action of toolbar button b, for its menu
// item: it does nothing while b is disabled or a dialog is open. The
// item's shortcut fires even while the item is greyed out.
func (ui *UI) whenEnabled(b *widget.Button, action func()) func() {
	return ui.unlessDialog(func() {
		if !b.Disabled() {
			action()
		}
	})
}

// typedKey handles a key press that reached no widget, or the note list.
// It reports whether it used the key.
func (ui *UI) typedKey(e *fyne.KeyEvent) bool {
	if e.Name != fyne.KeyF2 {
		return false
	}
	ui.renameItem.Action()
	return true
}

// showAbout opens the About Scribble dialog, with the app icon beside the
// details.
func (ui *UI) showAbout() {
	var content fyne.CanvasObject = ui.aboutContent()
	if icon := fyne.CurrentApp().Icon(); icon != nil {
		img := canvas.NewImageFromResource(icon)
		img.FillMode = canvas.ImageFillContain
		img.SetMinSize(fyne.NewSquareSize(64))
		content = container.NewBorder(nil, nil, container.NewVBox(img), nil, content)
	}
	dialog.NewCustom("About Scribble", "Close", content, ui.w).Show()
}

// aboutContent is the About dialog's details, with a link to a newer
// release when the server runs one.
func (ui *UI) aboutContent() *fyne.Container {
	u, _ := url.Parse(repoURL)
	content := container.NewVBox(
		widget.NewLabelWithStyle("Scribble "+appVersion(), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Tomboy-inspired Markdown notes"),
		widget.NewLabel("Johan Sundell · MPL-2.0"),
		widget.NewHyperlink(repoURL, u),
		widget.NewLabel(ui.c.BaseURL),
	)
	if ui.newRelease != "" {
		release, _ := url.Parse(releaseURL + ui.newRelease)
		content.Add(widget.NewHyperlink(releaseText(ui.newRelease), release))
	}
	return content
}

func appVersion() string {
	if (Version == "" || Version == "dev") && fyne.CurrentApp() != nil && fyne.CurrentApp().Metadata().Version != "" {
		return "v" + fyne.CurrentApp().Metadata().Version
	}
	if Version == "" {
		return "dev"
	}
	return Version
}

// updateMenu greys out and labels the menu items like their toolbar
// buttons. The window redraws its menu bar only when something changed.
func (ui *UI) updateMenu() {
	changed := false
	for item, b := range map[*fyne.MenuItem]*widget.Button{
		ui.saveItem: ui.Save, ui.renameItem: ui.Rename, ui.deleteItem: ui.Delete, ui.toggleItem: ui.EditToggle,
	} {
		if item.Disabled != b.Disabled() {
			item.Disabled = b.Disabled()
			changed = true
		}
	}
	if label := ui.EditToggle.Text + " note"; ui.toggleItem.Label != label {
		ui.toggleItem.Label = label
		changed = true
	}
	if changed {
		ui.w.SetMainMenu(ui.menu)
	}
}

// NoteList is the note list. It passes key presses to typedKey first, so F2
// renames the note after a click on it has focused the list.
type NoteList struct {
	widget.List
	ui *UI
}

// FocusGained is called when the list gains focus. A clicked Edit menu item
// then no longer acts on the text field or viewer that had focus before.
func (l *NoteList) FocusGained() {
	l.ui.lastFocus = nil
	l.List.FocusGained()
}

// TypedKey is called when a key is pressed while the list has focus.
func (l *NoteList) TypedKey(e *fyne.KeyEvent) {
	if !l.ui.typedKey(e) {
		l.List.TypedKey(e)
	}
}

// editable is a widget the Edit menu acts on: a TextField or the Viewer.
type editable interface {
	fyne.Focusable
	Visible() bool
}

// TextField is a text entry that remembers it had focus last, so a clicked
// Edit menu item still reaches it. The note editor's also edits Markdown
// (#54): the formatting shortcuts, lists that continue on Enter and smart
// paste. mdedit.go works each change out; TextField makes it.
type TextField struct {
	widget.Entry
	ui *UI

	markdown bool // the Markdown editing is on: the note editor, not the search box
	shift    bool // a Shift key is held
	// undo holds the Markdown changes that took Entry more than one undo
	// step, newest last, and redo the ones undone. See applyEdit.
	undo, redo []undoGroup
}

// undoGroup is a Markdown change that Ctrl+Z undoes as one: the text
// before and after it, and the undo steps it took Entry.
type undoGroup struct {
	before, after string
	steps         int
}

func newTextField(ui *UI) *TextField {
	f := &TextField{ui: ui}
	f.ExtendBaseWidget(f)
	return f
}

// FocusGained is called when the field gains focus.
func (f *TextField) FocusGained() {
	f.ui.lastFocus = f
	f.Entry.FocusGained()
}

// SetText replaces the text, which also clears the undo history.
func (f *TextField) SetText(text string) {
	f.undo, f.redo = nil, nil
	f.Entry.SetText(text)
}

// KeyDown is called when a key is pressed down.
func (f *TextField) KeyDown(e *fyne.KeyEvent) {
	if isShift(e) {
		f.shift = true
	}
	f.Entry.KeyDown(e)
}

// KeyUp is called when a key is released.
func (f *TextField) KeyUp(e *fyne.KeyEvent) {
	if isShift(e) {
		f.shift = false
	}
	f.Entry.KeyUp(e)
}

func isShift(e *fyne.KeyEvent) bool {
	return e.Name == desktop.KeyShiftLeft || e.Name == desktop.KeyShiftRight
}

// TypedKey is called when a key is typed. Enter at the end of a list item
// continues the list; Shift+Enter is a plain newline.
func (f *TextField) TypedKey(e *fyne.KeyEvent) {
	if f.markdown && (e.Name == fyne.KeyReturn || e.Name == fyne.KeyEnter) && !f.shift && f.SelectedText() == "" {
		if edit, ok := continueList(f.Text, f.CursorTextOffset()); ok {
			f.applyEdit(edit)
			return
		}
	}
	f.Entry.TypedKey(e)
}

// TypedShortcut is called for a keyboard shortcut, or an Edit menu item
// passing one on.
func (f *TextField) TypedShortcut(s fyne.Shortcut) {
	if f.markdown {
		switch s := s.(type) {
		case *fyne.ShortcutUndo:
			if f.undoGroup() {
				return
			}
		case *fyne.ShortcutRedo:
			if f.redoGroup() {
				return
			}
		case *fyne.ShortcutPaste:
			if f.smartPaste(s.Clipboard) {
				return
			}
		case *desktop.CustomShortcut:
			if fm := formatKeys[s.KeyName]; fm != nil && s.Modifier == fyne.KeyModifierShortcutDefault {
				f.format(fm)
				return
			}
		}
	}
	f.Entry.TypedShortcut(s)
}

// format makes the change fm works out from the selection or cursor.
func (f *TextField) format(fm formatting) {
	start, end := f.selection()
	f.applyEdit(fm(f.Text, start, end))
}

// smartPaste pastes a URL over selected text as a link, and cells from a
// spreadsheet as a table. It reports false, with the selection as it was,
// for a paste Entry should make as usual.
func (f *TextField) smartPaste(clip fyne.Clipboard) bool {
	if clip == nil {
		return false
	}
	content := clip.Content()
	start, end := f.selection()
	edit, ok := pasteURL(start, end, content)
	if !ok {
		edit, ok = pasteTable(f.Text, start, end, content)
	}
	if !ok {
		f.setSelection(start, end)
		return false
	}
	f.applyEdit(edit)
	return true
}

var (
	shiftKey = &fyne.KeyEvent{Name: desktop.KeyShiftLeft}
	rightKey = &fyne.KeyEvent{Name: fyne.KeyRight}
)

// selection returns the start and end of the selection, equal at the
// cursor when there is none. Entry does not say which end its cursor is
// at, so this moves the cursor to the end, as Right does, which ends the
// selection.
func (f *TextField) selection() (start, end int) {
	n := runeLen(f.SelectedText())
	f.clearSelection()
	end = f.CursorTextOffset()
	return end - n, end
}

// clearSelection ends the selection, if any, with the cursor at its end.
func (f *TextField) clearSelection() {
	if f.SelectedText() != "" {
		f.Entry.KeyUp(shiftKey) // with Shift held, Right would grow it
		f.Entry.TypedKey(rightKey)
	}
}

// setCursor puts the cursor at offset p. Entry places
// its cursor by row and column, and with word wrap a row is a line on the
// screen, not one of the text's: the row is the last one starting at or
// before p.
func (f *TextField) setCursor(p int) {
	row, begin := 0, 0
	for r := 1; ; r++ {
		f.CursorRow, f.CursorColumn = r, 0
		b := f.CursorTextOffset() // 0 past the last row
		if b <= begin || b > p {
			break
		}
		row, begin = r, b
	}
	f.CursorRow, f.CursorColumn = row, p-begin
	f.Refresh()
}

// setSelection selects [start, end), or puts the cursor at start when they
// are equal. Entry has no API for it, so it selects the way Shift+Right
// does.
func (f *TextField) setSelection(start, end int) {
	f.clearSelection()
	f.setCursor(start)
	if end <= start {
		return
	}
	f.Entry.KeyDown(shiftKey)
	f.setCursor(end - 1)
	// Right from the end of a wrapped row goes to the start of the next,
	// the same offset.
	for i := 0; i < 2 && f.CursorTextOffset() < end; i++ {
		f.Entry.TypedKey(rightKey)
	}
	f.Entry.KeyUp(shiftKey)
}

// applyEdit makes e's change with Entry's own editing, so that undo works.
// Entry has no API to replace text, but a paste inserts its text as one
// undo step that never merges with the typing around it, and erasing a
// selection is one more. So each op selects what it replaces, if anything,
// and pastes its text. One that takes more than one step, such as wrapping
// a selection, is kept in undo, and TypedShortcut undoes all its steps on
// one Ctrl+Z while the text is still as the change left it.
func (f *TextField) applyEdit(e mdEdit) {
	before, steps := f.Text, 0
	for _, op := range e.ops {
		f.setSelection(op.at, op.at+op.n)
		if op.n > 0 {
			steps++
			if op.s == "" {
				f.Entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})
				continue
			}
		}
		f.Entry.TypedShortcut(&fyne.ShortcutPaste{Clipboard: textClipboard(op.s)})
		steps++
	}
	f.setSelection(e.selStart, e.selEnd)
	if f.shift {
		f.Entry.KeyDown(shiftKey)
	}
	f.redo = nil
	if steps > 1 {
		f.undo = append(f.undo, undoGroup{before: before, after: f.Text, steps: steps})
	}
}

// undoGroup undoes the last Markdown change in one go when the text is as
// it left it, and reports whether it did. The Undo of Entry's own
// right-click menu bypasses it and goes a step at a time.
func (f *TextField) undoGroup() bool {
	n := len(f.undo)
	if n == 0 || f.undo[n-1].after != f.Text {
		return false
	}
	g := f.undo[n-1]
	f.undo = f.undo[:n-1]
	f.clearSelection()
	for range g.steps {
		f.Entry.Undo()
	}
	f.redo = append(f.redo, g)
	return true
}

// redoGroup is undoGroup's opposite.
func (f *TextField) redoGroup() bool {
	n := len(f.redo)
	if n == 0 || f.redo[n-1].before != f.Text {
		return false
	}
	g := f.redo[n-1]
	f.redo = f.redo[:n-1]
	f.clearSelection()
	for range g.steps {
		f.Entry.Redo()
	}
	f.undo = append(f.undo, g)
	return true
}

// textClipboard is a clipboard holding a fixed text, for pasting it.
type textClipboard string

func (c textClipboard) Content() string   { return string(c) }
func (c textClipboard) SetContent(string) {}
