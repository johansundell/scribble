package gui_test

import (
	"context"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/gui"
)

// listRow is the note list's row showing text: the widget around its label,
// which has the row's context menu.
func listRow(t *testing.T, ui *gui.UI, text string) fyne.SecondaryTappable {
	t.Helper()
	var row fyne.SecondaryTappable
	for _, o := range test.LaidOutObjects(ui.List) {
		if r, ok := o.(fyne.SecondaryTappable); ok {
			row = r
		}
		if l, ok := o.(*widget.Label); ok && l.Visible() && l.Text == text {
			if row == nil {
				t.Fatalf("the row %q has no context menu", text)
			}
			return row
		}
	}
	t.Fatalf("the list shows no row %q (shows %q)", text, listTitles(ui))
	return nil
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
