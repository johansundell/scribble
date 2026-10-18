package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/theme"
)

// MatchTitleBar makes w's title bar dark or light to match the theme in s,
// now and whenever s changes. On Windows the system draws the title bar and
// Fyne picks its colour from the Windows app mode, not from FYNE_THEME, so
// a dark theme on a light Windows gets a light bar.
//
// Fyne sets the title bar again when it creates the window and on every
// settings change, so call this once the app has started: its listener is
// then added after Fyne's and has the last word.
func MatchTitleBar(w fyne.Window, s fyne.Settings) {
	nw, ok := w.(driver.NativeWindow)
	if !ok {
		return
	}
	apply := func(s fyne.Settings) {
		dark := s.ThemeVariant() == theme.VariantDark
		nw.RunNative(func(ctx any) { setTitleBarDark(ctx, dark) })
	}
	apply(s)
	s.AddListener(apply)
}
