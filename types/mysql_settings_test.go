package types

import (
	"testing"
	"time"
)

func TestAppSettingsValidate_MySQLRequirements(t *testing.T) {
	cases := []struct {
		name    string
		s       AppSettings
		wantErr bool
	}{
		{
			name: "mysql enabled missing required fields",
			s: AppSettings{
				Port:    ":8080",
				Timeout: 10 * time.Second,
				Storage: StorageMySQL,
				MySQL: struct {
					Username string `json:"username"`
					Password string `json:"password"`
					Host     string `json:"host"`
					Port     string `json:"port"`
					Database string `json:"database"`
				}{},
			},
			wantErr: true,
		},
		{
			name: "mysql enabled with required fields",
			s: AppSettings{
				Port:    ":8080",
				Timeout: 10 * time.Second,
				Storage: StorageMySQL,
				MySQL: struct {
					Username string `json:"username"`
					Password string `json:"password"`
					Host     string `json:"host"`
					Port     string `json:"port"`
					Database string `json:"database"`
				}{Username: "user", Host: "db.local", Database: "appdb"},
			},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.s.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
