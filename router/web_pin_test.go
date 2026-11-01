package router_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestWebPin(t *testing.T) {
	e := newWebEnv(t)
	a := e.note(t, "# Alpha")
	e.note(t, "# Beta")
	pinPath := fmt.Sprintf("/note/%d/pin", a.ID)

	// Session and origin, like every web form.
	wantRedirect(t, e.do(postForm(pinPath, url.Values{"pinned": {"true"}})), "/login")
	c := e.login(t)
	req := postForm(pinPath, url.Values{"pinned": {"true"}}, c)
	req.Header.Set("Origin", "https://evil.test")
	if w := e.do(req); w.Code != http.StatusForbidden {
		t.Errorf("cross-origin pin = %d, want 403", w.Code)
	}
	if e.get(t, a.ID).Pinned {
		t.Fatal("a refused post pinned the note")
	}

	// An unpinned note offers Pin note, beside the others and under ⋯,
	// keeping the filter.
	w := e.do(get(fmt.Sprintf("/note/%d?project=work", a.ID), c))
	wantBody(t, w, http.StatusOK,
		fmt.Sprintf(`action="/note/%d/pin?project=work"`, a.ID), `name="pinned" value="true"`, ">Pin note<",
		`data-filtered="pin"`, `data-filtered="pin-more"`)
	wantNot(t, w.Body.String(), "Unpin note", "pin-badge")

	wantRedirect(t, e.do(postForm(pinPath+"?project=work", url.Values{"pinned": {"true"}}, c)),
		fmt.Sprintf("/note/%d?project=work", a.ID))
	if !e.get(t, a.ID).Pinned {
		t.Fatal("pin did not pin")
	}

	// Pinned: first in the list with a badge, and Unpin note is offered.
	w = e.do(get(fmt.Sprintf("/note/%d", a.ID), c))
	wantBody(t, w, http.StatusOK, `name="pinned" value="false"`, ">Unpin note<", `class="pin-badge"`)
	body := w.Body.String()
	if ia, ib := strings.Index(body, ">Alpha<"), strings.Index(body, ">Beta<"); ia < 0 || ib < 0 || ia > ib {
		t.Errorf("pinned Alpha is not listed before Beta")
	}

	wantRedirect(t, e.do(postForm(pinPath, url.Values{"pinned": {"false"}}, c)), fmt.Sprintf("/note/%d", a.ID))
	if e.get(t, a.ID).Pinned {
		t.Fatal("unpin did not unpin")
	}

	// A missing note.
	w = e.do(postForm("/note/999/pin", url.Values{"pinned": {"true"}}, c))
	if w.Code == http.StatusSeeOther {
		t.Errorf("pinning a missing note redirected")
	}
}
