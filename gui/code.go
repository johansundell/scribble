package gui

import (
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// copiedFor is how long a Copy button says "Copied" after a tap.
const copiedFor = 2 * time.Second

// afterFunc runs f on the UI thread after d, unless the returned stop is
// called first. Swappable for testing.
var afterFunc = func(d time.Duration, f func()) (stop func() bool) {
	return time.AfterFunc(d, func() { fyne.Do(f) }).Stop
}

// codeRole is how highlighted code shows one kind of token, as in the web
// app (#160): the color in GitHub Light and GitHub Dark, and the
// background behind it, if any. classes are Chroma's CSS classes for the
// tokens, as app.css lists them.
type codeRole struct {
	classes             string
	light, dark         string
	lightBack, darkBack string
	errorColor          bool // drawn in the theme's error color instead
}

var codeRoles = []codeRole{
	{classes: "k kc kd kn kp kr kt", light: "#cf222e", dark: "#ff7b72"},
	{classes: "no nl py ld m mb mf mh mi il mo o ow or", light: "#0550ae", dark: "#79c0ff"},
	{classes: "nf fm nb ni nd", light: "#6639ba", dark: "#d2a8ff"},
	{classes: "l s sa sb sc dl sd s2 se sh si sx sr s1 ss", light: "#0a3069", dark: "#a5d6ff"},
	{classes: "nv vc vg vi vm", light: "#953800", dark: "#79c0ff"},
	{classes: "nt", light: "#0550ae", dark: "#7ee787"},
	{classes: "nc ne", light: "#1f2328", dark: "#f0883e"},
	{classes: "c ch cm c1 cs cp cpf", light: "#57606a", dark: "#8b949e"},
	{classes: "gd", light: "#82071e", dark: "#ffa198", lightBack: "#ffebe9", darkBack: "#490202"},
	{classes: "gi", light: "#116329", dark: "#56d364", lightBack: "#dafbe1", darkBack: "#0f5323"},
	{classes: "err gr", errorColor: true},
}

// roleOf maps each token type that is not drawn in the text color to its
// role.
var roleOf = func() map[chroma.TokenType]*codeRole {
	byClass := map[string]chroma.TokenType{}
	for tt, class := range chroma.StandardTypes {
		byClass[class] = tt
	}
	m := map[chroma.TokenType]*codeRole{}
	for i := range codeRoles {
		for _, class := range strings.Fields(codeRoles[i].classes) {
			if tt, ok := byClass[class]; ok {
				m[tt] = &codeRoles[i]
			}
		}
	}
	return m
}()

// codeColors returns the color and background of tokens of type tt, nil
// for the text color and no background.
func codeColors(tt chroma.TokenType, dark bool, th fyne.Theme, v fyne.ThemeVariant) (fg, bg color.Color) {
	role := roleOf[tt]
	switch {
	case role == nil:
		return nil, nil
	case role.errorColor:
		return th.Color(theme.ColorNameError, v), nil
	}
	fgHex, bgHex := role.light, role.lightBack
	if dark {
		fgHex, bgHex = role.dark, role.darkBack
	}
	fg = hexColor(fgHex)
	if bgHex != "" {
		bg = hexColor(bgHex)
	}
	return fg, bg
}

// hexColor parses "#rrggbb".
func hexColor(s string) color.Color {
	c := chroma.MustParseColour(s)
	return color.NRGBA{R: c.Red(), G: c.Green(), B: c.Blue(), A: 0xff}
}

// codeBlock is a fenced or indented code block in the viewer. It replaces
// Fyne's CodeBlockSegment, which drops the language: its visual draws the
// code highlighted when Chroma knows the language, and a Copy button.
type codeBlock struct {
	Text    string
	Lang    string // the fence's language, "" if none
	quoting int    // how many blockquotes it is in
	copied  func() // called after the Copy button copied the code
}

func (c *codeBlock) Inline() bool              { return false }
func (c *codeBlock) Textual() string           { return c.Text }
func (c *codeBlock) Visual() fyne.CanvasObject { return newCodeView(c) }
func (c *codeBlock) Update(o fyne.CanvasObject) {
	v := o.(*codeView)
	if v.seg != c {
		v.seg = c
		v.resetCopy()
	}
	v.Refresh()
}
func (c *codeBlock) Select(_, _ fyne.Position) {}
func (c *codeBlock) SelectedText() string      { return c.Text }
func (c *codeBlock) Unselect()                 {}

// markdownExtensions are those Fyne's markdown parser uses, so a parse
// here finds the same code blocks it did.
var markdownExtensions = goldmark.WithExtensions(extension.Strikethrough, extension.TaskList, extension.Table)

// withCodeBlocks replaces the code blocks Fyne parsed from md in segs with
// codeBlocks that know their language, calling copied after a Copy.
func withCodeBlocks(md string, segs []widget.RichTextSegment, copied func()) []widget.RichTextSegment {
	// Fyne drops empty blocks and a block's last newline.
	var found []*codeBlock
	source := []byte(md)
	doc := goldmark.New(markdownExtensions).Parser().Parse(text.NewReader(source))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var lang string
		switch n := n.(type) {
		case *ast.FencedCodeBlock:
			lang = string(n.Language(source))
		case *ast.CodeBlock:
		default:
			return ast.WalkContinue, nil
		}
		var b strings.Builder
		for i := 0; i < n.Lines().Len(); i++ {
			line := n.Lines().At(i)
			b.Write(line.Value(source))
		}
		if b.Len() == 0 {
			return ast.WalkSkipChildren, nil
		}
		quoting := 0
		for p := n.Parent(); p != nil; p = p.Parent() {
			if p.Kind() == ast.KindBlockquote {
				quoting++
			}
		}
		found = append(found, &codeBlock{Text: strings.TrimSuffix(b.String(), "\n"), Lang: lang, quoting: quoting, copied: copied})
		return ast.WalkSkipChildren, nil
	})

	var replace func([]widget.RichTextSegment)
	replace = func(segs []widget.RichTextSegment) {
		for i, s := range segs {
			switch s := s.(type) {
			case *widget.CodeBlockSegment:
				// A block the two parses disagree on stays plain
				// without costing the ones after it their language.
				segs[i] = &codeBlock{Text: s.Text, copied: copied}
				for j, b := range found {
					if b.Text == s.Text {
						segs[i], found = b, found[j+1:]
						break
					}
				}
			case *widget.ListSegment:
				replace(s.Items)
			case *widget.ParagraphSegment:
				replace(s.Texts)
			}
		}
	}
	replace(segs)
	return segs
}

// codeView draws a codeBlock: its lines on a rounded, bordered panel, as
// Fyne draws a code block, and a Copy button in the top right corner that
// stays put when the code scrolls sideways.
type codeView struct {
	widget.BaseWidget
	seg *codeBlock

	copy   *widget.Button
	scroll *container.Scroll
	reset  func() bool // stops putting the button back to Copy, nil if none
}

func newCodeView(seg *codeBlock) *codeView {
	v := &codeView{seg: seg}
	v.copy = widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), v.copyCode)
	v.copy.Importance = widget.LowImportance
	v.ExtendBaseWidget(v)
	return v
}

// copyCode puts the code on the clipboard and says so for a while.
func (v *codeView) copyCode() {
	fyne.CurrentApp().Clipboard().SetContent(v.seg.Text)
	if v.seg.copied != nil {
		v.seg.copied()
	}
	v.resetCopy()
	v.copy.SetText("Copied")
	v.copy.SetIcon(theme.ConfirmIcon())
	v.reset = afterFunc(copiedFor, v.resetCopy)
}

// resetCopy puts the Copy button back to Copy.
func (v *codeView) resetCopy() {
	if v.reset != nil {
		v.reset()
		v.reset = nil
	}
	v.copy.SetText("Copy")
	v.copy.SetIcon(theme.ContentCopyIcon())
}

// indent is how far the panel is from the left of the view: as far as
// Fyne indents a code block in a blockquote.
func (v *codeView) indent() float32 {
	return v.Theme().Size(theme.SizeNameLineSpacing) * 4 * float32(v.seg.quoting)
}

// textOrigin is where the code's first line starts, in the view.
func (v *codeView) textOrigin() fyne.Position {
	x := v.indent()
	if v.scroll != nil {
		x -= v.scroll.Offset.X
	}
	return fyne.NewPos(x, 0)
}

func (v *codeView) CreateRenderer() fyne.WidgetRenderer {
	r := &codeViewRenderer{v: v, bg: canvas.NewRectangle(color.Transparent), lines: &fyne.Container{Layout: &fixedLayout{}}}
	v.scroll = container.NewHScroll(r.lines)
	r.Refresh()
	return r
}

type codeViewRenderer struct {
	v     *codeView
	bg    *canvas.Rectangle
	lines *fyne.Container // the code, drawn by draw
}

func (r *codeViewRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.v.scroll, r.v.copy}
}

func (r *codeViewRenderer) Destroy() {}

func (r *codeViewRenderer) Layout(s fyne.Size) {
	indent := r.v.indent()
	panel := fyne.NewSize(s.Width-indent, s.Height)
	r.bg.Move(fyne.NewPos(indent, 0))
	r.bg.Resize(panel)
	r.v.scroll.Move(fyne.NewPos(indent, 0))
	r.v.scroll.Resize(panel)
	inset := r.v.Theme().Size(theme.SizeNamePadding)
	b := r.v.copy.MinSize()
	r.v.copy.Resize(b)
	r.v.copy.Move(fyne.NewPos(s.Width-b.Width-inset, inset))
}

func (r *codeViewRenderer) MinSize() fyne.Size {
	inset := r.v.Theme().Size(theme.SizeNamePadding)
	b := r.v.copy.MinSize()
	code := r.lines.MinSize()
	return fyne.NewSize(r.v.indent()+b.Width+2*inset, max(code.Height, b.Height+2*inset))
}

func (r *codeViewRenderer) Refresh() {
	th, variant := r.v.Theme(), fyne.CurrentApp().Settings().ThemeVariant()
	r.bg.FillColor = th.Color(theme.ColorNameInputBackground, variant)
	r.bg.StrokeColor = th.Color(theme.ColorNameInputBorder, variant)
	r.bg.StrokeWidth = 1
	r.bg.CornerRadius = th.Size(theme.SizeNameInputRadius)
	r.draw(th, variant)
	r.bg.Refresh()
	r.v.copy.Refresh()
	r.v.scroll.Refresh()
}

// draw lays out the code's tokens as texts, inset by the inner padding,
// leaving room on the right to scroll the code out from under the button.
func (r *codeViewRenderer) draw(th fyne.Theme, variant fyne.ThemeVariant) {
	pad := th.Size(theme.SizeNameInnerPadding)
	size := th.Size(theme.SizeNameText)
	style := fyne.TextStyle{Monospace: true}
	height := codeLineHeight(th)
	dark := isDark(th.Color(theme.ColorNameBackground, variant))
	text := th.Color(theme.ColorNameForeground, variant)

	var objs []fyne.CanvasObject
	width := float32(0)
	lines := highlight(r.v.seg.Text, r.v.seg.Lang)
	for i, line := range lines {
		y := pad + float32(i)*height
		x := func(off int) float32 { return pad + fyne.MeasureText(line.text[:off], size, style).Width }
		for _, tok := range line.tokens {
			fg, bg := codeColors(tok.Type, dark, th, variant)
			if fg == nil {
				fg = text
			}
			if bg != nil {
				rect := canvas.NewRectangle(bg)
				rect.Move(fyne.NewPos(x(tok.start), y))
				rect.Resize(fyne.NewSize(x(tok.start+len(tok.Value))-x(tok.start), height))
				objs = append(objs, rect)
			}
			// Each text starts after a tab, not before one: a text puts
			// its tab stops from where it starts, not the line.
			off := tok.start
			for _, piece := range strings.SplitAfter(tok.Value, "\t") {
				if t := strings.TrimSuffix(piece, "\t"); t != "" {
					txt := canvas.NewText(t, fg)
					txt.TextSize, txt.TextStyle = size, style
					txt.Move(fyne.NewPos(x(off), y))
					txt.Resize(fyne.MeasureText(t, size, style))
					objs = append(objs, txt)
				}
				off += len(piece)
			}
		}
		width = max(width, x(len(line.text)))
	}
	inset := th.Size(theme.SizeNamePadding)
	r.lines.Objects = objs
	r.lines.Layout.(*fixedLayout).size = fyne.NewSize(width+pad+r.v.copy.MinSize().Width+inset,
		2*pad+float32(len(lines))*height)
	r.lines.Refresh()
}

// codeLineHeight is the height of a line of code.
func codeLineHeight(th fyne.Theme) float32 {
	return fyne.MeasureText("M", th.Size(theme.SizeNameText), fyne.TextStyle{Monospace: true}).Height
}

// isDark reports whether c, a theme's background, is dark.
func isDark(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return 299*r+587*g+114*b < 500*0xffff
}

// codeToken is a token of a line of code, starting at byte start.
type codeToken struct {
	chroma.Token
	start int
}

// codeLine is a line of code and its tokens.
type codeLine struct {
	text   string
	tokens []codeToken
}

// highlight splits code into lines of tokens, typed by Chroma's lexer for
// lang; a line is a single text token when Chroma does not know lang.
func highlight(code, lang string) []codeLine {
	var lines []codeLine
	plain := func() []codeLine {
		lines = lines[:0]
		for _, l := range strings.Split(code, "\n") {
			lines = append(lines, codeLine{text: l, tokens: []codeToken{{Token: chroma.Token{Type: chroma.Text, Value: l}}}})
		}
		return lines
	}
	lexer := lexers.Get(lang)
	if lang == "" || lexer == nil {
		return plain()
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return plain()
	}
	line := codeLine{}
	for _, tok := range it.Tokens() {
		for {
			before, after, found := strings.Cut(tok.Value, "\n")
			if before != "" {
				line.tokens = append(line.tokens, codeToken{Token: chroma.Token{Type: tok.Type, Value: before}, start: len(line.text)})
				line.text += before
			}
			if !found {
				break
			}
			lines = append(lines, line)
			line = codeLine{}
			tok.Value = after
		}
	}
	lines = append(lines, line)
	// Chroma ends the code with a newline it adds.
	if n := len(strings.Split(code, "\n")); len(lines) > n {
		lines = lines[:n]
	}
	return lines
}

// fixedLayout leaves its objects where they were put, at a set size.
type fixedLayout struct{ size fyne.Size }

func (l *fixedLayout) Layout([]fyne.CanvasObject, fyne.Size) {}
func (l *fixedLayout) MinSize([]fyne.CanvasObject) fyne.Size { return l.size }
