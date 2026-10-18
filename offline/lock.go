package offline

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// ErrLocked is returned by Open when another process is already holding
// an exclusive lock on the cache.
var ErrLocked = errors.New("offline cache is locked by another process")

// LockedError provides details on which cache path and PID holds the lock.
type LockedError struct {
	Path string
	PID  int
}

func (e *LockedError) Error() string {
	if e.PID > 0 {
		return fmt.Sprintf("offline cache %s is locked by process %d", e.Path, e.PID)
	}
	return fmt.Sprintf("offline cache %s is locked by another process", e.Path)
}

func (e *LockedError) Unwrap() error {
	return ErrLocked
}

type lockHandle struct {
	file *os.File
	path string
}

// acquireLock opens or creates path and acquires an exclusive non-blocking lock.
func acquireLock(path string) (*lockHandle, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}

	locked, err := tryLockFile(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !locked {
		pid := readPID(f)
		_ = f.Close()
		return nil, &LockedError{Path: path, PID: pid}
	}

	// We own the lock. Write our PID for diagnostics.
	_ = f.Truncate(0)
	_, _ = f.Seek(0, io.SeekStart)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	_ = f.Sync()

	return &lockHandle{file: f, path: path}, nil
}

func (h *lockHandle) Close() error {
	if h == nil || h.file == nil {
		return nil
	}
	_ = unlockFile(h.file)
	err := h.file.Close()
	h.file = nil
	return err
}

func readPID(f *os.File) int {
	_, _ = f.Seek(0, io.SeekStart)
	b := make([]byte, 64)
	n, err := f.Read(b)
	if err != nil || n == 0 {
		return 0
	}
	s := strings.TrimSpace(string(b[:n]))
	pid, err := strconv.Atoi(s)
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}
