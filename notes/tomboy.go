package notes

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// ParseTomboy reads a Tomboy .note XML document and returns the equivalent
// Scribble Markdown body (beginning with "# <Title>\n").
func ParseTomboy(r io.Reader) (string, error) {
	dec := xml.NewDecoder(r)
	var (
		title      string
		inTitle    bool
		inContent  bool
		sawNote    bool
		listDepth      int
		itemDepth      int
		justClosedList bool
		pastLine1      bool
		firstPlain     strings.Builder
		body           strings.Builder
	)
	ensureNewline := func() {
		if s := body.String(); s != "" && !strings.HasSuffix(s, "\n") {
			body.WriteByte('\n')
		}
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			justClosedList = false
			switch {
			case t.Name.Local == "note" && !sawNote:
				sawNote = true
			case t.Name.Local == "title" && !inContent:
				inTitle = true
			case t.Name.Local == "note-content":
				inContent = true
			case inContent:
				if m := tomboyInlineMarker(t.Name, true); m != "" {
					body.WriteString(m)
				} else if t.Name.Local == "list" {
					listDepth++
				} else if t.Name.Local == "list-item" {
					itemDepth++
					ensureNewline()
					body.WriteString(strings.Repeat("  ", max(0, listDepth-1)) + "- ")
				}
			}
		case xml.EndElement:
			switch {
			case t.Name.Local == "title" && !inContent:
				inTitle = false
			case t.Name.Local == "note-content":
				inContent = false
			case inContent:
				if m := tomboyInlineMarker(t.Name, false); m != "" {
					body.WriteString(m)
				} else if t.Name.Local == "list" {
					listDepth = max(0, listDepth-1)
					justClosedList = listDepth == 0
				} else if t.Name.Local == "list-item" {
					itemDepth = max(0, itemDepth-1)
					ensureNewline()
				}
			}
		case xml.CharData:
			s := strings.ReplaceAll(string(t), "\r\n", "\n")
			if inTitle {
				title += s
			} else if inContent {
				if listDepth > itemDepth && strings.TrimSpace(s) == "" {
					continue
				}
				if justClosedList {
					justClosedList = false
					s = strings.TrimPrefix(s, "\n")
				}
				if !pastLine1 {
					if before, _, ok := strings.Cut(s, "\n"); ok {
						firstPlain.WriteString(before)
						pastLine1 = true
					} else {
						firstPlain.WriteString(s)
					}
				}
				body.WriteString(s)
			}
		}
	}
	title = titleText(title)
	if !sawNote || title == "" {
		return "", fmt.Errorf("tomboy note has no title")
	}
	content := body.String()
	// Tomboy writes the note title again as the first line of <note-content>.
	if strings.EqualFold(titleText(firstPlain.String()), title) {
		if _, rest, ok := strings.Cut(content, "\n"); ok {
			content = rest
		} else {
			content = ""
		}
	}
	content = strings.TrimLeft(content, "\n")
	content = strings.TrimRight(content, " \t\n")
	if content == "" {
		return "# " + title + "\n", nil
	}
	return "# " + title + "\n\n" + content + "\n", nil
}

func tomboyInlineMarker(name xml.Name, open bool) string {
	switch {
	case name.Local == "internal" && (name.Space == "http://beatniksoftware.com/tomboy/link" || name.Space == "link"):
		if open {
			return "[["
		}
		return "]]"
	case name.Local == "bold":
		return "**"
	case name.Local == "italic":
		return "*"
	case name.Local == "strikethrough":
		return "~~"
	case name.Local == "monospace":
		return "`"
	}
	return ""
}
