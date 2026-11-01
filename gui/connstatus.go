package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ConnStatus is the connection status indicator at the bottom-right of the
// window. It displays a colored dot and text ("Connecting...", "Connected",
// "Offline", "Update needed") and triggers an immediate sync when clicked.
type ConnStatus struct {
	widget.BaseWidget
	ui      *UI
	content *fyne.Container
}

func newConnStatus(ui *UI) *ConnStatus {
	ui.ConnDot = canvas.NewCircle(theme.DisabledColor())
	ui.ConnLabel = widget.NewLabel("Connecting...")
	dotBox := container.NewCenter(container.NewGridWrap(fyne.NewSize(8, 8), ui.ConnDot))
	content := container.NewHBox(dotBox, ui.ConnLabel)
	cs := &ConnStatus{ui: ui, content: content}
	cs.ExtendBaseWidget(cs)
	return cs
}

// CreateRenderer provides the widget's render tree.
func (cs *ConnStatus) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(cs.content)
}

// Tapped handles a click or tap on the indicator, triggering an immediate sync.
func (cs *ConnStatus) Tapped(*fyne.PointEvent) {
	if cs.ui != nil {
		cs.ui.retrySync()
	}
}

// Cursor shows a pointer cursor to signal that the status indicator is clickable.
func (cs *ConnStatus) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (ui *UI) setConnecting() {
	ui.ConnDot.FillColor = theme.DisabledColor()
	ui.ConnDot.Refresh()
	ui.ConnLabel.SetText("Connecting...")
}

func (ui *UI) setConnected() {
	ui.ConnDot.FillColor = theme.SuccessColor()
	ui.ConnDot.Refresh()
	ui.ConnLabel.SetText("Connected")
	ui.updateButtons() // Share needs the server
}

func (ui *UI) setOffline() {
	ui.ConnDot.FillColor = theme.ErrorColor()
	ui.ConnDot.Refresh()
	ui.ConnLabel.SetText("Offline")
	ui.updateButtons()
}

func (ui *UI) retrySync() {
	if ui.outdated.Load() {
		return // a too-old version never syncs again
	}
	ui.setConnecting()
	ui.Sync()
}
