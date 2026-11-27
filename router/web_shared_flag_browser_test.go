package router_test

import (
	"os"
	"strings"
	"testing"
)

// On a phone, a long title wraps and the badges (#208) stay on the row, at
// its right edge, in order, without sideways scrolling.
func TestWebSharedFlagBrowser(t *testing.T) {
	wd := os.Getenv("SCRIBBLE_WEBDRIVER")
	if wd == "" {
		t.Skip("SCRIBBLE_WEBDRIVER is not set")
	}
	title := "A shared note with a very long title that cannot fit on one phone line " + strings.Repeat("x", 60)
	srv, ns := newBrowserServer(t, "# "+title+"\nbody", "# Beta")
	n := ns[0]

	d := newDriver(t, wd, 375, 667, 2, false)
	d.login(srv)
	d.open(srv + notePath(n) + "/share")
	d.click(`button[value="share"]`)
	d.waitFor(`return !!document.getElementById("share-url")`)
	d.click(`button[value="share-edit"]`)
	d.waitFor(`return !!document.getElementById("edit-url")`)
	d.open(srv + notePath(n))
	d.click(".more-menu summary")
	d.click(`[data-filtered="pin-more"] button`)
	d.waitFor(`return !!document.querySelector(".note-list .pin-badge")`)

	d.open(srv + "/")
	d.checkPage("shared list")
	var got struct {
		Badges     []string
		Tops       []float64
		Rights     []float64
		LinkRight  float64
		LinkHeight float64
	}
	d.execInto(&got, `var a = document.querySelector(".note-list li a");
		var b = Array.from(a.querySelectorAll(".share-badge, .pin-badge"));
		var r = a.getBoundingClientRect();
		return {
			badges: b.map(function (e) { return e.textContent }),
			tops: b.map(function (e) { return Math.round(e.getBoundingClientRect().top) }),
			rights: b.map(function (e) { return e.getBoundingClientRect().right }),
			linkRight: r.right,
			linkHeight: r.height
		}`)
	if strings.Join(got.Badges, " ") != "🔗 ✏️ 📌" {
		t.Fatalf("badges = %q, want 🔗 ✏️ 📌", got.Badges)
	}
	for i := range got.Tops {
		if got.Tops[i] != got.Tops[0] {
			t.Errorf("badges are on more than one line: tops %v", got.Tops)
		}
		if got.Rights[i] > got.LinkRight {
			t.Errorf("badge %s ends at %v, past the row's %v", got.Badges[i], got.Rights[i], got.LinkRight)
		}
	}
	if got.LinkHeight < 60 {
		t.Errorf("the long title doesn't wrap: row %v px high", got.LinkHeight)
	}
}
