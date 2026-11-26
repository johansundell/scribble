package router_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/images"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/types"
)

// pngOf is a w×1 PNG; different widths make different images.
func pngOf(t *testing.T, w int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// imageUpload is a same-origin multipart POST of data as "file", named name.
func imageUpload(t *testing.T, path, name string, data []byte, cookies ...*http.Cookie) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(data)
	mw.Close()
	req := httptest.NewRequest("POST", path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", "http://example.com")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return req
}

// uploaded is the URL an upload answered with.
func uploaded(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("upload = %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	var res struct{ URL string }
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("%s: %v", w.Body.String(), err)
	}
	return res.URL
}

func prepared(t *testing.T, data []byte) images.Image {
	t.Helper()
	img, err := images.Prepare(data)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestWebImages_UploadAndServe(t *testing.T) {
	e := newWebEnv(t)
	data := pngOf(t, 3)
	want := prepared(t, data).Path()

	// Uploading needs the session; a post from elsewhere is refused.
	wantRedirect(t, e.do(imageUpload(t, handlers.ImagesUploadPath, "a.png", data)), "/login")
	c := e.login(t)
	req := imageUpload(t, handlers.ImagesUploadPath, "a.png", data, c)
	req.Header.Set("Origin", "https://evil.test")
	if w := e.do(req); w.Code != http.StatusForbidden {
		t.Errorf("cross-origin upload = %d, want 403", w.Code)
	}

	// The name says nothing: the bytes make the URL, and the same file
	// twice is stored once.
	if got := uploaded(t, e.do(imageUpload(t, handlers.ImagesUploadPath, "photo.gif", data, c))); got != want {
		t.Errorf("url = %q, want %q", got, want)
	}
	if got := uploaded(t, e.do(imageUpload(t, handlers.ImagesUploadPath, "again.png", data, c))); got != want {
		t.Errorf("second url = %q, want %q", got, want)
	}
	if list, _ := e.store.ListImages(context.Background()); len(list) != 1 {
		t.Errorf("stored %d images, want 1", len(list))
	}

	// Anyone can fetch it, without logging in.
	w := e.do(get(want))
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatalf("GET %s = %d, %d bytes", want, w.Code, w.Body.Len())
	}
	for k, v := range map[string]string{
		"Content-Type":            "image/png",
		"Cache-Control":           "public, max-age=31536000, immutable",
		"X-Content-Type-Options":  "nosniff",
		"X-Robots-Tag":            "noindex",
		"Content-Security-Policy": "sandbox",
	} {
		if got := w.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}

	// Unknown, malformed, or with another type's extension: 404.
	id := prepared(t, data).ID
	for _, p := range []string{
		"/images/" + strings.Repeat("0", 64) + ".png",
		"/images/" + id + ".jpg",
		"/images/" + id,
		"/images/nope.png",
	} {
		if w := e.do(get(p)); w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, w.Code)
		}
	}
}

func TestWebImages_UploadRefusals(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	w := e.do(imageUpload(t, handlers.ImagesUploadPath, "x.png", svg, c))
	wantBody(t, w, http.StatusUnsupportedMediaType, "PNG, JPEG, GIF and WebP")

	big := append(pngOf(t, 1), make([]byte, images.MaxBytes)...)
	w = e.do(imageUpload(t, handlers.ImagesUploadPath, "big.png", big, c))
	wantBody(t, w, http.StatusRequestEntityTooLarge, "10 MB")

	req := httptest.NewRequest("POST", handlers.ImagesUploadPath, nil)
	req.Header.Set("Origin", "http://example.com")
	req.AddCookie(c)
	if w := e.do(req); w.Code != http.StatusBadRequest {
		t.Errorf("no file = %d, want 400", w.Code)
	}
	if list, _ := e.store.ListImages(context.Background()); len(list) != 0 {
		t.Errorf("stored %d images, want none", len(list))
	}
}

func TestWebImages_OffWithoutWebApp(t *testing.T) {
	e := newWebEnv(t)
	img := prepared(t, pngOf(t, 2))
	if err := e.store.PutImage(context.Background(), img); err != nil {
		t.Fatal(err)
	}
	h := mustNewHandler(t, e.store, false, os.DirFS(".."), "test", "dev")
	r, err := router.NewRouter(router.Config{
		Handler:  h,
		LogSink:  &recordingSink{},
		Settings: types.AppSettings{AuthToken: "secret-token"},
		Assets:   os.DirFS(".."),
	})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, get(img.Path()))
	if w.Code != http.StatusNotFound {
		t.Errorf("GET image with the web app off = %d, want 404", w.Code)
	}
}

func TestWebImages_EditLinkUpload(t *testing.T) {
	e := newWebEnv(t)
	n := e.note(t, "# Shared\nbody")
	tok := e.shareEdit(t, n)
	data := pngOf(t, 4)
	path := handlers.EditSharedImagesPath(tok)

	// No session, and from anywhere: the token is the key.
	req := imageUpload(t, path, "a.png", data)
	req.Header.Set("Origin", "null")
	got := uploaded(t, e.do(req))
	if want := prepared(t, data).Path(); got != want {
		t.Errorf("url = %q, want %q", got, want)
	}
	if w := e.do(imageUpload(t, handlers.EditSharedImagesPath(strings.Repeat("0", 32)), "a.png", data)); w.Code != http.StatusNotFound {
		t.Errorf("unknown token = %d, want 404", w.Code)
	}

	// Revoking the link stops uploads; what was uploaded stays.
	if err := e.store.UnshareNoteEdit(context.Background(), n.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.do(imageUpload(t, path, "b.png", pngOf(t, 5))); w.Code != http.StatusNotFound {
		t.Errorf("upload after revoke = %d, want 404", w.Code)
	}
	if w := e.do(get(got)); w.Code != http.StatusOK {
		t.Errorf("image after revoke = %d, want 200", w.Code)
	}
}

func TestWebImages_Render(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	id := strings.Repeat("ab", 32)
	n := e.note(t, strings.Join([]string{
		"# Pictures",
		"",
		"![ours](/images/" + id + ".png)",
		"",
		"![web](https://example.org/x.png)",
		"",
		"![svg](/images/" + id + ".svg) ![other](/healthz) ![up](/images/../notes) ![rel](images/" + id + ".png)",
	}, "\n"))

	want := []string{
		`<img src="/images/` + id + `.png" alt="ours">`,
		`<img src="https://example.org/x.png" alt="web">`,
		"<p>svg other up rel</p>",
	}
	w := e.do(get(notePath(n), c))
	wantBody(t, w, http.StatusOK, want...)
	wantNot(t, w.Body.String(), id+`.svg"`, `src="/healthz"`, `src="/images/../`, `src="images/`)

	// A share link's page shows them the same way.
	tok, err := e.store.ShareNote(context.Background(), n.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantBody(t, e.do(get("/s/"+tok)), http.StatusOK, want...)
}

func TestWebImages_Editor(t *testing.T) {
	e := newWebEnv(t)
	c := e.login(t)
	n := e.note(t, "# Shared\nbody")
	tok := e.shareEdit(t, n)

	for path, upload := range map[string]string{
		editPath(n):           handlers.ImagesUploadPath,
		"/new":                handlers.ImagesUploadPath,
		"/e/" + tok + "/edit": handlers.EditSharedImagesPath(tok),
	} {
		wantBody(t, e.do(get(path, c)), http.StatusOK,
			fmt.Sprintf(`data-images="%s"`, upload),
			"data-image-button", `aria-label="Image"`,
			`type="file"`, `accept="image/png,image/jpeg,image/gif,image/webp"`,
			"<code>![description](image URL)</code>",
		)
	}
}

func TestImages_ExportImport(t *testing.T) {
	e := newWebEnv(t)
	api := func(req *http.Request) *httptest.ResponseRecorder {
		req.Header.Set("Authorization", "Bearer secret-token")
		return e.do(req)
	}
	ctx := context.Background()
	img := prepared(t, pngOf(t, 6))
	if err := e.store.PutImage(ctx, img); err != nil {
		t.Fatal(err)
	}
	e.note(t, "# Alpha\n![pic]("+img.Path()+")")

	// The export carries the image beside the notes.
	w := api(get("/export"))
	wantZipDownload(t, w)
	files := map[string][]byte{}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		rc, _ := f.Open()
		var buf bytes.Buffer
		buf.ReadFrom(rc)
		rc.Close()
		files[f.Name] = buf.Bytes()
	}
	if !bytes.Equal(files["images/"+img.FileName()], img.Data) || len(files) != 2 {
		t.Fatalf("export holds %d files, want Alpha.md and images/%s", len(files), img.FileName())
	}

	// Into an empty server, the image comes along.
	fresh := newWebEnv(t)
	req := uploadRequest(t, "/import", w.Body.Bytes(), nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	if w := fresh.do(req); w.Code != http.StatusOK {
		t.Fatalf("import = %d: %s", w.Code, w.Body.String())
	}
	got, err := fresh.store.GetImage(ctx, img.ID)
	if err != nil || !bytes.Equal(got.Data, img.Data) || got.MIME != "image/png" {
		t.Errorf("imported image = %+v, %v", got, err)
	}

	// An image whose name isn't the hash of its bytes, or that isn't an
	// accepted type, fails the import, as an upload would.
	other := prepared(t, pngOf(t, 7))
	svgName := strings.Repeat("ab", 32) + ".png"
	for name, data := range map[string]string{
		"images/" + other.FileName():  string(img.Data),
		"images/" + svgName:           "<svg></svg>",
		"images/" + other.ID + ".jpg": string(other.Data),
	} {
		w := api(uploadRequest(t, "/import", zipOf(t, map[string]string{"Beta.md": "# Beta", name: data}), nil))
		wantBody(t, w, http.StatusBadRequest)
		if _, err := e.store.GetNoteByTitle(ctx, "Beta"); err == nil {
			t.Errorf("%s: the import went ahead", name)
		}
	}
	// Other files, in images/ or not, are skipped as before.
	w = api(uploadRequest(t, "/import", zipOf(t, map[string]string{"Beta.md": "# Beta", "images/photo.png": "x", "pic.png": "y"}), nil))
	wantBody(t, w, http.StatusOK, `"imported":1`)
}
