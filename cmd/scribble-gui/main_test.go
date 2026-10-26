package main

import (
	"bytes"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"github.com/johansundell/scribble/offline"
)

func TestNewAppLoadsDotEnvBeforeCreatingTheApp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("FYNE_THEME=dark\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FYNE_THEME", "")
	os.Unsetenv("FYNE_THEME")

	var themeAtCreate string
	a, envErr := newApp(func() error { return loadEnv(dir) }, func() fyne.App {
		themeAtCreate = os.Getenv("FYNE_THEME")
		return test.NewTempApp(t)
	})
	if envErr != nil {
		t.Fatal(envErr)
	}
	if a == nil {
		t.Fatal("newApp returned no app")
	}
	if themeAtCreate != "dark" {
		t.Errorf("FYNE_THEME when the app was created = %q, want %q", themeAtCreate, "dark")
	}
}

func TestNewAppStillCreatesTheAppWhenDotEnvIsUnreadable(t *testing.T) {
	dir := t.TempDir()
	// A directory named .env cannot be read as a file.
	if err := os.Mkdir(filepath.Join(dir, ".env"), 0o700); err != nil {
		t.Fatal(err)
	}

	a, envErr := newApp(func() error { return loadEnv(dir) }, func() fyne.App {
		return test.NewTempApp(t)
	})
	if envErr == nil {
		t.Error("unreadable .env: got no error")
	}
	if a == nil {
		t.Fatal("newApp returned no app; the error could not be shown")
	}
}

func TestLoadEnvReadsDotEnvInDir(t *testing.T) {
	dir := t.TempDir()
	env := "SCRIBBLE_URL=http://notes:8080\nSCRIBBLE_TOKEN=from-file\nSCRIBBLE_HOTKEY=none\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCRIBBLE_URL", "")
	os.Unsetenv("SCRIBBLE_URL")
	t.Setenv("SCRIBBLE_HOTKEY", "")
	os.Unsetenv("SCRIBBLE_HOTKEY")
	t.Setenv("SCRIBBLE_TOKEN", "from-environment")

	if err := loadEnv(dir); err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]string{
		"SCRIBBLE_URL":    "http://notes:8080",
		"SCRIBBLE_HOTKEY": "none",
		"SCRIBBLE_TOKEN":  "from-environment", // already set: the environment wins
	} {
		if got := os.Getenv(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestLoadEnvWithoutDotEnvIsFine(t *testing.T) {
	if err := loadEnv(t.TempDir()); err != nil {
		t.Fatalf("missing .env: %v", err)
	}
}

func TestOpenCacheLocksAndPreventsSecondInstance(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	first, err := openCache("http://localhost:8080")
	if err != nil {
		t.Fatalf("first openCache: %v", err)
	}
	defer first.Close()

	second, err := openCache("http://localhost:8080")
	if err == nil {
		_ = second.Close()
		t.Fatal("second openCache succeeded, want ErrLocked")
	}
	if !errors.Is(err, offline.ErrLocked) {
		t.Fatalf("second openCache error %v, want ErrLocked", err)
	}
}

// appWithIcon keeps its icon, which the test app drops.
type appWithIcon struct {
	fyne.App
	icon fyne.Resource
}

func (a *appWithIcon) Icon() fyne.Resource     { return a.icon }
func (a *appWithIcon) SetIcon(r fyne.Resource) { a.icon = r }

func TestNewAppSetsTheAppIcon(t *testing.T) {
	a, _ := newApp(func() error { return nil }, func() fyne.App { return &appWithIcon{App: test.NewTempApp(t)} })
	icon := a.Icon()
	if icon == nil {
		t.Fatal("the app has no icon")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(icon.Content()))
	if err != nil {
		t.Fatalf("the app icon is not a PNG: %v", err)
	}
	if cfg.Width != 512 || cfg.Height != 512 {
		t.Errorf("the app icon is %dx%d, want 512x512", cfg.Width, cfg.Height)
	}
}
