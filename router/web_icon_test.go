package router_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Every page links the vector app icon, with a PNG fallback for browsers
// without SVG favicons (#106).
func TestWebIcon_FaviconLinks(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha")
	e.do(postForm(sharePath(n), url.Values{"action": {"share"}}, c))
	tok := e.shareToken(t, n)
	if tok == "" {
		t.Fatal("the note was not shared")
	}

	for _, req := range []*http.Request{
		get("/login"), get("/", c), get(notePath(n), c), get("/s/" + tok), get("/healthz"),
	} {
		w := e.do(req)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d", req.URL, w.Code)
			continue
		}
		body := w.Body.String()
		svg := `<link rel="icon" type="image/svg+xml" href="/assets/img/icon.svg" />`
		png := `<link rel="icon" type="image/png" sizes="64x64" href="/assets/img/favicon.png" />`
		if !strings.Contains(body, svg) || !strings.Contains(body, png) {
			t.Errorf("GET %s lacks the favicon links %q and %q", req.URL, svg, png)
		}
	}

	w := e.do(get("/assets/img/icon.svg"))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `viewBox="0 0 512 512"`) {
		t.Errorf("GET /assets/img/icon.svg = %d: %.80s", w.Code, w.Body.String())
	}
	wantPNG(t, e, "/assets/img/favicon.png", "64x64")
}
