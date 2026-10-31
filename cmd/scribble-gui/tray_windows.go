package main

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

// iconICO is assets/img/icon.ico, copied here by make icons. The Windows
// tray loads its icon with LoadImage(IMAGE_ICON), which reads only ICO
// files, so it cannot take Icon.png (#120).
//
//go:embed icon.ico
var iconICO []byte

// trayIcon is the system tray icon: the multi-size ICO app icon.
func trayIcon() fyne.Resource {
	return fyne.NewStaticResource("icon.ico", iconICO)
}
