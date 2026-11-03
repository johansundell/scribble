package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/johansundell/scribble/client"
	"github.com/joho/godotenv"
)

// config is the server and token sn uses, and the file they were read
// from ("" when none was).
type config struct {
	URL, Token, Source string
}

// configFiles are the files sn reads its settings from, first found wins
// (#165): ~/.scribble, then scribble/.env in the user's config folder.
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
		vars, err := godotenv.Parse(f)
		f.Close()
		if err != nil {
			return cfg, fmt.Errorf("cannot read %s: %w", path, err)
		}
		cfg.Source = path
		cfg.URL = setting(vars, "SCRIBBLE_URL", "url")
		cfg.Token = setting(vars, "SCRIBBLE_TOKEN", "token")
		break
	}
	if v := getenv("SCRIBBLE_URL"); v != "" {
		cfg.URL = v
	}
	if v := getenv("SCRIBBLE_TOKEN"); v != "" {
		cfg.Token = v
	}
	if cfg.URL == "" {
		cfg.URL = client.DefaultURL
	}
	return cfg, nil
}

// setting is the value of the first of names in vars, ignoring case.
func setting(vars map[string]string, names ...string) string {
	for _, name := range names {
		for k, v := range vars {
			if strings.EqualFold(k, name) {
				return v
			}
		}
	}
	return ""
}

// displayPath is path with the home folder shown as ~, as sn version
// prints it.
func displayPath(path string) string {
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
