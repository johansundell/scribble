package envfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johansundell/scribble/envfile"
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

func TestWriteCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")

	if err := envfile.Write(path, map[string]string{"SCRIBBLE_URL": "http://notes:8080", "SCRIBBLE_TOKEN": "secret"}); err != nil {
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

func TestWriteUpdatesKeysAndKeepsTheRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	old := "# my settings\nFYNE_THEME=dark\nexport SCRIBBLE_TOKEN=old\n\nSCRIBBLE_HOTKEY=none # no hotkey\nSCRIBBLE_TOKEN=older\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := envfile.Write(path, map[string]string{"SCRIBBLE_URL": "https://notes.example", "SCRIBBLE_TOKEN": "new"}); err != nil {
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

func TestWriteRoundTripsAwkwardValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	token := `a#b "c" \d $HOME 'e' !f`

	if err := envfile.Write(path, map[string]string{"SCRIBBLE_TOKEN": token}); err != nil {
		t.Fatal(err)
	}

	if got := readEnvFile(t, path)["SCRIBBLE_TOKEN"]; got != token {
		t.Errorf("SCRIBBLE_TOKEN reads back as %q, want %q", got, token)
	}
}

func TestWriteAddsTrailingNewlineBeforeAppending(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("FYNE_THEME=dark"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := envfile.Write(path, map[string]string{"SCRIBBLE_TOKEN": "t"}); err != nil {
		t.Fatal(err)
	}

	env := readEnvFile(t, path)
	if env["FYNE_THEME"] != "dark" || env["SCRIBBLE_TOKEN"] != "t" {
		t.Errorf(".env holds %v", env)
	}
}

func TestWriteReplacesTheFileInOneStep(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("SCRIBBLE_TOKEN=old\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	// A hard link keeps the old file: written in place, it would change too.
	if err := os.Link(path, filepath.Join(dir, "old")); err != nil {
		t.Skip("no hard links here:", err)
	}

	if err := envfile.Write(path, map[string]string{"SCRIBBLE_TOKEN": "new"}); err != nil {
		t.Fatal(err)
	}

	if old, _ := os.ReadFile(filepath.Join(dir, "old")); string(old) != "SCRIBBLE_TOKEN=old\n" {
		t.Errorf("the .env was written in place, not replaced: the old file now holds %q", old)
	}
	if got := readEnvFile(t, path)["SCRIBBLE_TOKEN"]; got != "new" {
		t.Errorf("SCRIBBLE_TOKEN = %q, want new", got)
	}
	if info, err := os.Stat(path); err != nil {
		t.Error(err)
	} else if info.Mode().Perm() != 0o640 {
		t.Errorf("the replaced .env has mode %v, want the old file's 0640", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the folder holds %v, want only .env and old", names)
	}
}

func TestWriteBacksUpAFileItCannotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	broken := "FYNE_THEME=dark\nSCRIBBLE_TOKEN=\"unterminated\n"
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := envfile.Write(path, map[string]string{"SCRIBBLE_URL": "https://notes.example"}); err != nil {
		t.Fatal(err)
	}

	bak, err := os.ReadFile(path + ".bak")
	if err != nil || string(bak) != broken {
		t.Errorf(".env.bak holds %q (%v), want the broken file as it was", bak, err)
	}
	if info, err := os.Stat(path + ".bak"); err == nil && info.Mode().Perm()&0o077 != 0 {
		t.Errorf(".env.bak has mode %v, want it private to the user", info.Mode().Perm())
	}
}

func TestWriteMakesNoBackupOfAGoodFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("FYNE_THEME=dark\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := envfile.Write(path, map[string]string{"SCRIBBLE_TOKEN": "t"}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Error("a .env.bak was made of a file that reads fine")
	}
}

func TestWriteKeepsTheFirstBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	broken := "SCRIBBLE_TOKEN=old\nFYNE_THEME=\"unterminated\n"
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tok := range []string{"one", "two"} {
		if err := envfile.Write(path, map[string]string{"SCRIBBLE_TOKEN": tok}); err != nil {
			t.Fatal(err)
		}
	}

	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != broken {
		t.Errorf(".env.bak holds %q after a second save, want the original %q", bak, broken)
	}
}

func TestWriteThroughASymlinkKeepsTheLink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.env")
	if err := os.WriteFile(real, []byte("FYNE_THEME=dark\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".env")
	if err := os.Symlink(real, path); err != nil {
		t.Skip("no symlinks here:", err)
	}

	if err := envfile.Write(path, map[string]string{"SCRIBBLE_TOKEN": "t"}); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Error(".env is no longer a symlink")
	}
	if env := readEnvFile(t, real); env["SCRIBBLE_TOKEN"] != "t" || env["FYNE_THEME"] != "dark" {
		t.Errorf("the linked file holds %v", env)
	}
}

func TestWriteRemovesKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("# keep\nA=1\nB=2\nexport C=3\nB=4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := envfile.Write(path, map[string]string{"D": "5"}, "B", "C", "missing"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "# keep\nA=1\nD=5\n"; string(got) != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
}
