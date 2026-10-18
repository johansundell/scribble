//go:build linux || openbsd

package gui_test

import (
	"fmt"
	"testing"

	"github.com/johansundell/scribble/gui"
	"golang.design/x/hotkey"
)

func TestParseHotkeyX11DigitsUseASCIIKeysyms(t *testing.T) {
	for i := 0; i <= 9; i++ {
		spec := fmt.Sprintf("Ctrl+Alt+%d", i)
		_, got, err := gui.ParseHotkey(spec)
		want := hotkey.Key('0' + i)
		if err != nil || got != want {
			t.Fatalf("ParseHotkey(%q) = (%#x, %v), want ASCII keysym %#x", spec, got, err, want)
		}
	}
}
