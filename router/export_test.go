package router_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/notes"
)

// zipOf is a ZIP archive holding files.
func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// uploadRequest is a multipart POST of archive as "file", with the other
// form fields.
func uploadRequest(t *testing.T, path string, archive []byte, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	fw, err := mw.CreateFormFile("file", "notes.zip")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(archive)
	mw.Close()
	req := httptest.NewRequest("POST", path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", "http://example.com") // same origin, as a browser sends
	return req
}

func unzip(t *testing.T, b []byte) map[string]string {
	t.Helper()
	list, err := notes.ReadZip(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, n := range list {
		out[n.Title] = n.Body
	}
	return out
}

var exportName = regexp.MustCompile(`^attachment; filename="scribble-notes-\d{4}-\d{2}-\d{2}\.zip"$`)

func wantZipDownload(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("export = %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q", ct)
	}
	cd := w.Header().Get("Content-Disposition")
	if !exportName.MatchString(cd) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if today := time.Now().Format("2006-01-02"); !strings.Contains(cd, today) {
		t.Errorf("Content-Disposition %q lacks today's date %s", cd, today)
	}
}

func TestExportImportAPI(t *testing.T) {
	e := newWebEnv(t)
	api := func(req *http.Request) *httptest.ResponseRecorder {
		req.Header.Set("Authorization", "Bearer secret-token")
		return e.do(req)
	}
	e.note(t, "# Alpha\nfirst")
	e.note(t, "# Beta\nsecond")

	// Both need the token.
	if w := e.do(get("/export")); w.Code != http.StatusUnauthorized {
		t.Errorf("export without token = %d", w.Code)
	}
	if w := e.do(uploadRequest(t, "/import", zipOf(t, nil), nil)); w.Code != http.StatusUnauthorized {
		t.Errorf("import without token = %d", w.Code)
	}

	w := api(get("/export"))
	wantZipDownload(t, w)
	got := unzip(t, w.Body.Bytes())
	if got["Alpha"] != "# Alpha\nfirst" || got["Beta"] != "# Beta\nsecond" || len(got) != 2 {
		t.Errorf("exported %v", got)
	}

	archive := zipOf(t, map[string]string{
		"Alpha.md": "# Alpha\nchanged",
		"Gamma.md": "# Gamma\nnew",
		"todo.txt": "milk",
	})
	var res struct{ Imported, Skipped int }
	w = api(uploadRequest(t, "/import", archive, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("import = %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Imported != 2 || res.Skipped != 1 {
		t.Errorf("import result %s (%v)", w.Body.String(), err)
	}
	ctx := context.Background()
	if n, _ := e.store.GetNoteByTitle(ctx, "Alpha"); n.Body != "# Alpha\nfirst" {
		t.Errorf("skip mode changed Alpha: %q", n.Body)
	}
	if n, err := e.store.GetNoteByTitle(ctx, "todo"); err != nil || n.Body != "# todo\n\nmilk" {
		t.Errorf("todo = %+v, %v", n, err)
	}

	// Overwrite replaces existing notes; an unchanged one is skipped.
	w = api(uploadRequest(t, "/import", archive, map[string]string{"overwrite": "true"}))
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Imported != 1 || res.Skipped != 2 {
		t.Errorf("overwrite result %d %s (%v)", w.Code, w.Body.String(), err)
	}
	if n, _ := e.store.GetNoteByTitle(ctx, "Alpha"); n.Body != "# Alpha\nchanged" {
		t.Errorf("overwrite left Alpha as %q", n.Body)
	}

	// Not a ZIP, or no file: 400.
	if w := api(uploadRequest(t, "/import", []byte("nope"), nil)); w.Code != http.StatusBadRequest {
		t.Errorf("bad archive = %d", w.Code)
	}
	req := httptest.NewRequest("POST", "/import", nil)
	if w := api(req); w.Code != http.StatusBadRequest {
		t.Errorf("no file = %d", w.Code)
	}
}

func TestExportImportWeb(t *testing.T) {
	e := newWebEnv(t)
	e.note(t, "# Alpha\nfirst")
	wantRedirect(t, e.do(get(handlers.ExportPath)), "/login?next="+handlers.ExportPath)
	wantRedirect(t, e.do(uploadRequest(t, handlers.ImportPath, zipOf(t, nil), nil)), "/login")

	c := e.login(t)
	// The footer offers both, the import in a dialog.
	wantBody(t, e.do(get("/", c)), http.StatusOK,
		`href="`+handlers.ExportPath+`"`, ">Export notes</a>",
		"data-import-open", ">Import notes</button>",
		`<dialog id="import"`, `action="`+handlers.ImportPath+`"`, `enctype="multipart/form-data"`,
		`type="file" name="file" accept=".zip`, `type="checkbox" name="overwrite"`,
	)

	w := e.do(get(handlers.ExportPath, c))
	wantZipDownload(t, w)
	if got := unzip(t, w.Body.Bytes()); got["Alpha"] != "# Alpha\nfirst" {
		t.Errorf("exported %v", got)
	}

	archive := zipOf(t, map[string]string{"Alpha.md": "# Alpha\nnew", "Beta.md": "# Beta\n"})
	req := uploadRequest(t, handlers.ImportPath, archive, nil)
	req.AddCookie(c)
	wantBody(t, e.do(req), http.StatusOK, "Imported 1 note, skipped 1.")

	req = uploadRequest(t, handlers.ImportPath, archive, map[string]string{"overwrite": "on"})
	req.AddCookie(c)
	wantBody(t, e.do(req), http.StatusOK, "Imported 1 note, skipped 1.")
	if n, _ := e.store.GetNoteByTitle(context.Background(), "Alpha"); n.Body != "# Alpha\nnew" {
		t.Errorf("overwrite left Alpha as %q", n.Body)
	}

	req = uploadRequest(t, handlers.ImportPath, []byte("nope"), nil)
	req.AddCookie(c)
	wantBody(t, e.do(req), http.StatusBadRequest, "not a ZIP archive")

	req = uploadRequest(t, handlers.ImportPath, archive, nil)
	req.AddCookie(c)
	req.Header.Set("Origin", "https://evil.test")
	if w := e.do(req); w.Code != http.StatusForbidden {
		t.Errorf("cross-origin import = %d, want 403", w.Code)
	}
}
