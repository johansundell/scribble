package notes

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/johansundell/scribble/images"
)

// MaxZipEntryBytes caps one file read from a ZIP archive, unpacked, so a
// small archive can't unpack into gigabytes. It matches the server's limit
// on one note.
const MaxZipEntryBytes = 4 << 20

// ImportSummary is what an import did, as a sentence for people.
func ImportSummary(imported, skipped int) string {
	noun := "notes"
	if imported == 1 {
		noun = "note"
	}
	return fmt.Sprintf("Imported %d %s, skipped %d.", imported, noun, skipped)
}

// ZipFileName is the default name of an export made on day t:
// scribble-notes-YYYY-MM-DD.zip.
func ZipFileName(t time.Time) string {
	return "scribble-notes-" + t.Format("2006-01-02") + ".zip"
}

// unsafeFileChars are replaced with '_' in file names made from titles.
var unsafeFileChars = strings.NewReplacer(
	"/", "_", `\`, "_", ":", "_", "*", "_", "?", "_",
	`"`, "_", "<", "_", ">", "_", "|", "_",
)

// maxFileNameBytes caps a file name made from a title, leaving room for a
// "-N" suffix and ".md" under the usual 255-byte limit.
const maxFileNameBytes = 200

// fileNameFor is title as a file name, without extension: the characters
// file systems refuse become '_', it is cut to maxFileNameBytes, and leading
// or trailing dots and spaces are dropped. An empty result is "untitled".
func fileNameFor(title string) string {
	name := unsafeFileChars.Replace(title)
	if len(name) > maxFileNameBytes {
		name = strings.ToValidUTF8(name[:maxFileNameBytes], "")
	}
	name = strings.Trim(name, ". ")
	if name == "" {
		return "untitled"
	}
	return name
}

// ImageDir is the folder of an archive's images (#201): images/<file name>,
// where the file name is the image's, <sha256>.<ext>.
const ImageDir = "images/"

// ZipImage is an image in an archive. Data reads its bytes, so an archive's
// images need not all be in memory at once.
type ZipImage struct {
	// FileName is the image's file name, without ImageDir.
	FileName string
	Data     func() ([]byte, error)
}

// WriteZip writes list to w as a ZIP archive with one "<title>.md" file
// per note, holding its Markdown body as it is, and imgs in ImageDir.
// Titles that make the same file name (ignoring case) get "-2", "-3", ...
// in the order given.
func WriteZip(w io.Writer, list []Note, imgs ...ZipImage) error {
	zw := zip.NewWriter(w)
	used := map[string]bool{}
	for _, n := range list {
		base := fileNameFor(n.Title)
		name := base + ".md"
		for i := 2; used[strings.ToLower(name)]; i++ {
			name = base + "-" + strconv.Itoa(i) + ".md"
		}
		used[strings.ToLower(name)] = true
		f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: n.UpdatedAt})
		if err != nil {
			return err
		}
		if _, err := io.WriteString(f, n.Body); err != nil {
			return err
		}
	}
	for _, img := range imgs {
		data, err := img.Data()
		if err != nil {
			return err
		}
		// Stored, not deflated: image formats are compressed already.
		f, err := zw.CreateHeader(&zip.FileHeader{Name: ImageDir + img.FileName, Method: zip.Store})
		if err != nil {
			return err
		}
		if _, err := f.Write(data); err != nil {
			return err
		}
	}
	return zw.Close()
}

// ReadZip reads the notes in a ZIP archive: every .md and .txt file as
// Markdown and every .note file as Tomboy XML, in any folder. Other files,
// and macOS's __MACOSX and dot files, are skipped. A text file whose body
// doesn't start with a heading is titled after its file name, unless it is
// an export of a note whose title line has no '#'. Each Note has only Title
// and Body set; titles may repeat.
func ReadZip(r io.ReaderAt, size int64) ([]Note, error) {
	list, _, err := ReadArchive(r, size)
	return list, err
}

// ReadArchive reads the notes in a ZIP archive as ReadZip does, and its
// images: the files in a folder named images whose names are image file
// names, <sha256>.<ext>. Their Data reads them from r, up to
// images.MaxBytes each; it doesn't check them.
func ReadArchive(r io.ReaderAt, size int64) ([]Note, []ZipImage, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, nil, fmt.Errorf("not a ZIP archive: %w", err)
	}
	var out []Note
	var imgs []ZipImage
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || strings.HasPrefix(f.Name, "__MACOSX/") {
			continue
		}
		name := strings.ReplaceAll(f.Name, `\`, "/") // some Windows tools write \
		file := path.Base(name)
		if _, _, ok := images.ParseFileName(file); ok && path.Base(path.Dir(name)) == "images" {
			imgs = append(imgs, ZipImage{FileName: file, Data: func() ([]byte, error) {
				data, err := readZipFile(f, images.MaxBytes)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", f.Name, err)
				}
				return data, nil
			}})
			continue
		}
		ext := strings.ToLower(path.Ext(file))
		if strings.HasPrefix(file, ".") || (ext != ".md" && ext != ".txt" && ext != ".note") {
			continue
		}
		data, err := readZipFile(f, MaxZipEntryBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		var body string
		if ext == ".note" {
			if body, err = ParseTomboy(bytes.NewReader(data)); err != nil {
				return nil, nil, fmt.Errorf("%s: %w", f.Name, err)
			}
		} else {
			body = textBody(strings.TrimSuffix(file, path.Ext(file)), string(data))
		}
		out = append(out, Note{Title: TitleOf(body), Body: body})
	}
	return out, imgs, nil
}

// readZipFile reads f, refusing it when it unpacks to more than max bytes.
func readZipFile(f *zip.File, max int) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > max {
		return nil, fmt.Errorf("larger than %d bytes", max)
	}
	return data, nil
}

// textBody is the note body for a text file named name (without extension).
func textBody(name, text string) string {
	text = strings.ReplaceAll(strings.TrimPrefix(text, "\ufeff"), "\r\n", "\n")
	title := TitleOf(text)
	if strings.HasPrefix(strings.TrimSpace(TitleLine(text)), "#") || exportedAs(name, title) {
		return text
	}
	if title == "" {
		return "# " + name + "\n"
	}
	return "# " + name + "\n\n" + text
}

// exportedAs reports whether name is the file name WriteZip gives title:
// fileNameFor(title), maybe with a "-N" suffix.
func exportedAs(name, title string) bool {
	if title == "" {
		return false
	}
	base := fileNameFor(title)
	if strings.EqualFold(name, base) {
		return true
	}
	rest, ok := strings.CutPrefix(strings.ToLower(name), strings.ToLower(base)+"-")
	if !ok {
		return false
	}
	_, err := strconv.Atoi(rest)
	return err == nil
}
