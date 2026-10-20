package gui

import (
	"net/url"
	"runtime"

	"fyne.io/fyne/v2"
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

// showAbout opens the About Scribble dialog.
func (ui *UI) showAbout() {
	dialog.NewCustom("About Scribble", "Close", ui.aboutContent(), ui.w).Show()
}

// aboutContent is the About dialog's content, with a link to a newer
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
// Edit menu item still reaches it.
type TextField struct {
	widget.Entry
	ui *UI
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
