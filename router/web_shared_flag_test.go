package router_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// listItem is the <li> of the note list that links note title.
func listItem(t *testing.T, body, title string) string {
	t.Helper()
	i := strings.Index(body, ">"+title)
	if i < 0 {
		t.Fatalf("%q is not in the list", title)
	}
	start := strings.LastIndex(body[:i], "<li>")
	end := strings.Index(body[i:], "</li>")
	if start < 0 || end < 0 {
		t.Fatalf("%q is not in a list item", title)
	}
	return body[start : i+end]
}

// The list flags notes with a working view link 🔗 and edit link ✏️ (#208),
// before the pin badge.
func TestWebSharedFlag(t *testing.T) {
	e := newWebEnv(t)
	ctx := context.Background()
	e.note(t, "# Private")
	view := e.note(t, "# Viewed")
	edit := e.note(t, "# Edited")
	both := e.note(t, "# Both")
	expired := e.note(t, "# Expired")
	past := time.Now().Add(-time.Hour)
	for _, step := range []func() (string, error){
		func() (string, error) { return e.store.ShareNote(ctx, view.ID, nil) },
		func() (string, error) { return e.store.ShareNoteEdit(ctx, edit.ID, nil) },
		func() (string, error) { return e.store.ShareNote(ctx, both.ID, nil) },
		func() (string, error) { return e.store.ShareNoteEdit(ctx, both.ID, nil) },
		func() (string, error) { return e.store.ShareNote(ctx, expired.ID, &past) },
	} {
		if _, err := step(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.store.PinNote(ctx, both.ID, true); err != nil {
		t.Fatal(err)
	}

	c := e.login(t)
	w := e.do(get("/", c))
	if w.Code != http.StatusOK {
		t.Fatalf("GET / = %d", w.Code)
	}
	body := w.Body.String()
	viewBadge := `<span class="share-badge" role="img" aria-label="Shared (view link)" title="Shared (view link)">🔗</span>`
	editBadge := `<span class="share-badge" role="img" aria-label="Shared (edit link)" title="Shared (edit link)">✏️</span>`

	for _, title := range []string{"Private", "Expired"} {
		wantNot(t, listItem(t, body, title), "share-badge")
	}
	if li := listItem(t, body, "Viewed"); !strings.Contains(li, viewBadge) || strings.Contains(li, "✏️") {
		t.Errorf("view-shared row = %s, want only the view badge", li)
	}
	if li := listItem(t, body, "Edited"); !strings.Contains(li, editBadge) || strings.Contains(li, "🔗") {
		t.Errorf("edit-shared row = %s, want only the edit badge", li)
	}
	li := listItem(t, body, "Both")
	iv, ie, ip := strings.Index(li, viewBadge), strings.Index(li, editBadge), strings.Index(li, "pin-badge")
	if iv < 0 || ie < 0 || ip < 0 || !(iv < ie && ie < ip) {
		t.Errorf("shared, pinned row = %s, want 🔗 ✏️ 📌 in that order", li)
	}

	// The filtered list, which list.js swaps in, has them too.
	wantBody(t, e.do(get("/?q=Viewed", c)), http.StatusOK, viewBadge)
}
