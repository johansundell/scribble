package gui

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/client"
)

// tooOld is the update message shown once the server refuses this version.
const tooOld = "This version is too old for the server. Download the latest release."

// releaseURL is the page of release version.
const releaseURL = "https://github.com/johansundell/scribble/releases/tag/"

// openURL opens a link in the browser. Swappable for testing.
var openURL = func(u *url.URL) error { return fyne.CurrentApp().OpenURL(u) }

// newVersionWidgets builds the update message (Upgrade) and the "a newer
// version is out" link (NewRelease), both hidden until needed.
func (ui *UI) newVersionWidgets() {
	ui.UpgradeLabel = widget.NewLabel(tooOld)
	ui.UpgradeButton = widget.NewButton("Download", func() {
		u, _ := url.Parse(client.ReleasesURL)
		ui.report(openURL(u))
	})
	ui.UpgradeButton.Importance = widget.HighImportance
	ui.Upgrade = container.NewHBox(ui.UpgradeLabel, ui.UpgradeButton)
	ui.Upgrade.Hide()
	ui.NewRelease = widget.NewHyperlink("", nil)
	ui.NewRelease.Hide()
}

// setOutdated is the app's state once the server refuses this version as
// too old (ADR 8): the update message stays up and the indicator says
// "Update needed". Syncing stops and queued edits stay on disk for a newer
// version to send. The notes stay readable from the local copy.
func (ui *UI) setOutdated() {
	ui.outdated.Store(true)
	ui.ConnDot.FillColor = theme.WarningColor()
	ui.ConnDot.Refresh()
	ui.ConnLabel.SetText("Update needed")
	ui.Upgrade.Show()
	ui.updateButtons()
}

// noticeNewRelease shows the "Scribble vX.Y.Z is available" link when the
// server runs a newer release than this app. It is shown once per session
// and left alone after that.
func (ui *UI) noticeNewRelease() {
	if ui.newRelease != "" {
		return
	}
	server, _ := ui.c.ServerVersion()
	if !client.NewerRelease(appVersion(), server) {
		return
	}
	ui.newRelease = server
	ui.NewRelease.SetText(releaseText(server))
	_ = ui.NewRelease.SetURLFromString(releaseURL + server)
	ui.NewRelease.Show()
}

func releaseText(version string) string { return "Scribble " + version + " is available" }
