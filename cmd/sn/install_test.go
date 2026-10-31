package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The installer (#130) against a fake GitHub that serves one release,
// v1.2.3, laid out as make dist lays it out.

const fakeSn = "#!/bin/sh\necho sn v1.2.3\n"

type fakeRelease struct {
	srv     *httptest.Server
	badHash bool
}

func newFakeRelease(t *testing.T) *fakeRelease {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("the installer is for Linux and macOS")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	name := "scribble_v1.2.3_" + runtime.GOOS + "_" + runtime.GOARCH
	archive := tarGz(t, map[string]string{"sn": fakeSn, "scribble-server": "server"})
	sum := sha512.Sum512(archive)

	f := &fakeRelease{}
	mux := http.NewServeMux()
	mux.HandleFunc("/johansundell/scribble/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/johansundell/scribble/releases/tag/v1.2.3", http.StatusFound)
	})
	mux.HandleFunc("/johansundell/scribble/releases/tag/v1.2.3", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/johansundell/scribble/releases/download/v1.2.3/"+name+".tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/johansundell/scribble/releases/download/v1.2.3/"+name+".sha512", func(w http.ResponseWriter, r *http.Request) {
		h := hex.EncodeToString(sum[:])
		if f.badHash {
			h = strings.Repeat("0", len(h))
		}
		w.Write([]byte(h + "  " + name + ".tar.gz\n"))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	gz.Close()
	return buf.Bytes()
}

// install runs the script with env on top of a clean environment.
func (f *fakeRelease) install(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{"../../scripts/install-sn.sh"}, args...)...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "VERSION=") && !strings.HasPrefix(kv, "INSTALL_DIR=") && !strings.HasPrefix(kv, "GITHUB_URL=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, append([]string{"GITHUB_URL=" + f.srv.URL, "INSTALL_DIR=" + dir}, env...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func wantSn(t *testing.T, dir, body string) {
	t.Helper()
	p := filepath.Join(dir, "sn")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != body {
		t.Errorf("%s holds %q, want %q", p, b, body)
	}
	if body == fakeSn {
		st, _ := os.Stat(p)
		if st.Mode().Perm() != 0o755 {
			t.Errorf("%s has mode %v, want 0755", p, st.Mode().Perm())
		}
	}
	if _, err := os.Stat(p + ".new"); err == nil {
		t.Errorf("%s.new was left behind", p)
	}
}

func TestInstallLatest(t *testing.T) {
	f := newFakeRelease(t)
	// A directory that doesn't exist yet, in one the user can write to.
	dir := filepath.Join(t.TempDir(), "bin")
	out, err := f.install(t, dir, nil)
	if err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	wantSn(t, dir, fakeSn)
	if want := "installed sn v1.2.3 to " + filepath.Join(dir, "sn"); !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "scribble-server")); err == nil {
		t.Error("installed more than sn")
	}
}

func TestInstallVersionReplacesSn(t *testing.T) {
	f := newFakeRelease(t)
	for _, tc := range []struct {
		name string
		env  []string
		args []string
	}{
		{"argument", nil, []string{"v1.2.3"}},
		{"VERSION", []string{"VERSION=v1.2.3"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "sn"), []byte("old"), 0o755)
			out, err := f.install(t, dir, tc.env, tc.args...)
			if err != nil {
				t.Fatalf("%v:\n%s", err, out)
			}
			wantSn(t, dir, fakeSn)
		})
	}
}

func TestInstallFailsAndKeepsSn(t *testing.T) {
	f := newFakeRelease(t)
	for _, tc := range []struct {
		name    string
		badHash bool
		args    []string
		want    string
	}{
		{"checksum mismatch", true, nil, ""},
		{"invalid tag", false, []string{"1.2"}, "1.2"},
		{"invalid tag with suffix", false, []string{"v1.2.3-rc1"}, "v1.2.3-rc1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.badHash = tc.badHash
			t.Cleanup(func() { f.badHash = false })
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "sn"), []byte("old"), 0o755)
			out, err := f.install(t, dir, nil, tc.args...)
			if err == nil {
				t.Fatalf("want a failure, got:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("output lacks %q:\n%s", tc.want, out)
			}
			wantSn(t, dir, "old")
		})
	}
}
