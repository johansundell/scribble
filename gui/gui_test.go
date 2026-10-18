package gui_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/gui"
	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

type discardSink struct{}

func (discardSink) Enqueue(types.UsageLog) {}

// newServer starts a real scribble server on a temp SQLite file and returns
// a client for it.
func newServer(t *testing.T) *client.Client {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "gui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h, err := handlers.NewHandler(s, false, fstest.MapFS{}, "test", "dev")
	if err != nil {
		t.Fatal(err)
	}
	r, err := router.NewRouter(router.Config{
		Handler: h, LogSink: discardSink{}, Settings: types.AppSettings{AuthToken: "tok"},
		Assets: fstest.MapFS{}, Version: "dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return client.New(srv.URL, "tok")
}

func create(t *testing.T, c *client.Client, body string) {
	t.Helper()
	if _, err := c.Create(context.Background(), body); err != nil {
		t.Fatal(err)
	}
}

// open shows the GUI in a headless test window.
func open(t *testing.T, c *client.Client) (*gui.UI, fyne.Window) {
	t.Helper()
	a := test.NewTempApp(t)
	w := test.NewWindow(nil)
	// Some tests close the window themselves; the test driver panics if it
	// is closed twice.
	t.Cleanup(func() {
		if slices.Contains(a.Driver().AllWindows(), w) {
			w.Close()
		}
	})
	ui := gui.New(w, c)
	w.Resize(fyne.NewSize(900, 600))
	return ui, w
}

// listTitles returns the titles the note list is showing, top to bottom.
func listTitles(ui *gui.UI) []string {
	var out []string
	for _, o := range test.LaidOutObjects(ui.List) {
		if l, ok := o.(*widget.Label); ok && l.Visible() && l.Text != "" {
			out = append(out, l.Text)
		}
	}
	return out
}

func assertTitles(t *testing.T, ui *gui.UI, want ...string) {
	t.Helper()
	if got := listTitles(ui); !slices.Equal(got, want) {
		t.Fatalf("list shows %q, want %q", got, want)
	}
}

func TestListsNotesNewestFirst(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Groceries\nmilk")
	create(t, c, "# Ideas\nfly")

	ui, _ := open(t, c)

	assertTitles(t, ui, "Ideas", "Groceries")
}

func TestSearchFiltersListAsYouType(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Groceries\nmilk and eggs")
	create(t, c, "# Ideas\nfly to the moon")

	ui, _ := open(t, c)
	test.Type(ui.Search, "moon")
	assertTitles(t, ui, "Ideas")

	ui.Search.SetText("")
	assertTitles(t, ui, "Ideas", "Groceries")
}

// segment returns the viewer's text segment with exactly this text, looking
// inside lists and paragraphs too.
func segment(ui *gui.UI, text string) *widget.TextSegment {
	return findSegment(ui.Viewer.Segments, text)
}

func findSegment(segs []widget.RichTextSegment, text string) *widget.TextSegment {
	for _, s := range segs {
		switch s := s.(type) {
		case *widget.TextSegment:
			if s.Text == text {
				return s
			}
		case interface {
			Segments() []widget.RichTextSegment
		}:
			if ts := findSegment(s.Segments(), text); ts != nil {
				return ts
			}
		}
	}
	return nil
}

func TestSelectingNoteShowsRenderedMarkdown(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Groceries\nmilk")
	create(t, c, "# Ideas\nfly **high**")

	ui, _ := open(t, c)
	ui.List.Select(0)

	if s := segment(ui, "high"); s == nil || !s.Style.TextStyle.Bold {
		t.Fatalf("want bold %q in the viewer, got segments %#v", "high", ui.Viewer.Segments)
	}
	if segment(ui, "milk") != nil {
		t.Error("viewer still shows another note")
	}
}

func byTitle(t *testing.T, c *client.Client, title string) *notes.Note {
	t.Helper()
	n, err := c.ByTitle(context.Background(), title)
	if err != nil {
		t.Fatalf("note %q: %v", title, err)
	}
	return n
}

func TestEditAndSaveNote(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")

	ui, _ := open(t, c)
	ui.List.Select(0)
	test.Tap(ui.EditToggle)

	if !ui.Editor.Visible() || ui.Viewer.Visible() {
		t.Fatal("edit mode should show the editor instead of the viewer")
	}
	if ui.Editor.Text != "# Ideas\nfly" {
		t.Fatalf("editor holds %q, want the raw Markdown", ui.Editor.Text)
	}

	ui.Editor.SetText("# Big ideas\nfly **higher**")
	test.Tap(ui.Save)

	saved := byTitle(t, c, "Big ideas")
	if saved.Body != "# Big ideas\nfly **higher**" || saved.Revision != 2 {
		t.Fatalf("server holds %+v", saved)
	}
	if ui.Editor.Visible() || !ui.Viewer.Visible() {
		t.Error("saving should return to view mode")
	}
	if s := segment(ui, "higher"); s == nil || !s.Style.TextStyle.Bold {
		t.Error("viewer does not show the saved note")
	}
	assertTitles(t, ui, "Big ideas")
}

func TestSaveRefusedWhenNoteChangedElsewhere(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")

	ui, _ := open(t, c)
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nmine")

	theirs := byTitle(t, c, "Ideas")
	if _, err := c.Update(context.Background(), theirs.ID, "# Ideas\ntheirs", theirs.Revision); err != nil {
		t.Fatal(err)
	}
	test.Tap(ui.Save)

	if got := byTitle(t, c, "Ideas").Body; got != "# Ideas\ntheirs" {
		t.Fatalf("server holds %q; the other edit was overwritten", got)
	}
	if !ui.Editor.Visible() || ui.Editor.Text != "# Ideas\nmine" {
		t.Error("the user's edit should stay open in the editor")
	}
	if !strings.Contains(ui.Status.Text, "changed elsewhere") {
		t.Errorf("status says %q", ui.Status.Text)
	}
}

func TestCreateNewNote(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")

	ui, _ := open(t, c)
	ui.List.Select(0)
	test.Tap(ui.NewNote)

	if !ui.Editor.Visible() || ui.Editor.Text != "" {
		t.Fatalf("New should open an empty editor, got %q", ui.Editor.Text)
	}
	ui.Editor.SetText("\n\nShopping list\n- **eggs**")
	test.Tap(ui.Save)

	if got := byTitle(t, c, "Shopping list").Body; got != "\n\nShopping list\n- **eggs**" {
		t.Fatalf("server holds %q", got)
	}
	if byTitle(t, c, "Ideas").Body != "# Ideas\nfly" {
		t.Error("the previously open note was changed")
	}
	if s := segment(ui, "eggs"); s == nil || !s.Style.TextStyle.Bold {
		t.Error("viewer does not show the new note")
	}
	assertTitles(t, ui, "Shopping list", "Ideas")
}

// tapDialogButton taps the button labelled text in the dialog on top of w.
func tapDialogButton(t *testing.T, w fyne.Window, text string) {
	t.Helper()
	top := w.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("no dialog is showing")
	}
	for _, o := range test.LaidOutObjects(top) {
		if b, ok := o.(*widget.Button); ok && b.Text == text {
			test.Tap(b)
			return
		}
	}
	t.Fatalf("the dialog has no %q button", text)
}

func TestTogglingToViewPreviewsUnsavedEdits(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")

	ui, _ := open(t, c)
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nfly **higher**")

	test.Tap(ui.EditToggle)
	if s := segment(ui, "higher"); s == nil || !s.Style.TextStyle.Bold {
		t.Error("view mode should preview the unsaved edit")
	}
	test.Tap(ui.EditToggle)
	if ui.Editor.Text != "# Ideas\nfly **higher**" {
		t.Fatalf("editor holds %q; the unsaved edit was lost", ui.Editor.Text)
	}
	if byTitle(t, c, "Ideas").Body != "# Ideas\nfly" {
		t.Error("toggling should not save")
	}
}

func TestNewNoteFromFreshStart(t *testing.T) {
	c := newServer(t)

	ui, _ := open(t, c)
	test.Tap(ui.NewNote)
	ui.Editor.SetText("# Plans\n**soon**")

	if ui.EditToggle.Disabled() {
		t.Fatal("the new note's draft cannot be previewed")
	}
	test.Tap(ui.EditToggle)
	if s := segment(ui, "soon"); s == nil || !s.Style.TextStyle.Bold {
		t.Error("view mode should preview the draft")
	}
	if !ui.Delete.Disabled() {
		t.Error("an unsaved draft cannot be deleted")
	}
	test.Tap(ui.Save)
	byTitle(t, c, "Plans")
	assertTitles(t, ui, "Plans")
}

func TestSwitchingNotesAsksBeforeDiscardingEdits(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Groceries\nmilk")
	create(t, c, "# Ideas\nfly")

	ui, w := open(t, c)
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nunsaved")

	ui.List.Select(1)
	tapDialogButton(t, w, "Cancel")
	if ui.Editor.Text != "# Ideas\nunsaved" || !ui.Editor.Visible() {
		t.Fatalf("cancelling should keep the edit open, editor holds %q", ui.Editor.Text)
	}

	test.Tap(ui.NewNote)
	tapDialogButton(t, w, "Cancel")
	if ui.Editor.Text != "# Ideas\nunsaved" {
		t.Fatalf("cancelling New should keep the edit, editor holds %q", ui.Editor.Text)
	}

	ui.List.Select(1)
	tapDialogButton(t, w, "Discard")
	if segment(ui, "milk") == nil {
		t.Error("discarding should open the other note")
	}
	if byTitle(t, c, "Ideas").Body != "# Ideas\nfly" {
		t.Error("discarding should not save")
	}
}

func TestReopeningAfterConflictLoadsLatestVersion(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Groceries\nmilk")
	create(t, c, "# Ideas\nfly")

	ui, w := open(t, c)
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nmine")
	theirs := byTitle(t, c, "Ideas")
	if _, err := c.Update(context.Background(), theirs.ID, "# Ideas\ntheirs", theirs.Revision); err != nil {
		t.Fatal(err)
	}
	test.Tap(ui.Save) // refused: 412

	ui.List.Select(1)
	tapDialogButton(t, w, "Discard")
	ui.List.Select(0)
	if s := segment(ui, "theirs"); s == nil {
		t.Fatalf("reopening should show the latest version, got %#v", ui.Viewer.Segments)
	}

	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\ntheirs and mine")
	test.Tap(ui.Save)
	if got := byTitle(t, c, "Ideas").Body; got != "# Ideas\ntheirs and mine" {
		t.Fatalf("save after reopening failed: server holds %q, status %q", got, ui.Status.Text)
	}
}

func TestClosingAsksBeforeDiscardingEdits(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")

	ui, w := open(t, c)
	closed := false
	w.SetOnClosed(func() { closed = true })

	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Ideas\nunsaved")

	ui.Close()
	tapDialogButton(t, w, "Cancel")
	if closed || ui.Editor.Text != "# Ideas\nunsaved" {
		t.Fatal("cancelling should keep the window and the edit open")
	}

	ui.Close()
	tapDialogButton(t, w, "Discard")
	if !closed {
		t.Fatal("discarding should close the window")
	}
}

func TestClosingWithoutChangesClosesStraightAway(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")

	ui, w := open(t, c)
	closed := false
	w.SetOnClosed(func() { closed = true })
	ui.List.Select(0)

	ui.Close()
	if !closed {
		t.Fatal("the window should close without asking")
	}
}

func TestSearchKeepsErrorInStatus(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Groceries\nmilk")
	create(t, c, "# Ideas\nfly")

	ui, _ := open(t, c)
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Groceries\ndup title")
	test.Tap(ui.Save)
	if !strings.Contains(ui.Status.Text, "already has that title") {
		t.Fatalf("status says %q", ui.Status.Text)
	}

	test.Type(ui.Search, "fly")
	if !strings.Contains(ui.Status.Text, "already has that title") {
		t.Errorf("searching cleared the error, status says %q", ui.Status.Text)
	}
}

func TestProjectFilterNarrowsList(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Plans\nfor #Scribble and #home")
	create(t, c, "# Bugs\n#scribble sync bug")
	create(t, c, "# Groceries\nmilk")

	ui, _ := open(t, c)
	if want := []string{"All notes", "home", "scribble"}; !slices.Equal(ui.Project.Options, want) {
		t.Fatalf("project options %q, want %q", ui.Project.Options, want)
	}
	if ui.Project.Selected != "All notes" {
		t.Errorf("starts on %q, want All notes", ui.Project.Selected)
	}

	ui.Project.SetSelected("scribble")
	assertTitles(t, ui, "Bugs", "Plans")

	test.Type(ui.Search, "sync")
	assertTitles(t, ui, "Bugs")

	ui.Search.SetText("")
	ui.Project.SetSelected("All notes")
	assertTitles(t, ui, "Groceries", "Bugs", "Plans")
}

// link returns the viewer's hyperlink with exactly this text.
func link(ui *gui.UI, text string) *widget.HyperlinkSegment {
	return findLink(ui.Viewer.Segments, text)
}

func findLink(segs []widget.RichTextSegment, text string) *widget.HyperlinkSegment {
	for _, s := range segs {
		switch s := s.(type) {
		case *widget.HyperlinkSegment:
			if s.Text == text {
				return s
			}
		case interface {
			Segments() []widget.RichTextSegment
		}:
			if l := findLink(s.Segments(), text); l != nil {
				return l
			}
		}
	}
	return nil
}

// tapLink taps the rendered hyperlink with this text in the viewer, the way
// a user clicks it.
func tapLink(t *testing.T, ui *gui.UI, text string) {
	t.Helper()
	for _, o := range test.LaidOutObjects(ui.Viewer) {
		if h, ok := o.(*widget.Hyperlink); ok && h.Text == text {
			// Taps beside the text are ignored, and a link may be laid out
			// wider than its text; aim at the middle of the text.
			test.TapAt(h, fyne.NewPos(h.MinSize().Width/2, h.Size().Height/2))
			return
		}
	}
	t.Fatalf("the viewer shows no %q link, segments %#v", text, ui.Viewer.Segments)
}

func TestTappingProjectLinkSwitchesFilter(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Plans\nfor #Scribble and #home\n\n- see #home-lab\n\n| where |\n| --- |\n| at #work |")
	create(t, c, "# Bugs\n#scribble sync bug")
	create(t, c, "# Lab\n#home-lab")
	create(t, c, "# Job\n#work")

	ui, _ := open(t, c)
	ui.List.Select(3) // Plans
	if link(ui, "#Plans") != nil {
		t.Error("the title line must not become a project link")
	}

	tapLink(t, ui, "#Scribble")
	if ui.Project.Selected != "scribble" {
		t.Fatalf("project filter is %q, want scribble", ui.Project.Selected)
	}
	assertTitles(t, ui, "Bugs", "Plans")

	tapLink(t, ui, "#home-lab") // inside a list item
	assertTitles(t, ui, "Lab", "Plans")
	if segment(ui, "for ") == nil {
		t.Error("switching project should keep the open note")
	}

	tapLink(t, ui, "#work") // inside a table cell
	assertTitles(t, ui, "Job", "Plans")
}

func TestNewNoteInProjectStaysInProject(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Plans\n#scribble")

	ui, w := open(t, c)
	test.Tap(ui.NewNote)
	if ui.Editor.Text != "" {
		t.Fatalf("New outside a project should be empty, got %q", ui.Editor.Text)
	}
	ui.Project.SetSelected("scribble")
	test.Tap(ui.NewNote)
	tapDialogButton(t, w, "Discard") // the empty draft from the first New
	if ui.Editor.Text != "\n\n#scribble\n" {
		t.Fatalf("editor holds %q, want the project tag pre-filled", ui.Editor.Text)
	}

	test.Type(ui.Editor, "Roadmap") // the cursor starts on the title line
	test.Tap(ui.Save)
	assertTitles(t, ui, "Roadmap", "Plans")
}

func TestTappingTagOfUnsavedDraftSwitchesFilter(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Plans\nmilk")

	ui, _ := open(t, c)
	test.Tap(ui.NewNote)
	ui.Editor.SetText("# Draft\nfor #brand-new")
	test.Tap(ui.EditToggle)

	tapLink(t, ui, "#brand-new")
	if ui.Project.Selected != "brand-new" {
		t.Fatalf("project filter is %q, want brand-new", ui.Project.Selected)
	}
	assertTitles(t, ui)
}

func TestNewNoteInProjectStartsOnTitleLine(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Plans\nfirst line\nsecond line #scribble")

	ui, _ := open(t, c)
	ui.Project.SetSelected("scribble")
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.CursorRow, ui.Editor.CursorColumn = 2, 5 // in the note's body
	test.Tap(ui.EditToggle)                            // back to view, no edits

	test.Tap(ui.NewNote)
	test.Type(ui.Editor, "Roadmap")
	if ui.Editor.Text != "Roadmap\n\n#scribble\n" {
		t.Fatalf("editor holds %q; typing should land on the title line", ui.Editor.Text)
	}
}

func TestSavingUntitledProjectDraftIsRefused(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Plans\n#scribble")

	ui, _ := open(t, c)
	ui.Project.SetSelected("scribble")
	test.Tap(ui.NewNote)
	ui.Project.SetSelected("All notes") // the check must not rely on the filter
	test.Tap(ui.Save)

	if _, err := c.ByTitle(context.Background(), "scribble"); !client.IsNotFound(err) {
		t.Fatalf("the untitled draft was saved as %q: %v", "scribble", err)
	}
	if !ui.Editor.Visible() || ui.Editor.Text != "\n\n#scribble\n" {
		t.Errorf("the draft should stay open, editor holds %q", ui.Editor.Text)
	}
	if !strings.Contains(ui.Status.Text, "title") {
		t.Errorf("status says %q", ui.Status.Text)
	}
}

func TestSavingNewTagAddsProject(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Plans\nnothing yet")

	ui, _ := open(t, c)
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	ui.Editor.SetText("# Plans\nnow #tagged")
	test.Tap(ui.Save)

	if want := []string{"All notes", "tagged"}; !slices.Equal(ui.Project.Options, want) {
		t.Fatalf("project options %q, want %q", ui.Project.Options, want)
	}
}

func TestDeleteNoteAfterConfirming(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Groceries\nmilk")
	create(t, c, "# Ideas\nfly")

	ui, w := open(t, c)
	ui.List.Select(0)

	test.Tap(ui.Delete)
	tapDialogButton(t, w, "Cancel")
	byTitle(t, c, "Ideas")
	assertTitles(t, ui, "Ideas", "Groceries")

	test.Tap(ui.Delete)
	tapDialogButton(t, w, "Delete")

	if _, err := c.ByTitle(context.Background(), "Ideas"); !client.IsNotFound(err) {
		t.Fatalf("note still on the server: %v", err)
	}
	assertTitles(t, ui, "Groceries")
	if segment(ui, "fly") != nil {
		t.Error("viewer still shows the deleted note")
	}
}
