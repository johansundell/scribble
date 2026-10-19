package router_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestWebShortcuts_Overlay(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	w := e.do(get("/app", c))
	wantBody(t, w, http.StatusOK,
		// Hidden until shortcuts.js shows it: without JavaScript there are none.
		`<button type="button" class="link-button" data-shortcuts-open hidden>Keyboard shortcuts</button>`,
		`<dialog id="shortcuts" class="shortcuts" aria-labelledby="shortcuts-heading">`,
		// Every shortcut from #52, the editor's from #53 too.
		"<kbd data-mod>Ctrl</kbd>+<kbd>S</kbd>",
		"<kbd data-mod>Ctrl</kbd>+<kbd>E</kbd>",
		"<kbd>Alt</kbd>+<kbd>N</kbd>",
		"<kbd>n</kbd>",
		"<kbd>/</kbd>",
		"<kbd>F2</kbd>",
		"<kbd data-mod>Ctrl</kbd>+<kbd>B</kbd>",
		"<kbd data-mod>Ctrl</kbd>+<kbd>I</kbd>",
		"<kbd data-mod>Ctrl</kbd>+<kbd>K</kbd>",
		"<kbd data-mod>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>P</kbd>",
		"<kbd>?</kbd>",
		"<kbd>Esc</kbd>",
		`data-shortcut="new"`,
		`data-shortcut="search"`,
	)
	wantNot(t, e.do(get("/app/login")).Body.String(), `id="shortcuts"`, "data-shortcuts-open")
	if w := e.do(get("/assets/js/shortcuts.js")); w.Code != http.StatusOK {
		t.Errorf("GET shortcuts.js = %d, want 200", w.Code)
	}
}

func TestWebShortcuts_Targets(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha")

	// The open note: Ctrl+E edits, F2 renames.
	wantBody(t, e.do(get(fmt.Sprintf("/app/notes/%d", n.ID), c)), http.StatusOK,
		fmt.Sprintf(`href="/app/notes/%d/edit" data-filtered="edit" data-shortcut="edit"`, n.ID),
		fmt.Sprintf(`href="/app/notes/%d/rename" data-filtered="rename" data-shortcut="rename"`, n.ID),
	)

	// Its editor: Ctrl+S saves, Ctrl+E goes back to the note, F2 renames.
	wantBody(t, e.do(get(editPath(n), c)), http.StatusOK,
		`value="save" data-shortcut="save"`,
		fmt.Sprintf(`href="/app/notes/%d" data-filtered="cancel" data-shortcut="view"`, n.ID),
		`data-filtered="rename" data-rename data-shortcut="rename"`,
	)

	// A new note has no note to view yet.
	w := e.do(get("/app/new", c))
	wantBody(t, w, http.StatusOK, `value="save" data-shortcut="save"`)
	wantNot(t, w.Body.String(), `data-shortcut="view"`)
}
