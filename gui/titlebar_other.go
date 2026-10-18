//go:build !windows

package gui

// setTitleBarDark leaves the title bar to the window manager: only Windows
// needs telling.
func setTitleBarDark(ctx any, dark bool) {
	if d, ok := ctx.(interface{ SetTitleBarDark(bool) }); ok {
		d.SetTitleBarDark(dark)
	}
}
