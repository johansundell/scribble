package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestWriteEnvCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")

	if err := writeEnv(path, map[string]string{"SCRIBBLE_URL": "http://notes:8080", "SCRIBBLE_TOKEN": "secret"}); err != nil {
		t.Fatal(err)
	}

	env := readEnvFile(t, path)
	if env["SCRIBBLE_URL"] != "http://notes:8080" || env["SCRIBBLE_TOKEN"] != "secret" || len(env) != 2 {
		t.Errorf(".env holds %v", env)
	}
	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o077 != 0 {
		t.Errorf("a new .env holding the token has mode %v, want it private to the user", info.Mode().Perm())
	}
}

func TestWriteEnvUpdatesKeysAndKeepsTheRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	old := "# my settings\nFYNE_THEME=dark\nexport SCRIBBLE_TOKEN=old\n\nSCRIBBLE_HOTKEY=none # no hotkey\nSCRIBBLE_TOKEN=older\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeEnv(path, map[string]string{"SCRIBBLE_URL": "https://notes.example", "SCRIBBLE_TOKEN": "new"}); err != nil {
		t.Fatal(err)
	}

	env := readEnvFile(t, path)
	for k, want := range map[string]string{
		"FYNE_THEME":      "dark",
		"SCRIBBLE_HOTKEY": "none",
		"SCRIBBLE_TOKEN":  "new",
		"SCRIBBLE_URL":    "https://notes.example",
	} {
		if env[k] != want {
			t.Errorf("%s = %q, want %q", k, env[k], want)
		}
	}
	raw, _ := os.ReadFile(path)
	text := string(raw)
	if !strings.HasPrefix(text, "# my settings\nFYNE_THEME=dark\n") || !strings.Contains(text, "SCRIBBLE_HOTKEY=none # no hotkey\n") {
		t.Errorf("the other lines were not kept as they were:\n%s", text)
	}
	if strings.Contains(text, "old") {
		t.Errorf("an old token is still in the file:\n%s", text)
	}
	if strings.Index(text, "SCRIBBLE_TOKEN") > strings.Index(text, "SCRIBBLE_HOTKEY") {
		t.Errorf("SCRIBBLE_TOKEN moved from where it was:\n%s", text)
	}
}

func TestWriteEnvRoundTripsAwkwardValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	token := `a#b "c" \d $HOME 'e' !f`

	if err := writeEnv(path, map[string]string{"SCRIBBLE_TOKEN": token}); err != nil {
		t.Fatal(err)
	}

	if got := readEnvFile(t, path)["SCRIBBLE_TOKEN"]; got != token {
		t.Errorf("SCRIBBLE_TOKEN reads back as %q, want %q", got, token)
	}
}

func TestWriteEnvAddsTrailingNewlineBeforeAppending(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("FYNE_THEME=dark"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeEnv(path, map[string]string{"SCRIBBLE_TOKEN": "t"}); err != nil {
		t.Fatal(err)
	}

	env := readEnvFile(t, path)
	if env["FYNE_THEME"] != "dark" || env["SCRIBBLE_TOKEN"] != "t" {
		t.Errorf(".env holds %v", env)
	}
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
	w.SetContent(setupScreen(w, "SCRIBBLE_TOKEN is not set.", gui.Settings{URL: "https://notes.sudde.net"},
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
