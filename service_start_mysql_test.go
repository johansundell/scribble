package main

import (
	"testing"
	"time"

	"github.com/johansundell/scribble/types"
)

func TestAppSettingsValidate_MySQLMissingRequiredFields(t *testing.T) {
	settings := types.AppSettings{
		Port:    ":8080",
		Timeout: 15 * time.Second,
		Storage: types.StorageMySQL,
	}

	if err := settings.Validate(); err == nil {
		t.Fatal("expected validation to fail when MySQL required fields are missing")
	}
}
