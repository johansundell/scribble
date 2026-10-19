package router_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// listPart is the note list of a page, so a title in the open note doesn't
// count as listed.
func listPart(t *testing.T, body string) string {
	t.Helper()
	start, end := strings.Index(body, `<nav class="note-list"`), strings.Index(body, "</nav>")
	if start < 0 || end < start {
		t.Fatalf("no note list in:\n%s", body)
	}
	return body[start:end]
}

func wantListed(t *testing.T, body string, listed, notListed []string) {
	t.Helper()
	list := listPart(t, body)
	for _, s := range listed {
		if !strings.Contains(list, s) {
			t.Errorf("list lacks %q:\n%s", s, list)
		}
	}
	for _, s := range notListed {
		if strings.Contains(list, s) {
			t.Errorf("list has %q:\n%s", s, list)
		}
	}
}

func TestWebFilter_Search(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	e.note(t, "# Nginx setup\nconf")
	e.note(t, "# Other\nmentions NGINX in the body")
	e.note(t, "# Third\nnothing")

	w := e.do(get("/app?q=nginx", c))
	wantBody(t, w, http.StatusOK, `type="search"`, `name="q"`, `value="nginx"`, `action="/app"`, "/assets/js/list.js")
	wantListed(t, w.Body.String(), []string{"Nginx setup", "Other", "?q=nginx"}, []string{"Third"})

	w = e.do(get("/app?q=zzz", c))
	wantBody(t, w, http.StatusOK, "No notes match")
	wantNot(t, w.Body.String(), "No notes yet")
}

func TestWebFilter_Project(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	e.note(t, "# Alpha\n#work")
	e.note(t, "# Beta\n#home")
	e.note(t, "# Gamma\n#work and nginx")

	w := e.do(get("/app", c))
	wantBody(t, w, http.StatusOK, `<option value="">All notes</option>`, `<option value="home">#home</option>`, `<option value="work">#work</option>`)

	w = e.do(get("/app?project=Work", c))
	wantBody(t, w, http.StatusOK, `<option value="work" selected>#work</option>`)
	wantListed(t, w.Body.String(), []string{"Alpha", "Gamma", "?project=work"}, []string{"Beta"})

	// q and project combine.
	w = e.do(get("/app?project=work&q=nginx", c))
	wantListed(t, w.Body.String(), []string{"Gamma"}, []string{"Alpha", "Beta"})

	// Not a project name: no filter.
	w = e.do(get("/app?project=42", c))
	wantListed(t, w.Body.String(), []string{"Alpha", "Beta", "Gamma"}, []string{"project=42"})

	// A project no note has any more is still shown as chosen.
	w = e.do(get("/app?project=gone", c))
	wantBody(t, w, http.StatusOK, `<option value="gone" selected>#gone</option>`, "No notes match")
}

func TestWebFilter_SurvivesNavigation(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	a := e.note(t, "# Alpha\n#work nginx")
	b := e.note(t, "# Beta\n#home")
	filter := "?project=work&amp;q=nginx"

	w := e.do(get("/app?q=nginx&project=work", c))
	wantBody(t, w, http.StatusOK,
		fmt.Sprintf(`href="/app/notes/%d%s"`, a.ID, filter),
		`href="/app/new`+filter+`"`,
	)

	// Opening a note keeps the filter, and the note opens even when the
	// filter hides it.
	w = e.do(get(fmt.Sprintf("/app/notes/%d?q=nginx&project=work", b.ID), c))
	wantBody(t, w, http.StatusOK, "<h1>Beta</h1>", fmt.Sprintf(`href="/app/notes/%d/edit%s"`, b.ID, filter), `value="nginx"`)
	wantListed(t, w.Body.String(), []string{"Alpha"}, []string{"Beta"})

	// The editor keeps it: on the form, Cancel and after saving.
	w = e.do(get(fmt.Sprintf("/app/notes/%d/edit?q=nginx&project=work", a.ID), c))
	wantBody(t, w, http.StatusOK,
		fmt.Sprintf(`action="/app/notes/%d/edit%s"`, a.ID, filter),
		fmt.Sprintf(`href="/app/notes/%d%s" data-filtered="cancel" data-shortcut="view">Cancel`, a.ID, filter),
	)
	save := url.Values{"body": {"# Alpha\n#work nginx!"}, "revision": {"1"}, "action": {"save"}}
	wantRedirect(t, e.do(postForm(fmt.Sprintf("/app/notes/%d/edit?q=nginx&project=work", a.ID), save, c)),
		fmt.Sprintf("/app/notes/%d/edit?project=work&q=nginx", a.ID))
	done := url.Values{"body": {"# Alpha\n#work nginx!!"}, "revision": {"2"}, "action": {"done"}}
	wantRedirect(t, e.do(postForm(fmt.Sprintf("/app/notes/%d/edit?project=work", a.ID), done, c)),
		fmt.Sprintf("/app/notes/%d?project=work", a.ID))

	w = e.do(postForm("/app/new?project=work", url.Values{"body": {"Gamma\n\n#work"}, "tag": {"#work"}, "action": {"done"}}, c))
	if loc := w.Header().Get("Location"); w.Code != http.StatusSeeOther || !strings.HasSuffix(loc, "?project=work") {
		t.Errorf("new note: %d to %q, want the filter kept", w.Code, loc)
	}

	// New in a project filter prefills its tag.
	w = e.do(get("/app/new?project=work&q=nginx", c))
	wantBody(t, w, http.StatusOK, "\n\n#work\n</textarea>", `action="/app/new`+filter+`"`)
}

func TestWebFilter_TagLinks(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Tagged #title\nsee #Work and #42 and page#anchor\n\n`#code`")

	w := e.do(get(fmt.Sprintf("/app/notes/%d", n.ID), c))
	wantBody(t, w, http.StatusOK, `<a href="/app?project=work">#Work</a>`, "<code>#code</code>", "#42", "page#anchor")
	wantNot(t, w.Body.String(), "project=title", "project=42", "project=anchor", "project=code", "project://")
}
