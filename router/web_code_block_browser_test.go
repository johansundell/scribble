package router_test

import (
	"fmt"
	"os"
	"testing"
)

// TestWebCodeBlockBrowser checks in Chrome that the toolbar's Code block
// button puts in a fenced block (#176). It needs a WebDriver server, as
// TestWebPhoneBrowser does.
func TestWebCodeBlockBrowser(t *testing.T) {
	wd := os.Getenv("SCRIBBLE_WEBDRIVER")
	if wd == "" {
		t.Skip("SCRIBBLE_WEBDRIVER is not set")
	}
	srv, ns := newBrowserServer(t, "# Alpha\nrun go test now")
	d := newDriver(t, wd, 1024, 768, 1, false)
	d.login(srv)
	d.open(fmt.Sprintf("%s/note/%d/edit", srv, ns[0].ID))
	d.waitFor(`return document.querySelector("form.editor").dataset.markdown === "true"`)

	var label string
	d.execInto(&label, `var b = document.querySelector("[data-code-block]");
		return b.getAttribute("aria-label") + "|" + b.title`)
	if label != "Code block|Code block" {
		t.Errorf("aria-label|title = %q, want Code block for both", label)
	}

	type state struct {
		Value    string
		Selected string
		Focus    string
	}
	read := func() state {
		var s state
		d.execInto(&s, `var t = document.getElementById("note-text");
			return {value: t.value, selected: t.value.slice(t.selectionStart, t.selectionEnd),
				focus: document.activeElement.id}`)
		return s
	}
	for _, c := range []struct {
		name       string
		text       string
		start, end int
		want, sel  string
		cursorAt   int // with no selection, where the cursor ends
	}{
		{"single line selection", "run go test now", 4, 11, "run \n```\ngo test\n```\n now", "go test", -1},
		{"multiline selection", "a\nb\nc", 0, 3, "```\na\nb\n```\nc", "a\nb", -1},
		{"no selection", "ab", 2, 2, "ab\n```\n\n```", "", 7},
	} {
		t.Run(c.name, func(t *testing.T) {
			d.exec(`var t = document.getElementById("note-text"); t.value = arguments[0];
				t.focus(); t.setSelectionRange(arguments[1], arguments[2])`, c.text, c.start, c.end)
			d.click("[data-code-block]")
			got := read()
			if got.Value != c.want || got.Selected != c.sel || got.Focus != "note-text" {
				t.Errorf("got %+v, want text %q with %q selected and the focus on note-text", got, c.want, c.sel)
			}
			if c.cursorAt >= 0 {
				var at int
				d.execInto(&at, `return document.getElementById("note-text").selectionStart`)
				if at != c.cursorAt {
					t.Errorf("cursor at %d, want %d (the block's blank line)", at, c.cursorAt)
				}
			}
		})
	}

	// Ctrl+Z takes the block back out in one step.
	d.keys("", "z", "")
	if got := read().Value; got != "ab" {
		t.Errorf("after Ctrl+Z: %q, want \"ab\"", got)
	}
}
