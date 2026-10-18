// Package notes holds the Note type and the [[wiki-link]] and #project
// helpers shared by the server, the CLI and the GUI client. It has no
// dependencies on the rest of the module so clients stay small.
package notes

import (
	"regexp"
	"slices"
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
		if t := titleText(line); t != "" {
			return t
		}
	}
	return ""
}

// titleText is line without its leading '#' marks and spaces; a line that
// is empty afterwards cannot be the title.
func titleText(line string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
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

// projectRe matches a #project tag: '#' then an ASCII letter, at the start of
// a line or after whitespace. Group 1 is the separator, group 2 the name.
var projectRe = regexp.MustCompile(`(?m)(^|\s)#([A-Za-z][A-Za-z0-9_-]*)`)

// titleEnd returns the offset just past the line TitleOf takes the title
// from, so "# Title" is never read as a project tag.
func titleEnd(body string) int {
	off := 0
	for {
		line, _, more := strings.Cut(body[off:], "\n")
		end := off + len(line)
		if titleText(line) != "" || !more {
			return end
		}
		off = end + 1
	}
}

// Projects returns the #project tags in body, lowercase without the '#',
// deduplicated and sorted. The title line is ignored.
func Projects(body string) []string {
	var out []string
	for _, m := range projectRe.FindAllStringSubmatch(body[titleEnd(body):], -1) {
		out = append(out, strings.ToLower(m[2]))
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// HasProject reports whether body is tagged #project (case-insensitive; a
// leading '#' on project is optional).
func HasProject(body, project string) bool {
	_, found := slices.BinarySearch(Projects(body), NormalizeProject(project))
	return found
}

// NormalizeProject turns "#Name" or "Name" into the stored form "name".
func NormalizeProject(project string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(project), "#"))
}

// tagOnlyRe matches a line that is nothing but one #project tag.
var tagOnlyRe = regexp.MustCompile(`^#[A-Za-z][A-Za-z0-9_-]*$`)

// TitleIsTag reports whether the title line is nothing but a #project tag,
// as in a project draft whose title was never typed. Such a tag names the
// note instead of putting it in the project.
func TitleIsTag(body string) bool {
	i := titleEnd(body)
	return tagOnlyRe.MatchString(strings.TrimSpace(body[strings.LastIndex(body[:i], "\n")+1 : i]))
}

// verbatimRe matches the Markdown a project link must not be put inside:
// fenced code blocks, code spans, reference definitions, links and images
// (inline, full, collapsed or shortcut; text and destination) and autolinks.
// A link's text may wrap lines but not span a blank one. Reference
// definitions come before links so "[x]: #y" is taken whole.
var verbatimRe = regexp.MustCompile("(?ms)" +
	"^ {0,3}```.*?^ {0,3}```" + `|` +
	"^ {0,3}~~~.*?^ {0,3}~~~" + `|` +
	"`[^`\n]*`" + `|` +
	`^ {0,3}\[[^\]\n]+\]:[^\n]*` + `|` +
	`!?\[(?:[^\]\n]|\n[^\]\n])*\](?:\([^)\n]*\)|\[[^\]\n]*\])?` + `|` +
	`<[^>\n]*>`)

// LinkifyProjectsForView rewrites #project tags (outside the title line)
// into Markdown links with the project:// scheme, leaving tags inside code,
// links and other verbatim Markdown alone. Used by GUI clients.
func LinkifyProjectsForView(body string) string {
	i := titleEnd(body)
	rest := body[i:]
	verbatim := verbatimRe.FindAllStringIndex(rest, -1)
	var b strings.Builder
	b.WriteString(body[:i])
	last, v := 0, 0
	for _, m := range projectRe.FindAllStringSubmatchIndex(rest, -1) {
		hash, end := m[4]-1, m[1]
		// Both lists are in order, so one pass over the spans suffices.
		for v < len(verbatim) && verbatim[v][1] <= hash {
			v++
		}
		if v < len(verbatim) && verbatim[v][0] <= hash {
			continue
		}
		name := rest[m[4]:m[5]]
		b.WriteString(rest[last:hash])
		b.WriteString("[#" + name + "](project://" + strings.ToLower(name) + ")")
		last = end
	}
	b.WriteString(rest[last:])
	return b.String()
}

// LinkifyForView rewrites [[Title]] into a Markdown link with the note://
// scheme, ready for a Markdown renderer. Used by GUI clients.
func LinkifyForView(body string) string {
	return linkRe.ReplaceAllStringFunc(body, func(s string) string {
		t := strings.TrimSpace(s[2 : len(s)-2])
		return "[" + t + "](note://" + strings.ReplaceAll(t, " ", "%20") + ")"
	})
}
