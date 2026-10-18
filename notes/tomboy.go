package notes

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// tomboyTextEscaper escapes literal Markdown inline punctuation in Tomboy
// character data so plain text does not accidentally trigger emphasis,
// strikethrough, code spans or wiki-links.
var tomboyTextEscaper = strings.NewReplacer(
	`\`, `\\`,
	`*`, `\*`,
	`_`, `\_`,
	`~`, `\~`,
	"`", "\\`",
	`[`, `\[`,
	`]`, `\]`,
)

// ParseTomboy reads a Tomboy .note XML document and returns the equivalent
// Scribble Markdown body (beginning with "# <Title>\n").
func ParseTomboy(r io.Reader) (string, error) {
	dec := xml.NewDecoder(r)
	var (
		title          string
		inTitle        bool
		inContent      bool
		sawNote        bool
		listDepth      int
		itemDepth      int
		linkDepth      int
		monoDepth      int
		urlDepth       int
		justClosedList bool
		pastLine1      bool
		firstPlain     strings.Builder
		monoBuf        strings.Builder
		body           strings.Builder
	)
	ensureNewline := func() {
		if s := body.String(); s != "" && !strings.HasSuffix(s, "\n") {
			body.WriteByte('\n')
		}
	}
	flushPostListGap := func() {
		if justClosedList {
			justClosedList = false
			ensureNewline()
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
			switch {
			case t.Name.Local == "note" && !sawNote:
				sawNote = true
			case t.Name.Local == "title" && !inContent:
				inTitle = true
			case t.Name.Local == "note-content":
				inContent = true
			case inContent:
				switch {
				case isTomboyInternalLink(t.Name):
					if linkDepth == 0 && monoDepth == 0 {
						flushPostListGap()
						body.WriteString("[[")
					}
					linkDepth++
				case isTomboyURLLink(t.Name):
					urlDepth++
				case t.Name.Local == "monospace":
					if linkDepth == 0 {
						if monoDepth == 0 {
							flushPostListGap()
							monoBuf.Reset()
						}
						monoDepth++
					}
				case linkDepth == 0 && monoDepth == 0:
					if m := tomboyInlineMarker(t.Name); m != "" {
						flushPostListGap()
						body.WriteString(m)
					} else if t.Name.Local == "list" {
						flushPostListGap()
						listDepth++
					} else if t.Name.Local == "list-item" {
						itemDepth++
						ensureNewline()
						body.WriteString(strings.Repeat("  ", max(0, listDepth-1)) + "- ")
					}
				}
			}
		case xml.EndElement:
			switch {
			case t.Name.Local == "title" && !inContent:
				inTitle = false
			case t.Name.Local == "note-content":
				inContent = false
			case inContent:
				switch {
				case isTomboyInternalLink(t.Name):
					if linkDepth > 0 {
						linkDepth--
						if linkDepth == 0 && monoDepth == 0 {
							body.WriteString("]]")
						}
					}
				case isTomboyURLLink(t.Name):
					urlDepth = max(0, urlDepth-1)
				case t.Name.Local == "monospace":
					if linkDepth == 0 && monoDepth > 0 {
						monoDepth--
						if monoDepth == 0 {
							body.WriteString(formatCodeSpan(monoBuf.String()))
						}
					}
				case linkDepth == 0 && monoDepth == 0:
					if m := tomboyInlineMarker(t.Name); m != "" {
						body.WriteString(m)
					} else if t.Name.Local == "list" {
						listDepth = max(0, listDepth-1)
						justClosedList = listDepth == 0 && body.Len() > 0
					} else if t.Name.Local == "list-item" {
						itemDepth = max(0, itemDepth-1)
						ensureNewline()
					}
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
				if !pastLine1 {
					if before, _, ok := strings.Cut(s, "\n"); ok {
						firstPlain.WriteString(before)
						pastLine1 = true
					} else {
						firstPlain.WriteString(s)
					}
				}
				if monoDepth > 0 {
					monoBuf.WriteString(s)
					continue
				}
				if justClosedList {
					s = strings.TrimLeft(s, "\n")
					if s == "" {
						continue
					}
					flushPostListGap()
				}
				if linkDepth == 0 && urlDepth == 0 {
					s = tomboyTextEscaper.Replace(s)
				}
				body.WriteString(s)
			}
		}
	}
	title = strings.TrimSpace(title)
	if !sawNote || title == "" {
		return "", fmt.Errorf("tomboy note has no title")
	}
	content := body.String()
	// Tomboy writes the note title again as the first line of <note-content>.
	if strings.EqualFold(strings.TrimSpace(firstPlain.String()), title) {
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

func isTomboyInternalLink(name xml.Name) bool {
	return name.Local == "internal" && (name.Space == "http://beatniksoftware.com/tomboy/link" || name.Space == "link")
}

func isTomboyURLLink(name xml.Name) bool {
	return name.Local == "url" && (name.Space == "http://beatniksoftware.com/tomboy/link" || name.Space == "link")
}

func tomboyInlineMarker(name xml.Name) string {
	switch name.Local {
	case "bold":
		return "**"
	case "italic":
		return "*"
	case "strikethrough":
		return "~~"
	}
	return ""
}

func formatCodeSpan(s string) string {
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	if s != "" && (strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`")) {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}
