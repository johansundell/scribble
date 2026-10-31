package gui_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/gui"
	"github.com/johansundell/scribble/notes"
)

// pickFiles makes the file dialogs answer path, and records the name the
// save dialog suggested.
func pickFiles(t *testing.T, path string) *string {
	t.Helper()
	var suggested string
	t.Cleanup(gui.SetFilePickersForTest(
		func(name string, cb func(fyne.URIWriteCloser, error)) {
			suggested = name
			cb(storage.Writer(storage.NewFileURI(path)))
		},
		func(cb func(fyne.URIReadCloser, error)) {
			cb(storage.Reader(storage.NewFileURI(path)))
		},
	))
	return &suggested
}

// dialogText is the text of the labels in the top dialog.
func dialogText(w fyne.Window) string {
	var out []string
	if top := w.Canvas().Overlays().Top(); top != nil {
		for _, o := range test.LaidOutObjects(top) {
			if l, ok := o.(*widget.Label); ok {
				out = append(out, l.Text)
			}
		}
	}
	return strings.Join(out, "\n")
}

func TestFileMenuExportsNotes(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	_, w := open(t, c)
	path := filepath.Join(t.TempDir(), "out.zip")
	suggested := pickFiles(t, path)

	menuItem(t, w, "File", "Export notes…").Action()
	if !strings.HasPrefix(*suggested, "scribble-notes-") || !strings.HasSuffix(*suggested, ".zip") {
		t.Errorf("suggested name %q", *suggested)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	list, err := notes.ReadZip(bytes.NewReader(b), int64(len(b)))
	if err != nil || len(list) != 1 || list[0].Body != "# Ideas\nfly" {
		t.Fatalf("exported %+v, %v", list, err)
	}
	if !strings.Contains(dialogText(w), "out.zip") {
		t.Errorf("export dialog says %q", dialogText(w))
	}
}

func TestFileMenuImportsNotes(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	path := filepath.Join(t.TempDir(), "in.zip")
	var buf bytes.Buffer
	notes.WriteZip(&buf, []notes.Note{{Title: "Ideas", Body: "# Ideas\nswim"}, {Title: "Plans", Body: "# Plans\n"}})
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	pickFiles(t, path)

	// Skipping existing notes, the default.
	menuItem(t, w, "File", "Import notes…").Action()
	tapDialogButton(t, w, "Import")
	if got := dialogText(w); !strings.Contains(got, "Imported 1 note, skipped 1.") {
		t.Errorf("result dialog says %q", got)
	}
	tapDialogButton(t, w, "OK")
	assertTitles(t, ui, "Plans", "Ideas")

	// Overwriting them, with the box ticked.
	menuItem(t, w, "File", "Import notes…").Action()
	for _, o := range test.LaidOutObjects(w.Canvas().Overlays().Top()) {
		if cb, ok := o.(*widget.Check); ok {
			cb.SetChecked(true)
		}
	}
	tapDialogButton(t, w, "Import")
	tapDialogButton(t, w, "OK")
	if n := byTitle(t, c, "Ideas"); n.Body != "# Ideas\nswim" {
		t.Errorf("overwrite left %q", n.Body)
	}

	// An archive without notes says so, and offers no Import.
	var empty bytes.Buffer
	notes.WriteZip(&empty, nil)
	os.WriteFile(path, empty.Bytes(), 0o644)
	menuItem(t, w, "File", "Import notes…").Action()
	if got := dialogText(w); !strings.Contains(got, "holds no .md, .txt or .note files") {
		t.Errorf("empty archive dialog says %q", got)
	}
	tapDialogButton(t, w, "OK")
	os.WriteFile(path, buf.Bytes(), 0o644)

	// Cancel imports nothing; a file that isn't a ZIP is refused.
	menuItem(t, w, "File", "Import notes…").Action()
	tapDialogButton(t, w, "Cancel")
	os.WriteFile(path, []byte("nope"), 0o644)
	menuItem(t, w, "File", "Import notes…").Action()
	if w.Canvas().Overlays().Top() != nil {
		t.Error("a bad file should not ask to import")
	}
	if !strings.Contains(ui.Status.Text, "ZIP") {
		t.Errorf("status = %q", ui.Status.Text)
	}
}
