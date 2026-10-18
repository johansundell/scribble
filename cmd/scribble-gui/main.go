// Command scribble-gui is the desktop client for a scribble server.
//
//	SCRIBBLE_URL    server address (default http://localhost:8080)
//	SCRIBBLE_TOKEN  the server's AUTH_TOKEN
package main

import (
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/gui"
)

var Version = "dev"

func main() {
	a := app.New()
	w := a.NewWindow("Scribble " + Version)
	w.Resize(fyne.NewSize(1000, 700))

	token := os.Getenv("SCRIBBLE_TOKEN")
	baseURL := os.Getenv("SCRIBBLE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	if token == "" {
		w.SetContent(widget.NewLabel("SCRIBBLE_TOKEN is not set. Set it to the server's AUTH_TOKEN and start scribble-gui again."))
	} else {
		gui.New(w, client.New(baseURL, token))
	}
	w.ShowAndRun()
}
