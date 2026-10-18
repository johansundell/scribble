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

}
