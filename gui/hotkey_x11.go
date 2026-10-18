//go:build linux || openbsd

package gui

import "golang.design/x/hotkey"

const (
	// ModAlt is the Alt modifier (X11 Mod1).
	ModAlt = hotkey.Mod1
	// ModSuper is the Super/Windows modifier (X11 Mod4).
	ModSuper = hotkey.Mod4
)
