package gui_test

import (
	"strings"
	"testing"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/gui"
)

// openNote shows the GUI with one note, body, open in view mode.
func openNote(t *testing.T, body string) (*gui.UI, fyne.Window) {
	t.Helper()
	c := newServer(t)
	create(t, c, body)
	ui, w := open(t, c)
	ui.List.Select(0)
	return ui, w
}

// drawnText is a text an object draws, and where, relative to the object.
type drawnText struct {
	*canvas.Text
	pos fyne.Position
}

// drawnTexts returns the texts o draws, looking inside its widgets. The test
// driver's AbsolutePositionForObject misses some of them.
func drawnTexts(o fyne.CanvasObject) []drawnText {
	var out []drawnText
	var walk func(o fyne.CanvasObject, pos fyne.Position)
	walk = func(o fyne.CanvasObject, pos fyne.Position) {
		if !o.Visible() {
			return
		}
		switch o := o.(type) {
		case *canvas.Text:
			out = append(out, drawnText{o, pos})
		case *fyne.Container:
			for _, child := range o.Objects {
				walk(child, pos.Add(child.Position()))
			}
		case fyne.Widget:
			for _, child := range test.WidgetRenderer(o).Objects() {
				walk(child, pos.Add(child.Position()))
			}
		}
	}
	walk(o, fyne.Position{})
	return out
}

// textAt returns where the rendered text want starts in the viewer, in the
// viewer's coordinates, and the height of its line.
func textAt(t *testing.T, ui *gui.UI, want string) (fyne.Position, float32) {
	t.Helper()
	for _, txt := range drawnTexts(ui.Viewer) {
		if i := strings.Index(txt.Text.Text, want); i >= 0 {
			return txt.pos.AddXY(fyne.MeasureText(txt.Text.Text[:i], txt.TextSize, txt.TextStyle).Width, 0), txt.Size().Height
		}
	}
	t.Fatalf("the viewer shows no %q", want)
	return fyne.Position{}, 0
}

// width is the rendered width of s in the viewer's body text.
func width(s string) float32 {
	return fyne.MeasureText(s, theme.TextSize(), fyne.TextStyle{}).Width
}

func pointIn(t *testing.T, ui *gui.UI, text string) fyne.Position {
	t.Helper()
	p, h := textAt(t, ui, text)
	return p.AddXY(width(text)/2, h/2)
}

// drag drags the mouse across the viewer from the start of the rendered
// text from to the end of the rendered text to, as the desktop driver
// reports it: where the mouse is now and how far it moved since the last
// event. The test driver's Drag gives where the drag started instead.
func drag(t *testing.T, ui *gui.UI, from, to string) {
	t.Helper()
	start, h := textAt(t, ui, from)
	end, _ := textAt(t, ui, to)
	start = start.AddXY(1, h/2)
	end = end.AddXY(width(to)-1, h/2)
	half := fyne.NewPos((start.X+end.X)/2, (start.Y+end.Y)/2)
	ui.Viewer.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: half}, Dragged: fyne.NewDelta(half.X-start.X, half.Y-start.Y)})
	ui.Viewer.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: end}, Dragged: fyne.NewDelta(end.X-half.X, end.Y-half.Y)})
	ui.Viewer.DragEnd()
}

func click(ui *gui.UI, p fyne.Position) {
	ui.Viewer.MouseDown(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: p}, Button: desktop.MouseButtonPrimary})
}

func TestDraggingSelectsViewerText(t *testing.T) {
	ui, _ := openNote(t, "# Ideas\nfly to the moon")

	drag(t, ui, "to", "the")
	if got := ui.Viewer.SelectedText(); got != "to the" {
		t.Fatalf("selected %q, want %q", got, "to the")
	}
	highlighted := false
	sel := theme.Color(theme.ColorNameSelection)
	for _, o := range test.LaidOutObjects(ui.Viewer) {
		if r, ok := o.(*canvas.Rectangle); ok && r.Visible() && r.FillColor == sel && r.Size().Width > 0 {
			highlighted = true
		}
	}
	if !highlighted {
		t.Fatal("the selection is not highlighted")
	}

	click(ui, pointIn(t, ui, "fly"))
	if got := ui.Viewer.SelectedText(); got != "" {
		t.Fatalf("a click should drop the selection, still selected %q", got)
	}
}

func TestDraggingFromALinkSelectsInsteadOfFollowing(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Recipes\nsoup")
	create(t, c, "# Plans\nsee [[Recipes]] for dinner")
	ui, _ := open(t, c)
	ui.List.Select(0)

	// A link is not draggable, so the drag goes to the viewer under it.
	if _, ok := any(&widget.Hyperlink{}).(fyne.Draggable); ok {
		t.Fatal("links are draggable now, so a drag from one no longer reaches the viewer")
	}
	drag(t, ui, "Recipes", "for")
	if got := ui.Viewer.SelectedText(); got != "Recipes for" {
		t.Fatalf("selected %q, want %q", got, "Recipes for")
	}
	if strings.HasPrefix(ui.Editor.Text, "# Recipes") {
		t.Fatal("dragging across a link should not follow it")
	}
}

func TestDoubleClickSelectsWordAndTripleClickSelectsLine(t *testing.T) {
	ui, _ := openNote(t, "# Ideas\nfly to the moon\n\nnext line")

	p := pointIn(t, ui, "moon")
	ui.Viewer.DoubleTapped(&fyne.PointEvent{Position: p})
	if got := ui.Viewer.SelectedText(); got != "moon" {
		t.Fatalf("double-click selected %q, want %q", got, "moon")
	}
	click(ui, p)
	if got := ui.Viewer.SelectedText(); got != "fly to the moon" {
		t.Fatalf("triple-click selected %q, want %q", got, "fly to the moon")
	}

	// A click elsewhere right after a double-click is just a click.
	ui.Viewer.DoubleTapped(&fyne.PointEvent{Position: p})
	click(ui, pointIn(t, ui, "next"))
	if got := ui.Viewer.SelectedText(); got != "" {
		t.Fatalf("a click away from a double-click selected %q", got)
	}
}

func TestCopyingViewerSelection(t *testing.T) {
	ui, w := openNote(t, "# Ideas\nfly to the moon")
	clip := fyne.CurrentApp().Clipboard()

	ui.Viewer.DoubleTapped(&fyne.PointEvent{Position: pointIn(t, ui, "moon")})
	ui.Viewer.TypedShortcut(&fyne.ShortcutCopy{Clipboard: clip})
	if got := clip.Content(); got != "moon" {
		t.Fatalf("Ctrl+C copied %q, want %q", got, "moon")
	}

	ui.Viewer.DoubleTapped(&fyne.PointEvent{Position: pointIn(t, ui, "fly")})
	test.TapSecondaryAt(ui.Viewer, pointIn(t, ui, "fly"))
	tapMenuItem(t, w, "Copy")
	if got := clip.Content(); got != "fly" {
		t.Fatalf("right-click > Copy copied %q, want %q", got, "fly")
	}
	if w.Canvas().Overlays().Top() != nil {
		t.Fatal("the context menu should close after Copy")
	}
	if got := ui.Viewer.SelectedText(); got != "fly" {
		t.Fatalf("right-click dropped the selection, now %q", got)
	}
}

// tapMenuItem taps the item labelled label in the pop-up menu on top of w.
func tapMenuItem(t *testing.T, w fyne.Window, label string) {
	t.Helper()
	top := w.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("no pop-up menu is open")
	}
	for _, txt := range drawnTexts(top) {
		if txt.Text.Text == label {
			test.TapCanvas(w.Canvas(), top.Position().Add(txt.pos).AddXY(1, 1))
			return
		}
	}
	t.Fatalf("the pop-up menu has no %q item", label)
}

func TestEditMenuCopyAndSelectAllReachTheViewer(t *testing.T) {
	ui, w := openNote(t, "# Ideas\nfly to the moon")
	clip := fyne.CurrentApp().Clipboard()
	clip.SetContent("")

	ui.Viewer.TypedShortcut(&fyne.ShortcutSelectAll{})
	if got := ui.Viewer.SelectedText(); got != "Ideas\nfly to the moon" {
		t.Fatalf("Ctrl+A selected %q", got)
	}

	ui.Viewer.DoubleTapped(&fyne.PointEvent{Position: pointIn(t, ui, "moon")})
	w.Canvas().Focus(ui.Viewer)
	menuItem(t, w, "Edit", "Select all").Action()
	menuItem(t, w, "Edit", "Copy").Action()
	if got := clip.Content(); got != "Ideas\nfly to the moon" {
		t.Fatalf("Edit > Select all, Copy copied %q", got)
	}

	// Clicking the menu bar takes the focus away; the items still reach
	// the viewer that had it.
	ui.Viewer.DoubleTapped(&fyne.PointEvent{Position: pointIn(t, ui, "fly")})
	w.Canvas().Unfocus()
	menuItem(t, w, "Edit", "Copy").Action()
	if got := clip.Content(); got != "fly" {
		t.Fatalf("Edit > Copy after a menu click copied %q, want %q", got, "fly")
	}

	// The other Edit items leave the viewer alone.
	w.Canvas().Focus(ui.Viewer)
	clip.SetContent("pasted")
	for _, label := range []string{"Undo", "Redo", "Cut", "Paste"} {
		menuItem(t, w, "Edit", label).Action()
	}
	if got := ui.Viewer.SelectedText(); got != "fly" {
		t.Fatalf("the selection changed to %q", got)
	}
	if clip.Content() != "pasted" || ui.Editor.Text != "# Ideas\nfly to the moon" {
		t.Fatalf("an Edit item changed the note or clipboard: %q, %q", ui.Editor.Text, clip.Content())
	}
}

func TestF2RenamesWithViewerFocused(t *testing.T) {
	ui, w := openNote(t, "# Ideas\nfly")
	w.Canvas().Focus(ui.Viewer)
	ui.Viewer.TypedKey(&fyne.KeyEvent{Name: fyne.KeyF2})
	if w.Canvas().Overlays().Top() == nil {
		t.Fatal("F2 with the viewer focused should open the rename dialog")
	}
}

func TestClickingLinksInViewerStillWorks(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Recipes\nsoup")
	create(t, c, "# Plans\nsee [[Recipes]] for #home")
	ui, w := open(t, c)
	ui.List.Select(0)
	abs := fyne.CurrentApp().Driver().AbsolutePositionForObject(ui.Viewer)

	test.TapCanvas(w.Canvas(), abs.Add(pointIn(t, ui, "#home")))
	if ui.Project.Selected != "home" {
		t.Fatalf("clicking #home selected project %q", ui.Project.Selected)
	}
	test.TapCanvas(w.Canvas(), abs.Add(pointIn(t, ui, "Recipes")))
	if !strings.HasPrefix(ui.Editor.Text, "# Recipes") {
		t.Fatalf("clicking [[Recipes]] opened %q", ui.Editor.Text)
	}
}

func TestSelectAllCoversRichContent(t *testing.T) {
	ui, _ := openNote(t, "# Plan\n\nuse `go` now\n\n- one\n- [x] done task\n\n| col | val |\n| --- | --- |\n| a | b |\n\n```\ncode one\ncode two\n```\n\nend")

	ui.Viewer.TypedShortcut(&fyne.ShortcutSelectAll{})
	got := ui.Viewer.SelectedText()
	for _, want := range []string{"Plan\n", "use go now", "one\n", "done task\n", "col\tval\na\tb\n", "code one\ncode two\n", "end"} {
		if !strings.Contains(got, want) {
			t.Errorf("Select all gave %q, missing %q", got, want)
		}
	}
}

// Every word the viewer draws can be picked out with a double-click: the
// viewer finds text where it is drawn, in links, task items, tables, code
// blocks and wrapped lines too.
func TestDoubleClickFindsEveryRenderedWord(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Target\nx")
	create(t, c, "# Plan\n\nuse `inline` and [[Target]] or #proj\n\n- bullet item\n- [ ] task item\n\n| head | other |\n| --- | --- |\n| cell | more |\n\n```\nblock code\nsecond line\n```\n\na long paragraph that should wrap around because the viewer is not very wide at all, no it is not, honestly it is quite narrow, narrower than you would think")
	ui, _ := open(t, c)
	ui.List.Select(0)

	checked := 0
	for _, txt := range drawnTexts(ui.Viewer) {
		text := txt.Text.Text
		fields := strings.Fields(text)
		if len(fields) == 0 || !unicode.IsLetter([]rune(fields[0])[0]) {
			continue
		}
		word := strings.TrimFunc(fields[0], func(r rune) bool { return !unicode.IsLetter(r) })
		lead := text[:strings.Index(text, word)]
		x := txt.pos.X + fyne.MeasureText(lead, txt.TextSize, txt.TextStyle).Width + fyne.MeasureText(word, txt.TextSize, txt.TextStyle).Width/2
		ui.Viewer.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(x, txt.pos.Y+txt.Size().Height/2)})
		if got := ui.Viewer.SelectedText(); got != word {
			t.Errorf("double-clicking %q in %q selected %q", word, text, got)
		}
		checked++
	}
	if checked < 14 {
		t.Fatalf("checked only %d words", checked)
	}
}

func TestViewerSelectionClearsOnAnotherNote(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Other\ntext")
	create(t, c, "# Ideas\nfly")
	ui, _ := open(t, c)
	ui.List.Select(0)
	ui.Viewer.TypedShortcut(&fyne.ShortcutSelectAll{})
	ui.List.Select(1)
	if got := ui.Viewer.SelectedText(); got != "" {
		t.Fatalf("selection carried over to another note: %q", got)
	}
}

var _ fyne.Focusable = (*gui.Viewer)(nil)
var _ desktop.Cursorable = (*gui.Viewer)(nil)
