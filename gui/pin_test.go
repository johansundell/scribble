package gui_test

import (
	"context"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/gui"
)

// row is a note list row: it takes left clicks and has a context menu.
type row interface {
	fyne.CanvasObject
	fyne.Tappable
	fyne.SecondaryTappable
}

// listRow is the note list's row showing text: the widget around its label,
// which has the row's context menu. Fyne hands a click to the innermost
// widget that takes clicks of either kind, so the row must take both.
func listRow(t *testing.T, ui *gui.UI, text string) row {
	t.Helper()
	var r fyne.SecondaryTappable
	for _, o := range test.LaidOutObjects(ui.List) {
		if s, ok := o.(fyne.SecondaryTappable); ok {
			r = s
		}
		if l, ok := o.(*widget.Label); ok && l.Visible() && l.Text == text {
			if r == nil {
				t.Fatalf("the row %q has no context menu", text)
			}
			both, ok := r.(row)
			if !ok {
				t.Fatalf("the row %q takes no left click", text)
			}
			return both
		}
	}
	t.Fatalf("the list shows no row %q (shows %q)", text, listTitles(ui))
	return nil
}

// #155: the row's context menu made Fyne hand it left clicks too, which it
// dropped. Fyne's test driver picks only left-click widgets, so it can't
// show that; listRow checks the row takes both instead.
func TestClickingAListRowOpensTheNoteAndFocusesTheList(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Groceries\nmilk")
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)

	test.Tap(listRow(t, ui, "Groceries"))
	if ui.Editor.Text != "# Groceries\nmilk" {
		t.Fatalf("clicking Groceries opened %q", ui.Editor.Text)
	}
	// So F2 and the arrow keys reach the list.
	if w.Canvas().Focused() != ui.List {
		t.Fatalf("clicking a note focused %T, not the list", w.Canvas().Focused())
	}

	test.Tap(listRow(t, ui, "Ideas"))
	if ui.Editor.Text != "# Ideas\nfly" {
		t.Fatalf("clicking Ideas opened %q", ui.Editor.Text)
	}
}

func TestPinFromTheFileMenu(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Groceries\nmilk")
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)

	if !menuItem(t, w, "File", "Pin note").Disabled {
		t.Error("Pin note enabled with no note open")
	}
	ui.List.Select(1) // Groceries
	item := menuItem(t, w, "File", "Pin note")
	if item.Disabled {
		t.Fatal("Pin note disabled on a saved note")
	}
	item.Action()

	assertTitles(t, ui, "📌 Groceries", "Ideas")
	if n := byTitle(t, c, "Groceries"); !n.Pinned {
		t.Fatal("the server's note is not pinned")
	}
	// The open note is still Groceries, and the item now unpins it.
	menuItem(t, w, "File", "Unpin note").Action()
	assertTitles(t, ui, "Ideas", "Groceries")
	if n := byTitle(t, c, "Groceries"); n.Pinned {
		t.Fatal("the server's note is still pinned")
	}
	menuItem(t, w, "File", "Pin note")
}

func TestPinFromTheListsContextMenu(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Groceries\nmilk")
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)

	// Any row, not only the open note's.
	test.TapSecondary(listRow(t, ui, "Groceries"))
	tapMenuItem(t, w, "Pin note")
	assertTitles(t, ui, "📌 Groceries", "Ideas")

	test.TapSecondary(listRow(t, ui, "📌 Groceries"))
	tapMenuItem(t, w, "Unpin note")
	assertTitles(t, ui, "Ideas", "Groceries")
}

func TestPinWithUnsavedEditsKeepsThem(t *testing.T) {
	c := newServer(t)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	id := byTitle(t, c, "Ideas").ID
	ui.List.Select(0)
	test.Tap(ui.EditToggle)
	test.Type(ui.Editor, "x")

	menuItem(t, w, "File", "Pin note").Action()
	assertTitles(t, ui, "📌 Ideas")
	if ui.Save.Disabled() {
		t.Fatal("pinning dropped the unsaved edit")
	}
	test.Tap(ui.Save)
	if n, err := c.Get(context.Background(), id); err != nil || !n.Pinned || n.Body != "x# Ideas\nfly" {
		t.Fatalf("after pin and save the server has %+v, %v", n, err)
	}
}

func TestPinNeedsTheServer(t *testing.T) {
	c := newServer(t)
	cb := plug(c)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	ui.List.Select(0)
	cb.unplugged.Store(true)
	ui.Sync()
	if !menuItem(t, w, "File", "Pin note").Disabled {
		t.Error("Pin note enabled while offline")
	}
}

// A server from before pinned notes (API level 4) has no endpoint for them.
func TestPinNeedsAServerWithPinnedNotes(t *testing.T) {
	c := startServer(t, nil, 4)
	create(t, c, "# Ideas\nfly")
	ui, w := open(t, c)
	ui.List.Select(0)
	if !menuItem(t, w, "File", "Pin note").Disabled {
		t.Error("Pin note enabled against an API level 4 server")
	}
}
