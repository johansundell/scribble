// Package notes holds the Note type and the [[wiki-link]] helpers shared by
// the server, the CLI and the GUI client. It has no dependencies on the rest
// of the module so clients stay small.
package notes

import (
	"regexp"
	"strings"
	"time"
)

// Note is a single Markdown note. The title is not stored separately by
// clients: it is the first non-empty line of Body, with leading '#' and
// spaces removed.
type Note struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Revision  int64     `json:"revision"` // per-note edit counter, for conflict detection
	Seq       int64     `json:"seq"`      // global change counter, for sync
	Deleted   bool      `json:"deleted,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TitleOf returns the title implied by a Markdown body.
func TitleOf(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
		if line != "" {
			return line
		}
	}
	return ""
}

// SetTitle replaces the title line of body, keeping any leading '#' marks.
func SetTitle(body, title string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		prefix := ""
		if strings.HasPrefix(trimmed, "#") {
			hashes := trimmed[:len(trimmed)-len(strings.TrimLeft(trimmed, "#"))]
			prefix = hashes + " "
		}
		lines[i] = prefix + title
		return strings.Join(lines, "\n")
	}
	return title + "\n" + body
}

var linkRe = regexp.MustCompile(`\[\[([^\[\]\n]+)\]\]`)

// Links returns the titles referenced as [[Title]] in body.
func Links(body string) []string {
	var out []string
	for _, m := range linkRe.FindAllStringSubmatch(body, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

// HasLinkTo reports whether body contains a [[title]] link (case-insensitive).
func HasLinkTo(body, title string) bool {
	for _, l := range Links(body) {
		if strings.EqualFold(l, title) {
			return true
		}
	}
	return false
}

// RewriteLinks changes [[oldTitle]] to [[newTitle]] (case-insensitive).
func RewriteLinks(body, oldTitle, newTitle string) string {
	return linkRe.ReplaceAllStringFunc(body, func(s string) string {
		if strings.EqualFold(strings.TrimSpace(s[2:len(s)-2]), oldTitle) {
			return "[[" + newTitle + "]]"
		}
		return s
	})
}

// LinkifyForView rewrites [[Title]] into a Markdown link with the note://
// scheme, ready for a Markdown renderer. Used by GUI clients.
func LinkifyForView(body string) string {
	return linkRe.ReplaceAllStringFunc(body, func(s string) string {
		t := strings.TrimSpace(s[2 : len(s)-2])
		return "[" + t + "](note://" + strings.ReplaceAll(t, " ", "%20") + ")"
	})
}
