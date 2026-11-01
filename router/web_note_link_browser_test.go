package router_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestWebNoteLinkBrowser checks the picker in Chrome. It needs a WebDriver
// server, as TestWebPhoneBrowser does.
func TestWebNoteLinkBrowser(t *testing.T) {
	wd := os.Getenv("SCRIBBLE_WEBDRIVER")
	if wd == "" {
		t.Skip("SCRIBBLE_WEBDRIVER is not set")
	}
	srv, ns := newBrowserServer(t,
		"# Alpha\nSee here.",
		"# Beta\n#work",
		"# Better notes\n#work",
		"# Gamma\n#home",
	)
	alpha := ns[0]

	t.Run("wide", func(t *testing.T) {
		d := newDriver(t, wd, 1024, 768, 1, false)
		d.login(srv)
		d.open(fmt.Sprintf("%s/note/%d/edit", srv, alpha.ID))
		d.waitFor(`return document.querySelector("form.editor").dataset.markdown === "true"`)

		// "here" selected: the button opens the picker searching for it.
		d.exec(`var t = document.getElementById("note-text"); var at = t.value.indexOf("here");
			t.focus(); t.setSelectionRange(at, at + 4)`)
		d.click("[data-note-link]")
		d.waitFor(`return document.getElementById("note-link-dialog").open`)
		var got struct {
			Query, Focus string
		}
		d.execInto(&got, `return {query: document.getElementById("note-link-search").value,
			focus: document.activeElement.id}`)
		if got.Query != "here" || got.Focus != "note-link-search" {
			t.Errorf("picker opened with %q, focus on %q; want \"here\" in the focused search", got.Query, got.Focus)
		}
		if titles := d.pickerTitles(); len(titles) != 0 {
			t.Errorf("\"here\" lists %v, want none", titles)
		}

		// Typing narrows the list; the open note is left out.
		d.typeSearch("")
		if titles := d.pickerTitles(); strings.Join(titles, ",") != "Gamma,Better notes,Beta" {
			t.Errorf("all titles = %v, want Gamma, Better notes, Beta", titles)
		}
		d.typeSearch("bet")
		if titles := d.pickerTitles(); strings.Join(titles, ",") != "Better notes,Beta" {
			t.Errorf("\"bet\" lists %v", titles)
		}

		// Down picks the second; Enter links it in place of the selection.
		d.keys("\uE015", "\uE007")                                    // ArrowDown, Enter
		d.waitFor(`return document.activeElement.id === "note-text"`) // the dialog closed
		var after struct {
			Value, Focus string
		}
		d.execInto(&after, `return {value: document.getElementById("note-text").value, focus: document.activeElement.id}`)
		if !strings.Contains(after.Value, "See [[Beta]].") || after.Focus != "note-text" {
			t.Errorf("after Enter: text %q, focus on %q", after.Value, after.Focus)
		}

		// Ctrl+Z takes the link back out in one step.
		d.keys("\uE009", "z", "\uE009") // Ctrl down, z, Ctrl up
		d.execInto(&after.Value, `return document.getElementById("note-text").value`)
		if !strings.Contains(after.Value, "See here.") {
			t.Errorf("after Ctrl+Z: %q, want \"See here.\" back", after.Value)
		}

		// Ctrl+Shift+K opens it too; one Esc closes it, even with a search
		// filled in from the selection, and changes nothing.
		d.exec(`var t = document.getElementById("note-text"); var at = t.value.indexOf("here");
			t.focus(); t.setSelectionRange(at, at + 4)`)
		d.keys("\uE009", "\uE008", "k", "\uE008", "\uE009")
		d.waitFor(`return document.getElementById("note-link-dialog").open`)
		d.keys("\uE00C")                                              // Escape
		d.waitFor(`return document.activeElement.id === "note-text"`) // the dialog closed
		d.execInto(&after, `return {value: document.getElementById("note-text").value, focus: document.activeElement.id}`)
		if !strings.Contains(after.Value, "See here.") || strings.Contains(after.Value, "[[") || after.Focus != "note-text" {
			t.Errorf("after Esc: text %q, focus on %q", after.Value, after.Focus)
		}
		d.exec(`var t = document.getElementById("note-text"); t.setSelectionRange(t.value.length, t.value.length)`)

		// A tap on a title links it at the cursor.
		d.click("[data-note-link]")
		d.waitFor(`return document.getElementById("note-link-dialog").open`)
		d.click(`#note-link-list [role=option]:first-child`)
		d.waitFor(`return document.activeElement.id === "note-text"`) // the dialog closed
		d.execInto(&after.Value, `return document.getElementById("note-text").value`)
		if !strings.HasSuffix(after.Value, "[[Gamma]]") {
			t.Errorf("after a tap: %q, want it to end with [[Gamma]]", after.Value)
		}
	})

	// With the list filtered, every note can still be linked to.
	t.Run("filtered", func(t *testing.T) {
		d := newDriver(t, wd, 1024, 768, 1, false)
		d.login(srv)
		d.open(fmt.Sprintf("%s/note/%d/edit?project=home", srv, alpha.ID))
		d.waitFor(`return document.querySelector("form.editor").dataset.markdown === "true"`)
		d.exec(`document.getElementById("note-text").focus()`)
		d.click("[data-note-link]")
		d.waitFor(`return document.getElementById("note-link-dialog").open`)
		d.waitFor(`return document.querySelectorAll("#note-link-list [role=option]").length === 3`)
		if titles := d.pickerTitles(); strings.Join(titles, ",") != "Gamma,Better notes,Beta" {
			t.Errorf("filtered list: picker has %v, want every other note", titles)
		}
	})

	// On a phone the picker fits and its targets are big enough.
	t.Run("phone", func(t *testing.T) {
		d := newDriver(t, wd, 375, 667, 2, false)
		d.login(srv)
		d.open(fmt.Sprintf("%s/note/%d/edit", srv, alpha.ID))
		d.waitFor(`return document.querySelector("form.editor").dataset.markdown === "true"`)
		d.exec(`document.getElementById("note-text").focus()`)
		d.exec(`document.querySelector("[data-note-link]").scrollIntoView()`)
		d.click("[data-note-link]")
		d.waitFor(`return document.getElementById("note-link-dialog").open`)
		d.checkPage("note link picker")
		var small []string
		d.execInto(&small, `return Array.from(document.querySelectorAll("#note-link-list [role=option]"))
			.filter(function (o) { return o.getBoundingClientRect().height < 44; })
			.map(function (o) { return o.textContent; })`)
		if len(small) > 0 {
			t.Errorf("titles under 44 px high: %v", small)
		}
	})
}

// waitFor waits until script returns true.
func (d *webDriver) waitFor(script string) {
	d.t.Helper()
	var ok bool
	for i := 0; i < 50; i++ {
		d.execInto(&ok, script)
		if ok {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	d.t.Fatalf("timed out waiting for: %s", script)
}

// typeSearch replaces the picker's search with s, as typing would.
func (d *webDriver) typeSearch(s string) {
	d.t.Helper()
	d.exec(`var q = document.getElementById("note-link-search"); q.value = arguments[0];
		q.dispatchEvent(new Event("input", {bubbles: true}))`, s)
}

// pickerTitles is the titles the picker lists.
func (d *webDriver) pickerTitles() []string {
	d.t.Helper()
	var titles []string
	d.execInto(&titles, `return Array.from(document.querySelectorAll("#note-link-list [role=option]"))
		.map(function (o) { return o.textContent; })`)
	return titles
}
