package gui_test

import (
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/gui"
	"github.com/johansundell/scribble/offline"
)

// cable fails every request while unplugged, like an unreachable server.
type cable struct {
	unplugged atomic.Bool
	// afterCreate, if set, runs once a note has been created through the
	// cable, before the client sees the answer.
	afterCreate func()
}

func (c *cable) RoundTrip(r *http.Request) (*http.Response, error) {
	if c.unplugged.Load() {
		return nil, errors.New("network is down")
	}
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err == nil && r.Method == "POST" && r.URL.Path == "/notes" && c.afterCreate != nil {
		c.afterCreate()
	}
	return resp, err
}

// plug routes c through a cable the test can unplug.
func plug(c *client.Client) *cable {
	cb := &cable{}
	c.HTTP.Transport = cb
	return cb
}

// openCached shows the GUI over the local copy at path and syncs once,
// like scribble-gui starting up.
func openCached(t *testing.T, c *client.Client, path string) (*gui.UI, fyne.Window) {
	t.Helper()
	test.NewTempApp(t)
	w := test.NewWindow(nil)
	t.Cleanup(func() {
		if slices.Contains(fyne.CurrentApp().Driver().AllWindows(), w) {
			w.Close()
		}
	})
	cache, err := offline.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ui := gui.New(w, c, cache)
	ui.Sync()
	w.Resize(fyne.NewSize(900, 600))
	return ui, w
}

func TestBrowsesAndSearchesSavedNotesWhileServerIsDown(t *testing.T) {
	c := newServer(t)
	cb := plug(c)
	create(t, c, "# Groceries\nmilk #home")
	create(t, c, "# Ideas\nfly")
	path := filepath.Join(t.TempDir(), "cache.json")
	openCached(t, c, path)

	cb.unplugged.Store(true)
	ui, _ := openCached(t, c, path)

	assertTitles(t, ui, "Ideas", "Groceries")
	if !strings.Contains(ui.Status.Text, "unreachable") {
		t.Errorf("status says %q", ui.Status.Text)
	}
	test.Type(ui.Search, "milk")
	assertTitles(t, ui, "Groceries")
	ui.List.Select(0)
	if segment(ui, "milk ") == nil && segment(ui, "milk") == nil {
		t.Errorf("viewer shows %#v", ui.Viewer.Segments)
	}
	if !slices.Contains(ui.Project.Options, "home") {
		t.Errorf("project choices %q", ui.Project.Options)
	}
}

func TestEditSavedWhileServerIsDownIsSentWhenItIsBack(t *testing.T) {
	c := newServer(t)
	cb := plug(c)
	create(t, c, "# Ideas\nfly")
	ui, _ := openCached(t, c, "")

	cb.unplugged.Store(true)
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nfly to the moon")
	test.Tap(ui.Save)

	if ui.Editor.Visible() || segment(ui, "fly to the moon") == nil {
		t.Fatal("the saved edit is not shown")
	}
	if !strings.Contains(ui.Status.Text, "unreachable") {
		t.Errorf("status says %q", ui.Status.Text)
	}
	if byTitle(t, client.New(c.BaseURL, c.Token), "Ideas").Body != "# Ideas\nfly" {
		t.Fatal("reached the server while it was down")
	}

	cb.unplugged.Store(false)
	ui.Sync()

	if got := byTitle(t, c, "Ideas").Body; got != "# Ideas\nfly to the moon" {
		t.Fatalf("server holds %q", got)
	}
	if ui.Status.Text != "" {
		t.Errorf("status still says %q", ui.Status.Text)
	}
}

func TestNoteCreatedWhileServerIsDownStaysOpenOnceSent(t *testing.T) {
	c := newServer(t)
	cb := plug(c)
	ui, _ := openCached(t, c, "")

	cb.unplugged.Store(true)
	test.Tap(ui.NewNote)
	ui.Editor.SetText("# Ideas\nfly")
	test.Tap(ui.Save)
	assertTitles(t, ui, "Ideas")
	if ui.Rename.Disabled() == false {
		t.Error("Rename needs the note on the server")
	}

	cb.unplugged.Store(false)
	ui.Sync()

	byTitle(t, c, "Ideas")
	assertTitles(t, ui, "Ideas")
	if segment(ui, "fly") == nil {
		t.Errorf("the note was closed: %#v", ui.Viewer.Segments)
	}
	if ui.Rename.Disabled() {
		t.Error("Rename should work once the note is on the server")
	}
}

func TestOfflineEditRefusedOnSyncReturnsToTheEditor(t *testing.T) {
	c := newServer(t)
	cb := plug(c)
	create(t, c, "# Other\nstuff")
	create(t, c, "# Ideas\nfly")
	ui, _ := openCached(t, c, "")

	cb.unplugged.Store(true)
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nmine")
	test.Tap(ui.Save)
	ui.List.Select(1) // Other
	cb.unplugged.Store(false)
	n := byTitle(t, c, "Ideas")
	if _, err := c.Update(t.Context(), n.ID, "# Ideas\ntheirs", n.Revision); err != nil {
		t.Fatal(err)
	}

	ui.Sync()

	if !strings.Contains(ui.Status.Text, "could not be synced") {
		t.Fatalf("status says %q", ui.Status.Text)
	}
	if byTitle(t, c, "Ideas").Body != "# Ideas\ntheirs" {
		t.Fatal("the stale edit overwrote the server")
	}

	ui.List.Select(0)
	if !ui.Editor.Visible() || ui.Editor.Text != "# Ideas\nmine" {
		t.Fatalf("editor visible %v with %q, want the refused edit", ui.Editor.Visible(), ui.Editor.Text)
	}
	if !strings.Contains(ui.Status.Text, "changed elsewhere") {
		t.Errorf("status says %q", ui.Status.Text)
	}
	test.Tap(ui.Save) // still stale: the usual conflict flow
	if byTitle(t, c, "Ideas").Body != "# Ideas\ntheirs" || !strings.Contains(ui.Status.Text, "changed elsewhere") {
		t.Fatalf("save went through; status %q", ui.Status.Text)
	}
}

func TestSyncShowsChangesMadeElsewhere(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	create(t, c, "# Old\ngone soon")
	ui, _ := openCached(t, c, "")
	ui.List.Select(1) // Ideas

	n := byTitle(t, c, "Ideas")
	if _, err := c.Update(t.Context(), n.ID, "# Ideas\nswim", n.Revision); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(t.Context(), byTitle(t, c, "Old").ID); err != nil {
		t.Fatal(err)
	}
	ui.Sync()

	assertTitles(t, ui, "Ideas")
	if segment(ui, "swim") == nil {
		t.Errorf("open note not refreshed: %#v", ui.Viewer.Segments)
	}
}

func TestOpenNoteWhoseEditIsRefusedOnSyncReturnsToTheEditor(t *testing.T) {
	c := newServer(t)
	cb := plug(c)
	create(t, c, "# Ideas\nfly")
	ui, _ := openCached(t, c, "")

	cb.unplugged.Store(true)
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nmine")
	test.Tap(ui.Save)
	cb.unplugged.Store(false)
	n := byTitle(t, c, "Ideas")
	if _, err := c.Update(t.Context(), n.ID, "# Ideas\ntheirs", n.Revision); err != nil {
		t.Fatal(err)
	}

	ui.Sync()

	if !ui.Editor.Visible() || ui.Editor.Text != "# Ideas\nmine" {
		t.Fatalf("editor visible %v with %q, want the refused edit", ui.Editor.Visible(), ui.Editor.Text)
	}
	if !strings.Contains(ui.Status.Text, "changed elsewhere") {
		t.Errorf("status says %q", ui.Status.Text)
	}
}

func TestSyncKeepsAnUnsavedPreview(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, _ := openCached(t, c, "")
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nmine")
	test.Tap(ui.EditToggle) // preview, not saved

	n := byTitle(t, c, "Ideas")
	if _, err := c.Update(t.Context(), n.ID, "# Ideas\ntheirs", n.Revision); err != nil {
		t.Fatal(err)
	}
	ui.Sync()

	if ui.Editor.Text != "# Ideas\nmine" {
		t.Fatalf("sync replaced the draft with %q", ui.Editor.Text)
	}
}

func TestSavingAgainAfterOfflineSaveWasSentIsNotAConflict(t *testing.T) {
	c := newServer(t)
	cb := plug(c)
	create(t, c, "# Ideas\nfly")
	ui, _ := openCached(t, c, "")

	cb.unplugged.Store(true)
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nfly high")
	test.Tap(ui.Save)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nfly higher")

	cb.unplugged.Store(false)
	ui.Sync() // sends "fly high" while the editor is still open
	test.Tap(ui.Save)

	if got := byTitle(t, c, "Ideas").Body; got != "# Ideas\nfly higher" {
		t.Fatalf("server holds %q; status %q", got, ui.Status.Text)
	}
}

func TestDraftOfNoteCreatedOfflineStaysStaleAgainstOtherWriters(t *testing.T) {
	c := newServer(t)
	cb := plug(c)
	ui, _ := openCached(t, c, "")

	cb.unplugged.Store(true)
	test.Tap(ui.NewNote)
	ui.Editor.SetText("# Ideas\nfly")
	test.Tap(ui.Save)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nmine")

	other := client.New(c.BaseURL, c.Token)
	cb.afterCreate = func() {
		cb.afterCreate = nil
		n := byTitle(t, other, "Ideas")
		if _, err := other.Update(t.Context(), n.ID, "# Ideas\ntheirs", n.Revision); err != nil {
			t.Error(err)
		}
	}
	cb.unplugged.Store(false)
	ui.Sync() // creates the note, then pulls their edit
	test.Tap(ui.Save)

	if got := byTitle(t, other, "Ideas").Body; got != "# Ideas\ntheirs" {
		t.Fatalf("their edit was overwritten: server holds %q", got)
	}
	if !strings.Contains(ui.Status.Text, "changed elsewhere") {
		t.Errorf("status says %q", ui.Status.Text)
	}
}
