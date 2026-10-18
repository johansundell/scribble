//go:build !windows

package gui

// setTitleBarDark does nothing: outside Windows the window manager draws the
// title bar and follows the desktop theme.
func setTitleBarDark(ctx any, dark bool) {}
