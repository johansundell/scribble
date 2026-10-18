package main

import (
	"os"
	"path/filepath"
	"testing"
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
