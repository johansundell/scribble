package gui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// The note editor's right-click menu and its "Link to note…" (#143).

// TappedSecondary shows the right-click menu. The note editor's menu adds
// "Link to note…" to Entry's text actions, and its Undo and Redo go through
// TypedShortcut, so a Markdown change still undoes in one go.
func (f *TextField) TappedSecondary(e *fyne.PointEvent) {
	if !f.markdown {
		f.Entry.TappedSecondary(e)
		return
	}
	c := fyne.CurrentApp().Driver().CanvasForObject(f)
	if c == nil {
		return
	}
	c.Focus(f)
	clip := fyne.CurrentApp().Clipboard()
	item := func(label string, s fyne.Shortcut) *fyne.MenuItem {
		return fyne.NewMenuItem(label, func() { f.TypedShortcut(s) })
	}
	cut, cp := item("Cut", &fyne.ShortcutCut{Clipboard: clip}), item("Copy", &fyne.ShortcutCopy{Clipboard: clip})
	cut.Disabled = f.SelectedText() == ""
	cp.Disabled = cut.Disabled
	menu := fyne.NewMenu("",
		item("Undo", &fyne.ShortcutUndo{}),
		item("Redo", &fyne.ShortcutRedo{}),
		fyne.NewMenuItemSeparator(),
		cut, cp,
		item("Paste", &fyne.ShortcutPaste{Clipboard: clip}),
		item("Select all", &fyne.ShortcutSelectAll{}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Link to note…", f.askNoteLink),
	)
	widget.ShowPopUpMenuAtPosition(menu, c, e.AbsolutePosition)
}

// askNoteLink asks which note to link to, searching for the selected text,
// and puts a [[link]] to it in place of the selection or at the cursor.
func (f *TextField) askNoteLink() {
	query := strings.TrimSpace(f.SelectedText())
	start, end := f.selection()

	var d *dialog.CustomDialog
	closed := false
	done := func(title string) {
		if closed { // a double click and Enter may both get here
			return
		}
		closed = true
		d.Hide()
		f.ui.w.Canvas().Focus(f)
		if title == "" {
			f.setSelection(start, end)
			return
		}
		f.applyEdit(insertNoteLink(start, end, title))
	}
	p := newNotePicker(f.ui.linkTitles(), done)
	d = dialog.NewCustomWithoutButtons("Link to note",
		container.NewBorder(p.query, nil, nil, nil, p.list), f.ui.w)
	d.SetButtons([]fyne.CanvasObject{widget.NewButton("Cancel", func() { done("") }), p.link})
	d.Resize(fyne.NewSize(400, 400))
	p.query.SetText(query)
	p.filter()
	d.Show()
	f.ui.w.Canvas().Focus(p.query)
}

// linkTitles is the titles of the notes the open one can link to: the
// others, leaving out any a [[link]] cannot hold.
func (ui *UI) linkTitles() []string {
	var out []string
	for _, n := range ui.store.List("", "") {
		if n.Title != "" && !strings.ContainsAny(n.Title, "[]\n") && (ui.current == nil || n.ID != ui.current.ID) {
			out = append(out, n.Title)
		}
	}
	return out
}

// notePicker is the note list of "Link to note…": a search field over the
// titles that match it, the first selected, and the Link button.
type notePicker struct {
	query    *pickerField
	list     *widget.List
	link     *widget.Button // links to the selected title; disabled when none match
	titles   []string       // every note that can be linked to
	shown    []string       // the titles matching the query
	selected int            // index into shown, -1 for none
	done     func(title string)
}

func newNotePicker(titles []string, done func(string)) *notePicker {
	p := &notePicker{titles: titles, done: done, selected: -1}
	p.link = widget.NewButton("Link", p.choose)
	p.link.Importance = widget.HighImportance
	p.query = &pickerField{p: p}
	p.query.ExtendBaseWidget(p.query)
	p.query.SetPlaceHolder("Search notes")
	p.query.OnChanged = func(string) { p.filter() }
	p.query.OnSubmitted = func(string) { p.choose() }
	p.list = widget.NewList(
		func() int { return len(p.shown) },
		func() fyne.CanvasObject { return newPickerItem(p) },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			item := o.(*pickerItem)
			item.id = i
			item.SetText(p.shown[i])
		})
	p.list.OnSelected = func(i widget.ListItemID) { p.selected = i }
	return p
}

// filter narrows the list to the titles holding the query, ignoring case,
// and selects the first.
func (p *notePicker) filter() {
	q := strings.ToLower(p.query.Text)
	p.shown = p.shown[:0]
	for _, t := range p.titles {
		if strings.Contains(strings.ToLower(t), q) {
			p.shown = append(p.shown, t)
		}
	}
	p.list.UnselectAll()
	p.selected = -1
	p.list.Refresh()
	if len(p.shown) > 0 {
		p.list.Select(0)
	}
	setEnabled(p.link, len(p.shown) > 0)
}

// choose links to the selected title, if any.
func (p *notePicker) choose() {
	if p.selected >= 0 && p.selected < len(p.shown) {
		p.done(p.shown[p.selected])
	}
}

// move selects the title by places away from the selected one.
func (p *notePicker) move(by int) {
	if i := p.selected + by; i >= 0 && i < len(p.shown) {
		p.list.Select(i)
		p.list.ScrollTo(i)
	}
}

// focusQuery gives the search field the focus back: a click on the list,
// which cannot hold it, takes it away.
func (p *notePicker) focusQuery() {
	if c := fyne.CurrentApp().Driver().CanvasForObject(p.query); c != nil {
		c.Focus(p.query)
	}
}

// pickerField is the picker's search field. Up and Down move through the
// list, Esc cancels.
type pickerField struct {
	widget.Entry
	p *notePicker
}

// TypedKey is called when a key is typed.
func (f *pickerField) TypedKey(e *fyne.KeyEvent) {
	switch e.Name {
	case fyne.KeyEscape:
		f.p.done("")
	case fyne.KeyDown:
		f.p.move(1)
	case fyne.KeyUp:
		f.p.move(-1)
	default:
		f.Entry.TypedKey(e)
	}
}

// pickerItem is a title in the picker's list: a click selects it, a double
// click links to it. Either leaves the focus in the search field, which
// takes Enter and Esc.
type pickerItem struct {
	widget.Label
	p  *notePicker
	id widget.ListItemID
}

func newPickerItem(p *notePicker) *pickerItem {
	item := &pickerItem{p: p}
	item.ExtendBaseWidget(item)
	return item
}

// Tapped selects the title.
func (i *pickerItem) Tapped(*fyne.PointEvent) {
	i.p.list.Select(i.id)
	i.p.focusQuery()
}

// DoubleTapped links to the title.
func (i *pickerItem) DoubleTapped(*fyne.PointEvent) {
	i.p.list.Select(i.id)
	i.p.choose()
}
