// Package notes holds the Note type and the [[wiki-link]] and #project
// helpers shared by the server, the CLI and the GUI client. It has no
// dependencies on the rest of the module so clients stay small.
package notes

import (
	"net/url"
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
// definitions (those never become text nodes, or are skipped here).
func plainTextSpans(body string) [][2]int {
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
			if _, ok := n.FirstChild().(*east.TaskCheckBox); ok {
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
	spans := plainTextSpans(body)
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

// addressRe matches bare URLs and email addresses. Markdown renders them as
// plain text, but a title inside one must not be linked.
var addressRe = regexp.MustCompile(`(?i)\b(?:[a-z][a-z0-9+.-]*://|www\.)\S+|[\w.+-]+@[\w-]+(?:\.[\w-]+)+`)

// AutoLink wraps bare mentions of titles in body as [[links]] and reports
// how many it added. A link keeps the mention's spelling unless that would
// not resolve (see mention.target). Matching is case-insensitive and on
// word boundaries; where mentions overlap, the longest wins. The title line, the note's own title, existing [[links]],
// bracketed text, URLs, email addresses, #project tags, code, Markdown links
// and task-list items are left alone.
func AutoLink(body string, titles []string) (string, int) {
	self := TitleOf(body)
	var candidates []string
	for _, t := range titles {
		t = strings.TrimSpace(t)
		// A title linkRe cannot hold would give a broken link.
		if t != "" && !strings.ContainsAny(t, "[]\n") && !strings.EqualFold(t, self) {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		return body, 0
	}

	var mentions []mention
	start := titleEnd(body)
	for _, sp := range mergeSpans(plainTextSpans(body)) {
		for i := max(sp[0], start); i < sp[1]; {
			for _, t := range candidates {
				if size := mentionAt(body, i, sp[1], t); size > 0 {
					text := body[i : i+size]
					mentions = append(mentions, mention{
						start: i, end: i + size, runes: utf8.RuneCountInString(text),
						title: t, asWritten: equalFoldASCII(text, t),
					})
				}
			}
			_, size := utf8.DecodeRuneInString(body[i:])
			i += size
		}
	}
	// Longest first, then earliest, then a title the mention spells (so of
	// "École" and "école", "école" links "école"); a mention survives if
	// none of its bytes are taken yet.
	slices.SortFunc(mentions, func(a, b mention) int {
		if a.runes != b.runes {
			return b.runes - a.runes
		}
		if a.start != b.start {
			return a.start - b.start
		}
		switch {
		case a.asWritten && !b.asWritten:
			return -1
		case b.asWritten && !a.asWritten:
			return 1
		}
		return 0
	})
	taken := protectedBytes(body)
	var linked []mention
	for _, m := range mentions {
		if !slices.Contains(taken[m.start:m.end], true) {
			for i := m.start; i < m.end; i++ {
				taken[i] = true
			}
			linked = append(linked, m)
		}
	}
	slices.SortFunc(linked, func(a, b mention) int { return a.start - b.start })

	var b strings.Builder
	last := 0
	for _, m := range linked {
		b.WriteString(body[last:m.start])
		b.WriteString("[[" + m.target(body) + "]]")
		last = m.end
	}
	b.WriteString(body[last:])
	return b.String(), len(linked)
}

// mention is a match of title at body[start:end], runes characters long.
// asWritten is set when the text differs from title only in ASCII case,
// which is all the store folds when it resolves a link.
type mention struct {
	start, end, runes int
	title             string
	asWritten         bool
}

// target is the link text for m: the mention's own spelling when that
// resolves to the title, otherwise the title itself.
func (m mention) target(body string) string {
	if m.asWritten {
		return body[m.start:m.end]
	}
	return m.title
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		if lowerASCII(a[i]) != lowerASCII(b[i]) {
			return false
		}
	}
	return true
}

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// bracketRe matches [bracketed text] on one line: possibly a Markdown link
// waiting for a reference definition, and not a place for a [[link]].
var bracketRe = regexp.MustCompile(`\[[^\[\]\n]*\]`)

// protectedBytes marks the bytes of body a mention must not overlap:
// [[links]], bracketed text, URLs, email addresses and #project tags.
func protectedBytes(body string) []bool {
	taken := make([]bool, len(body))
	mark := func(start, end int) {
		for i := start; i < end; i++ {
			taken[i] = true
		}
	}
	for _, re := range []*regexp.Regexp{linkRe, bracketRe, addressRe} {
		for _, m := range re.FindAllStringIndex(body, -1) {
			mark(m[0], m[1])
		}
	}
	for _, m := range projectRe.FindAllStringSubmatchIndex(body, -1) {
		mark(m[4]-1, m[1])
	}
	return taken
}

// mentionAt returns the byte length of the mention of title at body[i:],
// ending by end, or 0 if there is none. Case is folded rune by rune, so
// "STRAẞE" matches "Straße" although their byte lengths differ.
func mentionAt(body string, i, end int, title string) int {
	j := i
	for _, r := range title {
		if j >= end {
			return 0
		}
		got, size := utf8.DecodeRuneInString(body[j:end])
		if !foldEqual(r, got) {
			return 0
		}
		j += size
	}
	first, _ := utf8.DecodeRuneInString(title)
	lastRune, _ := utf8.DecodeLastRuneInString(title)
	before, _ := utf8.DecodeLastRuneInString(body[:i])
	after, _ := utf8.DecodeRuneInString(body[j:])
	if (isWordRune(first) && isWordRune(before)) || (isWordRune(lastRune) && isWordRune(after)) {
		return 0
	}
	return j - i
}

// foldEqual reports whether a and b are the same letter under Unicode
// simple case folding.
func foldEqual(a, b rune) bool {
	for r := a; ; {
		if r == b {
			return true
		}
		if r = unicode.SimpleFold(r); r == a {
			return false
		}
	}
}

// isWordRune reports whether r can be part of a word. Combining marks
// count, so "Göteborg" (a decomposed "Göteborg") is one word.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
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

// LinkifyForView rewrites [[Title]] into a Markdown link with the note:///
// scheme, ready for a Markdown renderer; LinkTitle reads the title back.
// Only links in plain text are rewritten, so code and Markdown links stay
// intact. Used by GUI clients.
func LinkifyForView(body string) string {
	spans := mergeSpans(plainTextSpans(body))
	var b strings.Builder
	last, s := 0, 0
	for _, m := range linkRe.FindAllStringSubmatchIndex(body, -1) {
		for s < len(spans) && spans[s][1] <= m[0] {
			s++
		}
		if s == len(spans) || m[0] < spans[s][0] || m[1] > spans[s][1] {
			continue
		}
		t := strings.TrimSpace(body[m[2]:m[3]])
		b.WriteString(body[last:m[0]])
		b.WriteString("[" + t + "](note:///" + url.PathEscape(t) + ")")
		last = m[1]
	}
	b.WriteString(body[last:])
	return b.String()
}

// LinkTitle returns the note title a link made by LinkifyForView points
// at, and false for any other link.
func LinkTitle(u *url.URL) (string, bool) {
	if u == nil || u.Scheme != "note" || u.Host != "" || !strings.HasPrefix(u.Path, "/") {
		return "", false
	}
	return u.Path[1:], true
}
