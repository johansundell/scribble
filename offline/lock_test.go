package offline_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/johansundell/scribble/offline"
)

func TestStoreExclusiveLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	first, err := offline.Open(path)
	if err != nil {
		t.Fatalf("first Open failed: %v", err)
	}

	second, err := offline.Open(path)
	if err == nil {
		_ = second.Close()
		t.Fatal("second Open succeeded, want ErrLocked")
	}
	if !errors.Is(err, offline.ErrLocked) {
		t.Fatalf("second Open error %v, want ErrLocked", err)
	}

	var locked *offline.LockedError
	if !errors.As(err, &locked) {
		t.Fatalf("second Open error %T, want *offline.LockedError", err)
	}
	if locked.PID != os.Getpid() {
		t.Errorf("locked.PID = %d, want %d", locked.PID, os.Getpid())
	}
	if locked.Path != path+".lock" {
		t.Errorf("locked.Path = %q, want %q", locked.Path, path+".lock")
	}

	if err := first.Close(); err != nil {
		t.Fatalf("first.Close() failed: %v", err)
	}

	// After closing first, second open on the same path must succeed.
	reopened, err := offline.Open(path)
	if err != nil {
		t.Fatalf("Open after Close failed: %v", err)
	}
	_ = reopened.Close()
}

func TestStoreLockDifferentPathsCanCoexist(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "cacheA.json")
	pathB := filepath.Join(dir, "cacheB.json")

	storeA, err := offline.Open(pathA)
	if err != nil {
		t.Fatalf("Open pathA failed: %v", err)
	}
	defer storeA.Close()

	storeB, err := offline.Open(pathB)
	if err != nil {
		t.Fatalf("Open pathB failed: %v", err)
	}
	defer storeB.Close()
}

func TestStoreInMemorySkipsLocking(t *testing.T) {
	first, err := offline.Open("")
	if err != nil {
		t.Fatalf("Open empty path failed: %v", err)
	}
	defer first.Close()

	second, err := offline.Open("")
	if err != nil {
		t.Fatalf("second Open empty path failed: %v", err)
	}
	defer second.Close()
}
