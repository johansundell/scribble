//go:build windows

package gui

import (
	"syscall"
	"unsafe"

	"fyne.io/fyne/v2/driver"
)

var (
	procDwmSetWindowAttribute = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	procSetWindowPos          = user32.NewProc("SetWindowPos")
)

const (
	// dwmwaUseImmersiveDarkMode is DWMWA_USE_IMMERSIVE_DARK_MODE, honoured
	// from Windows 10 20H1; older builds ignore it and keep a light bar.
	dwmwaUseImmersiveDarkMode = 20

	swpNoSize         = 0x0001
	swpNoMove         = 0x0002
	swpNoZOrder       = 0x0004
	swpNoActivate     = 0x0010
	swpFrameChanged   = 0x0020
	swpRedrawFrameNow = swpNoSize | swpNoMove | swpNoZOrder | swpNoActivate | swpFrameChanged
)

// setTitleBarDark asks the desktop window manager for a dark or light title
// bar on a Win32 window, then has it redraw the frame: Windows 10 otherwise
// keeps the old colour until the window is next activated.
func setTitleBarDark(ctx any, dark bool) {
	wc, ok := ctx.(driver.WindowsWindowContext)
	if !ok || wc.HWND == 0 {
		return
	}
	var on int32 // a Win32 BOOL, not a Go bool
	if dark {
		on = 1
	}
	if err := procDwmSetWindowAttribute.Find(); err != nil {
		return // no dwmapi.dll: nothing draws a dark title bar anyway
	}
	ret, _, _ := procDwmSetWindowAttribute.Call(wc.HWND, dwmwaUseImmersiveDarkMode,
		uintptr(unsafe.Pointer(&on)), unsafe.Sizeof(on))
	if ret != 0 {
		return // an older Windows that does not know the attribute
	}
	procSetWindowPos.Call(wc.HWND, 0, 0, 0, 0, 0, swpRedrawFrameNow)
}
