package gui

import (
	"image/color"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/alecthomas/chroma/v2"
	"github.com/johansundell/scribble/offline"
	"golang.design/x/hotkey"
)

// SetParseMarkdownForTest swaps the markdown parser for testing panic recovery.
func SetParseMarkdownForTest(fn func(string) *widget.RichText) func() {
	old := parseMarkdown
	parseMarkdown = fn
	return func() { parseMarkdown = old }
}

// PendingForTest is the queued edit of note id in ui's local copy.
func PendingForTest(ui *UI, id int64) (offline.Edit, bool) { return ui.store.Pending(id) }

// SetOpenURLForTest swaps how links are opened.
func SetOpenURLForTest(fn func(*url.URL) error) func() {
	old := openURL
	openURL = fn
	return func() { openURL = old }
}

// AboutTextForTest is the text of the labels and links in Help > About.
func AboutTextForTest(ui *UI) string {
	var out []string
	for _, o := range ui.aboutContent().Objects {
		switch o := o.(type) {
		case *widget.Label:
			out = append(out, o.Text)
		case *widget.Hyperlink:
			out = append(out, o.Text)
		}
	}
	return strings.Join(out, "\n")
}

// SetFilePickersForTest swaps the native save and open file dialogs.
func SetFilePickersForTest(save func(name string, cb func(fyne.URIWriteCloser, error)), open func(cb func(fyne.URIReadCloser, error))) func() {
	oldSave, oldOpen := pickSaveFile, pickOpenFile
	pickSaveFile = func(_ fyne.Window, name string, cb func(fyne.URIWriteCloser, error)) { save(name, cb) }
	pickOpenFile = func(_ fyne.Window, cb func(fyne.URIReadCloser, error)) { open(cb) }
	return func() { pickSaveFile, pickOpenFile = oldSave, oldOpen }
}

// SetRegisterHotkeyForTest swaps how global hotkeys are registered with the
// system, so tests never grab real desktop shortcuts.
func SetRegisterHotkeyForTest(fn func([]hotkey.Modifier, hotkey.Key) (<-chan hotkey.Event, func() error, error)) func() {
	old := registerHotkey
	registerHotkey = fn
	return func() { registerHotkey = old }
}

// CodeColorForTest is the colour of code tokens of type tt in GitHub Dark,
// or GitHub Light if not dark; false if they keep the text colour.
func CodeColorForTest(tt chroma.TokenType, dark bool) (color.Color, bool) {
	fg, _ := codeColors(tt, dark, test.Theme(), fyne.CurrentApp().Settings().ThemeVariant())
	return fg, fg != nil
}

// SetAfterFuncForTest swaps how the code blocks' Copy buttons wait to say
// Copy again.
func SetAfterFuncForTest(fn func(time.Duration, func()) func() bool) func() {
	old := afterFunc
	afterFunc = fn
	return func() { afterFunc = old }
}
