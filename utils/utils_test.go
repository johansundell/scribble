package utils

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestGetBinaryBasePath(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() returned an error: %v", err)
	}

	want := filepath.Dir(executable)
	if got := GetBinaryBasePath(); got != want {
		t.Fatalf("GetBinaryBasePath() = %q, want %q", got, want)
	}
}

func TestGetBinaryBasePath_Error(t *testing.T) {
	originalOsExecutable := osExecutable
	defer func() { osExecutable = originalOsExecutable }()

	osExecutable = func() (string, error) {
		return "", errors.New("mock error")
	}

	want := "./"
	if got := GetBinaryBasePath(); got != want {
		t.Fatalf("GetBinaryBasePath() = %q, want %q", got, want)
	}
}

func TestGetUrl(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "no query string",
			url:      "http://example.com/test",
			expected: "/test",
		},
		{
			name:     "with query string",
			url:      "http://example.com/test?a=1&b=2",
			expected: "/test?a=1&b=2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", tc.url, nil)
			got := GetUrl(req)
			if got != tc.expected {
				t.Errorf("GetUrl() = %q, want %q", got, tc.expected)
			}
		})
	}
}
