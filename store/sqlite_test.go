package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/johansundell/scribble/store"
)

func TestFileURI(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"scribble.db", "file:scribble.db"},
		{"/var/data/scribble.db", "file:/var/data/scribble.db"},
		{"path/with?question.db", "file:path/with%3fquestion.db"},
		{"path/with#hash.db", "file:path/with%23hash.db"},
		{"path/with%percent.db", "file:path/with%25percent.db"},
		{"/a?b#c%d/test.db", "file:/a%3fb%23c%25d/test.db"},
	}

	for _, tt := range tests {
		got := store.FileURI(tt.input)
		if got != tt.want {
			t.Errorf("FileURI(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNewSQLitePathsWithURICharacters(t *testing.T) {
	temp := t.TempDir()
	base := filepath.Join(temp, "a?b#c%d")
	if err := os.MkdirAll(base, 0o750); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(base, "scribble.db")
	s, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite with special characters failed: %v", err)
	}
	defer s.Close()
}
