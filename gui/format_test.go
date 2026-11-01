package gui_test

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/gui"
)

// editing opens a note in edit mode with text in the editor, the cursor at
// its end and the focus on the editor.
func editing(t *testing.T, text string) (*gui.UI, fyne.Window) {
	t.Helper()
	return editingWith(t, text)
}

// editingWith is editing, with notes others saved before the open one.
func editingWith(t *testing.T, text string, others ...string) (*gui.UI, fyne.Window) {
	t.Helper()
	c := newServer(t)
	for _, body := range others {
		create(t, c, body)
	}
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText(text)
	w.Canvas().Focus(ui.Editor)
	ui.Editor.TypedKey(&fyne.KeyEvent{Name: fyne.KeyPageDown})
	return ui, w
}

var shiftKey = &fyne.KeyEvent{Name: desktop.KeyShiftLeft}

// selectBack selects the n runes before the cursor, as Shift+Left does.
func selectBack(f *gui.TextField, n int) {
	f.KeyDown(shiftKey)
	for range n {
		f.TypedKey(&fyne.KeyEvent{Name: fyne.KeyLeft})
	}
	f.KeyUp(shiftKey)
}

// formatButton returns the toolbar button with the Octicon named icon.
func formatButton(t *testing.T, ui *gui.UI, icon string) *widget.ToolbarAction {
	t.Helper()
	for _, item := range ui.Format.Items {
		if a, ok := item.(*widget.ToolbarAction); ok && strings.HasSuffix(a.Icon.Name(), icon+".svg") {
			return a
		}
	}
	t.Fatalf("the toolbar has no %s button", icon)
	return nil
}

func TestFormatBarShowsTheNineActionsInEditModeOnly(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, _ := open(t, c)
	ui.List.Select(0)

	if ui.Format.Visible() {
		t.Fatal("the formatting toolbar shows in view mode")
	}
	test.Tap(ui.EditToggle)
	if !ui.Format.Visible() {
		t.Fatal("the formatting toolbar is hidden in edit mode")
	}

	var got []string
	for _, item := range ui.Format.Items {
		switch item := item.(type) {
		case *widget.ToolbarSeparator:
			got = append(got, "|")
		case *widget.ToolbarAction:
			// Themed icons follow the light and dark theme.
			if _, ok := item.Icon.(*theme.ThemedResource); !ok {
				t.Errorf("icon %s is not themed", item.Icon.Name())
			}
			got = append(got, strings.TrimSuffix(item.Icon.Name()[strings.LastIndex(item.Icon.Name(), "_")+1:], ".svg"))
		}
	}
	want := "heading bold italic | quote code link | list-unordered list-ordered tasklist"
	if strings.Join(got, " ") != want {
		t.Fatalf("toolbar is %q, want %q", strings.Join(got, " "), want)
	}

	test.Tap(ui.EditToggle)
	if ui.Format.Visible() {
		t.Fatal("the formatting toolbar stays after going back to view mode")
	}
}

func TestFormatButtonsChangeTheEditor(t *testing.T) {
	for _, c := range []struct {
		icon, text string
		selected   int // runes selected before the cursor, at the end
		want       string
		wantSel    string
	}{
		{"bold", "say hi", 2, "say **hi**", "hi"},
		{"bold", "say ", 0, "say ****", ""},
		{"italic", "say hi", 2, "say _hi_", "hi"},
		{"code", "run go", 2, "run `go`", "go"},
		{"link", "see docs", 4, "see [docs](url)", "url"},
		{"link", "see ", 0, "see [](url)", ""},
		{"heading", "# Ideas\nfly", 0, "# Ideas\n### fly", ""},
		{"quote", "a\nb", 3, "> a\n> b", ""},
		{"list-unordered", "a\nb", 3, "- a\n- b", ""},
		{"list-ordered", "a\nb\nc", 5, "1. a\n2. b\n3. c", ""},
		{"tasklist", "a", 0, "- [ ] a", ""},
	} {
		t.Run(c.icon+" "+c.text, func(t *testing.T) {
			ui, w := editing(t, c.text)
			selectBack(ui.Editor, c.selected)
			w.Canvas().Focus(ui.Search) // as a click on the button can
			formatButton(t, ui, c.icon).OnActivated()

			if ui.Editor.Text != c.want {
				t.Fatalf("editor holds %q, want %q", ui.Editor.Text, c.want)
			}
			if got := ui.Editor.SelectedText(); got != c.wantSel {
				t.Errorf("selected %q, want %q", got, c.wantSel)
			}
			if w.Canvas().Focused() != ui.Editor {
				t.Error("the button left the focus off the editor")
			}
		})
	}
}

func TestBoldWithNoSelectionPutsTheCursorBetween(t *testing.T) {
	ui, _ := editing(t, "say ")
	formatButton(t, ui, "bold").OnActivated()
	test.Type(ui.Editor, "hi")
	if ui.Editor.Text != "say **hi**" {
		t.Fatalf("editor holds %q", ui.Editor.Text)
	}
}

func TestLinkSelectsURLToTypeOver(t *testing.T) {
	ui, _ := editing(t, "see docs")
	selectBack(ui.Editor, 4)
	ui.Editor.TypedShortcut(ctrl(fyne.KeyK))
	test.Type(ui.Editor, "https://x.io")
	if ui.Editor.Text != "see [docs](https://x.io)" {
		t.Fatalf("editor holds %q", ui.Editor.Text)
	}
}

func TestFormatShortcuts(t *testing.T) {
	for _, c := range []struct {
		key  fyne.KeyName
		want string
	}{
		{fyne.KeyB, "say **hi**"},
		{fyne.KeyI, "say _hi_"},
		{fyne.KeyK, "say [hi](url)"},
	} {
		t.Run(string(c.key), func(t *testing.T) {
			ui, _ := editing(t, "say hi")
			selectBack(ui.Editor, 2)
			ui.Editor.TypedShortcut(ctrl(c.key))
			if ui.Editor.Text != c.want {
				t.Fatalf("Ctrl+%s left %q, want %q", c.key, ui.Editor.Text, c.want)
			}
		})
	}
}

func TestFormatShortcutsLeaveTheSearchBoxAlone(t *testing.T) {
	ui, w := editing(t, "x")
	w.Canvas().Focus(ui.Search)
	test.Type(ui.Search, "fly")
	for _, key := range []fyne.KeyName{fyne.KeyB, fyne.KeyI, fyne.KeyK} {
		ui.Search.TypedShortcut(ctrl(key))
	}
	if ui.Search.Text != "fly" {
		t.Fatalf("search box holds %q", ui.Search.Text)
	}
}

func TestOneUndoPerFormat(t *testing.T) {
	ui, w := editing(t, "a\nb\nc")
	undo := menuItem(t, w, "Edit", "Undo").Action
	redo := menuItem(t, w, "Edit", "Redo").Action

	menuItem(t, w, "Edit", "Select all").Action()
	formatButton(t, ui, "list-ordered").OnActivated()
	if ui.Editor.Text != "1. a\n2. b\n3. c" {
		t.Fatalf("editor holds %q", ui.Editor.Text)
	}
	selectBack(ui.Editor, 1)
	ui.Editor.TypedShortcut(ctrl(fyne.KeyB))
	if ui.Editor.Text != "1. a\n2. b\n3. **c**" {
		t.Fatalf("editor holds %q", ui.Editor.Text)
	}
	ui.Editor.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEnd})
	test.Type(ui.Editor, "!")

	for _, want := range []string{"1. a\n2. b\n3. **c**", "1. a\n2. b\n3. c", "a\nb\nc"} {
		undo()
		if ui.Editor.Text != want {
			t.Fatalf("Undo left %q, want %q", ui.Editor.Text, want)
		}
	}
	for _, want := range []string{"1. a\n2. b\n3. c", "1. a\n2. b\n3. **c**"} {
		redo()
		if ui.Editor.Text != want {
			t.Fatalf("Redo left %q, want %q", ui.Editor.Text, want)
		}
	}
}

func TestEnterContinuesLists(t *testing.T) {
	enter := &fyne.KeyEvent{Name: fyne.KeyReturn}
	ui, w := editing(t, "")
	test.Type(ui.Editor, "- [x] milk")
	ui.Editor.TypedKey(enter)
	test.Type(ui.Editor, "eggs")
	ui.Editor.TypedKey(enter)
	if ui.Editor.Text != "- [x] milk\n- [ ] eggs\n- [ ] " {
		t.Fatalf("editor holds %q", ui.Editor.Text)
	}
	ui.Editor.TypedKey(enter)
	if ui.Editor.Text != "- [x] milk\n- [ ] eggs\n" {
		t.Fatalf("Enter on an empty item left %q", ui.Editor.Text)
	}
	test.Type(ui.Editor, "1. one")
	ui.Editor.TypedKey(enter)
	test.Type(ui.Editor, "two")
	if !strings.HasSuffix(ui.Editor.Text, "\n1. one\n2. two") {
		t.Fatalf("editor holds %q", ui.Editor.Text)
	}

	// Shift+Enter is a plain newline.
	ui.Editor.KeyDown(shiftKey)
	ui.Editor.TypedKey(enter)
	ui.Editor.KeyUp(shiftKey)
	if !strings.HasSuffix(ui.Editor.Text, "\n2. two\n") {
		t.Fatalf("Shift+Enter left %q", ui.Editor.Text)
	}

	// One undo takes the ended item's marker back.
	ui.Editor.SetText("- a")
	ui.Editor.TypedKey(&fyne.KeyEvent{Name: fyne.KeyPageDown})
	ui.Editor.TypedKey(enter)
	ui.Editor.TypedKey(enter)
	menuItem(t, w, "Edit", "Undo").Action()
	if ui.Editor.Text != "- a\n- " {
		t.Fatalf("Undo left %q", ui.Editor.Text)
	}
}

func TestSmartPaste(t *testing.T) {
	for _, c := range []struct {
		name, text string
		selected   int
		clip, want string
	}{
		{"url over selection", "see docs", 4, "https://x.io", "see [docs](https://x.io)"},
		{"url with no selection", "see ", 0, "https://x.io", "see https://x.io"},
		{"cells", "", 0, "a\tb\n1\t2", "| a | b |\n| --- | --- |\n| 1 | 2 |"},
		{"plain over selection", "say hi", 2, "yo", "say yo"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ui, w := editing(t, c.text)
			selectBack(ui.Editor, c.selected)
			clip := w.Clipboard()
			clip.SetContent(c.clip)
			ui.Editor.TypedShortcut(&fyne.ShortcutPaste{Clipboard: clip})
			if ui.Editor.Text != c.want {
				t.Fatalf("editor holds %q, want %q", ui.Editor.Text, c.want)
			}
			menuItem(t, w, "Edit", "Undo").Action()
			if ui.Editor.Text != c.text && c.name != "plain over selection" {
				t.Fatalf("Undo left %q, want %q", ui.Editor.Text, c.text)
			}
		})
	}
}

// With word wrap a row of the editor is a line on the screen. The edits
// still land where the text says.
func TestFormatInWrappedText(t *testing.T) {
	ui, w := editing(t, "")
	w.Resize(fyne.NewSize(400, 600))
	long := strings.Repeat("word ", 60)
	ui.Editor.SetText(long + "end\n" + long + "tail")
	ui.Editor.TypedKey(&fyne.KeyEvent{Name: fyne.KeyPageDown})
	selectBack(ui.Editor, 4)
	ui.Editor.TypedShortcut(ctrl(fyne.KeyB))
	if want := long + "end\n" + long + "**tail**"; ui.Editor.Text != want {
		t.Fatalf("editor holds %q", ui.Editor.Text)
	}
	if got := ui.Editor.SelectedText(); got != "tail" {
		t.Fatalf("selected %q, want tail", got)
	}
	menuItem(t, w, "Edit", "Select all").Action()
	formatButton(t, ui, "quote").OnActivated()
	if want := "> " + long + "end\n> " + long + "**tail**"; ui.Editor.Text != want {
		t.Fatalf("editor holds %q", ui.Editor.Text)
	}
}

// Typing straight after a change must not merge into its last undo step,
// or one Ctrl+Z would leave half the formatting behind.
func TestUndoAfterTypingIntoAFormat(t *testing.T) {
	for _, c := range []struct {
		name, text string
		selected   int
		key        fyne.KeyName
		move       fyne.KeyName // a key pressed before typing, "" for none
		want       string       // after typing "x"
	}{
		{"bold, no selection", "say ", 0, fyne.KeyB, "", "say **x**"},
		{"bold, typed before the text", "say hi", 2, fyne.KeyB, fyne.KeyLeft, "say **xhi**"},
		{"italic, typed before the text", "say hi", 2, fyne.KeyI, fyne.KeyLeft, "say _xhi_"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ui, w := editing(t, c.text)
			selectBack(ui.Editor, c.selected)
			ui.Editor.TypedShortcut(ctrl(c.key))
			formatted := ui.Editor.Text
			if c.move != "" {
				ui.Editor.TypedKey(&fyne.KeyEvent{Name: c.move})
			}
			test.Type(ui.Editor, "x")
			if ui.Editor.Text != c.want {
				t.Fatalf("editor holds %q, want %q", ui.Editor.Text, c.want)
			}
			undo := menuItem(t, w, "Edit", "Undo").Action
			for undo(); ui.Editor.Text != formatted; undo() {
				if ui.Editor.Text == c.text {
					t.Fatal("undo skipped the formatted text")
				}
				if !strings.Contains(ui.Editor.Text, "x") {
					t.Fatalf("undo left %q, half formatted", ui.Editor.Text)
				}
			}
			undo()
			if ui.Editor.Text != c.text {
				t.Fatalf("undo left %q, want %q", ui.Editor.Text, c.text)
			}
		})
	}
}

func TestShiftIsForgottenWhenTheEditorLosesFocus(t *testing.T) {
	ui, w := editing(t, "- a")
	ui.Editor.KeyDown(shiftKey)
	w.Canvas().Focus(ui.Search) // Shift is released over there
	w.Canvas().Focus(ui.Editor)
	ui.Editor.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if ui.Editor.Text != "- a\n- " {
		t.Fatalf("Enter left %q, want the list continued", ui.Editor.Text)
	}
}
