//go:build windows

package main

import (
	"bytes"
	"testing"
)

// The Windows tray loads its icon with LoadImage(IMAGE_ICON), which only
// reads ICO files (#120).
func TestTrayIconIsAnICOOnWindows(t *testing.T) {
	icon := trayIcon()
	if icon.Name() != "icon.ico" {
		t.Errorf("tray icon name = %q, want %q", icon.Name(), "icon.ico")
	}
	if !bytes.HasPrefix(icon.Content(), []byte{0x00, 0x00, 0x01, 0x00}) {
		t.Errorf("tray icon does not start with the ICO header: % x", icon.Content()[:min(4, len(icon.Content()))])
	}
}
