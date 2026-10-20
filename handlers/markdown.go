package handlers

import (
	"bytes"
	"html"
	"html/template"
	"net/url"
	"strings"

	"github.com/johansundell/scribble/notes"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// webMarkdown renders notes for the web app: GFM, with raw HTML shown as
// text (never passed through) by escapedHTML.
var webMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(escapedHTML{}, 100))),
)

// renderNote renders body as HTML. [[links]] point at /title/<title>,
// with class "missing" when exists says there is no such note, and #project
// tags at that project's list. Links to anything but http(s) and mailto are
// dropped, keeping their text.
func renderNote(body string, exists func(title string) bool) (template.HTML, error) {
	return renderMarkdown(body, exists, func(path string) string { return path })
}

// renderSharedNote renders body for a share link's public page (#83).
// [[links]] and #project tags go through the PIN screen, and a [[link]] is
// never marked missing: that would tell the reader which notes exist.
func renderSharedNote(body string) (template.HTML, error) {
	return renderMarkdown(body, func(string) bool { return true }, LoginTo)
}

// renderMarkdown renders body, sending [[links]] and #project tags to
// to(their web app path).
func renderMarkdown(body string, exists func(title string) bool, to func(path string) string) (template.HTML, error) {
	source := []byte(notes.LinkifyProjectsForView(notes.LinkifyForView(body)))
	doc := webMarkdown.Parser().Parse(text.NewReader(source))
	fixLinks(doc, source, exists, to)
	var buf bytes.Buffer
	if err := webMarkdown.Renderer().Render(&buf, source, doc); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

// linkSchemes are the URL schemes a link in a note may use.
var linkSchemes = []string{"http", "https", "mailto"}

// fixLinks points [[links]] and #project tags at to(their web app path)
// and drops unsafe links.
func fixLinks(doc ast.Node, source []byte, exists func(string) bool, to func(string) string) {
	var drop []ast.Node
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Link:
			u, err := url.Parse(string(n.Destination))
			if title, ok := notes.LinkTitle(u); ok && err == nil {
				n.Destination = []byte(to(TitlePath(title)))
				if !exists(title) {
					n.SetAttributeString("class", []byte("missing"))
				}
			} else if err == nil && u.Scheme == "project" && projectTag(u.Host) != "" {
				n.Destination = []byte(to(ProjectPath(u.Host)))
			} else if !allowedURL(string(n.Destination), linkSchemes...) {
				drop = append(drop, n)
			}
		case *ast.AutoLink:
			if n.AutoLinkType == ast.AutoLinkURL && !allowedURL(string(n.URL(source)), linkSchemes...) {
				drop = append(drop, n)
			}
		case *ast.Image:
			if !allowedURL(string(n.Destination), "http", "https") {
				drop = append(drop, n)
			}
		}
		return ast.WalkContinue, nil
	})
	for _, n := range drop {
		unwrap(n, source)
	}
}

// unwrap replaces a link or image with its text.
func unwrap(n ast.Node, source []byte) {
	parent := n.Parent()
	if a, ok := n.(*ast.AutoLink); ok {
		parent.ReplaceChild(parent, n, ast.NewString(a.Label(source)))
		return
	}
	for c := n.FirstChild(); c != nil; {
		next := c.NextSibling()
		parent.InsertBefore(parent, n, c)
		c = next
	}
	parent.RemoveChild(parent, n)
}

// allowedURL reports whether raw is an absolute URL with one of schemes.
func allowedURL(raw string, schemes ...string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	for _, s := range schemes {
		if u.Scheme == s {
			return true
		}
	}
	return false
}

// escapedHTML renders raw HTML in a note as the text it is, so a note can
// never run script in the page.
type escapedHTML struct{}

func (escapedHTML) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHTMLBlock, renderHTMLBlock)
	reg.Register(ast.KindRawHTML, renderRawHTML)
}

func renderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.HTMLBlock)
	_, _ = w.WriteString(`<p class="raw-html">`)
	lines := n.Lines()
	for i := range lines.Len() {
		line := lines.At(i)
		_, _ = w.WriteString(html.EscapeString(string(line.Value(source))))
	}
	if n.HasClosure() {
		_, _ = w.WriteString(html.EscapeString(string(n.ClosureLine.Value(source))))
	}
	_, _ = w.WriteString("</p>\n")
	return ast.WalkContinue, nil
}

func renderRawHTML(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	n := node.(*ast.RawHTML)
	for i := range n.Segments.Len() {
		seg := n.Segments.At(i)
		_, _ = w.WriteString(html.EscapeString(string(seg.Value(source))))
	}
	return ast.WalkSkipChildren, nil
}
