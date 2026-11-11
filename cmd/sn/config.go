package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/johansundell/scribble/client"
)

// config is the server and token sn uses, and the file they were read
// from ("" when none was). After sn login (#184), Token is an OAuth2 access
// token that expires at Expiry and Refresh renews; with a static token
// (AUTH_TOKEN) Refresh is "" and Expiry zero.
type config struct {
	URL, Token, Source string
	Refresh            string
	Expiry             time.Time
}

// configFiles are the files sn reads its settings from, first found wins
// (#165): ~/.scribble, then scribble/.env in the user's config folder.
// Only the first that exists is read, even if it lacks a setting.
func configFiles() []string {
	var files []string
	if home, err := os.UserHomeDir(); err == nil {
		files = append(files, filepath.Join(home, ".scribble"))
	}
	if dir, err := os.UserConfigDir(); err == nil {
		files = append(files, filepath.Join(dir, "scribble", ".env"))
	}
	return files
}

// resolveConfig reads the first of files that exists and takes each
// setting from getenv when it is set and not empty, else from that file,
// else the default. A file that exists but cannot be read is an error.
func resolveConfig(getenv func(string) string, files []string) (config, error) {
	var cfg config
	for _, path := range files {
		f, err := os.Open(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return cfg, fmt.Errorf("cannot read %s: %w", path, err)
		}
		vars, err := parseConfig(f)
		f.Close()
		if err != nil {
			return cfg, fmt.Errorf("cannot read %s: %w", path, err)
		}
		cfg.Source = path
		cfg.URL = setting(vars, "scribble_url", "url")
		cfg.Token = setting(vars, "scribble_token", "token")
		cfg.Refresh = setting(vars, "scribble_refresh_token", "refresh_token")
		// An unreadable expiry is zero: the token is used until refused.
		cfg.Expiry, _ = time.Parse(time.RFC3339, setting(vars, "scribble_token_expiry", "token_expiry"))
		break
	}
	if v := getenv("SCRIBBLE_URL"); v != "" {
		cfg.URL = v
	}
	if v := getenv("SCRIBBLE_TOKEN"); v != "" {
		// A token from the environment is static: the saved login's
		// refresh token doesn't belong to it.
		cfg.Token, cfg.Refresh, cfg.Expiry = v, "", time.Time{}
	}
	if cfg.URL == "" {
		cfg.URL = client.DefaultURL
	}
	return cfg, nil
}

// parseConfig reads KEY=VALUE lines, skipping blank lines and lines
// starting with #. Keys are lower-cased and a later line wins. Values are
// taken as written, without expanding $VAR, except that one pair of
// surrounding quotes is removed; inside double quotes a backslash escapes
// the next character, as the GUI writes them (godotenv.Marshal).
func parseConfig(r io.Reader) (map[string]string, error) {
	vars := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vars[strings.ToLower(strings.TrimSpace(key))] = unquote(strings.TrimSpace(value))
	}
	return vars, sc.Err()
}

func unquote(v string) string {
	if len(v) < 2 || v[0] != v[len(v)-1] || (v[0] != '"' && v[0] != '\'') {
		return v
	}
	q, v := v[0], v[1:len(v)-1]
	if q == '\'' {
		return v
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] == '\\' && i+1 < len(v) {
			i++
			switch v[i] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			default:
				b.WriteByte(v[i])
			}
			continue
		}
		b.WriteByte(v[i])
	}
	return b.String()
}

// setting is the value of the first of names (lower case) in vars.
func setting(vars map[string]string, names ...string) string {
	for _, name := range names {
		if v, ok := vars[name]; ok {
			return v
		}
	}
	return ""
}

// sourceLabel is how sn version names the config file at path: with the
// home folder shown as ~, or "none" when there was no file.
func sourceLabel(path string) string {
	if path == "" {
		return "none (environment / defaults)"
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.Join("~", rel)
		}
	}
	return path
}
