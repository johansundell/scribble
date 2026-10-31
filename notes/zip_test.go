package notes

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func zipNames(t *testing.T, b []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		buf.ReadFrom(rc)
		rc.Close()
		out[f.Name] = buf.String()
	}
	return out
}

func TestWriteZipNamesFilesAfterTitles(t *testing.T) {
	var buf bytes.Buffer
	err := WriteZip(&buf, []Note{
		{Title: "Shopping", Body: "# Shopping\n\neggs\n"},
		{Title: `a/b\c:d*e?f"g<h>i|j`, Body: "# a/b\\c:d*e?f\"g<h>i|j\n"},
		{Title: "Dup", Body: "# Dup\none\n"},
		{Title: "dup", Body: "# dup\ntwo\n"},
		{Title: "Dup", Body: "# Dup\nthree\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := zipNames(t, buf.Bytes())
	want := map[string]string{
		"Shopping.md":            "# Shopping\n\neggs\n",
		"a_b_c_d_e_f_g_h_i_j.md": "# a/b\\c:d*e?f\"g<h>i|j\n",
		"Dup.md":                 "# Dup\none\n",
		"dup-2.md":               "# dup\ntwo\n",
		"Dup-3.md":               "# Dup\nthree\n",
	}
	if len(got) != len(want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	for name, body := range want {
		if got[name] != body {
			t.Errorf("%s = %q, want %q (all: %v)", name, got[name], body, got)
		}
	}
}

func TestFileNameFallsBackForEmptyOrDotTitles(t *testing.T) {
	for title, want := range map[string]string{"": "untitled", "..": "untitled", " .hidden ": "hidden"} {
		if got := fileNameFor(title); got != want {
			t.Errorf("fileNameFor(%q) = %q, want %q", title, got, want)
		}
	}
}

func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func readBodies(t *testing.T, b []byte) map[string]string {
	t.Helper()
	list, err := ReadZip(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, n := range list {
		if n.Title != TitleOf(n.Body) {
			t.Errorf("Title %q != TitleOf(body) %q", n.Title, TitleOf(n.Body))
		}
		out[n.Title] = n.Body
	}
	return out
}

func TestReadZipRoundTrip(t *testing.T) {
	in := []Note{
		{Title: "Shopping", Body: "# Shopping\n\neggs\n"},
		{Title: "a/b", Body: "# a/b\n"},
		{Title: "Plain title", Body: "Plain title\nno heading\n"},
		{Title: "Dup", Body: "# Dup\n"},
		{Title: "dup", Body: "# dup\n"},
	}
	var buf bytes.Buffer
	if err := WriteZip(&buf, in); err != nil {
		t.Fatal(err)
	}
	got := readBodies(t, buf.Bytes())
	for _, n := range in {
		if got[n.Title] != n.Body {
			t.Errorf("%q: body %q, want %q", n.Title, got[n.Title], n.Body)
		}
	}
	if len(got) != len(in) {
		t.Errorf("got %d notes, want %d: %v", len(got), len(in), got)
	}
}

func TestReadZipIsTolerant(t *testing.T) {
	tomboy := `<?xml version="1.0"?><note version="0.3" xmlns="http://beatniksoftware.com/tomboy"><title>Old note</title><text><note-content version="0.1">Old note
hello</note-content></text></note>`
	got := readBodies(t, buildZip(t, map[string]string{
		"dir/Todo.txt":          "buy milk\n",
		"Heading.md":            "## Heading\nbody\n",
		"Empty.md":              "",
		"old.note":              tomboy,
		"picture.png":           "\x89PNG",
		"__MACOSX/._Heading.md": "junk",
		".DS_Store":             "junk",
		"folder/":               "",
		"Upper.MD":              "# Upper\n",
		"Windows.md":            "# Windows\r\nline\r\n",
	}))
	want := map[string]string{
		"Todo":     "# Todo\n\nbuy milk\n",
		"Heading":  "## Heading\nbody\n",
		"Empty":    "# Empty\n",
		"Old note": "",
		"Upper":    "# Upper\n",
		"Windows":  "# Windows\nline\n",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want titles of %v", got, want)
	}
	for title, body := range want {
		b, ok := got[title]
		if !ok {
			t.Errorf("missing %q in %v", title, got)
			continue
		}
		if title == "Old note" {
			if !strings.HasPrefix(b, "# Old note\n") || !strings.Contains(b, "hello") {
				t.Errorf("tomboy body = %q", b)
			}
			continue
		}
		if b != body {
			t.Errorf("%q: body %q, want %q", title, b, body)
		}
	}
}

func TestReadZipRejectsNonZip(t *testing.T) {
	b := []byte("not a zip")
	if _, err := ReadZip(bytes.NewReader(b), int64(len(b))); err == nil {
		t.Fatal("want error")
	}
}

func TestReadZipRejectsOversizedEntry(t *testing.T) {
	b := buildZip(t, map[string]string{"Big.md": "# Big\n" + strings.Repeat("x", MaxZipEntryBytes)})
	if _, err := ReadZip(bytes.NewReader(b), int64(len(b))); err == nil {
		t.Fatal("want error for an entry over MaxZipEntryBytes")
	}
}

func TestZipFileName(t *testing.T) {
	if got := ZipFileName(mustDate(t, "2026-10-31")); got != "scribble-notes-2026-10-31.zip" {
		t.Errorf("ZipFileName = %q", got)
	}
}

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestReadZipHandlesBOMAndWindowsPaths(t *testing.T) {
	got := readBodies(t, buildZip(t, map[string]string{
		"Bom.md":               "\ufeff# Bom title\nbody\n",
		`dir\Windows path.txt`: "text\n",
	}))
	if got["Bom title"] != "# Bom title\nbody\n" {
		t.Errorf("BOM file: %v", got)
	}
	if got["Windows path"] != "# Windows path\n\ntext\n" {
		t.Errorf("backslash path: %v", got)
	}
}

func TestFileNameForCapsLength(t *testing.T) {
	long := strings.Repeat("ä", 300)
	name := fileNameFor(long)
	if len(name) > maxFileNameBytes || !utf8.ValidString(name) {
		t.Errorf("fileNameFor gave %d bytes, valid UTF-8 %v", len(name), utf8.ValidString(name))
	}
	// Round trip still works: the long title is in the heading.
	got := readBodies(t, func() []byte {
		var buf bytes.Buffer
		WriteZip(&buf, []Note{{Title: long, Body: "# " + long + "\n"}, {Title: long + "x", Body: long + "x\n"}})
		return buf.Bytes()
	}())
	if got[long] != "# "+long+"\n" {
		t.Errorf("long title lost: %d notes", len(got))
	}
}
