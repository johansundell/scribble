package gui_test

import (
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
	"github.com/johansundell/scribble/client"
)

// replaceServer points c at a new server whose database is a different
// one, as when the server's database is replaced (#167), and returns a
// client of its own for it.
func replaceServer(t *testing.T, c *client.Client, bodies ...string) *client.Client {
	t.Helper()
	fresh := newServer(t)
	for _, b := range bodies {
		create(t, fresh, b)
	}
	c.BaseURL = fresh.BaseURL
	return fresh
}

func serverTitles(t *testing.T, c *client.Client) []string {
	t.Helper()
	list, err := c.List(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, n := range list {
		out = append(out, n.Title)
	}
	slices.Sort(out)
	return out
}

// An open note with an unsent edit stays open across a reset, as the new
// note the edit becomes, and the new database's note with its old ID is
// left alone.
func TestOpenNoteWithAnUnsentEditFollowsItAcrossAReset(t *testing.T) {
	c := newServer(t)
	cb := plug(c)
	create(t, c, "# Ideas\nfly")
	ui, _ := openCached(t, c, "")
	ui.List.Select(0)

	cb.unplugged.Store(true)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nfly to the moon")
	test.Tap(ui.Save)
	fresh := replaceServer(t, c, "# Other\nstuff")
	cb.unplugged.Store(false)
	ui.Sync()

	if got := serverTitles(t, fresh); !slices.Equal(got, []string{"Ideas", "Other"}) {
		t.Errorf("server has %q", got)
	}
	if got := byTitle(t, fresh, "Other").Body; got != "# Other\nstuff" {
		t.Errorf("the new database's note was overwritten: %q", got)
	}
	if segment(ui, "fly to the moon") == nil {
		t.Errorf("the edited note is not open: %#v", ui.Viewer.Segments)
	}
	if !strings.Contains(ui.Status.Text, "replaced") {
		t.Errorf("status says %q", ui.Status.Text)
	}

	// Saving again edits the same new note.
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nfly to Mars")
	test.Tap(ui.Save)
	if got := byTitle(t, fresh, "Ideas").Body; got != "# Ideas\nfly to Mars" {
		t.Errorf("server holds %q", got)
	}
	if got := serverTitles(t, fresh); !slices.Equal(got, []string{"Ideas", "Other"}) {
		t.Errorf("server has %q", got)
	}
}

// An open note without edits is closed: its ID now names another note.
func TestOpenNoteIsClosedByAReset(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, _ := openCached(t, c, "")
	ui.List.Select(0)

	replaceServer(t, c, "# Other\nstuff")
	ui.Sync()

	assertTitles(t, ui, "Other")
	if segment(ui, "stuff") != nil || segment(ui, "fly") != nil {
		t.Errorf("a note is still open: %#v", ui.Viewer.Segments)
	}
}

// Text typed but not saved when the reset arrives is saved as a new note,
// not over the new database's note with the old ID.
func TestUnsavedTextIsSavedAsANewNoteAfterAReset(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, _ := openCached(t, c, "")
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nfly to the moon")

	fresh := replaceServer(t, c, "# Other\nstuff")
	ui.Sync()
	if ui.Editor.Text != "# Ideas\nfly to the moon" {
		t.Fatalf("editor holds %q", ui.Editor.Text)
	}
	test.Tap(ui.Save)

	if got := byTitle(t, fresh, "Other").Body; got != "# Other\nstuff" {
		t.Errorf("the new database's note was overwritten: %q", got)
	}
	if got := byTitle(t, fresh, "Ideas").Body; got != "# Ideas\nfly to the moon" {
		t.Errorf("server holds %q", got)
	}
}

// A save whose own sync finds the reset ends with the saved text open, as
// the new note it became.
func TestSaveThatMeetsAResetShowsTheSavedNote(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, _ := openCached(t, c, "")
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nfly to the moon")

	fresh := replaceServer(t, c, "# Other\nstuff")
	test.Tap(ui.Save)

	if got := byTitle(t, fresh, "Other").Body; got != "# Other\nstuff" {
		t.Errorf("the new database's note was overwritten: %q", got)
	}
	if got := byTitle(t, fresh, "Ideas").Body; got != "# Ideas\nfly to the moon" {
		t.Errorf("server holds %q", got)
	}
	if ui.Editor.Visible() || segment(ui, "fly to the moon") == nil {
		t.Errorf("the saved note is not shown: %#v", ui.Viewer.Segments)
	}
}
