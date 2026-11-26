package router_test

import (
	"os"
	"strings"
	"testing"
)

// TestWebShareEditBrowser checks #195 in Chrome: the share pane's two
// sections, and someone without a session editing the note through its
// edit link, being refused a new title, and seeing their save in the link's
// history. It needs a WebDriver server, as TestWebPhoneBrowser does.
func TestWebShareEditBrowser(t *testing.T) {
	wd := os.Getenv("SCRIBBLE_WEBDRIVER")
	if wd == "" {
		t.Skip("SCRIBBLE_WEBDRIVER is not set")
	}
	srv, ns := newBrowserServer(t, "# Alpha\nsecret before sharing")
	n := ns[0]
	share := notePath(n) + "/share"

	var link string
	t.Run("pane", func(t *testing.T) {
		d := newDriver(t, wd, 1024, 768, 1, false)
		d.login(srv)
		d.open(srv + notePath(n) + "/edit")
		d.exec(`var t = document.getElementById("note-text"); t.value = "# Alpha\nwhat was shared"; t.dispatchEvent(new Event("input"))`)
		d.click(`button[value="done"]`)
		d.waitPath(notePath(n))

		d.open(srv + share)
		d.wantText("#view-link-heading", "View link")
		d.wantText("#edit-link-heading", "Edit link")
		d.wantText(".share-warning", "Anyone with the edit link can change the note")
		d.exec(`document.getElementById("edit-ttl").value = "7d"`)
		d.click(`button[value="share-edit"]`)
		d.waitPath(share)
		d.execInto(&link, `return document.getElementById("edit-url").value`)
		if !strings.Contains(link, "/e/") {
			t.Fatalf("edit link %q", link)
		}
		// One Copy button per link, and only the edit link has a QR code.
		if !d.visible(`[data-copy="edit-url"]`) || d.visible(`[data-copy="share-url"]`) {
			t.Error("Copy link is not shown for the edit link alone")
		}
		var qrs int
		d.execInto(&qrs, `return document.querySelectorAll(".share .qr").length`)
		if qrs != 1 {
			t.Errorf("%d QR codes, want 1", qrs)
		}
		d.wantText(".share-link:nth-of-type(2) .muted", "Expires on")
		// The view link is still to be made.
		if !d.visible(`button[value="share"]`) {
			t.Error("making the edit link hid Create public link")
		}
	})
	if link == "" {
		t.Fatal("no edit link")
	}
	// The browser reaches the server at srv's host.
	link = srv + link[strings.Index(link, "/e/"):]
	path := strings.TrimPrefix(link, srv)

	t.Run("link", func(t *testing.T) {
		d := newDriver(t, wd, 1024, 768, 1, false) // no session
		d.open(link)
		d.wantText(".note-body h1", "Alpha")
		d.wantText(".shared", "Expires on")
		d.click(`a[href$="/edit"]`)
		d.waitPath(path + "/edit")

		// A new title is refused, and the text stays.
		d.exec(`var t = document.getElementById("note-text"); t.value = "# Renamed\nmine"; t.dispatchEvent(new Event("input"))`)
		d.click(`button[value="save"]`)
		d.wantText(".editor-error", "The title can't be changed through a share link")
		var text string
		d.execInto(&text, `return document.getElementById("note-text").value`)
		if text != "# Renamed\nmine" {
			t.Errorf("text after a refused save: %q", text)
		}

		// A save that keeps it goes through; Done goes back to the note.
		d.exec(`var t = document.getElementById("note-text"); t.value = "# Alpha\nedited via link"; t.dispatchEvent(new Event("input"))`)
		d.click(`button[value="done"]`)
		d.waitPath(path)
		d.wantText(".note-body", "edited via link")

		// History has only the save made through the link.
		d.click(`a[href$="/history"]`)
		d.waitPath(path + "/history")
		d.wantText(".history-list", "Edited via link")
		var page string
		d.execInto(&page, `return document.body.textContent`)
		if strings.Contains(page, "secret before sharing") || strings.Contains(page, "Restore") {
			t.Errorf("history shows too much:\n%s", page)
		}
	})
}
