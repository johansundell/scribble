package backup

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

const week = 7 * 24 * time.Hour

var pruneNow = func() time.Time { return time.Date(2026, 10, 19, 3, 0, 0, 0, time.UTC) }

// writeArchives creates an empty archive in dir for each age before now.
func writeArchives(t *testing.T, dir string, now time.Time, ages ...time.Duration) {
	t.Helper()
	for _, age := range ages {
		name := archivePrefix + now.Add(-age).UTC().Format(timestampLayout) + archiveSuffix
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestPruneRemovesArchivesOlderThanRetention(t *testing.T) {
	dir := t.TempDir()
	now := pruneNow()
	writeArchives(t, dir, now, 0, 24*time.Hour, week, week+time.Second, 30*24*time.Hour)

	removed, err := Prune(dir, week, now)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	want := []string{
		"scribble_20260919T030000Z.db.gz", // 30 days
		"scribble_20261012T025959Z.db.gz", // a week and a second
	}
	var got []string
	for _, p := range removed {
		got = append(got, filepath.Base(p))
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("removed %v, want %v", got, want)
	}
	// Exactly a week old is not older than the retention, so it stays.
	if left := listDir(t, dir); !slices.Equal(left, []string{
		"scribble_20261012T030000Z.db.gz",
		"scribble_20261018T030000Z.db.gz",
		"scribble_20261019T030000Z.db.gz",
	}) {
		t.Errorf("left %v", left)
	}
}

// TestPruneKeepsNewestArchive is the safety guardrail: however old, the most
// recent backup is never deleted, so a stalled backup job can't prune itself
// down to nothing.
func TestPruneKeepsNewestArchive(t *testing.T) {
	t.Run("single old archive", func(t *testing.T) {
		dir := t.TempDir()
		now := pruneNow()
		writeArchives(t, dir, now, 90*24*time.Hour)

		removed, err := Prune(dir, week, now)
		if err != nil {
			t.Fatalf("Prune: %v", err)
		}
		if len(removed) != 0 || len(listDir(t, dir)) != 1 {
			t.Errorf("the only archive must stay; removed %v", removed)
		}
	})
	t.Run("all archives old", func(t *testing.T) {
		dir := t.TempDir()
		now := pruneNow()
		writeArchives(t, dir, now, 30*24*time.Hour, 20*24*time.Hour, 10*24*time.Hour)

		if _, err := Prune(dir, week, now); err != nil {
			t.Fatalf("Prune: %v", err)
		}
		if left := listDir(t, dir); !slices.Equal(left, []string{"scribble_20261009T030000Z.db.gz"}) {
			t.Errorf("only the newest archive should stay, left %v", left)
		}
	})
}

// TestPruneIgnoresFutureArchiveForNewest checks that an archive stamped ahead
// of now (written while the clock was wrong) doesn't take the newest slot, so
// the latest real backup stays protected.
func TestPruneIgnoresFutureArchiveForNewest(t *testing.T) {
	dir := t.TempDir()
	now := pruneNow()
	writeArchives(t, dir, now, -365*24*time.Hour, 20*24*time.Hour, 30*24*time.Hour)

	if _, err := Prune(dir, week, now); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if left := listDir(t, dir); !slices.Equal(left, []string{
		"scribble_20260929T030000Z.db.gz", // newest real backup
		"scribble_20271019T030000Z.db.gz", // future: not older than the retention
	}) {
		t.Errorf("left %v", left)
	}
}

func TestPruneLeavesOtherFilesAlone(t *testing.T) {
	dir := t.TempDir()
	now := pruneNow()
	writeArchives(t, dir, now, 0)
	others := []string{
		"notes.txt",
		"scribble_old.db.gz",                     // no timestamp
		"scribble_20200101T000000Z.db",           // not compressed
		"other_20200101T000000Z.db.gz",        // another prefix
		".scribble_20200101T000000Z.db.gz.tmp",   // work file
		"scribble_20200101T000000Z.db.gz.backup", // renamed by hand
	}
	for _, name := range others {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A folder with an archive's name.
	if err := os.Mkdir(filepath.Join(dir, "scribble_20200101T000000Z.db.gz"), 0o750); err != nil {
		t.Fatal(err)
	}

	// A symlink with an archive's name.
	if err := os.Symlink("notes.txt", filepath.Join(dir, "scribble_20200102T000000Z.db.gz")); err != nil {
		t.Fatal(err)
	}

	removed, err := Prune(dir, week, now)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("Prune removed files that aren't archives: %v", removed)
	}
	if n := len(listDir(t, dir)); n != len(others)+3 {
		t.Errorf("dir has %d entries, want %d", n, len(others)+3)
	}
}

func TestPruneRejectsNonPositiveRetention(t *testing.T) {
	dir := t.TempDir()
	now := pruneNow()
	writeArchives(t, dir, now, 0, 30*24*time.Hour)
	for _, keep := range []time.Duration{0, -week} {
		if _, err := Prune(dir, keep, now); err == nil {
			t.Errorf("Prune(%v) should fail", keep)
		}
	}
	if n := len(listDir(t, dir)); n != 2 {
		t.Errorf("a rejected Prune removed files; %d left", n)
	}
}
