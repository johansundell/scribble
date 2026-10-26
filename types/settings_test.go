package types

import (
	"strings"
	"testing"
	"time"
)

func TestAppSettingsValidate(t *testing.T) {
	tests := []struct {
		name    string
		s       AppSettings
		wantErr bool
	}{
		{"valid defaults", AppSettings{Port: ":8080", Timeout: 10 * time.Second, Storage: StorageSQLite}, false},
		{"missing storage", AppSettings{Port: ":8080", Timeout: 10 * time.Second}, true},
		{"unknown storage", AppSettings{Port: ":8080", Timeout: 10 * time.Second, Storage: "postgres"}, true},
		{"missing port", AppSettings{Port: "", Timeout: 10 * time.Second, Storage: StorageSQLite}, true},
		{"invalid timeout", AppSettings{Port: ":8080", Timeout: 0, Storage: StorageSQLite}, true},
		{"mysql missing fields", AppSettings{Port: ":8080", Timeout: 10 * time.Second, Storage: StorageMySQL, MySQL: struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Host     string `json:"host"`
			Port     string `json:"port"`
			Database string `json:"database"`
		}{}}, true},
		{"mysql provided", AppSettings{Port: ":8080", Timeout: 10 * time.Second, Storage: StorageMySQL, MySQL: struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Host     string `json:"host"`
			Port     string `json:"port"`
			Database string `json:"database"`
		}{Username: "u", Host: "localhost", Database: "db"}}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.s.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestAppSettingsValidate_FileMaker(t *testing.T) {
	valid := func() AppSettings {
		return AppSettings{Port: ":8080", Timeout: 10 * time.Second, Storage: StorageFileMaker, FileMaker: FileMakerSettings{
			Host: "https://fms.example.com", Database: "Logging", Username: "u", Password: "p", Timeout: 10 * time.Second, LogTable: "Logs",
		}}
	}
	tests := []struct {
		name    string
		modify  func(*AppSettings)
		wantErr string
	}{
		{"valid", func(*AppSettings) {}, ""},
		{"missing host", func(s *AppSettings) { s.FileMaker.Host = "" }, "FMS_HOST"},
		{"missing password", func(s *AppSettings) { s.FileMaker.Password = "" }, "FMS_PASSWORD"},
		{"plain http", func(s *AppSettings) { s.FileMaker.Host = "http://fms.example.com" }, "https://"},
		{"no timeout", func(s *AppSettings) { s.FileMaker.Timeout = 0 }, "FMS_TIMEOUT"},
		{"empty table", func(s *AppSettings) { s.FileMaker.LogTable = "" }, "FMS_LOG_TABLE"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := valid()
			tc.modify(&s)
			err := s.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestAppSettingsValidate_Replica(t *testing.T) {
	valid := func() AppSettings {
		return AppSettings{Port: ":8080", Timeout: 10 * time.Second, Storage: StorageSQLite,
			FileMaker: FileMakerSettings{Host: "https://fms.example.com", Database: "Notes", Username: "u", Password: "p", Timeout: 10 * time.Second},
			Replica:   ReplicaSettings{Mode: ReplicaFileMaker, NotesTable: "Notes", PendingTable: "PendingEdits", InfoTable: "ScribbleInfo", Interval: 30 * time.Second},
		}
	}
	tests := []struct {
		name    string
		modify  func(*AppSettings)
		wantErr string
	}{
		{"valid", func(*AppSettings) {}, ""},
		{"off needs nothing", func(s *AppSettings) { s.Replica = ReplicaSettings{}; s.FileMaker = FileMakerSettings{} }, ""},
		{"unknown mode", func(s *AppSettings) { s.Replica.Mode = "mysql" }, "NOTES_REPLICA"},
		{"not sqlite", func(s *AppSettings) {
			s.Storage = StorageFileMaker
			s.FileMaker.LogTable = "Logs"
		}, "needs STORAGE=sqlite"},
		{"missing host", func(s *AppSettings) { s.FileMaker.Host = "" }, "when NOTES_REPLICA=filemaker"},
		{"plain http", func(s *AppSettings) { s.FileMaker.Host = "http://fms.example.com" }, "https://"},
		{"empty table", func(s *AppSettings) { s.Replica.PendingTable = "" }, "FMS_PENDING_TABLE"},
		{"no interval", func(s *AppSettings) { s.Replica.Interval = 0 }, "NOTES_REPLICA_INTERVAL"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := valid()
			tc.modify(&s)
			err := s.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestAppSettingsValidate_Web(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	valid := func() AppSettings {
		return AppSettings{Port: ":8080", Timeout: 10 * time.Second, Storage: StorageSQLite, AuthToken: "token",
			Web: WebSettings{PIN: "123456", SessionSecret: secret, SessionTimeout: 168 * time.Hour}}
	}
	tests := []struct {
		name    string
		modify  func(*AppSettings)
		wantErr string
	}{
		{"valid", func(*AppSettings) {}, ""},
		{"12 digits", func(s *AppSettings) { s.Web.PIN = "123456789012" }, ""},
		{"web off ignores the rest", func(s *AppSettings) { s.Web = WebSettings{SessionSecret: "short"} }, ""},
		{"4 digits", func(s *AppSettings) { s.Web.PIN = "1234" }, "WEB_PIN"},
		{"13 digits", func(s *AppSettings) { s.Web.PIN = "1234567890123" }, "WEB_PIN"},
		{"not digits", func(s *AppSettings) { s.Web.PIN = "12345a" }, "WEB_PIN"},
		{"non-ASCII digits", func(s *AppSettings) { s.Web.PIN = "١٢٣٤٥٦" }, "WEB_PIN"},
		{"missing secret", func(s *AppSettings) { s.Web.SessionSecret = "" }, "SESSION_SECRET"},
		{"short secret", func(s *AppSettings) { s.Web.SessionSecret = secret[:31] }, "SESSION_SECRET"},
		{"secret equals token", func(s *AppSettings) { s.AuthToken = secret }, "SESSION_SECRET"},
		{"no timeout", func(s *AppSettings) { s.Web.SessionTimeout = 0 }, "WEB_SESSION_TIMEOUT"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := valid()
			tc.modify(&s)
			err := s.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.wantErr, err)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), s.Web.PIN) {
				t.Errorf("error message leaks the PIN or the secret: %v", err)
			}
		})
	}
}
