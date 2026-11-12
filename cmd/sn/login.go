package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/envfile"
)

// stdin is where sn login reads its answers; tests replace it.
var stdin io.Reader = os.Stdin

// The variables sn login saves (#184, ADR 14). SCRIBBLE_TOKEN holds the
// access token, so a saved login reads like a static token, plus the
// refresh token that renews it and when each expires.
const (
	keyURL           = "SCRIBBLE_URL"
	keyToken         = "SCRIBBLE_TOKEN"
	keyRefresh       = "SCRIBBLE_REFRESH_TOKEN"
	keyExpiry        = "SCRIBBLE_TOKEN_EXPIRY"
	keyRefreshExpiry = "SCRIBBLE_REFRESH_EXPIRY"
)

// tokenKeys are the names, in lower case, sn reads the token settings
// under. Login and logout remove every spelling of them, so the saved
// login is the only one.
var tokenKeys = []string{"scribble_token", "token", "scribble_refresh_token", "refresh_token", "scribble_token_expiry", "token_expiry", "scribble_refresh_expiry", "refresh_expiry"}

// loginFile is the file sn login writes: the one sn read its settings
// from, else ~/.scribble.
func loginFile(cfg config) (string, error) {
	if cfg.Source != "" {
		return cfg.Source, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find your home folder for ~/.scribble: %w", err)
	}
	return filepath.Join(home, ".scribble"), nil
}

// cmdLogin asks for the server's address (args[0] when given, else read
// from in with cfg.URL as the default) and an authenticator code, logs in
// and saves the tokens.
func cmdLogin(ctx context.Context, cfg config, in io.Reader, out io.Writer, args []string) error {
	if len(args) > 1 {
		return errors.New("usage: sn login [<server url>]")
	}
	r := bufio.NewReader(in)
	url := cfg.URL
	if len(args) == 1 {
		url = args[0]
	} else {
		fmt.Fprintf(out, "Server URL [%s]: ", cfg.URL)
		answer, err := readLine(r)
		if err != nil {
			return err
		}
		if answer != "" {
			url = answer
		}
	}
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	fmt.Fprint(out, "Authenticator code: ")
	code, err := readLine(r)
	if err != nil {
		return err
	}
	if code == "" {
		return errors.New("no code entered")
	}

	c := client.New(url, "")
	c.Name, c.Version, c.ClientID = "cli", Version, client.ClientCLI
	tok, err := c.Login(ctx, code)
	if client.IsWrongCode(err) {
		return errors.New("that code is wrong or was already used; wait for the next one and try again")
	}
	if err != nil {
		return err
	}
	path, err := loginFile(cfg)
	if err != nil {
		return err
	}
	if err := saveLogin(path, url, tok); err != nil {
		return fmt.Errorf("logged in, but cannot save the login: %w", err)
	}
	fmt.Fprintf(out, "Logged in to %s; saved in %s\n", url, sourceLabel(path))
	if os.Getenv("SCRIBBLE_TOKEN") != "" {
		fmt.Fprintln(out, "Note: SCRIBBLE_TOKEN is set in the environment and is used instead; unset it to use this login.")
	}
	return nil
}

// readLine reads one line from r, trimmed. The last line may lack its
// newline; no line at all is an error.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if errors.Is(err, io.EOF) && line != "" {
		err = nil
	}
	if errors.Is(err, io.EOF) {
		return "", errors.New("no answer (end of input)")
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// saveLogin writes url and tok to the file at path, replacing every
// earlier URL and token setting, and makes the file readable only by its
// owner, as it holds the tokens.
func saveLogin(path, url string, tok client.Tokens) error {
	remove, err := keysIn(path, append([]string{"scribble_url", "url"}, tokenKeys...))
	if err != nil {
		return err
	}
	vars := tokenVars(tok)
	vars[keyURL] = url
	if err := envfile.Write(path, vars, remove...); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// saveTokens writes renewed tokens to the file at path.
func saveTokens(path string, tok client.Tokens) error {
	remove, err := keysIn(path, tokenKeys)
	if err != nil {
		return err
	}
	return envfile.Write(path, tokenVars(tok), remove...)
}

func tokenVars(tok client.Tokens) map[string]string {
	vars := map[string]string{keyToken: tok.Access, keyRefresh: tok.Refresh}
	if !tok.Expiry.IsZero() {
		vars[keyExpiry] = tok.Expiry.UTC().Format(time.RFC3339)
	}
	if !tok.RefreshExpiry.IsZero() {
		vars[keyRefreshExpiry] = tok.RefreshExpiry.UTC().Format(time.RFC3339)
	}
	return vars
}

// keysIn returns the variables set in the file at path whose lower-case
// name is in names, spelled as there, except the canonical names sn
// writes, which envfile.Write then updates in place. A missing file has
// none.
func keysIn(path string, names []string) ([]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	canonical := map[string]bool{keyURL: true, keyToken: true, keyRefresh: true, keyExpiry: true, keyRefreshExpiry: true}
	var keys []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sc.Text()), "export "))
		key, _, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if ok && want[strings.ToLower(key)] && !canonical[key] {
			keys = append(keys, key)
		}
	}
	return keys, sc.Err()
}

// cmdLogout removes the saved login, and any static token, from the file
// sn read its settings from, keeping the URL. The tokens are not revoked
// on the server; they expire.
func cmdLogout(cfg config, out io.Writer) error {
	if cfg.Source == "" {
		fmt.Fprintln(out, "Not logged in: there is no settings file.")
		return nil
	}
	remove, err := keysIn(cfg.Source, tokenKeys)
	if err != nil {
		return err
	}
	remove = append(remove, keyToken, keyRefresh, keyExpiry, keyRefreshExpiry)
	if err := envfile.Write(cfg.Source, nil, remove...); err != nil {
		return err
	}
	fmt.Fprintf(out, "Logged out; removed the tokens from %s\n", sourceLabel(cfg.Source))
	return nil
}
