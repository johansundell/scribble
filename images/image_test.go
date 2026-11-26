package images

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gifBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black}), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// webpBytes is the smallest RIFF WEBP header the type sniffer accepts.
func webpBytes() []byte {
	return []byte("RIFF\x1a\x00\x00\x00WEBPVP8L\x0d\x00\x00\x00\x2f\x00\x00\x00\x10\x07\x10\x11\x11\x88\x88\xfe\x07\x00")
}

// segment is a JPEG marker segment: 0xFF, marker, a two-byte length that
// counts itself, then payload.
func segment(marker byte, payload string) []byte {
	n := len(payload) + 2
	return append([]byte{0xFF, marker, byte(n >> 8), byte(n)}, payload...)
}

// withSegments is the JPEG j with segs put in after its SOI marker.
func withSegments(j []byte, segs ...[]byte) []byte {
	out := append([]byte{}, j[:2]...)
	for _, s := range segs {
		out = append(out, s...)
	}
	return append(out, j[2:]...)
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestPrepareTakesTheFormatFromTheBytes(t *testing.T) {
	for _, tc := range []struct {
		data []byte
		mime string
		ext  string
	}{
		{pngBytes(t), "image/png", "png"},
		{jpegBytes(t), "image/jpeg", "jpg"},
		{gifBytes(t), "image/gif", "gif"},
		{webpBytes(), "image/webp", "webp"},
	} {
		img, err := Prepare(tc.data)
		if err != nil {
			t.Fatalf("%s: %v", tc.mime, err)
		}
		if img.MIME != tc.mime {
			t.Errorf("MIME = %q, want %q", img.MIME, tc.mime)
		}
		if want := sum(tc.data) + "." + tc.ext; img.FileName() != want {
			t.Errorf("FileName = %q, want %q", img.FileName(), want)
		}
		if img.Path() != "/images/"+img.FileName() {
			t.Errorf("Path = %q", img.Path())
		}
	}
}

func TestPrepareRefusesOtherFormats(t *testing.T) {
	for name, data := range map[string][]byte{
		"svg":   []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"html":  []byte("<!doctype html><p>hi"),
		"text":  []byte("just text"),
		"empty": nil,
		"bmp":   append([]byte("BM"), make([]byte, 40)...),
	} {
		if _, err := Prepare(data); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want ErrUnsupported", name, err)
		}
	}
}

func TestPrepareRefusesTooLarge(t *testing.T) {
	data := append(pngBytes(t), make([]byte, MaxBytes)...)
	if _, err := Prepare(data); !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

func TestPrepareStripsJPEGMetadata(t *testing.T) {
	plain := jpegBytes(t)
	exif := segment(0xE1, "Exif\x00\x00GPS 59.33N 18.06E")
	xmp := segment(0xE1, "http://ns.adobe.com/xap/1.0/\x00<gps/>")
	tagged := withSegments(plain, exif, xmp)

	img, err := Prepare(tagged)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(img.Data, plain) {
		t.Fatalf("metadata kept: %d bytes, want %d", len(img.Data), len(plain))
	}
	if strings.Contains(string(img.Data), "GPS") {
		t.Error("GPS text left in the image")
	}
	// The hash is of what is stored, so the same photo with other metadata
	// is stored once, and preparing it again changes nothing.
	if img.ID != sum(plain) {
		t.Errorf("ID = %s, want the hash of the stripped bytes", img.ID)
	}
	again, err := Prepare(img.Data)
	if err != nil || again.ID != img.ID {
		t.Errorf("second Prepare = %v, %v; want the same ID", again.ID, err)
	}
}

func TestPrepareKeepsOtherJPEGSegments(t *testing.T) {
	icc := segment(0xE2, "ICC_PROFILE\x00profile")
	data := withSegments(jpegBytes(t), icc)
	img, err := Prepare(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(img.Data, data) {
		t.Error("a segment other than APP1 was changed")
	}
}

func TestPrepareRefusesBrokenJPEG(t *testing.T) {
	j := jpegBytes(t)
	cut := withSegments(j[:4], segment(0xE1, "Exif\x00\x00"))[:10] // a segment longer than the file
	if _, err := Prepare(cut); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestParseFileName(t *testing.T) {
	id := strings.Repeat("ab", 32)
	for name, want := range map[string]bool{
		id + ".png":                       true,
		id + ".jpg":                       true,
		id + ".gif":                       true,
		id + ".webp":                      true,
		id + ".svg":                       false,
		id + ".PNG":                       false,
		strings.ToUpper(id) + ".png":      false,
		id[:62] + ".png":                  false,
		id + "0.png":                      false,
		id:                                false,
		"../" + id + ".png":               false,
		strings.Repeat("zz", 32) + ".png": false,
	} {
		gotID, mime, ok := ParseFileName(name)
		if ok != want {
			t.Errorf("ParseFileName(%q) ok = %v, want %v", name, ok, want)
			continue
		}
		if ok && (gotID != id || mime == "") {
			t.Errorf("ParseFileName(%q) = %q, %q", name, gotID, mime)
		}
	}
}

func TestIsPath(t *testing.T) {
	id := strings.Repeat("ab", 32)
	for p, want := range map[string]bool{
		"/images/" + id + ".png":        true,
		"/images/" + id + ".svg":        false,
		"/images/../notes":              false,
		"//evil.example/" + id + ".png": false,
		"/images/" + id + ".png?x=1":    false,
		"images/" + id + ".png":         false,
	} {
		if got := IsPath(p); got != want {
			t.Errorf("IsPath(%q) = %v, want %v", p, got, want)
		}
	}
}
