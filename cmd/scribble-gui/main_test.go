package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/johansundell/scribble/offline"
)

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
