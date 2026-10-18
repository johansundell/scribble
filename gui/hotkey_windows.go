//go:build windows

package gui

import "golang.design/x/hotkey"

const (
	// ModAlt is the Alt modifier.
	ModAlt = hotkey.ModAlt
	// ModSuper is the Windows modifier.
	ModSuper = hotkey.ModWin
)
