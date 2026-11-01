package gui

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"golang.design/x/hotkey"
)

// DefaultHotkey is the global new-note shortcut used when SCRIBBLE_HOTKEY is unset.
const DefaultHotkey = "Ctrl+Alt+N"

// DefaultShowHotkey is the global show-window shortcut used when
// SCRIBBLE_SHOW_HOTKEY is unset (Ctrl+Option+S on macOS).
const DefaultShowHotkey = "Ctrl+Alt+S"

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
// uses def; "none" or "off" disables the hotkey and returns (nil, 0, nil).
func ParseHotkey(spec, def string) ([]hotkey.Modifier, hotkey.Key, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = def
	}
	switch strings.ToLower(spec) {
	case "none", "off":
		return nil, 0, nil
	}

	parts := strings.Split(spec, "+")
	if len(parts) < 2 {
		return nil, 0, fmt.Errorf("hotkey %q must have at least one modifier and a key (e.g. %s)", spec, def)
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
	return bindHotkey(keydown, ui.OpenNewNote)
}

// BindShowHotkey listens for keydown events and shows, restores and focuses
// the main window on the Fyne UI thread for each one, leaving focus inside
// the window where it was. The returned channel closes when keydown closes.
func (ui *UI) BindShowHotkey(keydown <-chan hotkey.Event) <-chan struct{} {
	return bindHotkey(keydown, ui.showWindow)
}

func bindHotkey(keydown <-chan hotkey.Event, action func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range keydown {
			fyne.DoAndWait(action)
		}
	}()
	return done
}

// registerHotkey registers a global desktop hotkey with the system and
// returns its keydown events and unregister function. Tests swap it out.
var registerHotkey = func(mods []hotkey.Modifier, key hotkey.Key) (<-chan hotkey.Event, func() error, error) {
	hk := hotkey.New(mods, key)
	if err := hk.Register(); err != nil {
		return nil, nil, err
	}
	keydown := hk.Keydown()
	go func() {
		for range keydown {
		}
	}()
	return keydown, hk.Unregister, nil
}

// RegisterHotkeys registers the global new-note hotkey (newNoteSpec, empty
// for DefaultHotkey), then the global show-window hotkey (showSpec, empty for
// DefaultShowHotkey). A hotkey that can't be parsed, can't be registered, or
// (for the show hotkey) uses the same keys as the new-note hotkey is skipped
// and its error is shown in the status bar. It returns the unregister
// functions of the hotkeys it registered.
func (ui *UI) RegisterHotkeys(newNoteSpec, showSpec string) []func() error {
	var unregister []func() error
	register := func(mods []hotkey.Modifier, key hotkey.Key, bind func(<-chan hotkey.Event) <-chan struct{}) {
		keydown, unreg, err := registerHotkey(mods, key)
		if err != nil {
			ui.reportHotkeyError(err)
			return
		}
		done := bind(keydown)
		go func() {
			<-done
			runtime.KeepAlive(unreg) // keeps the system hotkey alive while bound
		}()
		unregister = append(unregister, unreg)
	}

	newMods, newKey, err := ParseHotkey(newNoteSpec, DefaultHotkey)
	if err != nil {
		ui.reportHotkeyError(fmt.Errorf("new-note hotkey: %w", err))
	} else if newMods != nil {
		register(newMods, newKey, ui.BindHotkey)
	}

	showMods, showKey, err := ParseHotkey(showSpec, DefaultShowHotkey)
	switch {
	case err != nil:
		ui.reportHotkeyError(fmt.Errorf("show-window hotkey: %w", err))
	case showMods == nil:
	case newMods != nil && showKey == newKey && sameModifiers(showMods, newMods):
		ui.reportHotkeyError(errors.New("show-window hotkey uses the same keys as the new-note hotkey; set SCRIBBLE_SHOW_HOTKEY to other keys"))
	default:
		register(showMods, showKey, ui.BindShowHotkey)
	}
	return unregister
}

// reportHotkeyError shows err in the status bar unless it already shows an error.
func (ui *UI) reportHotkeyError(err error) {
	if ui.Status.Text == "" {
		ui.Status.SetText(err.Error())
	}
}

func sameModifiers(a, b []hotkey.Modifier) bool {
	return len(a) == len(b) && !slices.ContainsFunc(a, func(m hotkey.Modifier) bool { return !slices.Contains(b, m) })
}
