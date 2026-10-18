package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/johansundell/scribble/types"
	"github.com/johansundell/scribble/utils"
)

func unsetEnv(keys ...string) func() {
	orig := make(map[string]string)
	has := make(map[string]bool)
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			orig[k] = v
			has[k] = true
		}
		os.Unsetenv(k)
	}
	return func() {
		for _, k := range keys {
			if has[k] {
				os.Setenv(k, orig[k])
			} else {
				os.Unsetenv(k)
			}
		}
	}
}

func TestLoadSettings(t *testing.T) {
	defer unsetEnv("PORT", "DEBUG")()

	// Create a temporary env file
	tmpEnv, err := os.CreateTemp("", ".env.*")
	if err != nil {
		t.Fatalf("Failed to create temp env file: %v", err)
	}
	defer os.Remove(tmpEnv.Name())

	_, err = tmpEnv.WriteString("PORT=:9999\nDEBUG=false\n")
	if err != nil {
		t.Fatalf("Failed to write to temp env file: %v", err)
	}
	tmpEnv.Close()

	loadSettings(tmpEnv.Name())

	if settings.Port != ":9999" {
		t.Errorf("expected settings.Port :9999, got %s", settings.Port)
	}
	if settings.Debug != false {
		t.Errorf("expected settings.Debug false, got %v", settings.Debug)
	}

	// Restore original settings
	loadSettings()
}

func TestLoadSettings_Precedence(t *testing.T) {
	t.Setenv("PORT", ":8888") // Real environment
	
	tmpEnv, err := os.CreateTemp("", ".env.*")
	if err != nil {
		t.Fatalf("Failed to create temp env file: %v", err)
	}
	defer os.Remove(tmpEnv.Name())

	_, err = tmpEnv.WriteString("PORT=:9999\n") // .env file
	if err != nil {
		t.Fatalf("Failed to write to temp env file: %v", err)
	}
	tmpEnv.Close()

	loadSettings(tmpEnv.Name())

	if settings.Port != ":8888" {
		t.Errorf("expected settings.Port :8888 (real env wins), got %s", settings.Port)
	}
}

func TestLoadSettings_NoDefaultAuthToken(t *testing.T) {
	defer unsetEnv("AUTH_TOKEN", "PORT")()

	tmpEnv, err := os.CreateTemp("", ".env.*")
	if err != nil {
		t.Fatalf("Failed to create temp env file: %v", err)
	}
	defer os.Remove(tmpEnv.Name())

	_, err = tmpEnv.WriteString("PORT=:9999\n")
	if err != nil {
		t.Fatalf("Failed to write to temp env file: %v", err)
	}
	tmpEnv.Close()

	loadSettings(tmpEnv.Name())

	if settings.AuthToken != "" {
		t.Errorf("expected empty settings.AuthToken, got %q", settings.AuthToken)
	}

	// Restore original settings
	loadSettings()
}

func TestLoadSettings_MySQLPort(t *testing.T) {
	testPort := func(t *testing.T, content string) string {
		defer unsetEnv("MYSQL_PORT")()
		tmpEnv, err := os.CreateTemp("", ".env.*")
		if err != nil {
			t.Fatalf("Failed to create temp env file: %v", err)
		}
		defer os.Remove(tmpEnv.Name())

		if _, err := tmpEnv.WriteString(content); err != nil {
			t.Fatalf("Failed to write to temp env file: %v", err)
		}
		tmpEnv.Close()

		loadSettings(tmpEnv.Name())
		return settings.MySQL.Port
	}

	t.Run("port with leading colon", func(t *testing.T) {
		got := testPort(t, "MYSQL_PORT=:3306\n")
		if got != "3306" {
			t.Errorf("expected settings.MySQL.Port '3306', got %q", got)
		}
	})

	t.Run("port without leading colon", func(t *testing.T) {
		got := testPort(t, "MYSQL_PORT=3307\n")
		if got != "3307" {
			t.Errorf("expected settings.MySQL.Port '3307', got %q", got)
		}
	})

	t.Run("default port when unset", func(t *testing.T) {
		got := testPort(t, "DEBUG=true\n")
		if got != "3306" {
			t.Errorf("expected default settings.MySQL.Port '3306', got %q", got)
		}
	})

	// Restore original settings
	loadSettings()
}

func TestLoadSettings_SqlitePath(t *testing.T) {
	testPath := func(t *testing.T, content string) string {
		defer unsetEnv("SQLITE_PATH")()
		tmpEnv, err := os.CreateTemp("", ".env.*")
		if err != nil {
			t.Fatalf("Failed to create temp env file: %v", err)
		}
		defer os.Remove(tmpEnv.Name())

		if _, err := tmpEnv.WriteString(content); err != nil {
			t.Fatalf("Failed to write to temp env file: %v", err)
		}
		tmpEnv.Close()

		loadSettings(tmpEnv.Name())
		return settings.SqlitePath
	}

	t.Run("custom sqlite path", func(t *testing.T) {
		tmpDir := t.TempDir()
		customPath := filepath.Join(tmpDir, "app.db")
		got := testPath(t, "SQLITE_PATH="+customPath+"\n")
		if got != customPath {
			t.Errorf("expected settings.SqlitePath %q, got %q", customPath, got)
		}
	})

	t.Run("default sqlite path when unset", func(t *testing.T) {
		got := testPath(t, "DEBUG=true\n")
		expected := filepath.Join(utils.GetBinaryBasePath(), nameOfService+".db")
		if got != expected {
			t.Errorf("expected default settings.SqlitePath %q, got %q", expected, got)
		}
	})
}

func TestLoadSettings_Storage(t *testing.T) {
	loadWith := func(t *testing.T, content string) string {
		defer unsetEnv("STORAGE")()
		tmpEnv, err := os.CreateTemp("", ".env.*")
		if err != nil {
			t.Fatalf("Failed to create temp env file: %v", err)
		}
		defer os.Remove(tmpEnv.Name())
		if _, err := tmpEnv.WriteString(content); err != nil {
			t.Fatalf("Failed to write to temp env file: %v", err)
		}
		tmpEnv.Close()

		loadSettings(tmpEnv.Name())
		return settings.Storage
	}
	defer loadSettings()

	if got := loadWith(t, "PORT=:9999\n"); got != types.StorageSQLite {
		t.Errorf("expected default storage %q, got %q", types.StorageSQLite, got)
	}
	if got := loadWith(t, "STORAGE= MySQL \n"); got != types.StorageMySQL {
		t.Errorf("expected STORAGE to be trimmed and lower-cased to %q, got %q", types.StorageMySQL, got)
	}
	if got := loadWith(t, "STORAGE=postgres\n"); got != "postgres" {
		t.Errorf("expected unknown storage to be kept for validation, got %q", got)
	}
	if err := settings.Validate(); err == nil {
		t.Error("expected Validate to reject an unknown STORAGE")
	}
}

func TestLoadSettings_FileMaker(t *testing.T) {
	load := func(t *testing.T, content string) types.FileMakerSettings {
		defer unsetEnv("FMS_HOST", "FMS_DATABASE", "FMS_USERNAME", "FMS_PASSWORD", "FMS_TIMEOUT", "FMS_LOG_TABLE", "FMS_CA_FILE", "FMS_INSECURE_SKIP_VERIFY", "STORAGE")()
		tmpEnv, err := os.CreateTemp("", ".env.*")
		if err != nil {
			t.Fatalf("Failed to create temp env file: %v", err)
		}
		defer os.Remove(tmpEnv.Name())
		tmpEnv.WriteString(content)
		tmpEnv.Close()
		loadSettings(tmpEnv.Name())
		return settings.FileMaker
	}
	defer loadSettings()

	fm := load(t, "PORT=:9999\n")
	if fm.Timeout != 10*time.Second || fm.LogTable != "Logs" || fm.InsecureSkipVerify {
		t.Errorf("unexpected defaults: %+v", fm)
	}

	fm = load(t, "STORAGE=filemaker\nFMS_HOST=https://fms.example.com\nFMS_DATABASE=Logging\nFMS_USERNAME=u\nFMS_PASSWORD=p\nFMS_TIMEOUT=3s\nFMS_LOG_TABLE=ServiceLogs\nFMS_CA_FILE=/etc/ca.pem\nFMS_INSECURE_SKIP_VERIFY=true\n")
	want := types.FileMakerSettings{Host: "https://fms.example.com", Database: "Logging", Username: "u", Password: "p", Timeout: 3 * time.Second, LogTable: "ServiceLogs", CAFile: "/etc/ca.pem", InsecureSkipVerify: true}
	if fm != want {
		t.Errorf("expected %+v, got %+v", want, fm)
	}
	if err := settings.Validate(); err != nil {
		t.Errorf("expected loaded FileMaker settings to validate, got %v", err)
	}

	if fm = load(t, "FMS_TIMEOUT=soon\n"); fm.Timeout != 0 {
		t.Errorf("expected an invalid FMS_TIMEOUT to become 0 for Validate to reject, got %v", fm.Timeout)
	}
}

func TestLoadSettings_Timeout(t *testing.T) {
	load := func(content string) time.Duration {
		defer unsetEnv("TIMEOUT")()
		tmpEnv, err := os.CreateTemp("", ".env.*")
		if err != nil {
			t.Fatalf("Failed to create temp env file: %v", err)
		}
		defer os.Remove(tmpEnv.Name())
		tmpEnv.WriteString(content)
		tmpEnv.Close()
		loadSettings(tmpEnv.Name())
		return settings.Timeout
	}
	defer loadSettings()

	if got := load("PORT=:9999\n"); got != 15*time.Second {
		t.Errorf("expected default TIMEOUT 15s, got %v", got)
	}
	if got := load("TIMEOUT=20\n"); got != 20*time.Second {
		t.Errorf("expected TIMEOUT=20 to mean 20s, got %v", got)
	}
	if got := load("TIMEOUT=soon\n"); got != 0 {
		t.Errorf("expected an invalid TIMEOUT to become 0 for Validate to reject, got %v", got)
	}
}
