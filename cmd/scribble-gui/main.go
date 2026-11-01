// Command scribble-gui is the desktop client for a scribble server.
//
//	SCRIBBLE_URL          server address (default https://notes.sudde.net)
//	SCRIBBLE_TOKEN        the server's AUTH_TOKEN
//	SCRIBBLE_HOTKEY       global new-note shortcut (default Ctrl+Alt+N; "none" to disable)
//	SCRIBBLE_SHOW_HOTKEY  global shortcut that only shows the window (default Ctrl+Alt+S; "none" to disable)
//
// They can also be set in a .env file next to the executable, which
// File > Settings… writes SCRIBBLE_URL and SCRIBBLE_TOKEN to; variables
// already in the environment win. The file is read before Fyne starts, so
// Fyne's own variables (such as FYNE_THEME=dark) work there too.
//
// It keeps a copy of the notes on disk (see offline.DefaultPath), so it
// starts instantly and works while the server is unreachable.
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
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

// iconPNG is the app icon, rendered from assets/img/icon.svg by make icons
// (#106). fyne package and fyne-cross also take it from here.
//
//go:embed Icon.png
var iconPNG []byte

// pngIcon is iconPNG as a resource.
func pngIcon() fyne.Resource {
	return fyne.NewStaticResource("Icon.png", iconPNG)
}

func main() {
	a, envErr := newApp(loadEnvNextToExecutable, app.New)
	ver := Version
	if (ver == "" || ver == "dev") && a.Metadata().Version != "" {
		ver = "v" + a.Metadata().Version
	}
	gui.Version = ver
	w := newWindow(a, "Scribble "+ver)
	w.Resize(fyne.NewSize(1000, 700))

	var cleanup []func()
	defer func() {
		for _, f := range slices.Backward(cleanup) {
			f()
		}
	}()
	save := func(s gui.Settings) error {
		path, err := envPathNextToExecutable()
		if err != nil {
			return err
		}
		return saveSettings(path, s)
	}
	// start shows the notes, or the setup screen if there is no way to
	// reach the server yet. Saving settings there starts again.
	var start func(envErr error)
	start = func(envErr error) {
		current := gui.Settings{URL: client.ServerURL(), Token: os.Getenv("SCRIBBLE_TOKEN")}
		retry := func() { start(nil) }
		switch {
		case envErr != nil:
			w.SetContent(setupScreen(w, "Cannot read the .env file next to scribble-gui: "+envErr.Error(), current, save, retry))
		case current.Token == "":
			w.SetContent(setupScreen(w, "SCRIBBLE_TOKEN is not set. Enter the server's address and AUTH_TOKEN in Settings, or set them in the environment or in a .env file next to scribble-gui.", current, save, retry))
		default:
			cleanup = append(cleanup, startUI(a, w, ver, current, save)...)
		}
	}
	start(envErr)
	// After start-up, so it overrides the title bar Fyne sets from the
	// Windows app mode when it creates the window. SetOnStarted keeps one
	// callback: add any other start-up work to this one.
	a.Lifecycle().SetOnStarted(func() { gui.MatchTitleBar(w, a.Settings()) })
	w.ShowAndRun()
}

// startUI opens the local copy of the notes on server s and shows them,
// syncing in the background. It returns what to undo when the app quits.
func startUI(a fyne.App, w fyne.Window, ver string, s gui.Settings, save func(gui.Settings) error) []func() {
	cache, err := openCache(s.URL)
	if err != nil {
		var locked *offline.LockedError
		if errors.As(err, &locked) {
			msg := "Another scribble-gui instance is already running for this server."
			if locked.PID > 0 {
				msg = fmt.Sprintf("Another scribble-gui instance (process %d) is already running for this server.", locked.PID)
			}
			w.SetContent(container.NewVBox(
				widget.NewLabel(msg),
				widget.NewButton("Quit", func() { a.Quit() }),
			))
		} else {
			w.SetContent(widget.NewLabel("Cannot open the notes saved on this computer: " + err.Error()))
		}
		return nil
	}
	cleanup := []func(){func() { cache.Close() }}
	c := client.New(s.URL, s.Token)
	c.Name, c.Version = "gui", ver
	ui := gui.New(w, c, cache)
	ui.SaveSettings = save
	ui.StartSync(syncInterval)
	if desk, ok := a.(desktop.App); ok {
		ui.SetupTray(desk, trayIcon())
	}
	for _, unregister := range ui.RegisterHotkeys(os.Getenv("SCRIBBLE_HOTKEY"), os.Getenv("SCRIBBLE_SHOW_HOTKEY")) {
		cleanup = append(cleanup, func() { unregister() })
	}
	return cleanup
}

// newApp loads the .env file, then creates the app, so variables Fyne reads
// while starting up (such as FYNE_THEME) can be set there too. An error from
// readEnv is returned with the app, for the window to show. The app gets the
// Scribble icon, which its windows show in the title bar and taskbar.
func newApp(readEnv func() error, create func() fyne.App) (fyne.App, error) {
	err := readEnv()
	a := create()
	a.SetIcon(pngIcon())
	return a, err
}

// newWindow creates the main window with the app icon set on it directly,
// so taskbars, docks and task switchers show it on every platform.
func newWindow(a fyne.App, title string) fyne.Window {
	w := a.NewWindow(title)
	w.SetIcon(a.Icon())
	return w
}

// loadEnvNextToExecutable is loadEnv for the directory scribble-gui runs
// from. A desktop launcher starts it with an unpredictable working
// directory, so the file is looked for there rather than in it.
func loadEnvNextToExecutable() error {
	path, err := envPathNextToExecutable()
	if err != nil {
		return nil // nowhere to look; the environment still applies
	}
	return loadEnv(filepath.Dir(path))
}

// envPathNextToExecutable is the .env file next to scribble-gui.
func envPathNextToExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot find the folder scribble-gui runs from: %w", err)
	}
	return filepath.Join(filepath.Dir(exe), ".env"), nil
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
