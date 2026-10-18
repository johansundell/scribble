package utils

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
)

var osExecutable = os.Executable

func GetUrl(r *http.Request) string {
	path := r.URL.Path
	query := r.URL.RawQuery
	if query != "" {
		return path + "?" + query
	}
	return path
}

func GetBinaryBasePath() string {
	exe, err := osExecutable()
	if err != nil {
		log.Println("Failed to get executable path, using current directory as base path:", err)
		return "./"
	}
	return filepath.Dir(exe)
}
