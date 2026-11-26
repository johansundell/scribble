package router_test

import (
	"encoding/base64"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestWebImagesBrowser checks #201 in Chrome: an image dropped, pasted or
// chosen with the Image button is uploaded and its reference put at the
// cursor, a refused file leaves an error and no placeholder, the saved note
// shows the image, and an edit link's editor uploads too. It needs a
// WebDriver server, as TestWebPhoneBrowser does.
func TestWebImagesBrowser(t *testing.T) {
	wd := os.Getenv("SCRIBBLE_WEBDRIVER")
	if wd == "" {
		t.Skip("SCRIBBLE_WEBDRIVER is not set")
	}
	srv, ns := newBrowserServer(t, "# Pics\n")
	n := ns[0]
	png := func(w int) string { return base64.StdEncoding.EncodeToString(pngOf(t, w)) }

	// files is JavaScript making a DataTransfer of one file from base64.
	files := func(name, mime, b64 string) string {
		return `var b = Uint8Array.from(atob("` + b64 + `"), function (c) { return c.charCodeAt(0); });
			var dt = new DataTransfer(); dt.items.add(new File([b], "` + name + `", { type: "` + mime + `" }));`
	}
	drop := func(d *webDriver, name, mime, b64 string) {
		d.exec(files(name, mime, b64) + `var t = document.getElementById("note-text");
			t.focus(); t.setSelectionRange(t.value.length, t.value.length);
			t.dispatchEvent(new DragEvent("drop", { bubbles: true, cancelable: true, dataTransfer: dt }));`)
	}
	text := func(d *webDriver) string {
		var s string
		d.execInto(&s, `return document.getElementById("note-text").value`)
		return s
	}
	ref := regexp.MustCompile(`!\[([^\]]*)\]\((/images/[0-9a-f]{64}\.png)\)`)
	waitRefs := func(d *webDriver, count int) [][]string {
		t.Helper()
		d.waitFor(`var v = document.getElementById("note-text").value;
			return (v.match(/!\[[^\]]*\]\(\/images\/[0-9a-f]{64}\.png\)/g) || []).length === ` + strconv.Itoa(count) + ` && v.indexOf("Uploading") < 0`)
		return ref.FindAllStringSubmatch(text(d), -1)
	}

	var link string
	t.Run("app", func(t *testing.T) {
		d := newDriver(t, wd, 1024, 768, 1, false)
		d.login(srv)
		d.open(srv + notePath(n) + "/edit")
		if !d.visible("[data-image-button]") {
			t.Fatal("no Image button")
		}

		drop(d, "dropped.png", "image/png", png(3))
		got := waitRefs(d, 1)
		if got[0][1] != "dropped.png" {
			t.Errorf("dropped alt = %q", got[0][1])
		}

		// A pasted screenshot has no text with it.
		d.exec(files("image.png", "image/png", png(4)) + `var t = document.getElementById("note-text");
			t.focus(); t.setSelectionRange(t.value.length, t.value.length);
			t.dispatchEvent(new ClipboardEvent("paste", { bubbles: true, cancelable: true, clipboardData: dt }));`)
		waitRefs(d, 2)

		// The Image button opens the file picker; choosing sends change.
		d.exec(files("chosen.png", "image/png", png(5)) + `var i = document.querySelector("[data-image-file]");
			i.files = dt.files; i.dispatchEvent(new Event("change", { bubbles: true }));`)
		got = waitRefs(d, 3)

		// A refused file: its placeholder goes and the error shows.
		drop(d, "notes.txt", "text/plain", base64.StdEncoding.EncodeToString([]byte("plain text")))
		d.wantText(".editor-error", "only PNG, JPEG, GIF and WebP")
		d.waitFor(`return document.getElementById("note-text").value.indexOf("Uploading") < 0`)

		d.click(`button[value="done"]`)
		d.waitPath(notePath(n))
		d.waitFor(`var imgs = document.querySelectorAll(".note-body img");
			return imgs.length === 3 && Array.prototype.every.call(imgs, function (i) { return i.complete && i.naturalWidth > 0; })`)
		var src string
		d.execInto(&src, `return document.querySelector(".note-body img").getAttribute("src")`)
		if src != got[0][2] {
			t.Errorf("first image src = %q, want %q", src, got[0][2])
		}

		d.open(srv + notePath(n) + "/share")
		d.click(`button[value="share-edit"]`)
		d.waitFor(`return !!document.getElementById("edit-url")`)
		d.execInto(&link, `return document.getElementById("edit-url").value`)
	})
	if !strings.Contains(link, "/e/") {
		t.Fatalf("edit link %q", link)
	}
	// The browser reaches the server at srv's host.
	link = srv + link[strings.Index(link, "/e/"):]

	t.Run("edit link", func(t *testing.T) {
		d := newDriver(t, wd, 1024, 768, 1, false) // no session
		d.open(link + "/edit")
		drop(d, "linked.png", "image/png", png(6))
		got := waitRefs(d, 4)
		if got[3][1] != "linked.png" {
			t.Errorf("edit link alt = %q", got[3][1])
		}
	})
}
