package router_test

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

// Code blocks are highlighted on the server with Chroma's classes (#160),
// so app.css colours them for light and dark.
func TestWebCode_FencedBlockIsHighlighted(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	body := strings.Join([]string{
		"```go",
		"func main() { fmt.Println(\"<hi>\") }",
		"```",
	}, "\n")
	w := e.do(postForm("/preview", url.Values{"body": {body}}, c))
	wantBody(t, w, http.StatusOK,
		`<pre class="chroma">`,
		`<span class="kd">func</span>`,
		`&lt;hi&gt;`,
	)
	// No inline styles: the theme comes from app.css.
	wantNot(t, w.Body.String(), "style=", "<hi>")
}

// A block in a language Chroma doesn't know, or none, is plain escaped code.
func TestWebCode_UnknownLanguageIsPlain(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	for _, body := range []string{
		"```nosuchlang\n<b>x</b>\n```",
		"```\n<b>x</b>\n```",
	} {
		w := e.do(postForm("/preview", url.Values{"body": {body}}, c))
		wantBody(t, w, http.StatusOK, "<pre", "&lt;b&gt;x&lt;/b&gt;")
		wantNot(t, w.Body.String(), "<b>x", `<span class="`)
	}
}

// The note and shared pages load copy.js; the login screen doesn't.
func TestWebCode_PagesLoadCopyButton(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Code")
	tag := `<script src="/assets/js/copy.js" defer></script>`
	wantBody(t, e.do(get(notePath(n), c)), http.StatusOK, tag)
	e.do(postForm(sharePath(n), url.Values{"action": {"share"}}, c))
	wantBody(t, e.do(get("/s/"+e.shareToken(t, n))), http.StatusOK, tag)
	wantNot(t, e.do(get("/login")).Body.String(), "copy.js")
	if w := e.do(get("/assets/js/copy.js")); w.Code != http.StatusOK {
		t.Errorf("GET copy.js = %d, want 200", w.Code)
	}
}

// TestWebCodeBrowser checks #160 in Chrome: code is coloured for the theme,
// and each code block has a Copy button in the note view, after nav.js
// swaps in a note and in the editor's Preview tab. It needs a WebDriver
// server, as TestWebPhoneBrowser does. Over plain HTTP the button takes the
// execCommand fallback.
func TestWebCodeBrowser(t *testing.T) {
	wd := os.Getenv("SCRIBBLE_WEBDRIVER")
	if wd == "" {
		t.Skip("SCRIBBLE_WEBDRIVER is not set")
	}
	code := "```go\nfunc main() {}\n```"
	srv, ns := newBrowserServer(t, "# Alpha\nSee [[Beta]].\n\n"+code, "# Beta\n\n"+code)
	alpha, beta := ns[0], ns[1]
	d := newDriver(t, wd, 1024, 768, 1, false)
	d.login(srv)

	d.open(srv + notePath(alpha))
	var color string
	d.execInto(&color, `return getComputedStyle(document.querySelector(".note-body .chroma .kd")).color`)
	if color != "rgb(207, 34, 46)" {
		t.Errorf("light keyword colour = %s, want GitHub Light's", color)
	}
	d.exec(`document.documentElement.dataset.theme = "dark"`)
	d.execInto(&color, `return getComputedStyle(document.querySelector(".note-body .chroma .kd")).color`)
	if color != "rgb(255, 123, 114)" {
		t.Errorf("dark keyword colour = %s, want GitHub Dark's", color)
	}
	d.exec(`delete document.documentElement.dataset.theme`)

	d.wantText(".note-body .code-block .code-copy", "Copy")
	d.click(".note-body .code-copy")
	d.waitFor(`return document.querySelector(".note-body .code-copy").textContent === "✓ Copied"`)
	d.waitFor(`return document.querySelector(".note-body .code-copy").textContent === "Copy"`)

	// A note swapped in by nav.js.
	d.click(`.note-body a[href="/title/Beta"]`)
	d.waitNote(beta)
	d.waitFor(`return document.querySelectorAll(".note-body .code-copy").length === 1`)

	// The Preview tab.
	d.open(srv + editPath(beta))
	d.click(`[data-tab="preview"]`)
	d.waitFor(`return !!document.querySelector("#note-preview .code-block .code-copy")`)
}
