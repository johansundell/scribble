package router_test

import (
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"strings"
	"testing"
)

func TestWebMobile_Head(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	for _, path := range []string{"/app/login", "/app"} {
		var cookies []*http.Cookie
		if path != "/app/login" {
			cookies = append(cookies, c)
		}
		w := e.do(get(path, cookies...))
		wantBody(t, w, http.StatusOK,
			// Zooming stays on: no maximum-scale or user-scalable.
			`<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover, interactive-widget=resizes-content" />`,
			`<link rel="manifest" href="/app/manifest.webmanifest" />`,
			`<link rel="apple-touch-icon" href="/assets/img/apple-touch-icon.png" />`,
			`<meta name="apple-mobile-web-app-capable" content="yes" />`,
			`<meta name="apple-mobile-web-app-title" content="Scribble" />`,
			`<meta name="theme-color" media="(prefers-color-scheme: light)"`,
			`<meta name="theme-color" media="(prefers-color-scheme: dark)"`,
		)
		wantNot(t, w.Body.String(), "user-scalable", "maximum-scale", "serviceWorker")
	}
}

func TestWebMobile_Manifest(t *testing.T) {
	e := newWebEnv(t)
	// No session: the home-screen icon is fetched before login.
	w := e.do(get("/app/manifest.webmanifest"))
	if w.Code != http.StatusOK {
		t.Fatalf("GET manifest = %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/manifest+json" {
		t.Errorf("Content-Type = %q", got)
	}
	var m struct {
		Name            string `json:"name"`
		ShortName       string `json:"short_name"`
		StartURL        string `json:"start_url"`
		Scope           string `json:"scope"`
		Display         string `json:"display"`
		ThemeColor      string `json:"theme_color"`
		BackgroundColor string `json:"background_color"`
		Icons           []struct{ Src, Sizes, Type, Purpose string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest is not JSON: %v\n%s", err, w.Body.String())
	}
	if m.Name != "Scribble" || m.ShortName != "Scribble" || m.StartURL != "/app" || m.Scope != "/app" || m.Display != "standalone" {
		t.Errorf("manifest = %+v", m)
	}
	if m.ThemeColor == "" || m.BackgroundColor == "" {
		t.Errorf("manifest lacks colours: %+v", m)
	}

	// Each icon loads without a session, at the size it claims.
	sizes := map[string]bool{}
	for _, ic := range m.Icons {
		sizes[ic.Sizes+" "+ic.Purpose] = true
		wantPNG(t, e, ic.Src, ic.Sizes)
	}
	for _, want := range []string{"192x192 any", "512x512 any", "512x512 maskable"} {
		if !sizes[want] {
			t.Errorf("no %s icon in %+v", want, m.Icons)
		}
	}
	wantPNG(t, e, "/assets/img/apple-touch-icon.png", "180x180")
}

// wantPNG checks that path is a PNG image of size ("192x192"), served
// without a session.
func wantPNG(t *testing.T, e *webEnv, path, size string) {
	t.Helper()
	w := e.do(get(path))
	if w.Code != http.StatusOK {
		t.Errorf("GET %s = %d", path, w.Code)
		return
	}
	cfg, err := png.DecodeConfig(w.Body)
	if err != nil {
		t.Errorf("%s: %v", path, err)
		return
	}
	if got := fmt.Sprintf("%dx%d", cfg.Width, cfg.Height); got != size {
		t.Errorf("%s is %s, want %s", path, got, size)
	}
}

func TestWebMobile_Panes(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha\n#work")

	// The list alone has no note open.
	body := e.do(get("/app?q=al", c)).Body.String()
	if !strings.Contains(body, `<main class="app-main">`) {
		t.Errorf("/app shows the note pane on phones:\n%s", body)
	}
	wantNot(t, body, "back-link")

	// Every page about a note shows the note pane, with a back link that
	// keeps the filter.
	back := `<a class="button back-link" href="/app?project=work&amp;q=al" data-filtered="back">← Notes</a>`
	for _, path := range []string{
		fmt.Sprintf("/app/notes/%d", n.ID), editPath(n), renamePath(n), deletePath(n), "/app/new", "/app/notes/999",
	} {
		w := e.do(get(path+"?project=work&q=al", c))
		wantBody(t, w, w.Code, `<main class="app-main has-note">`, back, `<span class="note-bar-title">`)
	}
}

func TestWebMobile_NoteMenu(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha")
	w := e.do(get(fmt.Sprintf("/app/notes/%d", n.ID), c))
	wantBody(t, w, http.StatusOK,
		`<span class="note-bar-title">Alpha</span>`,
		// Rename and Delete under ⋯ on phones: a <details>, so no JavaScript
		// is needed. Delete there asks first, as the wide one does.
		`<details class="more-menu">`,
		`<summary class="button" aria-label="More actions">⋯</summary>`,
		fmt.Sprintf(`href="/app/notes/%d/rename" data-filtered="rename-more">Rename</a>`, n.ID),
		fmt.Sprintf(`href="/app/notes/%d/delete" data-filtered="delete-more" data-confirm="Delete &#34;Alpha&#34;? This cannot be undone.">Delete</a>`, n.ID),
	)
}

func TestWebMobile_EditorBar(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha")
	// Save, Done and Cancel are in the bar that sticks to the bottom.
	wantBody(t, e.do(get(editPath(n), c)), http.StatusOK,
		`<div class="editor-buttons editor-actions">`,
		`<span class="note-bar-title">Alpha</span>`,
	)
}
