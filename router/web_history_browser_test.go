package router_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestWebHistoryBrowser checks #110 in Chrome: the note's revision line,
// moving between versions with the keyboard, and Restore asking first. It
// needs a WebDriver server, as TestWebPhoneBrowser does.
func TestWebHistoryBrowser(t *testing.T) {
	wd := os.Getenv("SCRIBBLE_WEBDRIVER")
	if wd == "" {
		t.Skip("SCRIBBLE_WEBDRIVER is not set")
	}
	srv, ns := newBrowserServer(t, "# Alpha\nfirst text")
	n := ns[0]
	notePage := fmt.Sprintf("/note/%d", n.ID)
	history := notePage + "/history"

	t.Run("wide", func(t *testing.T) {
		d := newDriver(t, wd, 1024, 768, 1, false)
		d.login(srv)

		// Two more versions, through the app: a rename and an edit.
		d.open(srv + notePage + "/rename")
		d.exec(`var f = document.getElementById("rename-title"); f.value = "Beta"; f.form.requestSubmit()`)
		d.waitPath(notePage)
		d.open(srv + notePage + "/edit")
		d.exec(`var t = document.getElementById("note-text"); t.value = "# Beta\nlatest text"; t.dispatchEvent(new Event("input"))`)
		d.click(`button[value="done"]`)
		d.waitPath(notePage)

		// The header's time is shown in the reader's own zone, with UTC kept
		// in its tooltip.
		var meta struct{ Text, Title string }
		d.execInto(&meta, `var m = document.querySelector(".note-meta"), t = m.querySelector("time");
		return {text: m.textContent, title: t.title}`)
		if !strings.HasPrefix(meta.Text, "Revision 3 (2 edits) · Updated ") || !strings.HasSuffix(meta.Title, "UTC") || strings.HasSuffix(meta.Text, "UTC") {
			t.Errorf("note header %q, time tooltip %q", meta.Text, meta.Title)
		}

		// History opens on the version before the current one.
		d.click(`.note-actions > a[data-filtered="history"]`)
		d.waitPath(history)
		d.wantText(".history-banner p", "Viewing revision r2 from")
		d.wantText(".note-body", "first text")

		// From the newest version, Tab twice to the oldest and press Enter.
		d.exec(`document.querySelector('.history-list a[href$="rev=3"]').focus()`)
		d.keys("\uE004", "\uE004", "\uE007") // Tab, Tab, Enter
		d.waitPath(history + "?rev=1")
		d.wantText(".history-banner p", "Viewing revision r1 from")
		d.wantText(".note-body", "first text")

		// The current version has no Restore.
		d.click(`.history-list a[href$="rev=3"]`)
		d.waitPath(history + "?rev=3")
		if d.visible(`button[value="restore"]`) {
			t.Error("the current version offers Restore")
		}

		// Restore asks first; Cancel stays on the page.
		d.open(srv + history + "?rev=1")
		d.mark()
		d.click(`button[value="restore"]`)
		if got := d.alertText(); got != `Restore revision r1 of "Beta"?` {
			t.Errorf("Restore asks %q", got)
		}
		d.call("POST", "/alert/dismiss", map[string]any{}, nil)
		d.wantSamePage("cancelled restore")
		d.click(`button[value="restore"]`)
		d.alertText()
		d.call("POST", "/alert/accept", map[string]any{}, nil)
		d.waitPath(notePage)
		d.wantText(".note-body h1", "Alpha")
		d.wantText(".note-meta", "Revision 4 (3 edits)")
	})

	// On a phone History is under ⋯, and the pane fits the screen.
	t.Run("phone", func(t *testing.T) {
		p := newDriver(t, wd, 375, 667, 2, true)
		p.login(srv)
		p.open(srv + notePage)
		p.click(".more-menu summary")
		p.click(`a[data-filtered="history-more"]`)
		p.waitPath(history)
		p.checkPage("phone history")
	})
}

// wantText waits until the element sel's text contains want.
func (d *webDriver) wantText(sel, want string) {
	d.t.Helper()
	var got string
	for i := 0; i < 50; i++ {
		d.execInto(&got, `var el = document.querySelector(`+jsString(sel)+`); return el ? el.textContent : ""`)
		if strings.Contains(got, want) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	d.t.Errorf("%s: %q, want it to contain %q", sel, got, want)
}

// keys presses each key in turn on the focused element. A modifier stays
// down until it is pressed again.
func (d *webDriver) keys(keys ...string) {
	d.t.Helper()
	var acts []map[string]any
	down := map[string]bool{}
	for _, k := range keys {
		switch k {
		case "\uE008", "\uE009", "\uE03D": // Shift, Ctrl, Meta
			if down[k] {
				acts = append(acts, map[string]any{"type": "keyUp", "value": k})
			} else {
				acts = append(acts, map[string]any{"type": "keyDown", "value": k})
			}
			down[k] = !down[k]
		default:
			acts = append(acts, map[string]any{"type": "keyDown", "value": k}, map[string]any{"type": "keyUp", "value": k})
		}
	}
	d.call("POST", "/actions", map[string]any{"actions": []map[string]any{{"type": "key", "id": "keyboard", "actions": acts}}}, nil)
}
