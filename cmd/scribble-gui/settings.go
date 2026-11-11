package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/envfile"
	"github.com/johansundell/scribble/gui"
)

// The variables a login is saved in (#184, ADR 14). SCRIBBLE_TOKEN holds
// the access token, as it held AUTH_TOKEN before, with the refresh token
// and expiry that renew it beside it.
const (
	envURL     = "SCRIBBLE_URL"
	envToken   = "SCRIBBLE_TOKEN"
	envRefresh = "SCRIBBLE_REFRESH_TOKEN"
	envExpiry  = "SCRIBBLE_TOKEN_EXPIRY"
)

// login logs in to s.URL with the authenticator code in s as
// scribble-gui version ver, and saves the login to the .env file at path.
func login(path, ver string, s gui.Settings) (client.Tokens, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	url := strings.TrimRight(s.URL, "/")
	c := client.New(url, "")
	c.Name, c.Version, c.ClientID = "gui", ver, client.ClientGUI
	tok, err := c.Login(ctx, s.Code)
	if client.IsWrongCode(err) {
		return tok, errors.New("that code is wrong or was already used; wait for the next one and try again")
	}
	if err != nil {
		return tok, err
	}
	return tok, saveLogin(path, url, tok)
}

// saveLogin writes url and tok to the .env file at path and sets them in
// this process's environment too.
func saveLogin(path, url string, tok client.Tokens) error {
	vars := tokenVars(tok)
	vars[envURL] = url
	return saveVars(path, vars)
}

// saveTokens writes renewed tokens as saveLogin does.
func saveTokens(path string, tok client.Tokens) error {
	return saveVars(path, tokenVars(tok))
}

func tokenVars(tok client.Tokens) map[string]string {
	vars := map[string]string{envToken: tok.Access, envRefresh: tok.Refresh, envExpiry: ""}
	if !tok.Expiry.IsZero() {
		vars[envExpiry] = tok.Expiry.UTC().Format(time.RFC3339)
	}
	return vars
}

func saveVars(path string, vars map[string]string) error {
	if err := envfile.Write(path, vars); err != nil {
		return err
	}
	for k, v := range vars {
		os.Setenv(k, v)
	}
	return nil
}

// savedLogin is the login in the environment, which holds the .env file
// once it is loaded. A SCRIBBLE_TOKEN set in the environment from the start
// (overridden) is a static token, which the saved refresh token does not
// belong to.
func savedLogin(overridden gui.Overridden) client.Tokens {
	tok := client.Tokens{Access: os.Getenv(envToken)}
	if !overridden.Token {
		tok.Refresh = os.Getenv(envRefresh)
		// An unreadable expiry is zero: the token is used until refused.
		tok.Expiry, _ = time.Parse(time.RFC3339, os.Getenv(envExpiry))
	}
	return tok
}

// envOverrides marks the settings set in this process's environment. Call
// it before loading the .env file, which sets the rest.
func envOverrides() gui.Overridden {
	_, url := os.LookupEnv("SCRIBBLE_URL")
	_, token := os.LookupEnv("SCRIBBLE_TOKEN")
	return gui.Overridden{URL: url, Token: token}
}

// setupScreen is the window's content while scribble-gui cannot reach a
// server yet: msg, and a Settings… button that opens the Settings dialog
// filled in with current and noting the fields overridden marks. Once save
// succeeds it calls saved.
func setupScreen(w fyne.Window, msg string, current gui.Settings, overridden gui.Overridden, save func(gui.Settings) error, saved func()) fyne.CanvasObject {
	label := widget.NewLabel(msg)
	label.Wrapping = fyne.TextWrapWord
	return container.NewVBox(label, container.NewHBox(
		widget.NewButton("Settings…", func() {
			gui.ShowSettings(w, current, overridden, func(s gui.Settings) error {
				if err := save(s); err != nil {
					return err
				}
				saved()
				return nil
			})
		}),
	))
}
