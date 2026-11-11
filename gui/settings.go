package gui

import (
	"errors"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// Settings is what the Settings dialog asks for to log in (#184, ADR 14):
// the server's address and a code from the authenticator app.
type Settings struct {
	URL  string // the server's address
	Code string // a 6-digit authenticator code, spaces removed
}

// Overridden marks the settings scribble-gui's environment set when it
// started. Those win over the .env file Settings saves to (#137).
type Overridden struct{ URL, Token bool }

// ShowSettings opens the Settings dialog over w, with the address filled
// in from current. Each field overridden marks notes that the environment
// overrides it; it can still be changed, for the .env file. Save calls
// save with what was entered and shows any error it returns.
func ShowSettings(w fyne.Window, current Settings, overridden Overridden, save func(Settings) error) {
	addr := widget.NewEntry()
	addr.SetText(current.URL)
	addr.Validator = validateServerURL
	code := widget.NewEntry()
	code.SetPlaceHolder("123456")
	code.Validator = validateCode
	addrItem := widget.NewFormItem("Server URL", addr)
	if overridden.URL {
		addrItem.HintText = overrideHint("SCRIBBLE_URL")
	}
	codeItem := widget.NewFormItem("Authenticator code", code)
	codeItem.HintText = "From the authenticator app you set up for the web app"
	if overridden.Token {
		codeItem.HintText = overrideHint("SCRIBBLE_TOKEN")
	}
	d := dialog.NewForm("Settings", "Save", "Cancel",
		[]*widget.FormItem{addrItem, codeItem},
		func(ok bool) {
			if !ok {
				return
			}
			s := Settings{URL: strings.TrimSpace(addr.Text), Code: normalizeCode(code.Text)}
			if err := save(s); err != nil {
				dialog.ShowError(err, w)
			}
		}, w)
	d.Resize(fyne.NewSize(450, d.MinSize().Height))
	d.Show()
	w.Canvas().Focus(addr)
}

// overrideHint is the note under a field whose variable name is set in the
// environment.
func overrideHint(name string) string {
	return name + " is set in the environment, which wins over this from the next start"
}

// validateServerURL accepts an http or https address with a host.
func validateServerURL(s string) error {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("enter an address such as https://notes.example.com")
	}
	return nil
}

// normalizeCode drops the spaces apps show in the middle of a code.
func normalizeCode(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// validateCode accepts 6 digits, spaces aside.
func validateCode(s string) error {
	c := normalizeCode(s)
	if len(c) != 6 || strings.Trim(c, "0123456789") != "" {
		return errors.New("enter the 6-digit code from your authenticator app")
	}
	return nil
}

// showSettings opens the Settings dialog for File > Settings…. Logging in
// again to the same server takes effect at once; another server needs a
// restart.
func (ui *UI) showSettings() {
	if ui.SaveSettings == nil {
		return
	}
	current := Settings{URL: ui.c.BaseURL}
	ShowSettings(ui.w, current, ui.Overridden, func(s Settings) error {
		if err := ui.SaveSettings(s); err != nil {
			return err
		}
		if strings.TrimRight(s.URL, "/") == current.URL {
			dialog.ShowInformation("Logged in", "Logged in to "+s.URL+".", ui.w)
			return nil
		}
		dialog.ShowInformation("Settings saved", "Logged in. Restart Scribble to connect to "+s.URL+".", ui.w)
		return nil
	})
}
