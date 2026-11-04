package router_test

import (
	"fmt"
	"net/http"
	"testing"
)

// TestWebNoteLink_Markup checks #149's parts: the toolbar button, the
// picker dialog and its shortcut in the overlay.
func TestWebNoteLink_Markup(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha")
	for _, path := range []string{editPath(n), "/new"} {
		wantBody(t, e.do(get(path, c)), http.StatusOK,
			// data-md-button puts it in the toolbar's arrow-key focus.
			`<button type="button" class="md-button" data-md-button="note-link" data-note-link aria-label="Link to note" title="Link to note (Ctrl+Shift+K)">`,
		)
	}
	w := e.do(get("/", c))
	wantBody(t, w, http.StatusOK,
		`<dialog id="note-link-dialog" class="shortcuts note-link-dialog" aria-labelledby="note-link-heading">`,
		`<input type="search" id="note-link-search"`,
		`<ul id="note-link-list" class="note-link-list" role="listbox"`,
		"<kbd data-mod>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>K</kbd>",
	)
	// The note pane, outside the editor, has no toolbar.
	wantNot(t, e.do(get(fmt.Sprintf("/note/%d", n.ID), c)).Body.String(), "data-note-link ")
	wantNot(t, e.do(get("/login")).Body.String(), "note-link-dialog")
}

// TestWebCodeBlock_Markup checks #176's toolbar button: a Code block button
// for app.js in place of the toolbar element's inline-code <md-code>.
func TestWebCodeBlock_Markup(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Alpha")
	for _, path := range []string{editPath(n), "/new"} {
		w := e.do(get(path, c))
		wantBody(t, w, http.StatusOK,
			`<button type="button" class="md-button" data-md-button="code-block" data-code-block aria-label="Code block" title="Code block">`,
		)
		wantNot(t, w.Body.String(), "<md-code")
	}
}
