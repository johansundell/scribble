package gui

import (
	"strings"
	"time"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// A third click this soon after a double-click, and this close to it,
// selects the whole line.
const (
	tripleClickDelay    = 500 * time.Millisecond
	tripleClickDistance = 4
)

// Viewer shows the open note rendered as Markdown. Fyne's RichText cannot
// select text, so Viewer adds that on top: it reads where RichText drew each
// piece of text and maps the mouse to it. A click on a link still goes to
// the link.
type Viewer struct {
	widget.RichText
	ui *UI

	r *viewerRenderer
	// The selection runs from anchor to caret, as offsets into the text
	// of the runs from the last layout; it is empty when they are equal.
	anchor, caret int
	dragging      bool
	doubleAt      time.Time     // when the last double-click selected a word
	doublePos     fyne.Position // and where
}

func newViewer(ui *UI) *Viewer {
	v := &Viewer{ui: ui}
	v.Wrapping = fyne.TextWrapWord
	v.Scroll = fyne.ScrollNone // as NewRichText does; the window scrolls it
	v.ExtendBaseWidget(v)
	return v
}

// CreateRenderer wraps RichText's renderer with the selection highlight.
func (v *Viewer) CreateRenderer() fyne.WidgetRenderer {
	v.r = &viewerRenderer{v: v}
	v.r.inner = v.RichText.CreateRenderer()
	v.r.update()
	return v.r
}

// Refresh redraws the viewer after its Segments changed, dropping the
// selection.
func (v *Viewer) Refresh() {
	v.anchor, v.caret = 0, 0
	v.RichText.Refresh()
}

// SelectedText returns the selected text, "" if there is none.
func (v *Viewer) SelectedText() string {
	if v.r == nil {
		return ""
	}
	a, b := v.selection()
	return string(v.r.text[a:b])
}

// selection returns the selection's bounds in order, clamped to the text.
func (v *Viewer) selection() (int, int) {
	a, b := min(v.anchor, v.caret), max(v.anchor, v.caret)
	n := len(v.r.text)
	return min(a, n), min(b, n)
}

func (v *Viewer) selectRange(a, b int) {
	v.anchor, v.caret = a, b
	if v.r != nil {
		v.r.highlight()
	}
}

// selectAll selects all the text.
func (v *Viewer) selectAll() {
	if v.r != nil {
		v.selectRange(0, len(v.r.text))
	}
}

// expand selects the stretch of text around the offset at p whose runes
// all satisfy in, or the rune at p alone if it does not.
func (v *Viewer) expand(p fyne.Position, in func(rune) bool) {
	if v.r == nil {
		return
	}
	text := v.r.text
	i := v.r.runeAt(p)
	if i >= len(text) {
		return
	}
	if !in(text[i]) {
		v.selectRange(i, i+1)
		return
	}
	a, b := i, i
	for a > 0 && in(text[a-1]) {
		a--
	}
	for b < len(text) && in(text[b]) {
		b++
	}
	v.selectRange(a, b)
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func (v *Viewer) copySelection(clip fyne.Clipboard) {
	if text := v.SelectedText(); text != "" {
		if clip == nil {
			clip = fyne.CurrentApp().Clipboard()
		}
		clip.SetContent(text)
	}
}

// MouseDown is called when a mouse button is pressed over the viewer. A
// click drops the selection; a click right after a double-click selects
// the line.
func (v *Viewer) MouseDown(e *desktop.MouseEvent) {
	if e.Button != desktop.MouseButtonPrimary {
		return // keep the selection for the context menu
	}
	v.ui.w.Canvas().Focus(v)
	d := e.Position.Subtract(v.doublePos)
	if time.Since(v.doubleAt) < tripleClickDelay && max(d.X, -d.X, d.Y, -d.Y) <= tripleClickDistance {
		v.doubleAt = time.Time{}
		v.expand(e.Position, func(r rune) bool { return r != '\n' })
		return
	}
	if v.r != nil {
		i := v.r.offsetAt(e.Position)
		v.selectRange(i, i)
	}
}

// MouseUp is called when a mouse button is released over the viewer.
func (v *Viewer) MouseUp(*desktop.MouseEvent) {}

// Dragged selects from where the drag started to where the mouse is now.
// A drag that starts on a link reaches the viewer without a MouseDown.
func (v *Viewer) Dragged(e *fyne.DragEvent) {
	if v.r == nil {
		return
	}
	if !v.dragging {
		v.dragging = true
		v.anchor = v.r.offsetAt(e.Position.Subtract(e.Dragged))
	}
	v.selectRange(v.anchor, v.r.offsetAt(e.Position))
}

// DragEnd is called when the drag ends.
func (v *Viewer) DragEnd() { v.dragging = false }

// DoubleTapped selects the word under the mouse.
func (v *Viewer) DoubleTapped(e *fyne.PointEvent) {
	v.expand(e.Position, isWordRune)
	v.doubleAt, v.doublePos = time.Now(), e.Position
}

// TappedSecondary shows the context menu.
func (v *Viewer) TappedSecondary(e *fyne.PointEvent) {
	cp := fyne.NewMenuItem("Copy", func() { v.copySelection(nil) })
	cp.Disabled = v.SelectedText() == ""
	c := fyne.CurrentApp().Driver().CanvasForObject(v)
	widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", cp), c, e.AbsolutePosition)
}

// Cursor shows the text cursor over the viewer.
func (v *Viewer) Cursor() desktop.Cursor { return desktop.TextCursor }

// FocusGained is called when the viewer gains focus. A clicked Edit menu
// item then acts on it.
func (v *Viewer) FocusGained() { v.ui.lastFocus = v }

// FocusLost is called when the viewer loses focus.
func (v *Viewer) FocusLost() {}

// TypedRune is called when a character is typed while the viewer has focus.
func (v *Viewer) TypedRune(rune) {}

// TypedKey is called when a key is pressed while the viewer has focus; F2
// still renames the note.
func (v *Viewer) TypedKey(e *fyne.KeyEvent) { v.ui.typedKey(e) }

// TypedShortcut copies the selection or selects all; the viewer has nothing
// to undo, cut or paste.
func (v *Viewer) TypedShortcut(s fyne.Shortcut) {
	switch s := s.(type) {
	case *fyne.ShortcutCopy:
		v.copySelection(s.Clipboard)
	case *fyne.ShortcutSelectAll:
		v.selectAll()
	}
}

// textRun is a piece of text drawn on one line, at a known place in the
// viewer.
type textRun struct {
	text   []rune
	sep    string        // the text between the run before and this one
	start  int           // offset of text[0] in viewerRenderer.text
	pos    fyne.Position // top left of the first rune, in the viewer
	height float32
	size   float32
	style  fyne.TextStyle
	over   bool // drawn on an opaque background: highlight it from in front
}

// x returns where rune i of the run starts.
func (r *textRun) x(i int) float32 {
	return r.pos.X + fyne.MeasureText(string(r.text[:i]), r.size, r.style).Width
}

// index returns the offset in the run nearest to x.
func (r *textRun) index(x float32) int {
	prev := r.pos.X
	for i := range r.text {
		next := r.x(i + 1)
		if x < (prev+next)/2 {
			return i
		}
		prev = next
	}
	return len(r.text)
}

type viewerRenderer struct {
	v     *Viewer
	inner fyne.WidgetRenderer // RichText's own

	runs  []textRun
	text  []rune              // the runs' text, joined
	under []*canvas.Rectangle // highlight behind the text, the spare ones hidden
	over  []*canvas.Rectangle // highlight in front of text on its own background
}

func (r *viewerRenderer) Layout(s fyne.Size) {
	r.inner.Layout(s)
	r.update()
}

func (r *viewerRenderer) MinSize() fyne.Size { return r.inner.MinSize() }

func (r *viewerRenderer) Refresh() {
	r.inner.Refresh()
	r.update()
}

func (r *viewerRenderer) Destroy() { r.inner.Destroy() }

// Objects is the selection highlight behind the text, the text, and the
// highlight over text on its own background.
func (r *viewerRenderer) Objects() []fyne.CanvasObject {
	inner := r.inner.Objects()
	objs := make([]fyne.CanvasObject, 0, len(r.under)+len(inner)+len(r.over))
	for _, o := range r.under {
		objs = append(objs, o)
	}
	objs = append(objs, inner...)
	for _, o := range r.over {
		objs = append(objs, o)
	}
	return objs
}

// update finds the text RichText drew and highlights the selection in it.
func (r *viewerRenderer) update() {
	c := &runCollector{v: r.v, leaves: leaves(r.v.Segments)}
	for _, o := range r.inner.Objects() {
		c.object(o)
	}
	r.runs = c.runs
	r.text = r.text[:0]
	for i := range r.runs {
		run := &r.runs[i]
		if i > 0 {
			r.text = append(r.text, []rune(run.sep)...)
		}
		run.start = len(r.text)
		r.text = append(r.text, run.text...)
	}
	r.highlight()
}

// highlight draws the selection.
func (r *viewerRenderer) highlight() {
	a, b := r.v.selection()
	col := theme.ColorForWidget(theme.ColorNameSelection, r.v)
	under, over := 0, 0
	for i := range r.runs {
		run := &r.runs[i]
		s := min(max(a-run.start, 0), len(run.text))
		e := min(max(b-run.start, 0), len(run.text))
		if s >= e {
			continue
		}
		var rect *canvas.Rectangle
		if run.over {
			if over == len(r.over) {
				r.over = append(r.over, canvas.NewRectangle(col))
			}
			rect = r.over[over]
			over++
		} else {
			if under == len(r.under) {
				r.under = append(r.under, canvas.NewRectangle(col))
			}
			rect = r.under[under]
			under++
		}
		x0, x1 := run.x(s), run.x(e)
		rect.FillColor = col
		rect.Move(fyne.NewPos(x0, run.pos.Y))
		rect.Resize(fyne.NewSize(x1-x0, run.height))
		rect.Show()
		rect.Refresh()
	}
	for _, rect := range r.under[under:] {
		rect.Hide()
	}
	for _, rect := range r.over[over:] {
		rect.Hide()
	}
	canvas.Refresh(r.v)
}

// offsetAt returns the offset into the text nearest to p: where the caret
// would go.
func (r *viewerRenderer) offsetAt(p fyne.Position) int {
	run := r.runNear(p)
	if run == nil {
		return 0
	}
	return run.start + run.index(p.X)
}

// runeAt returns the offset of the rune under p, or the nearest one.
func (r *viewerRenderer) runeAt(p fyne.Position) int {
	run := r.runNear(p)
	if run == nil {
		return 0
	}
	i := run.index(p.X)
	if i > 0 && (i == len(run.text) || p.X < run.x(i)) {
		i--
	}
	return run.start + i
}

// runNear returns the run nearest to p, nil if there are none.
func (r *viewerRenderer) runNear(p fyne.Position) *textRun {
	if len(r.runs) == 0 {
		return nil
	}
	best, bestDY, bestDX := 0, float32(-1), float32(0)
	for i := range r.runs {
		run := &r.runs[i]
		dy := distance(p.Y, run.pos.Y, run.pos.Y+run.height)
		dx := distance(p.X, run.pos.X, run.x(len(run.text)))
		if bestDY < 0 || dy < bestDY || dy == bestDY && dx < bestDX {
			best, bestDY, bestDX = i, dy, dx
		}
	}
	return &r.runs[best]
}

// distance is how far v is outside [lo, hi].
func distance(v, lo, hi float32) float32 {
	switch {
	case v < lo:
		return lo - v
	case v > hi:
		return v - hi
	}
	return 0
}

// leaf is a segment RichText draws, and whether it starts a new row.
type leaf struct {
	seg    widget.RichTextSegment
	newRow bool
}

// leaves returns the segments RichText draws, in the order it draws them,
// with lists and paragraphs opened up the way RichText does.
func leaves(segs []widget.RichTextSegment) []leaf {
	var out []leaf
	newRow := true
	var walk func([]widget.RichTextSegment)
	walk = func(segs []widget.RichTextSegment) {
		for _, s := range segs {
			if b, ok := s.(widget.RichTextBlock); ok {
				children := b.Segments()
				walk(children)
				if len(children) > 0 && !children[len(children)-1].Inline() {
					newRow = true
				}
				continue
			}
			out = append(out, leaf{s, newRow})
			newRow = !s.Inline()
		}
	}
	walk(segs)
	return out
}

// runCollector matches the objects RichText laid out to the segments they
// draw and records their text. A segment that wraps is drawn by an object
// per line. Fyne gives no access to the renderers of widgets among the
// objects, so their text is placed the way those widgets draw it.
type runCollector struct {
	v      *Viewer
	leaves []leaf // still to be matched
	runs   []textRun

	text  []rune  // the text of the text or link segment being matched, nil if none
	shown int     // how much of text the objects so far showed
	lastY float32 // where the last of those objects is
}

// nextLeaf moves on to the next segment and returns it with the text that
// separates it from the one before.
func (c *runCollector) nextLeaf() (widget.RichTextSegment, string, bool) {
	c.text = nil
	if len(c.leaves) == 0 {
		return nil, "", false
	}
	l := c.leaves[0]
	c.leaves = c.leaves[1:]
	if l.newRow && len(c.runs) > 0 {
		return l.seg, "\n", true
	}
	return l.seg, "", true
}

// object records the text drawn by o, an object RichText laid out.
func (c *runCollector) object(o fyne.CanvasObject) {
	if run, ok := c.textRun(o); ok {
		if c.text != nil && run.pos.Y > c.lastY+run.height/2 {
			if k, ok := continues(c.text[c.shown:], run.text); ok {
				// The next line of a wrapped segment: wrapping drops the
				// space it breaks at, and a newline starts a new line.
				run.sep = string(c.text[c.shown : c.shown+k])
				c.shown += k + len(run.text)
				c.lastY = run.pos.Y
				c.runs = append(c.runs, run)
				return
			}
		}
		seg, sep, ok := c.nextLeaf()
		if !ok {
			return
		}
		run.sep = sep
		c.text, c.shown, c.lastY = []rune(seg.Textual()), len(run.text), run.pos.Y
		c.runs = append(c.runs, run)
		return
	}
	seg, sep, ok := c.nextLeaf()
	if !ok {
		return
	}
	th := c.v.Theme()
	switch seg := seg.(type) {
	case *widget.CheckBoxSegment:
		box, ok := o.(*fyne.Container)
		if !ok || len(box.Objects) == 0 {
			return
		}
		check := box.Objects[0]
		// The label follows the box and its focus ring.
		x := th.Size(theme.SizeNameInlineIcon) + th.Size(theme.SizeNameInnerPadding) + th.Size(theme.SizeNameInputBorder)*2
		c.runs = append(c.runs, textRun{text: []rune(seg.Text), sep: sep, pos: o.Position().Add(check.Position()).AddXY(x, 0),
			height: check.Size().Height, size: th.Size(theme.SizeNameText)})
	case *widget.CodeBlockSegment:
		c.codeRuns(seg.Text, o.Position(), sep)
	case *widget.TableSegment:
		c.tableRuns(o, seg, sep)
	}
}

// continues reports whether next, the text of an object, continues text,
// the rest of a segment's text, and how much of text comes first.
func continues(text, next []rune) (int, bool) {
	hasPrefix := func(s []rune) bool { return len(s) >= len(next) && string(s[:len(next)]) == string(next) }
	switch {
	case len(next) > 0 && hasPrefix(text):
		return 0, true
	case len(text) > 0 && unicode.IsSpace(text[0]) && hasPrefix(text[1:]):
		return 1, true
	}
	return 0, false
}

// textRun returns the run for o if it draws a line of a text or link
// segment.
func (c *runCollector) textRun(o fyne.CanvasObject) (textRun, bool) {
	pos := o.Position()
	switch o := o.(type) {
	case *canvas.Text:
		return canvasTextRun(o, pos), true
	case *fyne.Container:
		if len(o.Objects) == 0 {
			return textRun{}, false
		}
		switch inner := o.Objects[len(o.Objects)-1].(type) {
		case *canvas.Text: // inline code, over its background
			return canvasTextRun(inner, pos.Add(inner.Position())), true
		case *widget.Hyperlink:
			// Drawn by a RichText inset by the inner padding.
			th := c.v.Theme()
			pad := th.Size(theme.SizeNameInnerPadding)
			size := th.Size(theme.SizeNameText)
			if inner.SizeName != "" {
				size = th.Size(inner.SizeName)
			}
			at := pos.Add(inner.Position())
			w := fyne.MeasureText(inner.Text, size, inner.TextStyle).Width
			return textRun{text: []rune(inner.Text), pos: at.AddXY(pad+alignOffset(inner.Alignment, inner.Size().Width-2*pad, w), pad),
				height: inner.Size().Height - 2*pad, size: size, style: inner.TextStyle}, true
		}
	}
	return textRun{}, false
}

func canvasTextRun(t *canvas.Text, pos fyne.Position) textRun {
	w := fyne.MeasureText(t.Text, t.TextSize, t.TextStyle).Width
	return textRun{text: []rune(t.Text), pos: pos.AddXY(alignOffset(t.Alignment, t.Size().Width, w), 0),
		height: t.Size().Height, size: t.TextSize, style: t.TextStyle}
}

// codeRuns records a code block's lines: a monospace label inset by the
// inner padding, on its own background.
func (c *runCollector) codeRuns(text string, pos fyne.Position, sep string) {
	th := c.v.Theme()
	pad := th.Size(theme.SizeNameInnerPadding)
	size := th.Size(theme.SizeNameText)
	style := fyne.TextStyle{Monospace: true}
	height := fyne.MeasureText("M", size, style).Height
	for i, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if i > 0 {
			sep = "\n"
		}
		c.runs = append(c.runs, textRun{text: []rune(line), sep: sep, pos: pos.AddXY(pad, pad+float32(i)*height),
			height: height, size: size, style: style, over: true})
	}
}

// tableRuns records the cells of table t, drawn by o: a scroller over a
// border behind a grid of cells, each a RichText on a background. Cells are
// joined by tabs and rows by newlines.
func (c *runCollector) tableRuns(o fyne.CanvasObject, t *widget.TableSegment, sep string) {
	scroll, ok := o.(*container.Scroll)
	if !ok {
		return
	}
	stack, ok := scroll.Content.(*fyne.Container)
	if !ok || len(stack.Objects) < 2 {
		return
	}
	grid, ok := stack.Objects[1].(*fyne.Container)
	if !ok {
		return
	}
	rows := len(t.Rows)
	if t.Headers != nil {
		rows++
	}
	if rows == 0 {
		return
	}
	cols := len(grid.Objects) / rows
	at := scroll.Position().Subtract(scroll.Offset).Add(stack.Position()).Add(grid.Position())
	for i, cell := range grid.Objects {
		switch {
		case i == 0:
		case i%cols == 0:
			sep = "\n"
		default:
			sep = "\t"
		}
		if text, pos := findRichText(cell, at); text != nil {
			c.cellRuns(text, pos, sep)
		}
	}
}

// findRichText returns the RichText in o, whose parent is at parent, and
// where it is.
func findRichText(o fyne.CanvasObject, parent fyne.Position) (*widget.RichText, fyne.Position) {
	pos := parent.Add(o.Position())
	switch o := o.(type) {
	case *widget.RichText:
		return o, pos
	case *fyne.Container:
		for _, child := range o.Objects {
			if t, p := findRichText(child, pos); t != nil {
				return t, p
			}
		}
	}
	return nil, fyne.Position{}
}

// cellRuns records the text of a table cell: one unwrapped line of text and
// link segments, inset by the inner padding.
func (c *runCollector) cellRuns(t *widget.RichText, pos fyne.Position, sep string) {
	th := c.v.Theme()
	pad := th.Size(theme.SizeNameInnerPadding)
	var runs []textRun
	total := float32(0)
	align := fyne.TextAlignLeading
	for _, s := range t.Segments {
		run := textRun{size: th.Size(theme.SizeNameText), over: true}
		switch s := s.(type) {
		case *widget.TextSegment:
			if s.Style.SizeName != "" {
				run.size = th.Size(s.Style.SizeName)
			}
			run.text, run.style, align = []rune(s.Text), s.Style.TextStyle, s.Style.Alignment
		case *widget.HyperlinkSegment:
			run.text, run.style, align = []rune(s.Text), s.TextStyle, s.Alignment
		default:
			continue
		}
		run.pos = fyne.NewPos(total, 0)
		total += fyne.MeasureText(string(run.text), run.size, run.style).Width
		runs = append(runs, run)
	}
	origin := pos.AddXY(pad+alignOffset(align, t.Size().Width-2*pad, total), pad)
	for i, run := range runs {
		if i == 0 {
			run.sep = sep
		}
		run.pos = origin.Add(run.pos)
		run.height = t.Size().Height - 2*pad
		c.runs = append(c.runs, run)
	}
}

// alignOffset is where text of width w starts in a space of width space.
func alignOffset(a fyne.TextAlign, space, w float32) float32 {
	switch a {
	case fyne.TextAlignTrailing:
		return space - w
	case fyne.TextAlignCenter:
		return (space - w) / 2
	}
	return 0
}
