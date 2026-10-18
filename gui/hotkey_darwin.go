//go:build darwin

package gui

import "golang.design/x/hotkey"

const (
	// ModAlt is the Option/Alt modifier.
	ModAlt = hotkey.ModOption
	// ModSuper is the Command modifier.
	ModSuper = hotkey.ModCmd
)
