package gui

import (
	"embed"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// icons are GitHub's Octicons (MIT, see icons/LICENSE), as in the web
// editor's toolbar.
//
//go:embed icons/*.svg
var icons embed.FS

// octicon is the Octicon named name, drawn in the theme's text colour so
// it follows FYNE_THEME light and dark.
func octicon(name string) fyne.Resource {
	b, err := icons.ReadFile("icons/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return theme.NewThemedResource(fyne.NewStaticResource(name+".svg", b))
}

// A formatting is a change to the editor's text made from its selection
// [start, end), or its cursor when start == end.
type formatting func(text string, start, end int) mdEdit

var (
	bold   = wrapWith("**")
	italic = wrapWith("_")
)

func wrapWith(marker string) formatting {
	return func(_ string, start, end int) mdEdit { return wrapSelection(start, end, marker) }
}

func prefixWith(prefix func(int) string) formatting {
	return func(text string, start, end int) mdEdit { return prefixLines(text, start, end, prefix) }
}

func link(_ string, start, end int) mdEdit { return insertLink(start, end) }

// formatKeys are the formatting shortcuts, Ctrl (Cmd on macOS) plus the
// key. Ctrl+E is not inline code: it toggles view/edit mode.
var formatKeys = map[fyne.KeyName]formatting{
	fyne.KeyB: bold,
	fyne.KeyI: italic,
	fyne.KeyK: link,
}

// newFormatBar is the formatting toolbar above the editor, in GitHub's
// order. Fyne has no tooltips, so the buttons are icons alone.
func (ui *UI) newFormatBar() *widget.Toolbar {
	button := func(icon string, f formatting) widget.ToolbarItem {
		return widget.NewToolbarAction(octicon(icon), func() {
			ui.Editor.format(f)
			ui.w.Canvas().Focus(ui.Editor)
		})
	}
	return widget.NewToolbar(
		button("heading", prefixWith(headingPrefix)),
		button("bold", bold),
		button("italic", italic),
		widget.NewToolbarSeparator(),
		button("quote", prefixWith(quotePrefix)),
		button("code", wrapWith("`")),
		button("link", link),
		widget.NewToolbarSeparator(),
		button("list-unordered", prefixWith(bulletPrefix)),
		button("list-ordered", prefixWith(numberPrefix)),
		button("tasklist", prefixWith(taskPrefix)),
	)
}
