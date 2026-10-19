package types

import (
	"fmt"
	"strings"
	"time"
)

// Storage backends selectable with STORAGE.
const (
	StorageSQLite    = "sqlite"
	StorageMySQL     = "mysql"
	StorageFileMaker = "filemaker"
)

// StorageBackends lists every valid STORAGE value. The service must have a
// constructor for each (see openStore); a test checks that they match.
var StorageBackends = []string{StorageSQLite, StorageMySQL, StorageFileMaker}

// MySQLSettings configures STORAGE=mysql (MYSQL_* variables).
type MySQLSettings struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	Database string `json:"database"`
}

// FileMakerSettings configures STORAGE=filemaker (FMS_* variables).
type FileMakerSettings struct {
	Host               string        `json:"host"`
	Database           string        `json:"database"`
	Username           string        `json:"username"`
	Password           string        `json:"password"`
	Timeout            time.Duration `json:"timeout"`
	LogTable           string        `json:"logTable"`
	CAFile             string        `json:"caFile"`
	InsecureSkipVerify bool          `json:"insecureSkipVerify"`
}

// WebSettings configures the web app at /app. An empty PIN turns it off.
type WebSettings struct {
	PIN            string        `json:"pin"`            // WEB_PIN
	SessionSecret  string        `json:"sessionSecret"`  // SESSION_SECRET
	SessionTimeout time.Duration `json:"sessionTimeout"` // WEB_SESSION_TIMEOUT
	CookieSecure   bool          `json:"cookieSecure"`   // COOKIE_SECURE
}

// Enabled reports whether the web app is turned on.
func (w WebSettings) Enabled() bool { return w.PIN != "" }

// minSessionSecret is the shortest SESSION_SECRET accepted.
const minSessionSecret = 32

type AppSettings struct {
	Debug               bool              `json:"debug"`
	Port                string            `json:"port"`
	UseFileSystem       bool              `json:"useFileSystem"`
	Timeout             time.Duration     `json:"timeout"` // TIMEOUT, in whole seconds
	Storage             string            `json:"storage"`
	AuthToken           string            `json:"authToken"`
	SqlitePath          string            `json:"sqlitePath"`
	BackupDir           string            `json:"backupDir"`
	BackupRetentionDays int               `json:"backupRetentionDays"`
	MySQL               MySQLSettings     `json:"mysql"`
	FileMaker           FileMakerSettings `json:"filemaker"`
	Web                 WebSettings       `json:"web"`
	TrustedProxies      []string          `json:"trustedProxies"` // TRUSTED_PROXIES; empty trusts no proxy
}

// Validate verifies required settings and returns an error when configuration is invalid.
func (s AppSettings) Validate() error {
	if s.Port == "" {
		return fmt.Errorf("PORT must be set")
	}
	if s.Timeout <= 0 {
		return fmt.Errorf("TIMEOUT must be > 0")
	}
	switch s.Storage {
	case StorageSQLite:
	case StorageMySQL:
		if s.MySQL.Username == "" || s.MySQL.Host == "" || s.MySQL.Database == "" {
			return fmt.Errorf("MYSQL_USERNAME, MYSQL_HOST and MYSQL_DATABASE must be set when STORAGE=mysql")
		}
	case StorageFileMaker:
		fm := s.FileMaker
		if fm.Host == "" || fm.Database == "" || fm.Username == "" || fm.Password == "" {
			return fmt.Errorf("FMS_HOST, FMS_DATABASE, FMS_USERNAME and FMS_PASSWORD must be set when STORAGE=filemaker")
		}
		// Basic auth is sent with every request, so plain HTTP would leak the password.
		if !strings.HasPrefix(fm.Host, "https://") {
			return fmt.Errorf("FMS_HOST must start with https://, got %q", fm.Host)
		}
		if fm.Timeout <= 0 {
			return fmt.Errorf("FMS_TIMEOUT must be a positive duration such as 10s")
		}
		if fm.LogTable == "" {
			return fmt.Errorf("FMS_LOG_TABLE must not be empty")
		}
	default:
		return fmt.Errorf("STORAGE must be one of %s, got %q", strings.Join(StorageBackends, ", "), s.Storage)
	}
	return s.validateWeb()
}

// validateWeb checks the web app settings when WEB_PIN is set. The messages
// never include the PIN or the secret, since they end up in the service log.
func (s AppSettings) validateWeb() error {
	w := s.Web
	if !w.Enabled() {
		return nil
	}
	if !isPIN(w.PIN) {
		return fmt.Errorf("WEB_PIN must be 6 to 12 digits (0-9), or unset to turn the web app off")
	}
	if w.SessionSecret == "" {
		return fmt.Errorf("SESSION_SECRET must be set when WEB_PIN is set; generate one with: openssl rand -base64 33")
	}
	if len(w.SessionSecret) < minSessionSecret {
		return fmt.Errorf("SESSION_SECRET must be at least %d characters; generate one with: openssl rand -base64 33", minSessionSecret)
	}
	if w.SessionSecret == s.AuthToken {
		return fmt.Errorf("SESSION_SECRET must not be the same as AUTH_TOKEN")
	}
	if w.SessionTimeout <= 0 {
		return fmt.Errorf("WEB_SESSION_TIMEOUT must be a positive duration such as 168h")
	}
	return nil
}

func isPIN(pin string) bool {
	if len(pin) < 6 || len(pin) > 12 {
		return false
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
