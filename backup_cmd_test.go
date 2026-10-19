package main

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

func TestBackupOptions(t *testing.T) {
	tests := []struct {
		name               string
		args               []string
		sqlitePath, envDir string
		wantDB, wantDir    string
	}{
		{
			name:       "defaults put backups beside the database",
			sqlitePath: "/app/data/scribble.db",
			wantDB:     "/app/data/scribble.db",
			wantDir:    "/app/data/backups",
		},
		{
			name:       "BACKUP_DIR sets the directory",
			sqlitePath: "/app/data/scribble.db",
			envDir:     "/srv/backups",
			wantDB:     "/app/data/scribble.db",
			wantDir:    "/srv/backups",
		},
		{
			name:       "flags win over the environment",
			args:       []string{"-db", "/tmp/other.db", "-dir", "/tmp/out"},
			sqlitePath: "/app/data/scribble.db",
			envDir:     "/srv/backups",
			wantDB:     "/tmp/other.db",
			wantDir:    "/tmp/out",
		},
		{
			name:       "-db alone moves the default directory with it",
			args:       []string{"-db", "/tmp/other.db"},
			sqlitePath: "/app/data/scribble.db",
			wantDB:     "/tmp/other.db",
			wantDir:    "/tmp/backups",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := parseBackupArgs(tt.args, types.AppSettings{SqlitePath: tt.sqlitePath, BackupDir: tt.envDir, BackupRetentionDays: 7}, io.Discard)
			if err != nil {
				t.Fatalf("parseBackupArgs: %v", err)
			}
			opts := a.opts
			if opts.DBPath != tt.wantDB || opts.Dir != tt.wantDir {
				t.Errorf("got db=%q dir=%q, want db=%q dir=%q", opts.DBPath, opts.Dir, tt.wantDB, tt.wantDir)
			}
		})
	}
}

func TestBackupOptionsRejectsBadArgs(t *testing.T) {
	for _, args := range [][]string{{"-nope"}, {"extra"}} {
		if _, err := parseBackupArgs(args, types.AppSettings{SqlitePath: "/app/data/scribble.db", BackupRetentionDays: 7}, io.Discard); err == nil {
			t.Errorf("parseBackupArgs(%q) should fail", args)
		}
	}
}

func TestBackupOptionsRetention(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		envDays int
		want    string
	}{
		{"RETENTION_DAYS sets it", nil, 14, "14"},
		{"the flag wins", []string{"-retention-days", "3"}, 14, "3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := parseBackupArgs(tt.args, types.AppSettings{SqlitePath: "/app/data/scribble.db", BackupRetentionDays: tt.envDays}, io.Discard)
			if err != nil {
				t.Fatalf("parseBackupArgs: %v", err)
			}
			if a.retention != tt.want {
				t.Errorf("retention = %q days, want %q", a.retention, tt.want)
			}
		})
	}
}

func TestRetentionPeriod(t *testing.T) {
	if keep, err := retentionPeriod("7"); err != nil || keep != 7*24*time.Hour {
		t.Errorf("retentionPeriod(7) = %v, %v", keep, err)
	}
	// 0 is also what an invalid RETENTION_DAYS loads as. Huge values would
	// overflow time.Duration and wrap to a small or negative period.
	for _, days := range []string{"0", "-3", "36501", "200000", "week", "7.5", ""} {
		if _, err := retentionPeriod(days); err == nil || !strings.Contains(err.Error(), "RETENTION_DAYS") {
			t.Errorf("retentionPeriod(%q) err = %v, want one naming RETENTION_DAYS", days, err)
		}
	}
}

func TestRunBackupStorageNonSQLite(t *testing.T) {
	for _, storage := range []string{types.StorageMySQL, types.StorageFileMaker, "postgres"} {
		t.Run(storage, func(t *testing.T) {
			var stdout, stderr strings.Builder
			s := types.AppSettings{
				Storage:    storage,
				SqlitePath: "/some/path.db",
			}
			code := runBackup(nil, s, &stdout, &stderr)
			if code != 1 {
				t.Errorf("runBackup exit %d, want 1", code)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout should be empty, got %q", stdout.String())
			}
			want := "backup only supports STORAGE=sqlite"
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
			}
		})
	}
}

func TestRunBackupBadRetentionStillBacksUp(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"invalid RETENTION_DAYS", nil},
		{"malformed flag", []string{"-retention-days", "week"}},
		{"zero flag", []string{"-retention-days", "0"}},
	} {
		t.Run(tt.name, func(t *testing.T) { testBadRetentionStillBacksUp(t, tt.args) })
	}
}

func testBadRetentionStillBacksUp(t *testing.T, args []string) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "scribble.db")
	s, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	backups := filepath.Join(dir, "backups")
	if err := os.Mkdir(backups, 0o750); err != nil {
		t.Fatal(err)
	}
	old := "scribble_" + time.Now().UTC().AddDate(0, 0, -30).Format("20060102T150405Z") + ".db.gz"
	if err := os.WriteFile(filepath.Join(backups, old), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	// BackupRetentionDays 0 is what an invalid RETENTION_DAYS loads as; with
	// a retention flag, the flag decides.
	settings := types.AppSettings{
		Storage:    types.StorageSQLite,
		SqlitePath: dbPath,
	}
	if code := runBackup(args, settings, &stdout, &stderr); code != 1 {
		t.Fatalf("runBackup exit %d with a bad retention, want 1; stderr: %s", code, stderr.String())
	}
	if archive := strings.TrimSpace(stdout.String()); archive == "" {
		t.Error("the backup should still be stored and printed")
	} else if _, err := os.Stat(archive); err != nil {
		t.Errorf("archive missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(backups, old)); err != nil {
		t.Errorf("nothing should be pruned with a bad retention: %v", err)
	}
	if !strings.Contains(stderr.String(), "RETENTION_DAYS") {
		t.Errorf("stderr should name RETENTION_DAYS, got %q", stderr.String())
	}
}

func TestRunBackupPrunes(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "scribble.db")
	s, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	backups := filepath.Join(dir, "backups")
	if err := os.Mkdir(backups, 0o750); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	old := "scribble_" + now.AddDate(0, 0, -10).Format("20060102T150405Z") + ".db.gz"
	recent := "scribble_" + now.AddDate(0, 0, -2).Format("20060102T150405Z") + ".db.gz"
	for _, name := range []string{old, recent} {
		if err := os.WriteFile(filepath.Join(backups, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr strings.Builder
	settings := types.AppSettings{
		Storage:             types.StorageSQLite,
		SqlitePath:          dbPath,
		BackupRetentionDays: 7,
	}
	if code := runBackup(nil, settings, &stdout, &stderr); code != 0 {
		t.Fatalf("runBackup exit %d, stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(backups, old)); !os.IsNotExist(err) {
		t.Errorf("%s is past the retention and should be gone", old)
	}
	if _, err := os.Stat(filepath.Join(backups, recent)); err != nil {
		t.Errorf("%s is within the retention and should stay: %v", recent, err)
	}
	if !strings.Contains(stderr.String(), old) {
		t.Errorf("stderr should report the removed archive, got %q", stderr.String())
	}
}

func TestRunBackupCommand(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "scribble.db")
	s, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	backups := filepath.Join(dir, "out")

	var stdout, stderr strings.Builder
	settings := types.AppSettings{
		Storage:             types.StorageSQLite,
		SqlitePath:          dbPath,
		BackupDir:           backups,
		BackupRetentionDays: 7,
	}
	if code := runBackup(nil, settings, &stdout, &stderr); code != 0 {
		t.Fatalf("runBackup exit %d, stderr: %s", code, stderr.String())
	}
	entries, err := os.ReadDir(backups)
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "scribble_") || !strings.HasSuffix(entries[0].Name(), ".db.gz") {
		t.Fatalf("expected one scribble_<timestamp>.db.gz in %s, got %v (%v)", backups, entries, err)
	}
	if got := strings.TrimSpace(stdout.String()); got != filepath.Join(backups, entries[0].Name()) {
		t.Errorf("stdout = %q, want the archive path", got)
	}
}

func TestRunBackupCommandFails(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.db")
	backups := filepath.Join(dir, "backups")
	if err := os.Mkdir(backups, 0o750); err != nil {
		t.Fatal(err)
	}
	existing := "scribble_20261001T000000Z.db.gz"
	if err := os.WriteFile(filepath.Join(backups, existing), []byte("keep-me"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	settings := types.AppSettings{
		Storage:             types.StorageSQLite,
		SqlitePath:          missing,
		BackupDir:           backups,
		BackupRetentionDays: 7,
	}
	if code := runBackup(nil, settings, &stdout, &stderr); code != 1 {
		t.Fatalf("runBackup exit %d for a missing database, want 1", code)
	}
	if stderr.Len() == 0 {
		t.Error("runBackup should say why it failed")
	}
	if _, err := os.Stat(filepath.Join(backups, existing)); err != nil {
		t.Errorf("failed backup deleted existing archive: %v", err)
	}
}

func TestLoadSettings_BackupDirAndRetention(t *testing.T) {
	origDir := os.Getenv("BACKUP_DIR")
	origRet := os.Getenv("RETENTION_DAYS")
	defer func() {
		if origDir != "" {
			os.Setenv("BACKUP_DIR", origDir)
		} else {
			os.Unsetenv("BACKUP_DIR")
		}
		if origRet != "" {
			os.Setenv("RETENTION_DAYS", origRet)
		} else {
			os.Unsetenv("RETENTION_DAYS")
		}
	}()

	os.Setenv("BACKUP_DIR", "/srv/scribble-backups")
	os.Setenv("RETENTION_DAYS", "14")
	loadSettings(filepath.Join(t.TempDir(), "missing.env"))
	if settings.BackupDir != "/srv/scribble-backups" {
		t.Errorf("BackupDir = %q, want /srv/scribble-backups", settings.BackupDir)
	}
	if settings.BackupRetentionDays != 14 {
		t.Errorf("BackupRetentionDays = %d, want 14", settings.BackupRetentionDays)
	}
}

func TestCronSnippet(t *testing.T) {
	tests := []struct {
		name string
		exe  string
		args []string
		want string
	}{
		{
			name: "plain",
			exe:  "/opt/scribble/scribble-server",
			want: "0 3 * * * cd /opt/scribble && /opt/scribble/scribble-server backup 2>&1 | logger -t scribble-backup",
		},
		{
			name: "flags are passed on, quoted for the shell",
			exe:  "/opt/scribble/scribble-server",
			args: []string{"-dir", "/srv/my backups", "-retention-days", "14"},
			want: "0 3 * * * cd /opt/scribble && /opt/scribble/scribble-server backup -dir '/srv/my backups' -retention-days 14 2>&1 | logger -t scribble-backup",
		},
		{
			// cron turns an unescaped % into a newline.
			name: "percent and quote",
			exe:  "/opt/my scribble/scribble-server",
			args: []string{"-dir", "/srv/50%/it's"},
			want: `0 3 * * * cd '/opt/my scribble' && '/opt/my scribble/scribble-server' backup -dir '/srv/50\%/it'\''s' 2>&1 | logger -t scribble-backup`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cronSnippet(tt.exe, tt.args); got != tt.want {
				t.Errorf("cronSnippet:\n got %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestRunBackupCronSnippet(t *testing.T) {
	dir := t.TempDir()
	for _, flagName := range []string{"-cron-snippet", "--cron-snippet"} {
		var stdout, stderr strings.Builder
		args := []string{"-dir", dir, flagName, "-retention-days", "14"}
		settings := types.AppSettings{
			Storage:             types.StorageSQLite,
			SqlitePath:          filepath.Join(dir, "missing.db"),
			BackupRetentionDays: 7,
		}
		if code := runBackup(args, settings, &stdout, &stderr); code != 0 {
			t.Fatalf("%s: exit %d, stderr: %s", flagName, code, stderr.String())
		}
		line := strings.TrimSpace(stdout.String())
		if !strings.HasPrefix(line, "0 3 * * * cd /") || !strings.Contains(line, " backup -dir "+dir+" -retention-days 14 ") || !strings.Contains(line, "logger -t scribble-backup") {
			t.Errorf("%s printed %q", flagName, line)
		}
		if strings.Contains(line, "cron-snippet") {
			t.Errorf("%s: the crontab line must not print the snippet again: %q", flagName, line)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("%s took a backup: %v", flagName, entries)
		}
	}
}

func TestParseBackupArgsForwardsAbsolutePaths(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	a, err := parseBackupArgs([]string{"-cron-snippet", "-db", "data/scribble.db", "-dir", "backups", "-retention-days", "3"}, types.AppSettings{BackupRetentionDays: 7}, io.Discard)
	if err != nil {
		t.Fatalf("parseBackupArgs: %v", err)
	}
	want := []string{"-db", filepath.Join(wd, "data/scribble.db"), "-dir", filepath.Join(wd, "backups"), "-retention-days", "3"}
	if !slices.Equal(a.forwardedFlags, want) {
		t.Errorf("forwardedFlags = %q, want %q", a.forwardedFlags, want)
	}
}
