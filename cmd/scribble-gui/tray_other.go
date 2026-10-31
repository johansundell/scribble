//go:build !windows

package main

import "fyne.io/fyne/v2"

// trayIcon is the system tray icon: the PNG app icon.
func trayIcon() fyne.Resource {
	return fyne.NewStaticResource("Icon.png", iconPNG)
}
