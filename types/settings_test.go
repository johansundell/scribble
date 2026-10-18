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
