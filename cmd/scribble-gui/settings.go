package main

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/gui"
	"github.com/joho/godotenv"
)

// saveSettings writes s to the .env file at path and sets it in this
// process's environment too.
func saveSettings(path string, s gui.Settings) error {
	if err := writeEnv(path, map[string]string{"SCRIBBLE_URL": s.URL, "SCRIBBLE_TOKEN": s.Token}); err != nil {
		return err
	}
	os.Setenv("SCRIBBLE_URL", s.URL)
	os.Setenv("SCRIBBLE_TOKEN", s.Token)
	return nil
}

// setupScreen is the window's content while scribble-gui cannot reach a
// server yet: msg, and a Settings… button that opens the Settings dialog
// filled in with current. Once save succeeds it calls saved.
func setupScreen(w fyne.Window, msg string, current gui.Settings, save func(gui.Settings) error, saved func()) fyne.CanvasObject {
	label := widget.NewLabel(msg)
	label.Wrapping = fyne.TextWrapWord
	return container.NewVBox(label, container.NewHBox(
		widget.NewButton("Settings…", func() {
			gui.ShowSettings(w, current, func(s gui.Settings) error {
				if err := save(s); err != nil {
					return err
				}
				saved()
				return nil
			})
		}),
	))
}

// writeEnv sets the variables in set in the .env file at path, creating it
// if missing. A variable already there is changed where it is; one that is
// not is added at the end. Every other line, comments included, is kept.
func writeEnv(path string, set map[string]string) error {
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	var lines []string
	if text := strings.TrimRight(string(raw), "\r\n"); text != "" {
		lines = strings.Split(text, "\n")
	}
	written := map[string]bool{}
	out := lines[:0]
	for _, line := range lines {
		key, ok := envKey(line)
		if _, update := set[key]; !ok || !update {
			out = append(out, line)
			continue
		}
		if !written[key] { // a repeat of the key goes
			out = append(out, envLine(key, set[key]))
			written[key] = true
		}
	}
	for _, key := range slices.Sorted(maps.Keys(set)) {
		if !written[key] {
			out = append(out, envLine(key, set[key]))
		}
	}
	// A new file holds the token, so only the user may read it; an
	// existing one keeps its mode.
	return os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o600)
}

// envKey is the variable a .env line sets, if it sets one.
func envKey(line string) (string, bool) {
	line = strings.TrimSpace(line)
	line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	key, _, ok := strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" || strings.HasPrefix(key, "#") {
		return "", false
	}
	return key, true
}

// envLine is a .env line setting key to value, quoted so godotenv reads
// value back as it is.
func envLine(key, value string) string {
	line, _ := godotenv.Marshal(map[string]string{key: value})
	return line
}
