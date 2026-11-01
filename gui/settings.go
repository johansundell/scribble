package gui

import (
	"errors"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// Settings is how scribble-gui reaches its server: SCRIBBLE_URL and
// SCRIBBLE_TOKEN.
type Settings struct {
	URL   string // the server's address
	Token string // the server's AUTH_TOKEN
}

// ShowSettings opens the Settings dialog over w, filled in with current.
// Save calls save with what was entered and shows any error it returns.
func ShowSettings(w fyne.Window, current Settings, save func(Settings) error) {
	addr := widget.NewEntry()
	addr.SetText(current.URL)
	addr.Validator = validateServerURL
	token := widget.NewPasswordEntry()
	token.SetText(current.Token)
	token.Validator = func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New("enter the server's AUTH_TOKEN")
		}
		return nil
	}
	d := dialog.NewForm("Settings", "Save", "Cancel",
		[]*widget.FormItem{
			widget.NewFormItem("Server URL", addr),
			widget.NewFormItem("Auth token", token),
		},
		func(ok bool) {
			s := Settings{URL: strings.TrimSpace(addr.Text), Token: strings.TrimSpace(token.Text)}
			if !ok {
				return
			}
			if err := save(s); err != nil {
				dialog.ShowError(err, w)
			}
		}, w)
	d.Resize(fyne.NewSize(450, d.MinSize().Height))
	d.Show()
	w.Canvas().Focus(addr)
}

// validateServerURL accepts an http or https address with a host.
func validateServerURL(s string) error {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("enter an address such as https://notes.example.com")
	}
	return nil
}

// showSettings opens the Settings dialog for File > Settings…. The app
// is already connected, so new settings take effect after a restart.
func (ui *UI) showSettings() {
	if ui.SaveSettings == nil {
		return
	}
	current := Settings{URL: ui.c.BaseURL, Token: ui.c.Token}
	ShowSettings(ui.w, current, func(s Settings) error {
		if s == current {
			return nil
		}
		if err := ui.SaveSettings(s); err != nil {
			return err
		}
		dialog.ShowInformation("Settings saved", "Restart Scribble to connect with the new settings.", ui.w)
		return nil
	})
}
