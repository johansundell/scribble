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

type AppSettings struct {
	Debug         bool              `json:"debug"`
	Port          string            `json:"port"`
	UseFileSystem bool              `json:"useFileSystem"`
	Timeout       time.Duration     `json:"timeout"` // TIMEOUT, in whole seconds
	Storage       string            `json:"storage"`
	AuthToken     string            `json:"authToken"`
	SqlitePath    string            `json:"sqlitePath"`
	MySQL MySQLSettings     `json:"mysql"`
	FileMaker     FileMakerSettings `json:"filemaker"`
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
	return nil
}
