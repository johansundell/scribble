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
	c := newServer(t)
	create(t, c, "# Groceries\nmilk")
	create(t, c, "# Recipes\nsoup")
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText(text)
	w.Canvas().Focus(ui.Editor)
	ui.Editor.TypedKey(&fyne.KeyEvent{Name: fyne.KeyPageDown})
	return ui, w
}

// menuLabels is the item labels of the pop-up menu on top of w.
func menuLabels(t *testing.T, w fyne.Window) []string {
	t.Helper()
	top := w.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("no pop-up menu is open")
	}
	var out []string
	for _, txt := range drawnTexts(top) {
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
	var out []string
	for _, o := range test.LaidOutObjects(w.Canvas().Overlays().Top()) {
		if l, ok := o.(*widget.List); ok {
			for _, txt := range drawnTexts(l) {
				if txt.Text.Text != "" {
					out = append(out, txt.Text.Text)
				}
			}
			// The list keeps rows it no longer needs until it is laid out.
			return out[:min(len(out), l.Length())]
		}
	}
	return out
}

// pickerQuery is the picker's search field, which has the focus.
func pickerQuery(t *testing.T, w fyne.Window) interface {
	fyne.Focusable
	SelectedText() string
} {
	t.Helper()
	f, ok := w.Canvas().Focused().(interface {
		fyne.Focusable
		SelectedText() string
	})
	if !ok {
		t.Fatalf("the picker's search field has no focus, %T has", w.Canvas().Focused())
	}
	return f
}

func TestEditorRightClickMenuHasLinkToNote(t *testing.T) {
	ui, w := linking(t, "fly")
	test.TapSecondaryAt(ui.Editor, fyne.NewPos(5, 5))
	got := menuLabels(t, w)
	for _, want := range []string{"Undo", "Redo", "Cut", "Copy", "Paste", "Select all", "Link to note…"} {
		if !slices.Contains(got, want) {
			t.Errorf("the editor's right-click menu has no %q item: %q", want, got)
		}
	}

	w.Canvas().Overlays().Top().Hide()
	test.TapSecondaryAt(ui.Search, fyne.NewPos(5, 5))
	if got := menuLabels(t, w); slices.Contains(got, "Link to note…") {
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
	ui.Editor.TypedKey(&fyne.KeyEvent{Name: fyne.KeyLeft})
	ui.Editor.TypedKey(&fyne.KeyEvent{Name: fyne.KeyLeft})
	ui.Editor.TypedKey(&fyne.KeyEvent{Name: fyne.KeyLeft})
	ui.Editor.TypedKey(&fyne.KeyEvent{Name: fyne.KeyLeft})
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
	for _, o := range test.LaidOutObjects(w.Canvas().Overlays().Top()) {
		if d, ok := o.(fyne.DoubleTappable); ok {
			if l, ok := o.(interface{ fyne.Widget }); ok && slices.Contains(textsOf(l), "Recipes") {
				d.DoubleTapped(&fyne.PointEvent{})
				break
			}
		}
	}
	if ui.Editor.Text != "# Ideas\n[[Recipes]]" {
		t.Fatalf("double-clicking Recipes left %q", ui.Editor.Text)
	}
}

func textsOf(o fyne.CanvasObject) []string {
	var out []string
	for _, txt := range drawnTexts(o) {
		out = append(out, txt.Text.Text)
	}
	return out
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
