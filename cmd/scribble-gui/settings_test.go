package main

import (
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/gui"
	"github.com/joho/godotenv"
)

func readEnvFile(t *testing.T, path string) map[string]string {
	t.Helper()
	env, err := godotenv.Read(path)
	if err != nil {
		t.Fatalf("the written .env does not parse: %v", err)
	}
	return env
}

func TestSaveSettingsWritesDotEnvAndSetsTheEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("SCRIBBLE_HOTKEY=none\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCRIBBLE_URL", "")
	t.Setenv("SCRIBBLE_TOKEN", "")

	if err := saveSettings(path, gui.Settings{URL: "https://notes.example", Token: "tok"}); err != nil {
		t.Fatal(err)
	}

	env := readEnvFile(t, path)
	if env["SCRIBBLE_URL"] != "https://notes.example" || env["SCRIBBLE_TOKEN"] != "tok" || env["SCRIBBLE_HOTKEY"] != "none" {
		t.Errorf(".env holds %v", env)
	}
	if os.Getenv("SCRIBBLE_URL") != "https://notes.example" || os.Getenv("SCRIBBLE_TOKEN") != "tok" {
		t.Error("the saved settings are not in the environment for this run")
	}
}

func TestSetupScreenOffersSettings(t *testing.T) {
	test.NewTempApp(t)
	w := test.NewTempWindow(t, nil)
	var saved gui.Settings
	started := false
	w.SetContent(setupScreen(w, "SCRIBBLE_TOKEN is not set.", gui.Settings{URL: "https://notes.sudde.net"}, gui.Overridden{},
		func(s gui.Settings) error { saved = s; return nil },
		func() { started = true }))
	w.Resize(fyne.NewSize(600, 400))

	var button *widget.Button
	for _, o := range test.LaidOutObjects(w.Content()) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Settings…" {
			button = b
		}
	}
	if button == nil {
		t.Fatal("the setup screen has no Settings… button")
	}
	test.Tap(button)

	var entries []*widget.Entry
	for _, o := range test.LaidOutObjects(w.Canvas().Overlays().Top()) {
		if e, ok := o.(*widget.Entry); ok {
			entries = append(entries, e)
		}
	}
	if len(entries) != 2 || entries[0].Text != "https://notes.sudde.net" {
		t.Fatalf("the Settings dialog is not filled in with the default URL")
	}
	entries[1].SetText("tok")
	for _, o := range test.LaidOutObjects(w.Canvas().Overlays().Top()) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Save" {
			test.Tap(b)
		}
	}

	if saved != (gui.Settings{URL: "https://notes.sudde.net", Token: "tok"}) || !started {
		t.Errorf("saved %v, started %v; want the settings saved and the app started", saved, started)
	}
}

func TestEnvOverridesSeesVariablesSetInTheEnvironment(t *testing.T) {
	t.Setenv("SCRIBBLE_URL", "https://notes.example")
	t.Setenv("SCRIBBLE_TOKEN", "")
	os.Unsetenv("SCRIBBLE_TOKEN")

	if got := envOverrides(); got != (gui.Overridden{URL: true}) {
		t.Errorf("envOverrides() = %+v, want only the URL", got)
	}

	t.Setenv("SCRIBBLE_TOKEN", "") // set, even if empty: .env cannot change it
	if got := envOverrides(); got != (gui.Overridden{URL: true, Token: true}) {
		t.Errorf("envOverrides() = %+v, want both", got)
	}
}
