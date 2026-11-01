package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/notes"
)

// Pinned notes (#140) are listed first, marked pinMark. File > Pin note
// pins the open note and a list row's context menu any note. Pinning goes
// straight to the server: it isn't an edit, so it is never queued, and it
// leaves the editor's text and revision alone.

// pinAPILevel is the server API level that added pinned notes.
const pinAPILevel = 5

// pinMark comes before a pinned note's title in the list, as in sn ls.
const pinMark = "📌 "

// canPin reports whether note id can be pinned or unpinned now: it is on
// the server, and the server is reachable and new enough.
func (ui *UI) canPin(id int64) bool {
	if ui.outdated.Load() || ui.offline || id <= 0 {
		return false
	}
	_, api := ui.c.ServerVersion()
	return api == 0 || api >= pinAPILevel
}

// pinLabelOf is what the menu item that pins or unpins n says.
func pinLabelOf(n notes.Note) string {
	if n.Pinned {
		return "Unpin note"
	}
	return "Pin note"
}

// togglePinCurrent pins the open note, or unpins it.
func (ui *UI) togglePinCurrent() {
	if ui.current != nil {
		ui.togglePin(ui.current.ID)
	}
}

// togglePin pins note id, or unpins it, on the server and shows the list
// in its new order.
func (ui *UI) togglePin(id int64) {
	n, ok := ui.store.Get(id)
	if !ok || !ui.canPin(id) {
		return
	}
	ctx, cancel := requestContext()
	defer cancel()
	p, err := ui.c.Pin(ctx, id, !n.Pinned)
	if ui.reportOnline(err) {
		return
	}
	ui.Status.SetText("")
	if ui.report(ui.store.Put(*p)) {
		return
	}
	if ui.current != nil && ui.current.ID == id {
		// Only the pin: the editor may hold text not saved yet.
		ui.current.Pinned = p.Pinned
	}
	ui.refresh()
	ui.updateButtons()
}

// noteRow is a row of the note list: the note's title, and a context menu
// to pin or unpin it.
type noteRow struct {
	widget.BaseWidget
	ui    *UI
	label *widget.Label
	id    int64
}

func newNoteRow(ui *UI) *noteRow {
	r := &noteRow{ui: ui, label: widget.NewLabel("")}
	r.ExtendBaseWidget(r)
	return r
}

// show makes the row show note n.
func (r *noteRow) show(n notes.Note) {
	r.id = n.ID
	text := n.Title
	if n.Pinned {
		text = pinMark + text
	}
	r.label.SetText(text)
}

func (r *noteRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.label)
}

// TappedSecondary shows the row's context menu.
func (r *noteRow) TappedSecondary(e *fyne.PointEvent) {
	n, ok := r.ui.store.Get(r.id)
	if !ok {
		return
	}
	id := r.id
	pin := fyne.NewMenuItem(pinLabelOf(n), func() { r.ui.togglePin(id) })
	pin.Disabled = !r.ui.canPin(id)
	c := fyne.CurrentApp().Driver().CanvasForObject(r)
	widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", pin), c, e.AbsolutePosition)
}
