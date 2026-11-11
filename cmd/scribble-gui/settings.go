package main

import (
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/envfile"
	"github.com/johansundell/scribble/gui"
)

// saveSettings writes s to the .env file at path and sets it in this
// process's environment too.
func saveSettings(path string, s gui.Settings) error {
	if err := envfile.Write(path, map[string]string{"SCRIBBLE_URL": s.URL, "SCRIBBLE_TOKEN": s.Token}); err != nil {
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
