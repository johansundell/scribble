package gui_test

import (
	"runtime"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/gui"
)

// menuItem returns the main menu's item labelled label in the menu labelled
// menu. The test driver does not route shortcuts to the main menu, so tests
// check an item's shortcut and call its Action.
func menuItem(t *testing.T, w fyne.Window, menu, label string) *fyne.MenuItem {
	t.Helper()
	main := w.MainMenu()
	if main == nil {
		t.Fatal("the window has no main menu")
	}
	for _, m := range main.Items {
		if m.Label != menu {
			continue
		}
		for _, item := range m.Items {
			if item.Label == label {
				return item
			}
		}
		t.Fatalf("the %s menu has no %q item", menu, label)
	}
	t.Fatalf("the main menu has no %s menu", menu)
	return nil
}

func ctrl(key fyne.KeyName) fyne.Shortcut {
	return &desktop.CustomShortcut{KeyName: key, Modifier: fyne.KeyModifierShortcutDefault}
}

func TestMainMenuHasItemsWithShortcuts(t *testing.T) {
	_, w := open(t, newServer(t))

	var labels []string
	for _, m := range w.MainMenu().Items {
		labels = append(labels, m.Label)
	}
	if len(labels) != 4 || labels[0] != "File" || labels[1] != "Edit" || labels[2] != "View" || labels[3] != "Help" {
		t.Fatalf("menus are %q, want File, Edit, View, Help", labels)
	}

	redo := fyne.Shortcut(&fyne.ShortcutRedo{})
	if runtime.GOOS == "darwin" {
		redo = &desktop.CustomShortcut{KeyName: fyne.KeyZ, Modifier: fyne.KeyModifierSuper | fyne.KeyModifierShift}
	}
	for _, c := range []struct {
		menu, label string
		shortcut    fyne.Shortcut // nil for none
	}{
		{"File", "New note", ctrl(fyne.KeyN)},
		{"File", "Save", ctrl(fyne.KeyS)},
		{"File", "Rename…", &desktop.CustomShortcut{KeyName: fyne.KeyF2}},
		{"File", "Delete…", nil},
		{"File", "Quit", ctrl(fyne.KeyQ)},
		{"Edit", "Undo", &fyne.ShortcutUndo{}},
		{"Edit", "Redo", redo},
		{"Edit", "Cut", &fyne.ShortcutCut{}},
		{"Edit", "Copy", &fyne.ShortcutCopy{}},
		{"Edit", "Paste", &fyne.ShortcutPaste{}},
		{"Edit", "Select all", &fyne.ShortcutSelectAll{}},
		{"Edit", "Find", ctrl(fyne.KeyF)},
		{"View", "Edit note", ctrl(fyne.KeyE)},
		{"Help", "About Scribble", nil},
	} {
		item := menuItem(t, w, c.menu, c.label)
		if item.Action == nil {
			t.Errorf("%s > %s has no action", c.menu, c.label)
		}
		switch {
		case c.shortcut == nil && item.Shortcut != nil:
			t.Errorf("%s > %s has shortcut %s, want none", c.menu, c.label, item.Shortcut.ShortcutName())
		case c.shortcut == nil:
		case item.Shortcut == nil:
			t.Errorf("%s > %s has no shortcut, want %s", c.menu, c.label, c.shortcut.ShortcutName())
		case item.Shortcut.ShortcutName() != c.shortcut.ShortcutName():
			// The window matches a key press to a menu item by this name.
			t.Errorf("%s > %s has shortcut %s, want %s", c.menu, c.label, item.Shortcut.ShortcutName(), c.shortcut.ShortcutName())
		}
	}

	// Our own Quit, last in the first menu, stops Fyne adding one that
	// skips the unsaved-changes prompt.
	file := w.MainMenu().Items[0].Items
	if last := file[len(file)-1]; last.Label != "Quit" || !last.IsQuit {
		t.Fatalf("the File menu ends with %q (IsQuit %v), want our Quit", last.Label, last.IsQuit)
	}
}

func TestMenuItemsGreyOutWithToolbar(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)

	check := func(when string) {
		t.Helper()
		for _, row := range []struct {
			menu, label string
			button      *widget.Button
		}{
			{"File", "Save", ui.Save},
			{"File", "Rename…", ui.Rename},
			{"File", "Delete…", ui.Delete},
			{"View", "Edit note", ui.EditToggle},
		} {
			label := row.label
			if row.button == ui.EditToggle && ui.EditToggle.Text == "View" {
				label = "View note"
			}
			if got := menuItem(t, w, row.menu, label).Disabled; got != row.button.Disabled() {
				t.Errorf("%s: %s > %s disabled %v, toolbar button disabled %v", when, row.menu, label, got, row.button.Disabled())
			}
		}
		for _, label := range []string{"New note", "Quit"} {
			if menuItem(t, w, "File", label).Disabled {
				t.Errorf("%s: File > %s is disabled", when, label)
			}
		}
	}

	check("no note open")
	ui.List.Select(0)
	check("note open")
	test.Tap(ui.EditToggle)
	check("editing")
	test.Type(ui.Editor, "x")
	check("unsaved edits")

	menuItem(t, w, "View", "View note").Action()
	if ui.Editor.Visible() {
		t.Fatal("View note should switch to view mode")
	}
	menuItem(t, w, "View", "Edit note").Action()
	if !ui.Editor.Visible() {
		t.Fatal("Edit note should switch to edit mode")
	}
}

func TestDisabledMenuItemsDoNothing(t *testing.T) {
	// A menu shortcut fires even when its item is greyed out.
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)

	menuItem(t, w, "File", "Rename…").Action()
	menuItem(t, w, "File", "Delete…").Action()
	menuItem(t, w, "View", "Edit note").Action()
	if w.Canvas().Overlays().Top() != nil || ui.Editor.Visible() {
		t.Fatal("greyed-out menu items should do nothing")
	}
}

func TestMenuNewNoteAsksBeforeDiscardingEdits(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)

	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nunsaved")

	menuItem(t, w, "File", "New note").Action()
	tapDialogButton(t, w, "Cancel")
	if ui.Editor.Text != "# Ideas\nunsaved" {
		t.Fatalf("cancelling should keep the draft, got %q", ui.Editor.Text)
	}

	menuItem(t, w, "File", "New note").Action()
	tapDialogButton(t, w, "Discard")
	if !ui.Editor.Visible() || ui.Editor.Text != "" || w.Canvas().Focused() != ui.Editor {
		t.Fatalf("discarding should open a fresh focused editor, got %q", ui.Editor.Text)
	}
}

func TestMenuRenameAndDeleteAskLikeTheToolbar(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Groceries\nmilk")
	ui, w := open(t, c)
	ui.List.Select(0)

	menuItem(t, w, "File", "Rename…").Action()
	top := w.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("Rename… showed no dialog")
	}
	for _, o := range test.LaidOutObjects(top) {
		if e, ok := o.(*widget.Entry); ok {
			e.SetText("Food")
		}
	}
	tapDialogButton(t, w, "Rename")
	byTitle(t, c, "Food")

	menuItem(t, w, "File", "Delete…").Action()
	tapDialogButton(t, w, "Delete")
	assertTitles(t, ui)
}

func TestF2RenamesFromTheListOrWithNothingFocused(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Groceries\nmilk")
	ui, w := open(t, c)

	// Clicking a note focuses the list, which gets the key press.
	var tapped bool
	for _, o := range test.LaidOutObjects(ui.List) {
		if item, ok := o.(fyne.Tappable); ok && o != fyne.CanvasObject(ui.List) {
			item.Tapped(&fyne.PointEvent{})
			tapped = true
			break
		}
	}
	if !tapped || ui.Editor.Text != "# Groceries\nmilk" {
		t.Fatal("could not click the note in the list")
	}
	focused := w.Canvas().Focused()
	if focused == nil {
		t.Fatal("clicking a note should focus the list")
	}
	focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyF2})
	tapDialogButton(t, w, "Cancel")

	w.Canvas().Unfocus()
	w.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: fyne.KeyF2})
	tapDialogButton(t, w, "Cancel")
}

func TestMenuActionsDoNothingWhileADialogIsOpen(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := openWithSpy(t, c)
	ui.List.Select(0)

	test.Tap(ui.Delete) // the dialog stays open
	dialog := w.Canvas().Overlays().Top()
	for _, item := range []struct{ menu, label string }{
		{"File", "New note"}, {"File", "Rename…"}, {"File", "Delete…"}, {"File", "Quit"},
		{"Edit", "Find"}, {"View", "Edit note"}, {"Help", "About Scribble"},
	} {
		menuItem(t, w, item.menu, item.label).Action()
		if top := w.Canvas().Overlays().Top(); top != dialog || len(w.Canvas().Overlays().List()) != 1 {
			t.Fatalf("%s > %s changed the dialogs while one was open", item.menu, item.label)
		}
	}
	if ui.Editor.Visible() || w.closed || w.Canvas().Focused() == ui.Search {
		t.Fatal("a menu action acted while a dialog was open")
	}
	w.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: fyne.KeyF2})
	if len(w.Canvas().Overlays().List()) != 1 {
		t.Fatal("F2 opened a dialog over a dialog")
	}
}

func TestMenuQuitAsksBeforeDiscardingEdits(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := openWithSpy(t, c)

	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nunsaved")

	menuItem(t, w, "File", "Quit").Action()
	tapDialogButton(t, w, "Cancel")
	if w.closed {
		t.Fatal("cancelling Quit should keep the window open")
	}
	menuItem(t, w, "File", "Quit").Action()
	tapDialogButton(t, w, "Discard")
	if !w.closed {
		t.Fatal("discarding on Quit should close the window")
	}
}

func TestMenuQuitQuitsEvenWithATray(t *testing.T) {
	c := newServer(t)
	ui, w := openWithSpy(t, c)
	ui.SetupTray(&fakeTray{})

	menuItem(t, w, "File", "Quit").Action()
	if !w.closed {
		t.Fatal("Quit should close the window, not hide it to the tray")
	}
}

func TestEditMenuActsOnTheFocusedField(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	clipboard := fyne.CurrentApp().Clipboard()

	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	w.Canvas().Focus(ui.Editor)

	menuItem(t, w, "Edit", "Select all").Action()
	menuItem(t, w, "Edit", "Copy").Action()
	if got := clipboard.Content(); got != "# Ideas\nfly" {
		t.Fatalf("Copy put %q on the clipboard", got)
	}
	menuItem(t, w, "Edit", "Cut").Action()
	if ui.Editor.Text != "" {
		t.Fatalf("Cut left %q in the editor", ui.Editor.Text)
	}
	menuItem(t, w, "Edit", "Undo").Action()
	if ui.Editor.Text != "# Ideas\nfly" {
		t.Fatalf("Undo left %q in the editor", ui.Editor.Text)
	}
	menuItem(t, w, "Edit", "Redo").Action()
	if ui.Editor.Text != "" {
		t.Fatalf("Redo left %q in the editor", ui.Editor.Text)
	}

	w.Canvas().Focus(ui.Search)
	clipboard.SetContent("fly")
	menuItem(t, w, "Edit", "Paste").Action()
	if ui.Search.Text != "fly" {
		t.Fatalf("Paste put %q in the search box", ui.Search.Text)
	}

	for _, label := range []string{"Undo", "Redo", "Cut", "Copy", "Paste", "Select all"} {
		if menuItem(t, w, "Edit", label).Disabled {
			t.Errorf("Edit > %s is disabled", label)
		}
	}
}

// clickNote clicks the first note in the list, which focuses the list.
func clickNote(t *testing.T, ui *gui.UI) {
	t.Helper()
	for _, o := range test.LaidOutObjects(ui.List) {
		if item, ok := o.(fyne.Tappable); ok && o != fyne.CanvasObject(ui.List) {
			item.Tapped(&fyne.PointEvent{})
			return
		}
	}
	t.Fatal("the list shows no note to click")
}

func TestClickedEditMenuReachesTheFieldThatHadFocus(t *testing.T) {
	// Clicking the menu bar takes the focus away before the item runs;
	// Unfocus stands in for that click.
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	clipboard := fyne.CurrentApp().Clipboard()

	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	w.Canvas().Focus(ui.Editor)
	w.Canvas().Unfocus()
	menuItem(t, w, "Edit", "Select all").Action()
	menuItem(t, w, "Edit", "Copy").Action()
	if got := clipboard.Content(); got != "# Ideas\nfly" {
		t.Fatalf("clicked Copy put %q on the clipboard", got)
	}

	clipboard.SetContent("fly")
	w.Canvas().Focus(ui.Search)
	w.Canvas().Unfocus()
	menuItem(t, w, "Edit", "Paste").Action()
	if ui.Search.Text != "fly" {
		t.Fatalf("clicked Paste put %q in the search box", ui.Search.Text)
	}

	// The list had focus last, not a text field.
	ui.Search.SetText("")
	clickNote(t, ui)
	w.Canvas().Unfocus()
	menuItem(t, w, "Edit", "Paste").Action()
	if ui.Search.Text != "" || ui.Editor.Text != "# Ideas\nfly" {
		t.Fatalf("Paste after clicking the list changed a text field: search %q, editor %q", ui.Search.Text, ui.Editor.Text)
	}
}

func TestEditMenuLeavesTheFieldBehindADialogAlone(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	fyne.CurrentApp().Clipboard().SetContent("pasted")

	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	w.Canvas().Focus(ui.Editor)
	test.Tap(ui.Delete) // the dialog takes the focus
	menuItem(t, w, "Edit", "Paste").Action()
	if ui.Editor.Text != "# Ideas\nfly" {
		t.Fatalf("Paste reached the editor behind the dialog: %q", ui.Editor.Text)
	}
}

func TestViewNoteShortcutTakesFocusFromTheEditor(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	fyne.CurrentApp().Clipboard().SetContent("pasted")

	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	w.Canvas().Focus(ui.Editor)
	menuItem(t, w, "View", "View note").Action()
	if w.Canvas().Focused() == ui.Editor {
		t.Fatal("the hidden editor still has focus")
	}
	menuItem(t, w, "Edit", "Paste").Action()
	if ui.Editor.Text != "# Ideas\nfly" {
		t.Fatalf("Paste changed the hidden editor: %q", ui.Editor.Text)
	}
}

func TestFindFocusesSearchKeepingTheDraft(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)

	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nunsaved")

	menuItem(t, w, "Edit", "Find").Action()
	if w.Canvas().Focused() != ui.Search {
		t.Fatal("Find should focus the search box")
	}
	if w.Canvas().Overlays().Top() != nil || !ui.Editor.Visible() || ui.Editor.Text != "# Ideas\nunsaved" {
		t.Fatal("Find should leave the draft open without asking")
	}
}

func dialogTexts(t *testing.T, w fyne.Window) []string {
	t.Helper()
	top := w.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("no dialog is showing")
	}
	var out []string
	for _, o := range test.LaidOutObjects(top) {
		if l, ok := o.(*widget.Label); ok && l.Visible() && l.Text != "" {
			out = append(out, l.Text)
		}
	}
	return out
}

type appWithVersion struct {
	fyne.App
	version string
}

func (a *appWithVersion) Metadata() fyne.AppMetadata {
	return fyne.AppMetadata{Version: a.version}
}

func TestAboutDialogShowsAppAndServerDetails(t *testing.T) {
	c := newServer(t)
	_, w := open(t, c)

	origVersion := gui.Version
	origApp := fyne.CurrentApp()
	t.Cleanup(func() {
		gui.Version = origVersion
		fyne.SetCurrentApp(origApp)
	})

	checkDialog := func(wantTitle string) {
		t.Helper()
		menuItem(t, w, "Help", "About Scribble").Action()
		texts := dialogTexts(t, w)
		for _, want := range []string{
			wantTitle,
			"Tomboy-inspired Markdown notes",
			"Johan Sundell · MPL-2.0",
			c.BaseURL,
		} {
			if !slices.Contains(texts, want) {
				t.Errorf("About dialog labels %q do not contain %q", texts, want)
			}
		}

		const repo = "https://github.com/johansundell/scribble"
		var link *widget.Hyperlink
		for _, o := range test.LaidOutObjects(w.Canvas().Overlays().Top()) {
			if h, ok := o.(*widget.Hyperlink); ok && h.Visible() {
				link = h
				break
			}
		}
		if link == nil || link.Text != repo || link.URL == nil || link.URL.String() != repo {
			t.Fatalf("About dialog hyperlink = %+v, want %s", link, repo)
		}
		tapDialogButton(t, w, "Close")
		if w.Canvas().Overlays().Top() != nil {
			t.Fatal("Close did not dismiss the About dialog")
		}
	}

	// Default gui.Version ("dev") with no app metadata.
	gui.Version = "dev"
	checkDialog("Scribble dev")

	// Fallback to fyne.CurrentApp().Metadata().Version when gui.Version is "dev" or empty.
	fyne.SetCurrentApp(&appWithVersion{App: origApp, version: "0.0.8"})
	checkDialog("Scribble v0.0.8")
	gui.Version = ""
	checkDialog("Scribble v0.0.8")

	// Explicit gui.Version wins.
	gui.Version = "v1.2.3"
	checkDialog("Scribble v1.2.3")
}

