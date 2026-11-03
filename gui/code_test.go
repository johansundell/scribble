package gui_test

import (
	"image/color"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/alecthomas/chroma/v2"
	"github.com/johansundell/scribble/gui"
)

const goNote = "# Code\n\n```go\nfunc main() {\n\treturn \"hi\" // done\n}\n```\n\nafter"

// colorOf returns the colour the viewer draws text want in.
func colorOf(t *testing.T, ui *gui.UI, want string) color.Color {
	t.Helper()
	for _, txt := range drawnTexts(ui.Viewer) {
		if txt.Text.Text == want {
			return txt.Color
		}
	}
	t.Fatalf("the viewer draws no text %q", want)
	return nil
}

// codeButtons returns the Copy buttons of the code blocks in the viewer.
func codeButtons(ui *gui.UI) []*widget.Button {
	var out []*widget.Button
	for _, o := range test.LaidOutObjects(ui.Viewer) {
		if b, ok := o.(*widget.Button); ok && b.Visible() {
			out = append(out, b)
		}
	}
	return out
}

// lightTheme is Fyne's theme in its light variant.
type lightTheme struct{ fyne.Theme }

func (l lightTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return l.Theme.Color(n, theme.VariantLight)
}

func sameColor(a, b color.Color) bool {
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

func TestCodeColorsMatchTheWeb(t *testing.T) {
	for _, tc := range []struct {
		tt          chroma.TokenType
		light, dark string
	}{
		{chroma.Keyword, "#cf222e", "#ff7b72"},
		{chroma.KeywordType, "#cf222e", "#ff7b72"},
		{chroma.LiteralNumberInteger, "#0550ae", "#79c0ff"},
		{chroma.NameFunction, "#6639ba", "#d2a8ff"},
		{chroma.LiteralStringDouble, "#0a3069", "#a5d6ff"},
		{chroma.NameVariable, "#953800", "#79c0ff"},
		{chroma.NameTag, "#0550ae", "#7ee787"},
		{chroma.NameClass, "#1f2328", "#f0883e"},
		{chroma.CommentSingle, "#57606a", "#8b949e"},
		{chroma.GenericDeleted, "#82071e", "#ffa198"},
		{chroma.GenericInserted, "#116329", "#56d364"},
	} {
		for _, v := range []struct {
			dark bool
			want string
		}{{false, tc.light}, {true, tc.dark}} {
			got, ok := gui.CodeColorForTest(tc.tt, v.dark)
			if !ok {
				t.Errorf("%v (dark %v) has no colour, want %s", tc.tt, v.dark, v.want)
				continue
			}
			r, g, b, _ := got.RGBA()
			if hex := hexOf(r, g, b); hex != v.want {
				t.Errorf("%v (dark %v) is %s, want %s", tc.tt, v.dark, hex, v.want)
			}
		}
	}
	if _, ok := gui.CodeColorForTest(chroma.Punctuation, false); ok {
		t.Error("punctuation should keep the text colour")
	}
}

func hexOf(r, g, b uint32) string {
	const digits = "0123456789abcdef"
	out := []byte{'#'}
	for _, c := range []uint32{r >> 8, g >> 8, b >> 8} {
		out = append(out, digits[c>>4], digits[c&15])
	}
	return string(out)
}

func TestCodeBlockInAKnownLanguageIsHighlighted(t *testing.T) {
	ui, _ := openNote(t, goNote)

	// The test app's theme is dark.
	kw, _ := gui.CodeColorForTest(chroma.Keyword, true)
	if got := colorOf(t, ui, "func"); !sameColor(got, kw) {
		t.Errorf("func is drawn in %v, want the keyword colour %v", got, kw)
	}
	str, _ := gui.CodeColorForTest(chroma.LiteralStringDouble, true)
	if got := colorOf(t, ui, `"hi"`); !sameColor(got, str) {
		t.Errorf(`"hi" is drawn in %v, want the string colour %v`, got, str)
	}

	// A light theme switches to GitHub Light.
	fyne.CurrentApp().Settings().SetTheme(lightTheme{theme.DefaultTheme()})
	ui.Viewer.Refresh()
	kw, _ = gui.CodeColorForTest(chroma.Keyword, false)
	if got := colorOf(t, ui, "func"); !sameColor(got, kw) {
		t.Errorf("in a light theme func is drawn in %v, want %v", got, kw)
	}
}

func TestCodeBlockWithoutAKnownLanguageStaysPlain(t *testing.T) {
	for _, fence := range []string{"```", "```nosuchlanguage"} {
		ui, _ := openNote(t, "# Code\n\n"+fence+"\nfunc main() {}\n```")
		fg := theme.Color(theme.ColorNameForeground)
		for _, txt := range drawnTexts(ui.Viewer) {
			if strings.Contains(txt.Text.Text, "func") && !sameColor(txt.Color, fg) {
				t.Errorf("%s: %q is drawn in %v, want the text colour", fence, txt.Text.Text, txt.Color)
			}
		}
		if len(codeButtons(ui)) != 1 {
			t.Errorf("%s: a plain code block should have a Copy button too", fence)
		}
	}
}

func TestCodeBlockInAListOrQuoteIsHighlighted(t *testing.T) {
	ui, _ := openNote(t, "# Code\n\n- item\n\n  ```go\n  var x int\n  ```\n\n> ```go\n> const y = 1\n> ```")
	kw, _ := gui.CodeColorForTest(chroma.Keyword, true)
	for _, word := range []string{"var", "const"} {
		if got := colorOf(t, ui, word); !sameColor(got, kw) {
			t.Errorf("%s is drawn in %v, want the keyword colour", word, got)
		}
	}
}

func TestCopyButtonCopiesTheCode(t *testing.T) {
	var waits []time.Duration
	var reset func()
	defer gui.SetAfterFuncForTest(func(d time.Duration, f func()) func() bool {
		waits, reset = append(waits, d), f
		return func() bool { return true }
	})()
	ui, _ := openNote(t, goNote+"\n\n```\nsecond block\n```")
	clip := fyne.CurrentApp().Clipboard()
	clip.SetContent("")

	buttons := codeButtons(ui)
	if len(buttons) != 2 {
		t.Fatalf("found %d Copy buttons, want one per code block", len(buttons))
	}
	if buttons[0].Text != "Copy" {
		t.Fatalf("the button says %q", buttons[0].Text)
	}
	test.Tap(buttons[0])
	if got, want := clip.Content(), "func main() {\n\treturn \"hi\" // done\n}"; got != want {
		t.Fatalf("Copy put %q on the clipboard, want %q", got, want)
	}
	if ui.Status.Text != "Copied code to clipboard" {
		t.Errorf("the status says %q", ui.Status.Text)
	}
	if buttons[0].Text != "Copied" || buttons[0].Icon != theme.ConfirmIcon() {
		t.Errorf("after a tap the button says %q", buttons[0].Text)
	}
	test.Tap(buttons[1])
	if got := clip.Content(); got != "second block" {
		t.Fatalf("the second Copy put %q on the clipboard", got)
	}

	if len(waits) != 2 || waits[0] != 2*time.Second {
		t.Fatalf("the buttons wait %v to say Copy again, want 2s each", waits)
	}
	reset()
	if buttons[1].Text != "Copy" || buttons[1].Icon != theme.ContentCopyIcon() {
		t.Fatalf("after 2s the button says %q", buttons[1].Text)
	}
}

func TestSelectingHighlightedCode(t *testing.T) {
	ui, _ := openNote(t, goNote)

	ui.Viewer.TypedShortcut(&fyne.ShortcutSelectAll{})
	if got := ui.Viewer.SelectedText(); !strings.Contains(got, "Code\nfunc main() {\n\treturn \"hi\" // done\n}\nafter") {
		t.Fatalf("Select all gave %q", got)
	}

	dragMono(t, ui, "main", `"hi"`)
	if got, want := ui.Viewer.SelectedText(), "main() {\n\treturn \"hi\""; got != want {
		t.Fatalf("dragging selected %q, want %q", got, want)
	}

	for _, word := range []string{"func", "main", "return", "done"} {
		ui.Viewer.DoubleTapped(&fyne.PointEvent{Position: monoPointIn(t, ui, word)})
		if got := ui.Viewer.SelectedText(); got != word {
			t.Errorf("double-clicking %q selected %q", word, got)
		}
	}
	ui.Viewer.TypedShortcut(&fyne.ShortcutCopy{Clipboard: fyne.CurrentApp().Clipboard()})
	if got := fyne.CurrentApp().Clipboard().Content(); got != "done" {
		t.Fatalf("Ctrl+C copied %q", got)
	}
}

// monoWidth is the rendered width of s in a code block.
func monoWidth(s string) float32 {
	return fyne.MeasureText(s, theme.TextSize(), fyne.TextStyle{Monospace: true}).Width
}

// monoPointIn is the middle of the code text want in the viewer.
func monoPointIn(t *testing.T, ui *gui.UI, want string) fyne.Position {
	t.Helper()
	p, h := textAt(t, ui, want)
	return p.AddXY(monoWidth(want)/2, h/2)
}

// dragMono is drag for code: from the start of the code text from to the
// end of the code text to.
func dragMono(t *testing.T, ui *gui.UI, from, to string) {
	t.Helper()
	start, h := textAt(t, ui, from)
	end, _ := textAt(t, ui, to)
	start = start.AddXY(1, h/2)
	end = end.AddXY(monoWidth(to)-1, h/2)
	ui.Viewer.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: end}, Dragged: fyne.NewDelta(end.X-start.X, end.Y-start.Y)})
	ui.Viewer.DragEnd()
}
