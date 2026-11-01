package main

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
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

// envOverrides marks the settings set in this process's environment. Call
// it before loading the .env file, which sets the rest.
func envOverrides() gui.Overridden {
	_, url := os.LookupEnv("SCRIBBLE_URL")
	_, token := os.LookupEnv("SCRIBBLE_TOKEN")
	return gui.Overridden{URL: url, Token: token}
}

// setupScreen is the window's content while scribble-gui cannot reach a
// server yet: msg, and a Settings… button that opens the Settings dialog
// filled in with current and noting the fields overridden marks. Once save
// succeeds it calls saved.
func setupScreen(w fyne.Window, msg string, current gui.Settings, overridden gui.Overridden, save func(gui.Settings) error, saved func()) fyne.CanvasObject {
	label := widget.NewLabel(msg)
	label.Wrapping = fyne.TextWrapWord
	return container.NewVBox(label, container.NewHBox(
		widget.NewButton("Settings…", func() {
			gui.ShowSettings(w, current, overridden, func(s gui.Settings) error {
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
// A file godotenv cannot read is first copied to path.bak, in case the
// edit loses something in it; an existing path.bak is left alone, as it
// holds the file from before the first such edit. The new file replaces the old in one step,
// so a crash midway leaves one or the other, never half of each.
func writeEnv(path string, set map[string]string) error {
	// Through a symlink, so the rename replaces the file, not the link.
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// A new file holds the token, so only the user may read it; an
	// existing one keeps its mode.
	mode := fs.FileMode(0o600)
	if err == nil {
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
		if _, err := godotenv.UnmarshalBytes(raw); err != nil {
			bak, err := os.OpenFile(path+".bak", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err == nil {
				_, err = bak.Write(raw)
				if cerr := bak.Close(); err == nil {
					err = cerr
				}
			}
			if err != nil && !errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("cannot back up the .env file it cannot read: %w", err)
			}
		}
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
	return replaceFile(path, []byte(strings.Join(out, "\n")+"\n"), mode)
}

// replaceFile writes data to a file next to path, then renames it to path.
func replaceFile(path string, data []byte, mode fs.FileMode) error {
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(mode) // OpenFile's mode is cut by the umask
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
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
