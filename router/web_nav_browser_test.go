package router_test

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/johansundell/scribble/notes"
)

// TestWebNav_Script checks that the app pages load nav.js (#107), which
// swaps in the note instead of loading the whole page.
func TestWebNav_Script(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha")
	for _, path := range []string{"/", fmt.Sprintf("/note/%d", n.ID), editPath(n)} {
		wantBody(t, e.do(get(path, c)), http.StatusOK, `<script src="/assets/js/nav.js" defer></script>`)
	}
	wantNot(t, e.do(get("/login")).Body.String(), "nav.js")
	if w := e.do(get("/assets/js/nav.js")); w.Code != http.StatusOK {
		t.Errorf("GET nav.js = %d, want 200", w.Code)
	}
}

// TestWebNavBrowser checks #107 in Chrome: opening a note from the list, a
// backlink or a [[link]] swaps in the note, with no new page. It needs a
// WebDriver server, as TestWebPhoneBrowser does.
func TestWebNavBrowser(t *testing.T) {
	wd := os.Getenv("SCRIBBLE_WEBDRIVER")
	if wd == "" {
		t.Skip("SCRIBBLE_WEBDRIVER is not set")
	}
	// Enough notes that the list scrolls.
	var bodies []string
	for i := 1; i <= 60; i++ {
		bodies = append(bodies, fmt.Sprintf("# Filler %02d\nText %d.", i, i))
	}
	bodies = append(bodies, "# Alpha\nSee [[Beta]].", "# Beta\nThe second note.")
	srv, ns := newBrowserServer(t, bodies...)
	filler, alpha, beta := ns[0], ns[60], ns[61]

	t.Run("wide", func(t *testing.T) {
		d := newDriver(t, wd, 1024, 768, 1, false)
		d.login(srv)
		d.mark()

		// A title from far down the list.
		d.exec(`document.querySelector(arguments[0]).scrollIntoView()`, listLink(filler))
		var top float64
		d.execInto(&top, `return document.querySelector(".note-list").scrollTop`)
		if top == 0 {
			t.Fatal("the list doesn't scroll")
		}
		d.click(listLink(filler))
		d.waitNote(filler)
		d.wantSamePage("list title")
		var after float64
		d.execInto(&after, `return document.querySelector(".note-list").scrollTop`)
		if after != top {
			t.Errorf("list scrolled from %v to %v", top, after)
		}

		// A [[link]] in a note's text, then the backlink.
		d.click(listLink(alpha))
		d.waitNote(alpha)
		d.click(`.note-body a[href="/title/Beta"]`)
		d.waitNote(beta)
		d.click(fmt.Sprintf(`.backlinks a[href="/note/%d"]`, alpha.ID))
		d.waitNote(alpha)
		d.wantSamePage("links")

		// Back and Forward.
		d.exec(`history.back()`)
		d.waitNote(beta)
		d.exec(`history.back()`)
		d.waitNote(alpha)
		d.exec(`history.forward()`)
		d.waitNote(beta)
		d.wantSamePage("history")

		// note.js came with the swapped-in note: Delete asks.
		d.click(".note-actions > [data-confirm]")
		if got := d.alertText(); !strings.Contains(got, "Beta") {
			t.Errorf("Delete asks %q, want it to name Beta", got)
		}
		d.call("POST", "/alert/dismiss", map[string]any{}, nil)

		// A reload shows the same page.
		d.open(srv + fmt.Sprintf("/note/%d", beta.ID))
		d.waitNote(beta)

		// Unsaved edits still ask, and Cancel keeps the editor.
		d.open(fmt.Sprintf("%s/note/%d/edit", srv, alpha.ID))
		d.exec(`var t = document.getElementById("note-text"); t.value += "\nMore."; t.dispatchEvent(new Event("input"))`)
		d.mark()
		d.click(listLink(beta))
		if got := d.alertText(); !strings.Contains(got, "Leave without saving?") {
			t.Errorf("leaving the editor asks %q", got)
		}
		d.call("POST", "/alert/dismiss", map[string]any{}, nil)
		d.waitPath(fmt.Sprintf("/note/%d/edit", alpha.ID))
		if !d.visible("form.editor") {
			t.Error("Cancel left the editor")
		}
		d.wantSamePage("editor")
		// From the editor a title loads the whole page.
		d.click(listLink(beta))
		d.call("POST", "/alert/accept", map[string]any{}, nil)
		d.waitNote(beta)
		if d.marked() {
			t.Error("the editor swapped in a note; want a page load")
		}

		// An ended session goes to the login page.
		d.call("DELETE", "/cookie", nil, nil)
		d.click(listLink(alpha))
		d.waitPath(fmt.Sprintf("/login?next=/note/%d", alpha.ID))
	})

	t.Run("phone", func(t *testing.T) {
		d := newDriver(t, wd, 375, 667, 2, false)
		d.login(srv)
		d.mark()
		d.click(listLink(beta))
		d.waitNote(beta)
		d.wantSamePage("phone title")
		if d.visible(".note-list") || !d.visible(".note-body") {
			t.Error("phone: want only the note")
		}
		d.click(".back-link")
		d.waitPath("/")
		if !d.visible(".note-list") || d.visible(".note-view") {
			t.Error("phone: ← Notes doesn't show the list")
		}
	})
}

func listLink(n *notes.Note) string {
	return fmt.Sprintf(`.note-items a[href="/note/%d"]`, n.ID)
}

// mark sets a flag on the page that a page load clears.
func (d *webDriver) mark() {
	d.t.Helper()
	d.exec(`window.navMark = true`)
}

func (d *webDriver) marked() bool {
	d.t.Helper()
	var ok bool
	d.execInto(&ok, `return window.navMark === true`)
	return ok
}

func (d *webDriver) wantSamePage(step string) {
	d.t.Helper()
	if !d.marked() {
		d.t.Errorf("%s: the whole page loaded", step)
	}
}

// waitNote waits until n is open: the URL, the tab title, the note and the
// highlighted title all match it.
func (d *webDriver) waitNote(n *notes.Note) {
	d.t.Helper()
	d.waitPath(fmt.Sprintf("/note/%d", n.ID))
	var got struct {
		Title, Heading string
		Current        []string
		Rest           int
	}
	for i := 0; i < 50; i++ {
		d.execInto(&got, `var h = document.querySelector(".note-body h1");
			return {
				title: document.title,
				heading: h ? h.textContent : "",
				current: Array.from(document.querySelectorAll('.note-items a[aria-current="page"]')).map(function (a) { return a.getAttribute("href"); }),
				rest: document.readyState === "complete" ? 0 : 1,
			}`)
		if got.Heading == n.Title && got.Rest == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got.Heading != n.Title {
		d.t.Fatalf("note heading %q, want %q", got.Heading, n.Title)
	}
	if want := n.Title + " – Scribble"; got.Title != want {
		d.t.Errorf("tab title %q, want %q", got.Title, want)
	}
	if want := fmt.Sprintf("/note/%d", n.ID); len(got.Current) != 1 || got.Current[0] != want {
		d.t.Errorf("highlighted %v, want [%s]", got.Current, want)
	}
}

// alertText waits for an alert or confirm() and returns its text.
func (d *webDriver) alertText() string {
	d.t.Helper()
	var text string
	var err error
	for i := 0; i < 20; i++ {
		if err = d.try("GET", "/alert/text", nil, &text); err == nil {
			return text
		}
		time.Sleep(100 * time.Millisecond)
	}
	d.t.Fatal(err)
	return ""
}
