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

// settingsFields is the Server URL and Auth token fields of the dialog on
// top of w.
func settingsFields(t *testing.T, w fyne.Window) (url, token *widget.Entry) {
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

func TestSettingsMenuSavesNewURLAndToken(t *testing.T) {
	c := newServer(t)
	ui, w := open(t, c)
	var saved []gui.Settings
	ui.SaveSettings = func(s gui.Settings) error { saved = append(saved, s); return nil }

	menuItem(t, w, "File", "Settings…").Action()
	url, token := settingsFields(t, w)
	if url.Text != c.BaseURL || token.Text != "tok" {
		t.Fatalf("the dialog shows URL %q and token %q, want the current %q and %q", url.Text, token.Text, c.BaseURL, "tok")
	}
	if !token.Password {
		t.Error("the token field shows the token in the clear")
	}
	url.SetText("https://notes.example")
	token.SetText("new-token")
	tapDialogButton(t, w, "Save")

	want := gui.Settings{URL: "https://notes.example", Token: "new-token"}
	if !slices.Equal(saved, []gui.Settings{want}) {
		t.Fatalf("saved %v, want %v", saved, want)
	}
	if text := dialogText(w); !strings.Contains(text, "Restart Scribble") {
		t.Errorf("after saving, the dialog says %q; want it to ask for a restart", text)
	}
}

func TestSettingsCancelOrNoChangeSavesNothing(t *testing.T) {
	ui, w := open(t, newServer(t))
	ui.SaveSettings = func(gui.Settings) error { t.Error("settings were saved"); return nil }

	menuItem(t, w, "File", "Settings…").Action()
	url, _ := settingsFields(t, w)
	url.SetText("https://elsewhere.example")
	tapDialogButton(t, w, "Cancel")

	menuItem(t, w, "File", "Settings…").Action()
	tapDialogButton(t, w, "Save")
	if top := w.Canvas().Overlays().Top(); top != nil {
		t.Errorf("saving unchanged settings left a dialog open: %q", dialogText(w))
	}
}

func TestSettingsShowsSaveError(t *testing.T) {
	ui, w := open(t, newServer(t))
	ui.SaveSettings = func(gui.Settings) error { return errors.New("disk full") }

	menuItem(t, w, "File", "Settings…").Action()
	_, token := settingsFields(t, w)
	token.SetText("other")
	tapDialogButton(t, w, "Save")

	if text := dialogText(w); !strings.Contains(strings.ToLower(text), "disk full") {
		t.Errorf("after a failed save, the dialog says %q; want the error", text)
	}
}

func TestSettingsSaveNeedsAWebAddressAndAToken(t *testing.T) {
	w := test.NewTempWindow(t, nil)
	w.Resize(fyne.NewSize(600, 400))
	gui.ShowSettings(w, gui.Settings{URL: "https://notes.example", Token: "tok"}, gui.Overridden{}, func(gui.Settings) error {
		t.Error("invalid settings were saved")
		return nil
	})
	url, token := settingsFields(t, w)
	save := dialogButton(t, w, "Save")

	for _, c := range []struct{ url, token string }{
		{"notes.example", "tok"},
		{"ftp://notes.example", "tok"},
		{"https://", "tok"},
		{"https://notes.example", ""},
	} {
		// Fyne reports a field's error once the field loses focus.
		w.Canvas().Focus(url)
		url.SetText(c.url)
		w.Canvas().Focus(token)
		token.SetText(c.token)
		w.Canvas().Unfocus()
		if !save.Disabled() {
			t.Errorf("Save is enabled for URL %q and token %q", c.url, c.token)
		}
	}
	url.SetText("http://localhost:8080")
	token.SetText("tok")
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
	_, token := settingsFields(t, w)
	if token.Disabled() {
		t.Fatal("the overridden field cannot be changed")
	}
	token.SetText("for-the-file")
	tapDialogButton(t, w, "Save")
	if len(saved) != 1 || saved[0].Token != "for-the-file" {
		t.Errorf("saved %v, want the new token saved for the .env file", saved)
	}
}
