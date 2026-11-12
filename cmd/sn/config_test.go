package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joho/godotenv"
)

// TestMain points the home and config folders at an empty directory, so no
// test reads the ~/.scribble of whoever runs them. With SN_TEST_AS_SN=1 the
// test binary is sn itself, so script tests can run it as their sn.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "sn-home-*")
	if err != nil {
		panic(err)
	}
	for _, k := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "AppData"} {
		os.Setenv(k, home)
	}
	if os.Getenv("SN_TEST_AS_SN") == "1" {
		main()
		os.RemoveAll(home)
		os.Exit(0)
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func writeFile(t *testing.T, path, text string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolveConfigPrecedence(t *testing.T) {
	dir := t.TempDir()
	dot := writeFile(t, filepath.Join(dir, "dot"), "SCRIBBLE_URL=https://dot\nSCRIBBLE_TOKEN=dot-tok\n")
	gui := writeFile(t, filepath.Join(dir, "gui.env"), "SCRIBBLE_URL=https://gui\nSCRIBBLE_TOKEN=gui-tok\n")
	missing := filepath.Join(dir, "missing")

	cases := []struct {
		name       string
		env        map[string]string
		files      []string
		url, token string
		source     string
	}{
		{"environment wins over the file", map[string]string{"SCRIBBLE_URL": "https://env", "SCRIBBLE_TOKEN": "env-tok"},
			[]string{dot, gui}, "https://env", "env-tok", dot},
		{"environment fills one key, the file the other", map[string]string{"SCRIBBLE_TOKEN": "env-tok"},
			[]string{dot, gui}, "https://dot", "env-tok", dot},
		{"an empty variable counts as unset", map[string]string{"SCRIBBLE_URL": ""},
			[]string{dot}, "https://dot", "dot-tok", dot},
		{"the dotfile wins over the GUI file", nil, []string{dot, gui}, "https://dot", "dot-tok", dot},
		{"falls back to the GUI file", nil, []string{missing, gui}, "https://gui", "gui-tok", gui},
		{"defaults without files", nil, []string{missing}, "https://notes.sudde.net", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := resolveConfig(envOf(tc.env), tc.files)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.URL != tc.url || cfg.Token != tc.token || cfg.Source != tc.source {
				t.Errorf("got %+v, want url %q token %q source %q", cfg, tc.url, tc.token, tc.source)
			}
		})
	}
}

func TestResolveConfigKeys(t *testing.T) {
	cases := []struct {
		name, text, url, token string
	}{
		{"short names", "url=https://a\ntoken=t\n", "https://a", "t"},
		{"any case", "Url=https://a\nTOKEN=t\nscribble_url=https://b\n", "https://b", "t"},
		{"long names win over short ones", "SCRIBBLE_TOKEN=long\ntoken=short\n", "https://notes.sudde.net", "long"},
		{"comments, blank lines and spaces", "# my server\n\n  url = https://a  \n#token=no\ntoken=t\n", "https://a", "t"},
		{"quoted values, as the GUI writes them", "SCRIBBLE_URL=\"https://a\"\nSCRIBBLE_TOKEN='t'\n", "https://a", "t"},
		{"other keys are ignored", "EDITOR=vim\ntoken=t\n", "https://notes.sudde.net", "t"},
		{"$ is not expanded", "HOME=x\ntoken=a$HOME\n", "https://notes.sudde.net", "a$HOME"},
		{"a later line wins, whatever its case", "URL=https://a\nurl=https://b\nUrl=https://c\n", "https://c", ""},
		{"GUI escapes are undone", `SCRIBBLE_TOKEN="a\$b\"c\\d"`, "https://notes.sudde.net", `a$b"c\d`},
		{"lines without = are skipped", "nonsense\ntoken=t\n", "https://notes.sudde.net", "t"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFile(t, filepath.Join(t.TempDir(), "cfg"), tc.text)
			cfg, err := resolveConfig(envOf(nil), []string{path})
			if err != nil {
				t.Fatal(err)
			}
			if cfg.URL != tc.url || cfg.Token != tc.token {
				t.Errorf("got url %q token %q, want %q %q", cfg.URL, cfg.Token, tc.url, tc.token)
			}
		})
	}
}

// A config file that exists but can't be used is reported, naming it,
// rather than silently skipped for the next one.
func TestResolveConfigUnreadableFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a-directory")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := resolveConfig(envOf(nil), []string{dir})
	if err == nil || !strings.Contains(err.Error(), dir) {
		t.Errorf("got %v, want an error naming %s", err, dir)
	}
}

func TestConfigFiles(t *testing.T) {
	home, _ := os.UserHomeDir()
	conf, _ := os.UserConfigDir()
	want := []string{filepath.Join(home, ".scribble"), filepath.Join(conf, "scribble", ".env")}
	got := configFiles()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDisplayPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	for in, want := range map[string]string{
		"":                                   "none (environment / defaults)",
		filepath.Join(home, ".scribble"):     "~" + string(filepath.Separator) + ".scribble",
		filepath.Join(home, ".config", "x"):  filepath.Join("~", ".config", "x"),
		filepath.Join(home+"x", ".scribble"): filepath.Join(home+"x", ".scribble"),
	} {
		if got := sourceLabel(in); got != want {
			t.Errorf("sourceLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// sn reads ~/.scribble without anything in the environment, and version
// says where the settings came from.
func TestRunReadsTheDotfile(t *testing.T) {
	srvURL := serveVersion(t, "v0.0.12", 0)
	t.Setenv("SCRIBBLE_URL", "")
	t.Setenv("SCRIBBLE_TOKEN", "")
	home, _ := os.UserHomeDir()
	path := writeFile(t, filepath.Join(home, ".scribble"), "url="+srvURL+"\ntoken=tok\n")
	t.Cleanup(func() { os.Remove(path) })
	setVersion(t, "v0.0.12")

	var out bytes.Buffer
	if err := runWith(&out, []string{"version"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"url: " + srvURL + "\n", "token: static (never expires)\n", "config: " + filepath.Join("~", ".scribble") + "\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("version printed\n%s\nwant it to contain %q", out.String(), want)
		}
	}
	if err := runWith(&out, []string{"ls"}); err != nil {
		t.Errorf("ls with the token from ~/.scribble: %v", err)
	}
}

func TestRunReadsTheGUIConfig(t *testing.T) {
	srvURL := serveVersion(t, "v0.0.12", 0)
	t.Setenv("SCRIBBLE_URL", "")
	t.Setenv("SCRIBBLE_TOKEN", "")
	conf, _ := os.UserConfigDir()
	path := writeFile(t, filepath.Join(conf, "scribble", ".env"), "SCRIBBLE_URL="+srvURL+"\nSCRIBBLE_TOKEN=tok\n")
	t.Cleanup(func() { os.Remove(path) })

	var out bytes.Buffer
	if err := runWith(&out, []string{"version"}); err != nil {
		t.Fatal(err)
	}
	if want := "config: " + sourceLabel(path) + "\n"; !strings.Contains(out.String(), want) {
		t.Errorf("version printed\n%s\nwant it to contain %q", out.String(), want)
	}
}

func TestMissingTokenNamesTheDotfile(t *testing.T) {
	t.Setenv("SCRIBBLE_URL", "http://127.0.0.1:1")
	t.Setenv("SCRIBBLE_TOKEN", "")
	err := runWith(&bytes.Buffer{}, []string{"ls"})
	if err == nil || !strings.Contains(err.Error(), "~/.scribble") {
		t.Errorf("got %v, want it to point at ~/.scribble", err)
	}
}

// A token the GUI saves (with godotenv.Marshal) reads back unchanged.
func TestParseConfigReadsWhatTheGUIWrites(t *testing.T) {
	for _, tok := range []string{"plain", `a$b"c\d!e` + "`f", "with space", "#hash", "x='y'"} {
		line, err := godotenv.Marshal(map[string]string{"SCRIBBLE_TOKEN": tok})
		if err != nil {
			t.Fatal(err)
		}
		vars, err := parseConfig(strings.NewReader(line + "\n"))
		if err != nil {
			t.Fatal(err)
		}
		if got := vars["scribble_token"]; got != tok {
			t.Errorf("wrote %q as %s, read back %q", tok, line, got)
		}
	}
}
