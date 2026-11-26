// Package images checks and names the images notes show (#201, ADR 16). An
// image is stored once, under the SHA-256 of its bytes, and a note refers to
// it as ![alt](/images/<sha256>.<ext>).
package images

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

// MaxBytes caps one image.
const MaxBytes = 10 << 20

// PathPrefix starts an image's URL.
const PathPrefix = "/images/"

var (
	ErrTooLarge    = errors.New("the image is larger than 10 MB")
	ErrUnsupported = errors.New("only PNG, JPEG, GIF and WebP images can be added")
)

// exts maps each accepted type to its file extension.
var exts = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/gif":  "gif",
	"image/webp": "webp",
}

// Image is an image as stored: its ID (the SHA-256 of Data, as hex), type and
// bytes.
type Image struct {
	ID   string
	MIME string
	Data []byte
}

// FileName is the image's file name: <ID>.<ext>.
func (i Image) FileName() string {
	return i.ID + "." + exts[i.MIME]
}

// Path is the image's URL path, which notes refer to.
func (i Image) Path() string {
	return PathPrefix + i.FileName()
}

// Prepare checks data and makes the Image to store: the type comes from the
// bytes, never a file name, and a JPEG loses its APP1 segments (EXIF and
// XMP, which can hold where the photo was taken) before it is hashed.
func Prepare(data []byte) (Image, error) {
	if len(data) > MaxBytes {
		return Image{}, ErrTooLarge
	}
	mime := http.DetectContentType(data)
	if _, ok := exts[mime]; !ok {
		return Image{}, ErrUnsupported
	}
	if mime == "image/jpeg" {
		var err error
		if data, err = stripJPEG(data); err != nil {
			return Image{}, err
		}
	}
	sum := sha256.Sum256(data)
	return Image{ID: hex.EncodeToString(sum[:]), MIME: mime, Data: data}, nil
}

// stripJPEG drops the APP1 segments before the image data. The result is
// data itself when there are none, so stripping twice changes nothing.
func stripJPEG(data []byte) ([]byte, error) {
	var out bytes.Buffer
	out.Write(data[:2]) // SOI
	i := 2
	for {
		if i+4 > len(data) || data[i] != 0xFF {
			return nil, ErrUnsupported
		}
		marker := data[i+1]
		if marker == 0xFF { // a fill byte before a marker
			out.WriteByte(0xFF)
			i++
			continue
		}
		if marker == 0xDA { // start of scan: the rest is image data
			out.Write(data[i:])
			break
		}
		end := i + 2 + (int(data[i+2])<<8 | int(data[i+3]))
		if end > len(data) || end < i+4 {
			return nil, ErrUnsupported
		}
		if marker != 0xE1 {
			out.Write(data[i:end])
		}
		i = end
	}
	if out.Len() == len(data) {
		return data, nil
	}
	return out.Bytes(), nil
}

// ParseFileName reads an image file name, <sha256 hex>.<ext>, into its ID
// and type.
func ParseFileName(name string) (id, mime string, ok bool) {
	id, ext, found := strings.Cut(name, ".")
	if !found || len(id) != 64 || strings.ToLower(id) != id {
		return "", "", false
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", "", false
	}
	for m, e := range exts {
		if e == ext {
			return id, m, true
		}
	}
	return "", "", false
}

// IsPath reports whether p is an image's URL path, the only image source a
// note may use besides http(s).
func IsPath(p string) bool {
	name, ok := strings.CutPrefix(p, PathPrefix)
	if !ok {
		return false
	}
	_, _, ok = ParseFileName(name)
	return ok
}
