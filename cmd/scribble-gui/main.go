// Command scribble-gui is the desktop client for a scribble server.
//
//	SCRIBBLE_URL     server address (default http://localhost:8080)
//	SCRIBBLE_TOKEN   the server's AUTH_TOKEN
//	SCRIBBLE_HOTKEY  global new-note shortcut (default Ctrl+Alt+N; "none" to disable)
//
// They can also be set in a .env file next to the executable; variables
// already in the environment win.
//
// It keeps a copy of the notes on disk (see offline.DefaultPath), so it
// starts instantly and works while the server is unreachable.
package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/gui"
	"github.com/johansundell/scribble/offline"
	"github.com/joho/godotenv"
)

// syncInterval is how often the GUI asks the server for changes.
const syncInterval = 30 * time.Second

var Version = "dev"

func main() {
	a := app.New()
	ver := Version
	if (ver == "" || ver == "dev") && a.Metadata().Version != "" {
		ver = "v" + a.Metadata().Version
	}
	w := a.NewWindow("Scribble " + ver)
	w.Resize(fyne.NewSize(1000, 700))

	envErr := loadEnvNextToExecutable()
	token := os.Getenv("SCRIBBLE_TOKEN")
	baseURL := os.Getenv("SCRIBBLE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	if envErr != nil {
		w.SetContent(widget.NewLabel("Cannot read the .env file next to scribble-gui: " + envErr.Error()))
	} else if token == "" {
		w.SetContent(widget.NewLabel("SCRIBBLE_TOKEN is not set. Set it to the server's AUTH_TOKEN, in the environment or in a .env file next to scribble-gui, and start it again."))
	} else if cache, err := openCache(baseURL); err != nil {
		w.SetContent(widget.NewLabel("Cannot open the notes saved on this computer: " + err.Error()))
	} else {
		ui := gui.New(w, client.New(baseURL, token), cache)
		ui.StartSync(syncInterval)
		if desk, ok := a.(desktop.App); ok {
			ui.SetupTray(desk)
		}
		if unregister, err := ui.RegisterHotkey(os.Getenv("SCRIBBLE_HOTKEY")); err == nil && unregister != nil {
			defer unregister()
		}
	}
	w.ShowAndRun()
}

// loadEnvNextToExecutable is loadEnv for the directory scribble-gui runs
// from. A desktop launcher starts it with an unpredictable working
// directory, so the file is looked for there rather than in it.
func loadEnvNextToExecutable() error {
	exe, err := os.Executable()
	if err != nil {
		return nil // nowhere to look; the environment still applies
	}
	return loadEnv(filepath.Dir(exe))
}

// loadEnv sets the variables in dir/.env that are not set yet. A missing
// file is not an error.
func loadEnv(dir string) error {
	err := godotenv.Load(filepath.Join(dir, ".env"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func openCache(baseURL string) (*offline.Store, error) {
	path, err := offline.DefaultPath(baseURL)
	if err != nil {
		return nil, err
	}
	return offline.Open(path)
}
