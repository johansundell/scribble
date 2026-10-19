package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Prune deletes the archives in dir that are older than keep at now, and
// returns their paths. The age comes from the timestamp in the archive name,
// not the file time, which copying can change. Prune always keeps the newest
// archive, however old, so a backup job that stopped working can't prune the
// last backup away. An archive stamped after now (written while the clock was
// ahead) doesn't count as the newest, so it can't take that protection from
// the latest real backup. Files that aren't archives, and symlinks, are left
// alone.
func Prune(dir string, keep time.Duration, now time.Time) ([]string, error) {
	if keep <= 0 {
		return nil, fmt.Errorf("retention must be positive, got %v", keep)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	type archive struct {
		path    string
		takenAt time.Time
	}
	var archives []archive
	newest := -1
	for _, e := range entries {
		takenAt, ok := archiveTime(e.Name())
		if !ok {
			continue
		}
		// Info, not Type: Type is 0, which reads as regular, on filesystems
		// that don't report entry types.
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		archives = append(archives, archive{filepath.Join(dir, e.Name()), takenAt})
		if !takenAt.After(now) && (newest < 0 || takenAt.After(archives[newest].takenAt)) {
			newest = len(archives) - 1
		}
	}

	cutoff := now.Add(-keep)
	var removed []string
	var errs []error
	for i, a := range archives {
		if i == newest || !a.takenAt.Before(cutoff) {
			continue
		}
		if err := os.Remove(a.path); err != nil {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, a.path)
	}
	return removed, errors.Join(errs...)
}

// archiveTime returns when the archive called name was taken, and false when
// name isn't an archive name.
func archiveTime(name string) (time.Time, bool) {
	stamp, ok := strings.CutPrefix(name, archivePrefix)
	if !ok {
		return time.Time{}, false
	}
	if stamp, ok = strings.CutSuffix(stamp, archiveSuffix); !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(timestampLayout, stamp)
	return t, err == nil
}
