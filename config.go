package main

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/johansundell/scribble/types"
	"github.com/johansundell/scribble/utils"
	"github.com/joho/godotenv"
)

var settings types.AppSettings

func loadSettings(filenames ...string) {
	// Load or reload .env file (Load preserves already set environment variables)
	if len(filenames) > 0 {
		if err := godotenv.Load(filenames...); err != nil {
			log.Println("No .env file found, using default/environment values")
		}
	} else {
		if err := godotenv.Load(); err != nil {
			if exe, exeErr := os.Executable(); exeErr == nil {
				envPath := filepath.Join(filepath.Dir(exe), ".env")
				if err = godotenv.Load(envPath); err != nil {
					log.Println("No .env file found, using default/environment values")
				}
			} else {
				log.Println("No .env file found, using default/environment values")
			}
		}
	}

	settings = types.AppSettings{}

	settings.Debug, _ = strconv.ParseBool(os.Getenv("DEBUG"))
	settings.Port = os.Getenv("PORT")
	if settings.Port == "" {
		settings.Port = ":8080"
	}
	settings.UseFileSystem, _ = strconv.ParseBool(os.Getenv("USE_FILE_SYSTEM"))

	timeoutStr := os.Getenv("TIMEOUT")
	if timeoutStr != "" {
		// TIMEOUT is whole seconds; an invalid value becomes 0, which Validate rejects.
		seconds, _ := strconv.Atoi(timeoutStr)
		settings.Timeout = time.Duration(seconds) * time.Second
	} else {
		settings.Timeout = 15 * time.Second
	}

	settings.Storage = strings.ToLower(strings.TrimSpace(os.Getenv("STORAGE")))
	if settings.Storage == "" {
		settings.Storage = types.StorageSQLite
	}
	settings.SqlitePath = os.Getenv("SQLITE_PATH")
	if settings.SqlitePath == "" {
		settings.SqlitePath = filepath.Join(utils.GetBinaryBasePath(), nameOfService+".db")
	}
	settings.BackupDir = os.Getenv("BACKUP_DIR")
	settings.BackupRetentionDays = 7
	if v := os.Getenv("RETENTION_DAYS"); v != "" {
		settings.BackupRetentionDays, _ = strconv.Atoi(v)
	}
	settings.AuthToken = os.Getenv("AUTH_TOKEN")

	settings.MySQL.Username = os.Getenv("MYSQL_USERNAME")
	settings.MySQL.Password = os.Getenv("MYSQL_PASSWORD")
	settings.MySQL.Host = os.Getenv("MYSQL_HOST")
	settings.MySQL.Port = strings.TrimPrefix(os.Getenv("MYSQL_PORT"), ":")
	if settings.MySQL.Port == "" {
		settings.MySQL.Port = "3306"
	}
	settings.MySQL.Database = os.Getenv("MYSQL_DATABASE")

	settings.FileMaker.Host = os.Getenv("FMS_HOST")
	settings.FileMaker.Database = os.Getenv("FMS_DATABASE")
	settings.FileMaker.Username = os.Getenv("FMS_USERNAME")
	settings.FileMaker.Password = os.Getenv("FMS_PASSWORD")
	settings.FileMaker.Timeout = 10 * time.Second
	if v := os.Getenv("FMS_TIMEOUT"); v != "" {
		// An invalid value becomes 0, which Validate rejects.
		settings.FileMaker.Timeout, _ = time.ParseDuration(v)
	}
	settings.FileMaker.LogTable = os.Getenv("FMS_LOG_TABLE")
	if settings.FileMaker.LogTable == "" {
		settings.FileMaker.LogTable = "Logs"
	}
	settings.FileMaker.CAFile = os.Getenv("FMS_CA_FILE")
	settings.FileMaker.InsecureSkipVerify, _ = strconv.ParseBool(os.Getenv("FMS_INSECURE_SKIP_VERIFY"))

	settings.Replica.Mode = strings.ToLower(strings.TrimSpace(os.Getenv("NOTES_REPLICA")))
	settings.Replica.NotesTable = envOr("FMS_NOTES_TABLE", "Notes")
	settings.Replica.PendingTable = envOr("FMS_PENDING_TABLE", "PendingEdits")
	settings.Replica.InfoTable = envOr("FMS_INFO_TABLE", "ScribbleInfo")
	settings.Replica.Interval = 30 * time.Second
	if v := os.Getenv("NOTES_REPLICA_INTERVAL"); v != "" {
		// An invalid value becomes 0, which Validate rejects.
		settings.Replica.Interval, _ = time.ParseDuration(v)
	}

	settings.Web.PIN = os.Getenv("WEB_PIN")
	settings.Web.SessionSecret = os.Getenv("SESSION_SECRET")
	settings.Web.SessionTimeout = 168 * time.Hour
	if v := os.Getenv("WEB_SESSION_TIMEOUT"); v != "" {
		// An invalid value becomes 0, which Validate rejects.
		settings.Web.SessionTimeout, _ = time.ParseDuration(v)
	}
	settings.Web.CookieSecure = true
	if v, err := strconv.ParseBool(os.Getenv("COOKIE_SECURE")); err == nil {
		settings.Web.CookieSecure = v
	}
	for _, p := range strings.Split(os.Getenv("TRUSTED_PROXIES"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			settings.TrustedProxies = append(settings.TrustedProxies, p)
		}
	}
}

// envOr returns the environment variable key, or def when it is unset or
// empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
