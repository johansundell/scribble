// Package notes holds the Note type and the [[wiki-link]] and #project
// helpers shared by the server, the CLI and the GUI client. It has no
// dependencies on the rest of the module so clients stay small.
package notes

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
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

// TitleLine returns the line TitleOf takes the title from, trimmed but with
// its '#' marks, so "#tag" and "# tag" can be told apart.
func TitleLine(body string) string {
	i := titleEnd(body)
	return strings.TrimSpace(body[strings.LastIndex(body[:i], "\n")+1 : i])
}

// markdown parses like Fyne's RichText.ParseMarkdown, so the GUI renders the
// same structure LinkifyProjectsForView sees.
var markdown = goldmark.New(goldmark.WithExtensions(extension.Strikethrough, extension.TaskList, extension.Table))

// plainTextSpans returns the byte ranges of body that Markdown renders as
// plain text: not code, HTML, links, images, link destinations or reference
// definitions (those never become text nodes, or are skipped here). With
// forView, task-list items are skipped too, for Fyne's sake.
func plainTextSpans(body string, forView bool) [][2]int {
	var spans [][2]int
	doc := markdown.Parser().Parse(text.NewReader([]byte(body)))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Link, *ast.Image, *ast.AutoLink, *ast.CodeSpan, *ast.RawHTML:
			return ast.WalkSkipChildren, nil
		case *ast.TextBlock, *ast.Paragraph:
			// Fyne (v2.8) renders a task item from the single text node
			// after its checkbox: a link there panics it or drops the rest.
			if _, ok := n.FirstChild().(*east.TaskCheckBox); ok && forView {
				return ast.WalkSkipChildren, nil
			}
		case *ast.Text:
			spans = append(spans, [2]int{n.Segment.Start, n.Segment.Stop})
		}
		return ast.WalkContinue, nil
	})
	slices.SortFunc(spans, func(a, b [2]int) int { return a[0] - b[0] })
	return spans
}

// LinkifyProjectsForView rewrites #project tags (outside the title line)
// into Markdown links with the project:// scheme. Only tags in plain text
// are rewritten, so code, links and reference definitions stay intact.
// Used by GUI clients.
func LinkifyProjectsForView(body string) string {
	start := titleEnd(body)
	spans := plainTextSpans(body, true)
	var b strings.Builder
	last, s := 0, 0
	for _, m := range projectRe.FindAllStringSubmatchIndex(body, -1) {
		hash, end := m[4]-1, m[1]
		// Tags and spans are both in order, so one pass over the spans
		// suffices.
		for s < len(spans) && spans[s][1] <= hash {
			s++
		}
		// The whole tag must lie in one span: in "_see #home_" the regex
		// takes the closing '_' as well, and rewriting would eat it.
		if hash < start || s == len(spans) || hash < spans[s][0] || end > spans[s][1] {
			continue
		}
		name := body[m[4]:m[5]]
		b.WriteString(body[last:hash])
		b.WriteString("[#" + name + "](project://" + strings.ToLower(name) + ")")
		last = end
	}
	b.WriteString(body[last:])
	return b.String()
}

// urlRe matches bare URLs. Markdown renders them as plain text, but a title
// inside one must not be linked.
var urlRe = regexp.MustCompile(`(?i)\b(?:[a-z][a-z0-9+.-]*://|www\.)\S+`)

// AutoLink wraps bare mentions of titles in body as [[links]], keeping the
// mention's spelling. Matching is case-insensitive and on word boundaries,
// and a longer title wins over a shorter one it starts with. The title
// line, the note's own title, existing [[links]], URLs, code and Markdown
// links are left alone.
func AutoLink(body string, titles []string) string {
	self := TitleOf(body)
	var ts []string
	for _, t := range titles {
		if t = strings.TrimSpace(t); t != "" && !strings.EqualFold(t, self) {
			ts = append(ts, t)
		}
	}
	if len(ts) == 0 {
		return body
	}
	slices.SortFunc(ts, func(a, b string) int { return len(b) - len(a) })
	skip := append(linkRe.FindAllStringIndex(body, -1), urlRe.FindAllStringIndex(body, -1)...)
	start := titleEnd(body)

	var b strings.Builder
	last := 0
	for _, sp := range mergeSpans(plainTextSpans(body, false)) {
		for i := max(sp[0], start); i < sp[1]; {
			n := mentionAt(body, i, sp[1], ts)
			if n == 0 || overlapsAny(skip, i, i+n) {
				_, size := utf8.DecodeRuneInString(body[i:])
				i += size
				continue
			}
			b.WriteString(body[last:i])
			b.WriteString("[[" + body[i:i+n] + "]]")
			i += n
			last = i
		}
	}
	b.WriteString(body[last:])
	return b.String()
}

// mentionAt returns the length of the longest title (ts is sorted longest
// first) that body mentions at i, ending by end, or 0.
func mentionAt(body string, i, end int, ts []string) int {
	for _, t := range ts {
		j := i + len(t)
		if j > end || !strings.EqualFold(body[i:j], t) {
			continue
		}
		first, _ := utf8.DecodeRuneInString(t)
		lastR, _ := utf8.DecodeLastRuneInString(t)
		before, _ := utf8.DecodeLastRuneInString(body[:i])
		after, _ := utf8.DecodeRuneInString(body[j:])
		if (isWordRune(first) && isWordRune(before)) || (isWordRune(lastR) && isWordRune(after)) {
			continue
		}
		return len(t)
	}
	return 0
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// mergeSpans joins sorted spans that touch, since Markdown splits one run
// of text into several nodes at characters such as '['.
func mergeSpans(spans [][2]int) [][2]int {
	var out [][2]int
	for _, s := range spans {
		if len(out) > 0 && out[len(out)-1][1] == s[0] {
			out[len(out)-1][1] = s[1]
			continue
		}
		out = append(out, s)
	}
	return out
}

func overlapsAny(ranges [][]int, start, end int) bool {
	for _, r := range ranges {
		if start < r[1] && r[0] < end {
			return true
		}
	}
	return false
}

// LinkifyForView rewrites [[Title]] into a Markdown link with the note://
// scheme, ready for a Markdown renderer. Used by GUI clients.
func LinkifyForView(body string) string {
	return linkRe.ReplaceAllStringFunc(body, func(s string) string {
		t := strings.TrimSpace(s[2 : len(s)-2])
		return "[" + t + "](note://" + strings.ReplaceAll(t, " ", "%20") + ")"
	})
}
