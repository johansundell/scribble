package gui

import (
	"fmt"
	"runtime"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"golang.design/x/hotkey"
)

// DefaultHotkey is the global shortcut registered when SCRIBBLE_HOTKEY is unset.
const DefaultHotkey = "Ctrl+Alt+N"

// Cross-platform modifier aliases for golang.design/x/hotkey.
const (
	ModCtrl  = hotkey.ModCtrl
	ModShift = hotkey.ModShift
)

var hotkeyModifiers = map[string]hotkey.Modifier{
	"ctrl":    ModCtrl,
	"control": ModCtrl,
	"shift":   ModShift,
	"alt":     ModAlt,
	"option":  ModAlt,
	"super":   ModSuper,
	"win":     ModSuper,
	"cmd":     ModSuper,
	"command": ModSuper,
}

var hotkeyKeys = map[string]hotkey.Key{
	"a": hotkey.KeyA, "b": hotkey.KeyB, "c": hotkey.KeyC, "d": hotkey.KeyD,
	"e": hotkey.KeyE, "f": hotkey.KeyF, "g": hotkey.KeyG, "h": hotkey.KeyH,
	"i": hotkey.KeyI, "j": hotkey.KeyJ, "k": hotkey.KeyK, "l": hotkey.KeyL,
	"m": hotkey.KeyM, "n": hotkey.KeyN, "o": hotkey.KeyO, "p": hotkey.KeyP,
	"q": hotkey.KeyQ, "r": hotkey.KeyR, "s": hotkey.KeyS, "t": hotkey.KeyT,
	"u": hotkey.KeyU, "v": hotkey.KeyV, "w": hotkey.KeyW, "x": hotkey.KeyX,
	"y": hotkey.KeyY, "z": hotkey.KeyZ,
	"0": Key0, "1": Key1, "2": Key2, "3": Key3,
	"4": Key4, "5": Key5, "6": Key6, "7": Key7,
	"8": Key8, "9": Key9,
	"space":  hotkey.KeySpace,
	"return": hotkey.KeyReturn,
	"enter":  hotkey.KeyReturn,
	"escape": hotkey.KeyEscape,
	"esc":    hotkey.KeyEscape,
	"delete": hotkey.KeyDelete,
	"tab":    hotkey.KeyTab,
	"left":   hotkey.KeyLeft,
	"right":  hotkey.KeyRight,
	"up":     hotkey.KeyUp,
	"down":   hotkey.KeyDown,
	"f1":     hotkey.KeyF1, "f2": hotkey.KeyF2, "f3": hotkey.KeyF3, "f4": hotkey.KeyF4,
	"f5": hotkey.KeyF5, "f6": hotkey.KeyF6, "f7": hotkey.KeyF7, "f8": hotkey.KeyF8,
	"f9": hotkey.KeyF9, "f10": hotkey.KeyF10, "f11": hotkey.KeyF11, "f12": hotkey.KeyF12,
	"f13": hotkey.KeyF13, "f14": hotkey.KeyF14, "f15": hotkey.KeyF15, "f16": hotkey.KeyF16,
	"f17": hotkey.KeyF17, "f18": hotkey.KeyF18, "f19": hotkey.KeyF19, "f20": hotkey.KeyF20,
}

// ParseHotkey parses a hotkey specification like "Ctrl+Alt+N". An empty spec
// uses DefaultHotkey ("Ctrl+Alt+N"); "none" or "off" disables the hotkey and
// returns (nil, 0, nil).
func ParseHotkey(spec string) ([]hotkey.Modifier, hotkey.Key, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = DefaultHotkey
	}
	switch strings.ToLower(spec) {
	case "none", "off":
		return nil, 0, nil
	}

	parts := strings.Split(spec, "+")
	if len(parts) < 2 {
		return nil, 0, fmt.Errorf("hotkey %q must have at least one modifier and a key (e.g. %s)", spec, DefaultHotkey)
	}
	var mods []hotkey.Modifier
	for _, p := range parts[:len(parts)-1] {
		m, ok := hotkeyModifiers[strings.ToLower(strings.TrimSpace(p))]
		if !ok {
			return nil, 0, fmt.Errorf("unknown hotkey modifier %q in %q", p, spec)
		}
		if !slices.Contains(mods, m) {
			mods = append(mods, m)
		}
	}
	keyName := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	key, ok := hotkeyKeys[keyName]
	if !ok {
		return nil, 0, fmt.Errorf("unknown hotkey key %q in %q", parts[len(parts)-1], spec)
	}
	return mods, key, nil
}

// BindHotkey listens for keydown events and opens a new note on the Fyne UI
// thread for each one. The returned channel closes when keydown closes.
func (ui *UI) BindHotkey(keydown <-chan hotkey.Event) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range keydown {
			fyne.DoAndWait(ui.OpenNewNote)
		}
	}()
	return done
}

// RegisterHotkey parses spec and registers a global desktop hotkey that opens
// a new note when pressed. It returns an unregister function (nil if disabled).
func (ui *UI) RegisterHotkey(spec string) (func() error, error) {
	mods, key, err := ParseHotkey(spec)
	if err != nil || mods == nil {
		if err != nil && ui.Status.Text == "" {
			ui.Status.SetText(err.Error())
		}
		return nil, err
	}
	hk := hotkey.New(mods, key)
	if err := hk.Register(); err != nil {
		if ui.Status.Text == "" {
			ui.Status.SetText(err.Error())
		}
		return nil, err
	}
	done := ui.BindHotkey(hk.Keydown())
	go func() {
		<-done
		runtime.KeepAlive(hk)
	}()
	return hk.Unregister, nil
}
