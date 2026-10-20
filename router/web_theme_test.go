package router_test

import (
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The theme switcher (#87) is on every app page, the PIN screen too.
func TestWebTheme_Button(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	for _, req := range []*http.Request{get("/", c), get("/login")} {
		w := e.do(req)
		wantBody(t, w, http.StatusOK,
			// Hidden until theme.js shows it: without JavaScript it can't work.
			`data-theme-toggle data-shortcut="theme" aria-label="Theme: System (click to switch)" title="Theme: System (click to switch)" hidden>`,
			`<script src="/assets/js/theme.js" defer></script>`,
			`localStorage.getItem("theme")`,
		)
		// The saved theme is set before the stylesheet loads, so a pinned
		// theme never flashes the other one.
		body := w.Body.String()
		if i, j := strings.Index(body, `localStorage.getItem("theme")`), strings.Index(body, `/assets/css/app.css`); i < 0 || i > j {
			t.Errorf("%s: the theme script must come before app.css", req.URL)
		}
	}
	if w := e.do(get("/assets/js/theme.js")); w.Code != http.StatusOK {
		t.Errorf("GET theme.js = %d, want 200", w.Code)
	}
}

func TestWebTheme_Shortcut(t *testing.T) {
	e := newWebEnv(t)
	wantBody(t, e.do(get("/", e.login(t))), http.StatusOK,
		"<kbd data-alt>Alt</kbd>+<kbd>T</kbd>, or <kbd>t</kbd>",
	)
}

// app.css sets every colour token in each theme: light by default, dark
// from the system unless light is pinned, and dark when dark is pinned. The
// two dark lists are the same.
func TestWebTheme_CSSTokens(t *testing.T) {
	e := newWebEnv(t)
	w := e.do(get("/assets/css/app.css"))
	if w.Code != http.StatusOK {
		t.Fatalf("GET app.css = %d, want 200", w.Code)
	}
	css := w.Body.String()
	tokens := func(re string) map[string]string {
		t.Helper()
		m := regexp.MustCompile(re + `\s*\{([^}]*)\}`).FindStringSubmatch(css)
		if m == nil {
			t.Fatalf("app.css lacks %s", re)
		}
		set := map[string]string{}
		for _, d := range regexp.MustCompile(`(--[\w-]+)\s*:\s*([^;]+);`).FindAllStringSubmatch(m[1], -1) {
			set[d[1]] = d[2]
		}
		return set
	}
	light := tokens(`(?m)^:root`)
	if len(light) == 0 {
		t.Fatal(":root sets no tokens")
	}
	system := tokens(`@media \(prefers-color-scheme: dark\)\s*\{\s*:root:not\(\[data-theme="light"\]\)`)
	pinned := tokens(`(?m)^:root\[data-theme="dark"\]`)
	if !slices.Equal(slices.Sorted(maps.Keys(system)), slices.Sorted(maps.Keys(light))) {
		t.Errorf("system dark sets %v, want the tokens of :root %v", system, light)
	}
	if !maps.Equal(pinned, system) {
		t.Errorf("pinned dark %v differs from system dark %v", pinned, system)
	}
	// Pinned light keeps the browser's own controls light too.
	if !regexp.MustCompile(`:root\[data-theme="light"\]\s*\{\s*color-scheme:\s*light;`).MatchString(css) {
		t.Error(`app.css lacks :root[data-theme="light"] { color-scheme: light; }`)
	}
}
