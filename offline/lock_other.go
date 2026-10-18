//go:build !unix && !windows && !linux && !darwin && !freebsd && !openbsd && !netbsd

package offline

import "os"

func tryLockFile(f *os.File) (bool, error) {
	return true, nil
}

func unlockFile(f *os.File) error {
	return nil
}
