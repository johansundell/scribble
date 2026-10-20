package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/notes"
	"github.com/johansundell/scribble/store"
)

// The phone checks from #65, in Chrome's phone emulation. They need a
// WebDriver server: SCRIBBLE_WEBDRIVER is its URL (CI starts chromedriver),
// and SCRIBBLE_WEBDRIVER_HOST the name the browser reaches this machine by
// (default 127.0.0.1; host.docker.internal for a browser in Docker). Without
// SCRIBBLE_WEBDRIVER they are skipped.

// phoneNote has the parts that could widen a phone's page.
var phoneNote = "# A note with wide parts\n#work, see [[Beta]].\n\n" +
	"https://example.com/a/very/long/address/that/has/no/spaces/at/all/and/goes/on/and/on/" + strings.Repeat("x", 80) + "\n\n" +
	strings.Repeat("Unbreakable", 12) + "\n\n" +
	"| Column one | Column two | Column three | Column four | Column five | Column six |\n" +
	"|---|---|---|---|---|---|\n" +
	"| some long cell text | some long cell text | some long cell text | some long cell text | some long cell text | some long cell text |\n\n" +
	"```\nfunc aVeryLongLineOfCode() { return strings.Repeat(\"this line is much wider than a phone\", 3) }\n```\n\n" +
	strings.Repeat("A line of text to make the note longer than the screen.\n\n", 40)

func TestWebPhoneBrowser(t *testing.T) {
	wd := os.Getenv("SCRIBBLE_WEBDRIVER")
	if wd == "" {
		t.Skip("SCRIBBLE_WEBDRIVER is not set")
	}
	srv, n := newPhoneServer(t)
	for _, dev := range []struct {
		name          string
		width, height int
		ratio         float64
	}{
		{"iPhone SE", 375, 667, 2},
		{"Pixel 7", 412, 915, 2.625},
	} {
		for _, dark := range []bool{false, true} {
			name := dev.name + " light"
			if dark {
				name = dev.name + " dark"
			}
			t.Run(name, func(t *testing.T) {
				d := newDriver(t, wd, dev.width, dev.height, dev.ratio, dark)
				if got := d.exec(`return matchMedia("(prefers-color-scheme: dark)").matches`); string(got) != fmt.Sprint(dark) {
					t.Fatalf("prefers dark = %s, want %v", got, dark)
				}
				checkPhone(t, d, srv, n)
			})
		}
	}

	// From 700 px up, both panes show, as before.
	t.Run("wide", func(t *testing.T) {
		d := newDriver(t, wd, 1024, 768, 1, false)
		d.login(srv)
		d.open(fmt.Sprintf("%s/note/%d", srv, n.ID))
		for _, sel := range []string{".note-list", ".note-view", ".note-actions .wide-only"} {
			if !d.visible(sel) {
				t.Errorf("%s is hidden at 1024 px", sel)
			}
		}
		for _, sel := range []string{".back-link", ".more-menu"} {
			if d.visible(sel) {
				t.Errorf("%s shows at 1024 px", sel)
			}
		}
	})
}

func checkPhone(t *testing.T, d *webDriver, srv string, n *notes.Note) {
	// Login.
	d.open(srv + "/login")
	d.checkPage("login")
	d.login(srv)

	// The list, alone.
	list := srv + "/?project=work"
	d.open(list)
	d.checkPage("list")
	if !d.visible(".note-list") || d.visible(".note-view") {
		t.Error("list: want only the list")
	}

	// A note, alone, with its ⋯ menu and the back link.
	d.click(fmt.Sprintf(`.note-list a[href^="/note/%d"]`, n.ID))
	d.waitPath(fmt.Sprintf("/note/%d?project=work", n.ID))
	d.checkPage("note")
	if d.visible(".note-list") || !d.visible(".note-body") {
		t.Error("note: want only the note")
	}
	if d.visible(".more-items") {
		t.Error("note: the ⋯ menu is open")
	}
	d.click(".more-menu summary")
	if !d.visible(`[data-filtered="rename-more"]`) || !d.visible(`[data-filtered="delete-more"]`) {
		t.Error("note: ⋯ doesn't show Rename and Delete")
	}
	d.checkPage("note menu")
	d.click(".back-link")
	d.waitPath("/?project=work")

	// The editor: the action bar is on screen, the toolbar one row.
	d.open(fmt.Sprintf("%s/note/%d/edit", srv, n.ID))
	d.checkPage("editor")
	var bar struct{ Top, Bottom, Height float64 }
	d.execInto(&bar, `var r = document.querySelector(".editor-actions").getBoundingClientRect();
		return {top: r.top, bottom: r.bottom, height: innerHeight}`)
	if bar.Top < 0 || bar.Bottom > bar.Height {
		t.Errorf("editor: action bar at %v–%v, screen is %v high", bar.Top, bar.Bottom, bar.Height)
	}
	var rows int
	d.execInto(&rows, `var tops = {};
		document.querySelectorAll(".md-button").forEach(function (b) { tops[Math.round(b.getBoundingClientRect().top)] = 1; });
		return Object.keys(tops).length`)
	if rows != 1 {
		t.Errorf("editor: the toolbar has %d rows, want 1", rows)
	}
	var tabsAbove bool
	d.execInto(&tabsAbove, `return document.querySelector(".md-tabs").getBoundingClientRect().bottom <=
		document.querySelector(".md-toolbar").getBoundingClientRect().top`)
	if !tabsAbove {
		t.Error("editor: Write / Preview are not above the toolbar")
	}
	var fonts []string
	d.execInto(&fonts, `return Array.from(document.querySelectorAll("input:not([type=hidden]), select, textarea"))
		.filter(function (el) { return parseFloat(getComputedStyle(el).fontSize) < 16; })
		.map(function (el) { return el.name || el.id; })`)
	if len(fonts) > 0 {
		t.Errorf("editor: fields under 16 px: %v", fonts)
	}

	// Rename.
	d.open(fmt.Sprintf("%s/note/%d/rename", srv, n.ID))
	d.checkPage("rename")
	if !d.visible(".back-link") {
		t.Error("rename: no back link")
	}
}

// newPhoneServer serves the web app over HTTP to the browser, with
// phoneNote and the note it links to. It returns the URL the browser uses.
func newPhoneServer(t *testing.T) (string, *notes.Note) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	a, err := auth.New(auth.Config{
		PIN:        webPIN,
		Secret:     []byte("0123456789abcdef0123456789abcdef"),
		SessionTTL: time.Hour,
		// Plain HTTP: a browser drops Secure cookies from a host such as
		// host.docker.internal.
		SecureCookie: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "phone.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	n, err := s.CreateNote(context.Background(), phoneNote)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNote(context.Background(), "# Beta\n#work"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(newWebRouter(t, s, a, &testLogger{}, nil))
	t.Cleanup(srv.Close)
	host := os.Getenv("SCRIBBLE_WEBDRIVER_HOST")
	if host == "" {
		return srv.URL, n
	}
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	return "http://" + net.JoinHostPort(host, port), n
}

// webDriver is a WebDriver session, driven over its HTTP protocol.
type webDriver struct {
	t       *testing.T
	session string // base URL of the session
}

func newDriver(t *testing.T, base string, width, height int, ratio float64, dark bool) *webDriver {
	t.Helper()
	args := []string{"--headless=new", "--no-sandbox"}
	if dark {
		args = append(args, "--force-dark-mode")
	}
	caps := map[string]any{"capabilities": map[string]any{"alwaysMatch": map[string]any{
		"browserName": "chrome",
		"goog:chromeOptions": map[string]any{
			"args": args,
			"mobileEmulation": map[string]any{"deviceMetrics": map[string]any{
				"width": width, "height": height, "pixelRatio": ratio, "touch": width < 700,
			}},
		},
	}}}
	var res struct {
		SessionID string `json:"sessionId"`
	}
	d := &webDriver{t: t, session: strings.TrimSuffix(base, "/") + "/session"}
	d.call("POST", "", caps, &res)
	d.session += "/" + res.SessionID
	t.Cleanup(func() { d.call("DELETE", "", nil, nil) })
	return d
}

// call sends a WebDriver command and decodes its value into out.
func (d *webDriver) call(method, path string, body, out any) {
	d.t.Helper()
	if err := d.try(method, path, body, out); err != nil {
		d.t.Fatal(err)
	}
}

// try is call, returning the error instead of failing the test.
func (d *webDriver) try(method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, d.session+path, r)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("WebDriver %s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	var v struct{ Value json.RawMessage }
	raw, _ := io.ReadAll(res.Body)
	if err := json.Unmarshal(raw, &v); err != nil || res.StatusCode != http.StatusOK {
		return fmt.Errorf("WebDriver %s %s: %d %s", method, path, res.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(v.Value, out); err != nil {
			return fmt.Errorf("WebDriver %s %s: %v in %s", method, path, err, v.Value)
		}
	}
	return nil
}

// open loads u. A browser in Docker may be refused for a moment while the
// new port is forwarded to it, so that is tried again.
func (d *webDriver) open(u string) {
	d.t.Helper()
	var err error
	for i := 0; i < 20; i++ {
		err = d.try("POST", "/url", map[string]any{"url": u}, nil)
		if err == nil || !strings.Contains(err.Error(), "ERR_CONNECTION_REFUSED") {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err != nil {
		d.t.Fatal(err)
	}
}

func (d *webDriver) exec(script string, args ...any) json.RawMessage {
	d.t.Helper()
	var out json.RawMessage
	if args == nil {
		args = []any{}
	}
	d.call("POST", "/execute/sync", map[string]any{"script": script, "args": args}, &out)
	return out
}

func (d *webDriver) execInto(out any, script string) {
	d.t.Helper()
	if err := json.Unmarshal(d.exec(script), out); err != nil {
		d.t.Fatalf("%s: %v", script, err)
	}
}

// click taps the element as a user would, so it must be on screen.
func (d *webDriver) click(sel string) {
	d.t.Helper()
	var el map[string]string
	d.call("POST", "/element", map[string]any{"using": "css selector", "value": sel}, &el)
	for _, id := range el {
		d.call("POST", "/element/"+id+"/click", map[string]any{}, nil)
	}
}

func (d *webDriver) visible(sel string) bool {
	d.t.Helper()
	var ok bool
	d.execInto(&ok, `var el = document.querySelector(`+jsString(sel)+`);
		return !!el && el.checkVisibility({visibilityProperty: true})`)
	return ok
}

// waitPath waits until the page is at want (path and query).
func (d *webDriver) waitPath(want string) {
	d.t.Helper()
	var got string
	for i := 0; i < 50; i++ {
		d.execInto(&got, `return location.pathname + location.search`)
		if got == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	d.t.Fatalf("at %q, want %q", got, want)
}

func (d *webDriver) login(srv string) {
	d.t.Helper()
	d.open(srv + "/login")
	d.exec(`document.getElementById("pin").value = arguments[0]; document.querySelector(".pin-form").requestSubmit()`, webPIN)
	d.waitPath("/")
}

// checkPage checks that the page doesn't scroll sideways and that every tap
// target on it is at least 44×44 px. Links in a note's text only need the
// height: they flow with the words.
func (d *webDriver) checkPage(page string) {
	d.t.Helper()
	var wide struct{ Scroll, Width float64 }
	d.execInto(&wide, `return {scroll: document.documentElement.scrollWidth, width: innerWidth}`)
	if wide.Scroll > wide.Width {
		d.t.Errorf("%s: scrolls sideways (%v px wide on a %v px screen)", page, wide.Scroll, wide.Width)
	}
	var small []string
	d.execInto(&small, `var small = [];
		document.querySelectorAll("a[href], button, input:not([type=hidden]), select, textarea, summary, [role=button]").forEach(function (el) {
			// checkVisibility also leaves out what a closed <details> holds.
			if (!el.checkVisibility({visibilityProperty: true})) return;
			var inText = el.tagName === "A" && el.closest(".note-body");
			Array.from(el.getClientRects()).forEach(function (r) {
				if (r.height < 44 || (!inText && r.width < 44)) {
					small.push(el.tagName + " " + JSON.stringify((el.textContent || el.name || "").trim().slice(0, 30)) +
						" " + Math.round(r.width) + "×" + Math.round(r.height));
				}
			});
		});
		return small`)
	for _, s := range small {
		d.t.Errorf("%s: tap target under 44 px: %s", page, s)
	}
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
