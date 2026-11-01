package router_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johansundell/scribble/notes"
)

func TestPinAPI(t *testing.T) {
	e := newWebEnv(t)
	a := e.note(t, "# Alpha\nbody")
	e.note(t, "# Beta\nbody")
	pin := func(id int64, body string, status int) notes.Note {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/notes/%d/pin", id), strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret-token")
		req.Header.Set("Content-Type", "application/json")
		w := e.do(req)
		if w.Code != status {
			t.Fatalf("POST pin %q: status %d, want %d (body %q)", body, w.Code, status, w.Body.String())
		}
		var n notes.Note
		if status == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &n); err != nil {
				t.Fatalf("bad JSON %q: %v", w.Body.String(), err)
			}
		}
		return n
	}
	list := func() []notes.Note {
		t.Helper()
		var ns []notes.Note
		w := e.apiDo("GET", "/notes")
		if err := json.Unmarshal(w.Body.Bytes(), &ns); err != nil {
			t.Fatalf("bad JSON %q: %v", w.Body.String(), err)
		}
		return ns
	}

	// No body toggles.
	if n := pin(a.ID, "", http.StatusOK); !n.Pinned || n.Seq <= a.Seq || n.Revision != a.Revision {
		t.Fatalf("toggle on: %+v", n)
	}
	if ns := list(); ns[0].ID != a.ID || !ns[0].Pinned {
		t.Errorf("pinned Alpha not listed first: %+v", ns)
	}
	if n := pin(a.ID, "", http.StatusOK); n.Pinned {
		t.Errorf("toggle off: %+v", n)
	}
	// An explicit state is set, not toggled.
	if n := pin(a.ID, `{"pinned":true}`, http.StatusOK); !n.Pinned {
		t.Errorf("pinned true: %+v", n)
	}
	if n := pin(a.ID, `{"pinned":true}`, http.StatusOK); !n.Pinned {
		t.Errorf("pinned true again: %+v", n)
	}
	if n := pin(a.ID, `{"pinned":false}`, http.StatusOK); n.Pinned {
		t.Errorf("pinned false: %+v", n)
	}
	// The pin reaches synced clients.
	pin(a.ID, `{"pinned":true}`, http.StatusOK)
	w := e.apiDo("GET", fmt.Sprintf("/changes?since=%d", a.Seq))
	if !strings.Contains(w.Body.String(), `"pinned":true`) {
		t.Errorf("/changes lacks the pin: %s", w.Body.String())
	}

	pin(999, "", http.StatusNotFound)
	pin(a.ID, `{nope`, http.StatusBadRequest)
}
