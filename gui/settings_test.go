package gui_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/gui"
)

// settingsFields is the Server URL and Authenticator code fields of the
// dialog on top of w.
func settingsFields(t *testing.T, w fyne.Window) (url, code *widget.Entry) {
	t.Helper()
	top := w.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("no dialog is showing")
	}
	var entries []*widget.Entry
	for _, o := range test.LaidOutObjects(top) {
		if e, ok := o.(*widget.Entry); ok {
			entries = append(entries, e)
		}
	}
	if len(entries) != 2 {
		t.Fatalf("the dialog has %d text fields, want 2", len(entries))
	}
	return entries[0], entries[1]
}

// dialogButton is the button labelled text in the dialog on top of w.
func dialogButton(t *testing.T, w fyne.Window, text string) *widget.Button {
	t.Helper()
	top := w.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("no dialog is showing")
	}
	for _, o := range test.LaidOutObjects(top) {
		if b, ok := o.(*widget.Button); ok && b.Text == text {
			return b
		}
	}
	t.Fatalf("the dialog has no %q button", text)
	return nil
}

func TestSettingsMenuLogsInWithACode(t *testing.T) {
	c := newServer(t)
	ui, w := open(t, c)
	var saved []gui.Settings
	ui.SaveSettings = func(s gui.Settings) error { saved = append(saved, s); return nil }

	menuItem(t, w, "File", "Settings…").Action()
	url, code := settingsFields(t, w)
	if url.Text != c.BaseURL || code.Text != "" {
		t.Fatalf("the dialog shows URL %q and code %q, want the current %q and no code", url.Text, code.Text, c.BaseURL)
	}
	url.SetText("https://notes.example")
	code.SetText(" 123456 ")
	tapDialogButton(t, w, "Save")

	want := gui.Settings{URL: "https://notes.example", Code: "123456"}
	if !slices.Equal(saved, []gui.Settings{want}) {
		t.Fatalf("saved %v, want %v", saved, want)
	}
	if text := dialogText(w); !strings.Contains(text, "Restart Scribble") {
		t.Errorf("after logging in to another server, the dialog says %q; want it to ask for a restart", text)
	}
}

// Logging in again to the same server takes effect at once.
func TestSettingsLoginToTheSameServer(t *testing.T) {
	c := newServer(t)
	ui, w := open(t, c)
	ui.SaveSettings = func(gui.Settings) error { return nil }

	menuItem(t, w, "File", "Settings…").Action()
	_, code := settingsFields(t, w)
	code.SetText("123456")
	tapDialogButton(t, w, "Save")
	if text := dialogText(w); !strings.Contains(text, "Logged in") || strings.Contains(text, "Restart") {
		t.Errorf("after logging in again, the dialog says %q", text)
	}
}

func TestSettingsCancelSavesNothing(t *testing.T) {
	ui, w := open(t, newServer(t))
	ui.SaveSettings = func(gui.Settings) error { t.Error("settings were saved"); return nil }

	menuItem(t, w, "File", "Settings…").Action()
	url, code := settingsFields(t, w)
	url.SetText("https://elsewhere.example")
	code.SetText("123456")
	tapDialogButton(t, w, "Cancel")
}

func TestSettingsShowsSaveError(t *testing.T) {
	ui, w := open(t, newServer(t))
	ui.SaveSettings = func(gui.Settings) error { return errors.New("that code is wrong") }

	menuItem(t, w, "File", "Settings…").Action()
	_, code := settingsFields(t, w)
	code.SetText("123456")
	tapDialogButton(t, w, "Save")

	if text := dialogText(w); !strings.Contains(strings.ToLower(text), "that code is wrong") {
		t.Errorf("after a failed login, the dialog says %q; want the error", text)
	}
}

func TestSettingsSaveNeedsAWebAddressAndACode(t *testing.T) {
	w := test.NewTempWindow(t, nil)
	w.Resize(fyne.NewSize(600, 400))
	gui.ShowSettings(w, gui.Settings{URL: "https://notes.example"}, gui.Overridden{}, func(gui.Settings) error {
		t.Error("invalid settings were saved")
		return nil
	})
	url, code := settingsFields(t, w)
	save := dialogButton(t, w, "Save")

	for _, c := range []struct{ url, code string }{
		{"notes.example", "123456"},
		{"ftp://notes.example", "123456"},
		{"https://", "123456"},
		{"https://notes.example", ""},
		{"https://notes.example", "12345"},
		{"https://notes.example", "12345a"},
		{"https://notes.example", "1234567"},
	} {
		// Fyne reports a field's error once the field loses focus.
		w.Canvas().Focus(url)
		url.SetText(c.url)
		w.Canvas().Focus(code)
		code.SetText(c.code)
		w.Canvas().Unfocus()
		if !save.Disabled() {
			t.Errorf("Save is enabled for URL %q and code %q", c.url, c.code)
		}
	}
	url.SetText("http://localhost:8080")
	code.SetText("123 456")
	if save.Disabled() {
		t.Error("Save is disabled for valid settings")
	}
}

// envHints is the hint text about the environment under the fields of the dialog on top of w.
func envHints(w fyne.Window) []string {
	var out []string
	if top := w.Canvas().Overlays().Top(); top != nil {
		for _, o := range test.LaidOutObjects(top) {
			if t, ok := o.(*canvas.Text); ok && t.Visible() && strings.Contains(t.Text, "environment") {
				out = append(out, t.Text)
			}
		}
	}
	return out
}

func TestSettingsSaysWhichFieldsTheEnvironmentOverrides(t *testing.T) {
	ui, w := open(t, newServer(t))
	var saved []gui.Settings
	ui.SaveSettings = func(s gui.Settings) error { saved = append(saved, s); return nil }

	menuItem(t, w, "File", "Settings…").Action()
	if h := envHints(w); len(h) != 0 {
		t.Errorf("with nothing set in the environment, the dialog says %q", h)
	}
	tapDialogButton(t, w, "Cancel")

	ui.Overridden = gui.Overridden{Token: true}
	menuItem(t, w, "File", "Settings…").Action()
	h := envHints(w)
	if len(h) != 1 || !strings.Contains(h[0], "SCRIBBLE_TOKEN") {
		t.Fatalf("with SCRIBBLE_TOKEN set in the environment, the dialog says %q", h)
	}
	_, code := settingsFields(t, w)
	if code.Disabled() {
		t.Fatal("the overridden field cannot be changed")
	}
	code.SetText("123456")
	tapDialogButton(t, w, "Save")
	if len(saved) != 1 || saved[0].Code != "123456" {
		t.Errorf("saved %v, want the login saved for the .env file", saved)
	}
}
