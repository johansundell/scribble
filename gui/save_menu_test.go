package gui_test

import (
	"context"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
	"github.com/johansundell/scribble/gui"
)

// moveCursor puts the editor's cursor at row, col, like a click would.
func moveCursor(ui *gui.UI, row, col int) {
	ui.Editor.CursorRow, ui.Editor.CursorColumn = row, col
	ui.Editor.Refresh()
}

// assertStillEditing fails unless the editor shows text with the cursor at
// row, col.
func assertStillEditing(t *testing.T, ui *gui.UI, text string, row, col int) {
	t.Helper()
	if !ui.Editor.Visible() || ui.Viewer.Visible() {
		t.Fatal("File > Save should stay in edit mode")
	}
	if ui.Editor.Text != text {
		t.Fatalf("editor holds %q, want %q", ui.Editor.Text, text)
	}
	if ui.Editor.CursorRow != row || ui.Editor.CursorColumn != col {
		t.Fatalf("cursor at %d:%d, want %d:%d", ui.Editor.CursorRow, ui.Editor.CursorColumn, row, col)
	}
}

func TestMenuSaveSavesAndStaysInTheEditor(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	ui.List.Select(0)
	save := menuItem(t, w, "File", "Save")

	// Focus in the editor.
	test.Tap(ui.EditToggle)
	w.Canvas().Focus(ui.Editor)
	ui.Editor.SetText("# Ideas\nfly high")
	moveCursor(ui, 1, 3)
	save.Action()
	if got := byTitle(t, c, "Ideas"); got.Body != "# Ideas\nfly high" || got.Revision != 2 {
		t.Fatalf("server holds %+v, status %q", got, ui.Status.Text)
	}
	assertStillEditing(t, ui, "# Ideas\nfly high", 1, 3)
	if w.Canvas().Focused() != ui.Editor {
		t.Fatal("the editor should keep the focus")
	}
	if !ui.Save.Disabled() || !save.Disabled {
		t.Fatal("Save should grey out once saved")
	}

	// Focus in the search box: the next save goes on from the saved revision.
	ui.Editor.SetText("# Ideas\nfly higher")
	moveCursor(ui, 1, 5)
	w.Canvas().Focus(ui.Search)
	save.Action()
	if got := byTitle(t, c, "Ideas").Body; got != "# Ideas\nfly higher" {
		t.Fatalf("server holds %q, status %q", got, ui.Status.Text)
	}
	assertStillEditing(t, ui, "# Ideas\nfly higher", 1, 5)
	if w.Canvas().Focused() != ui.Search {
		t.Fatal("the search box should keep the focus")
	}

	// Nothing focused, previewing the edit in view mode.
	ui.Editor.SetText("# Ideas\nfly highest")
	moveCursor(ui, 1, 7)
	test.Tap(ui.EditToggle)
	w.Canvas().Unfocus()
	save.Action()
	if got := byTitle(t, c, "Ideas").Body; got != "# Ideas\nfly highest" {
		t.Fatalf("server holds %q, status %q", got, ui.Status.Text)
	}
	assertStillEditing(t, ui, "# Ideas\nfly highest", 1, 7)
	if ui.Status.Text != "" {
		t.Errorf("status says %q", ui.Status.Text)
	}
}

func TestMenuSaveDoesNothingWithoutChangesOrUnderADialog(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	ui.List.Select(0)
	save := menuItem(t, w, "File", "Save")

	save.Action() // a menu shortcut fires even when its item is greyed out
	if got := byTitle(t, c, "Ideas").Revision; got != 1 || ui.Editor.Visible() {
		t.Fatalf("File > Save acted with nothing to save (revision %d)", got)
	}

	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nunsaved")
	test.Tap(ui.Delete) // the dialog stays open
	save.Action()
	if got := byTitle(t, c, "Ideas").Body; got != "# Ideas\nfly" {
		t.Fatalf("File > Save saved behind a dialog: server holds %q", got)
	}
	if len(w.Canvas().Overlays().List()) != 1 {
		t.Fatal("File > Save changed the dialogs while one was open")
	}
}

func TestMenuSaveOfStaleEditAsksAndKeepsEditing(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nmine")
	theirs := byTitle(t, c, "Ideas")
	if _, err := c.Update(context.Background(), theirs.ID, "# Ideas\ntheirs", theirs.Revision); err != nil {
		t.Fatal(err)
	}
	test.Tap(ui.EditToggle) // previewing

	menuItem(t, w, "File", "Save").Action()
	if byTitle(t, c, "Ideas").Body != "# Ideas\ntheirs" {
		t.Fatal("the stale edit was saved over theirs")
	}
	tapDialogButton(t, w, "Keep editing")
	if !ui.Editor.Visible() || ui.Editor.Text != "# Ideas\nmine" {
		t.Fatalf("editor visible %v with %q, want the draft", ui.Editor.Visible(), ui.Editor.Text)
	}

	menuItem(t, w, "File", "Save").Action()
	tapDialogButton(t, w, "Overwrite")
	if got := byTitle(t, c, "Ideas").Body; got != "# Ideas\nmine" {
		t.Fatalf("server holds %q", got)
	}
	if ui.Editor.Visible() {
		t.Fatal("Overwrite should show the result in view mode, as from the button")
	}
}

func TestMenuSaveOfNewNoteStaysInTheEditor(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Plans\n#scribble")
	ui, w := open(t, c)
	ui.Project.SetSelected("scribble")
	save := menuItem(t, w, "File", "Save")

	test.Tap(ui.NewNote)
	save.Action()
	if !strings.Contains(ui.Status.Text, "title") {
		t.Fatalf("an untitled draft should be refused, status %q", ui.Status.Text)
	}
	assertStillEditing(t, ui, "\n\n#scribble\n", 0, 0)

	test.Type(ui.Editor, "Roadmap")
	save.Action()
	if got := byTitle(t, c, "Roadmap").Body; got != "Roadmap\n\n#scribble\n" {
		t.Fatalf("server holds %q, status %q", got, ui.Status.Text)
	}
	assertStillEditing(t, ui, "Roadmap\n\n#scribble\n", 0, 7)
	assertTitles(t, ui, "Roadmap", "Plans")
	if ui.Delete.Disabled() || !ui.Save.Disabled() {
		t.Fatal("the editor should be on the saved note")
	}

	// The next save updates that note rather than creating another.
	test.Type(ui.Editor, " v2")
	save.Action()
	assertTitles(t, ui, "Roadmap v2", "Plans")
}
