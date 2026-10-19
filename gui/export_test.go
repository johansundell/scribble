package gui

import "fyne.io/fyne/v2/widget"

// SetParseMarkdownForTest swaps the markdown parser for testing panic recovery.
func SetParseMarkdownForTest(fn func(string) *widget.RichText) func() {
	old := parseMarkdown
	parseMarkdown = fn
	return func() { parseMarkdown = old }
}
