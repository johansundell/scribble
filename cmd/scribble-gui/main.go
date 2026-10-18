// Command scribble-gui is the desktop client for a scribble server.
//
//	SCRIBBLE_URL     server address (default http://localhost:8080)
//	SCRIBBLE_TOKEN   the server's AUTH_TOKEN
//	SCRIBBLE_HOTKEY  global new-note shortcut (default Ctrl+Alt+N; "none" to disable)
//
// It keeps a copy of the notes on disk (see offline.DefaultPath), so it
// starts instantly and works while the server is unreachable.
package main

import (
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/gui"
	"github.com/johansundell/scribble/offline"
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

	token := os.Getenv("SCRIBBLE_TOKEN")
	baseURL := os.Getenv("SCRIBBLE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	if token == "" {
		w.SetContent(widget.NewLabel("SCRIBBLE_TOKEN is not set. Set it to the server's AUTH_TOKEN and start scribble-gui again."))
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

func openCache(baseURL string) (*offline.Store, error) {
	path, err := offline.DefaultPath(baseURL)
	if err != nil {
		return nil, err
	}
	return offline.Open(path)
}
