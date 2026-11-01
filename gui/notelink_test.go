package gui_test

import (
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/gui"
)

// linking opens note "Ideas" in edit mode with text in the editor, next to
// notes Groceries and Recipes, the cursor at the end of the text.
func linking(t *testing.T, text string) (*gui.UI, fyne.Window) {
	t.Helper()
	return editingWith(t, text, "# Groceries\nmilk", "# Recipes\nsoup")
}

// textsOf is the texts o draws.
func textsOf(o fyne.CanvasObject) []string {
	var out []string
	for _, txt := range drawnTexts(o) {
		out = append(out, txt.Text.Text)
	}
	return out
}

// pickLink opens the note picker from the editor's right-click menu.
func pickLink(t *testing.T, ui *gui.UI, w fyne.Window) {
	t.Helper()
	test.TapSecondaryAt(ui.Editor, fyne.NewPos(5, 5))
	tapMenuItem(t, w, "Link to note…")
	if w.Canvas().Overlays().Top() == nil {
		t.Fatal("Link to note… opened no dialog")
	}
}

// pickerChoices is the note titles the picker on top of w lists.
func pickerChoices(t *testing.T, w fyne.Window) []string {
	t.Helper()
	for _, o := range test.LaidOutObjects(w.Canvas().Overlays().Top()) {
		if l, ok := o.(*widget.List); ok {
			out := slices.DeleteFunc(textsOf(l), func(s string) bool { return s == "" })
			// The list keeps rows it no longer needs until it is laid out.
			return out[:min(len(out), l.Length())]
		}
	}
	t.Fatal("the dialog has no list")
	return nil
}

// pickerQuery is the picker's search field, which has the focus.
func pickerQuery(t *testing.T, w fyne.Window) fyne.Focusable {
	t.Helper()
	f := w.Canvas().Focused()
	if _, ok := f.(interface{ SelectedText() string }); !ok {
		t.Fatalf("the picker's search field has no focus, %T has", f)
	}
	return f
}

func TestEditorRightClickMenuHasLinkToNote(t *testing.T) {
	ui, w := linking(t, "fly")
	test.TapSecondaryAt(ui.Editor, fyne.NewPos(5, 5))
	got := textsOf(w.Canvas().Overlays().Top())
	for _, want := range []string{"Undo", "Redo", "Cut", "Copy", "Paste", "Select all", "Link to note…"} {
		if !slices.Contains(got, want) {
			t.Errorf("the editor's right-click menu has no %q item: %q", want, got)
		}
	}

	w.Canvas().Overlays().Top().Hide()
	test.TapSecondaryAt(ui.Search, fyne.NewPos(5, 5))
	if got := textsOf(w.Canvas().Overlays().Top()); slices.Contains(got, "Link to note…") {
		t.Errorf("the search box's right-click menu offers Link to note…: %q", got)
	}
}

func TestLinkToNoteInsertsTheChosenLinkAsOneUndo(t *testing.T) {
	ui, w := linking(t, "# Ideas\nbuy ")
	pickLink(t, ui, w)
	if got := pickerChoices(t, w); !slices.Equal(got, []string{"Recipes", "Groceries"}) {
		t.Fatalf("the picker lists %q, want the other notes", got)
	}

	test.Type(pickerQuery(t, w), "groc")
	if got := pickerChoices(t, w); !slices.Equal(got, []string{"Groceries"}) {
		t.Fatalf("typing groc left %q in the picker", got)
	}
	pickerQuery(t, w).TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})

	if w.Canvas().Overlays().Top() != nil {
		t.Fatal("Enter should close the picker")
	}
	if ui.Editor.Text != "# Ideas\nbuy [[Groceries]]" {
		t.Fatalf("editor holds %q", ui.Editor.Text)
	}
	if w.Canvas().Focused() != ui.Editor {
		t.Fatal("the editor should have the focus back")
	}
	test.Type(ui.Editor, "!")
	if ui.Editor.Text != "# Ideas\nbuy [[Groceries]]!" {
		t.Fatalf("typing after the link left %q", ui.Editor.Text)
	}

	undo := menuItem(t, w, "Edit", "Undo").Action
	undo()
	if ui.Editor.Text != "# Ideas\nbuy [[Groceries]]" {
		t.Fatalf("Undo left %q, want the link without the !", ui.Editor.Text)
	}
	undo()
	if ui.Editor.Text != "# Ideas\nbuy " {
		t.Fatalf("Undo left %q, want the link gone", ui.Editor.Text)
	}
}

func TestLinkToNoteReplacesTheSelectionItSearchesFor(t *testing.T) {
	ui, w := linking(t, "# Ideas\nsee recipes now")
	moveCursor(ui, 1, len("see recipes"))
	selectBack(ui.Editor, len("recipes"))
	pickLink(t, ui, w)

	if got := pickerChoices(t, w); !slices.Equal(got, []string{"Recipes"}) {
		t.Fatalf("the picker lists %q, want it searching for the selection", got)
	}
	dialogButton(t, w, "Link").OnTapped()
	if ui.Editor.Text != "# Ideas\nsee [[Recipes]] now" {
		t.Fatalf("editor holds %q", ui.Editor.Text)
	}
	ui.Editor.TypedShortcut(&fyne.ShortcutUndo{})
	if ui.Editor.Text != "# Ideas\nsee recipes now" {
		t.Fatalf("Ctrl+Z left %q", ui.Editor.Text)
	}
}

func TestLinkToNoteByDoubleClick(t *testing.T) {
	ui, w := linking(t, "# Ideas\n")
	pickLink(t, ui, w)
	var row fyne.DoubleTappable
	for _, o := range test.LaidOutObjects(w.Canvas().Overlays().Top()) {
		if d, ok := o.(fyne.DoubleTappable); ok && slices.Contains(textsOf(o), "Recipes") {
			row = d
		}
	}
	if row == nil {
		t.Fatal("the picker has no Recipes row to double-click")
	}
	row.DoubleTapped(&fyne.PointEvent{})
	if ui.Editor.Text != "# Ideas\n[[Recipes]]" {
		t.Fatalf("double-clicking Recipes left %q", ui.Editor.Text)
	}
}

func TestClickingAPickerRowKeepsEnterAndEsc(t *testing.T) {
	ui, w := linking(t, "# Ideas\n")
	pickLink(t, ui, w)
	tapMenuItem(t, w, "Groceries") // a click on the row, through the window
	pickerQuery(t, w).TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if ui.Editor.Text != "# Ideas\n[[Groceries]]" {
		t.Fatalf("Enter after clicking Groceries left %q", ui.Editor.Text)
	}
}

func TestLinkToNoteLeavesOutTitlesALinkCannotHold(t *testing.T) {
	ui, w := editingWith(t, "", "# Recipes\nsoup", "# Odd [1]\nx")
	pickLink(t, ui, w)
	if got := pickerChoices(t, w); !slices.Equal(got, []string{"Recipes"}) {
		t.Fatalf("the picker lists %q, want no title with brackets", got)
	}
}

func TestCancellingLinkToNoteChangesNothing(t *testing.T) {
	for _, how := range []string{"Cancel", "Esc"} {
		t.Run(how, func(t *testing.T) {
			ui, w := linking(t, "# Ideas\nsee recipes")
			selectBack(ui.Editor, len("recipes"))
			pickLink(t, ui, w)
			if how == "Cancel" {
				dialogButton(t, w, "Cancel").OnTapped()
			} else {
				pickerQuery(t, w).TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
			}
			if w.Canvas().Overlays().Top() != nil {
				t.Fatalf("%s should close the picker", how)
			}
			if ui.Editor.Text != "# Ideas\nsee recipes" || ui.Editor.SelectedText() != "recipes" {
				t.Fatalf("editor holds %q with %q selected", ui.Editor.Text, ui.Editor.SelectedText())
			}
			if w.Canvas().Focused() != ui.Editor {
				t.Fatal("the editor should have the focus back")
			}
		})
	}
}
