package gui

import (
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// setupMenu gives the window its File, Edit and View menus. The window runs
// a menu item's shortcut before the focused widget sees the key, and even
// while a dialog is open, so each action checks for that itself.
func (ui *UI) setupMenu() {
	ui.renameItem = menuItem("Rename…", &desktop.CustomShortcut{KeyName: fyne.KeyF2}, ui.whenEnabled(ui.Rename, ui.askRename))
	ui.deleteItem = menuItem("Delete…", nil, ui.whenEnabled(ui.Delete, ui.confirmDelete))
	ui.toggleItem = menuItem("Edit note", ctrl(fyne.KeyE), ui.whenEnabled(ui.EditToggle, ui.toggleMode))
	// Our own Quit asks about unsaved changes; with it last, Fyne adds none.
	quit := menuItem("Quit", ctrl(fyne.KeyQ), ui.quit)
	quit.IsQuit = true

	clip := fyne.CurrentApp().Clipboard()
	redo := ui.forward("Redo", &fyne.ShortcutRedo{})
	if runtime.GOOS == "darwin" {
		redo.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyZ, Modifier: fyne.KeyModifierSuper | fyne.KeyModifierShift}
	}
	ui.menu = fyne.NewMainMenu(
		fyne.NewMenu("File",
			menuItem("New note", ctrl(fyne.KeyN), ui.OpenNewNote),
			fyne.NewMenuItemSeparator(),
			ui.renameItem,
			ui.deleteItem,
			fyne.NewMenuItemSeparator(),
			quit,
		),
		fyne.NewMenu("Edit",
			ui.forward("Undo", &fyne.ShortcutUndo{}),
			redo,
			fyne.NewMenuItemSeparator(),
			ui.forward("Cut", &fyne.ShortcutCut{Clipboard: clip}),
			ui.forward("Copy", &fyne.ShortcutCopy{Clipboard: clip}),
			ui.forward("Paste", &fyne.ShortcutPaste{Clipboard: clip}),
			ui.forward("Select all", &fyne.ShortcutSelectAll{}),
			fyne.NewMenuItemSeparator(),
			menuItem("Find", ctrl(fyne.KeyF), ui.unlessDialog(func() { ui.w.Canvas().Focus(ui.Search) })),
		),
		fyne.NewMenu("View", ui.toggleItem),
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

// forward is an Edit menu item that passes shortcut on to the focused text
// field: as the menu's shortcut it no longer reaches the field by itself. A
// field in an open dialog has the focus, so it works there too.
func (ui *UI) forward(label string, shortcut fyne.Shortcut) *fyne.MenuItem {
	return menuItem(label, shortcut, func() {
		if f, ok := ui.w.Canvas().Focused().(fyne.Shortcutable); ok {
			f.TypedShortcut(shortcut)
		}
	})
}

// dialogOpen reports whether a dialog is showing over the window.
func (ui *UI) dialogOpen() bool { return ui.w.Canvas().Overlays().Top() != nil }

func (ui *UI) unlessDialog(action func()) func() {
	return func() {
		if !ui.dialogOpen() {
			action()
		}
	}
}

// whenEnabled is action, for a menu item that does what toolbar button b
// does: its shortcut fires even while the item is greyed out.
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

// updateMenu greys out and labels the menu items like their toolbar
// buttons. The window redraws its menu bar only when something changed.
func (ui *UI) updateMenu() {
	changed := false
	for item, b := range map[*fyne.MenuItem]*widget.Button{
		ui.renameItem: ui.Rename, ui.deleteItem: ui.Delete, ui.toggleItem: ui.EditToggle,
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

// NoteList is the note list. It takes the keys typedKey handles before the
// list does.
type NoteList struct {
	widget.List
	ui *UI
}

// TypedKey is called when a key is pressed while the list has focus.
func (l *NoteList) TypedKey(e *fyne.KeyEvent) {
	if !l.ui.typedKey(e) {
		l.List.TypedKey(e)
	}
}
