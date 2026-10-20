package gui

import (
	"net/url"
	"strings"

	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/offline"
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
