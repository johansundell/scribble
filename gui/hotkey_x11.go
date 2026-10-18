//go:build linux || openbsd

package gui

import "golang.design/x/hotkey"

const (
	// ModAlt is the Alt modifier (X11 Mod1).
	ModAlt = hotkey.Mod1
	// ModSuper is the Super/Windows modifier (X11 Mod4).
	ModSuper = hotkey.Mod4
)

// golang.design/x/hotkey v0.6.4 shifts Key1..Key0 by one on X11
// (Key1 = 0x0030 = XK_0, ..., Key0 = 0x0039 = XK_9); use the true X11
// ASCII keysyms (XK_0 = 0x0030 .. XK_9 = 0x0039).
const (
	Key0 hotkey.Key = 0x0030 + iota
	Key1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
	Key9
)

func restoreNativeWindow(ctx any) {
	if r, ok := ctx.(interface{ Restore() }); ok {
		r.Restore()
	}
}
