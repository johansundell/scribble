//go:build !windows

package main

import (
	"bytes"
	"testing"
)

func TestTrayIconIsThePNGAppIcon(t *testing.T) {
	icon := trayIcon()
	if icon.Name() != "Icon.png" {
		t.Errorf("tray icon name = %q, want %q", icon.Name(), "Icon.png")
	}
	if !bytes.Equal(icon.Content(), iconPNG) {
		t.Error("tray icon is not Icon.png")
	}
}
