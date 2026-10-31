package gui

import (
	"bytes"
	"context"
	"io"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
	"github.com/johansundell/scribble/notes"
)

// File > Export notes… and Import notes… (#117): every note as a ZIP
// archive of Markdown files, through the server.

// zipTimeout is how long an export or import may take.
const zipTimeout = 2 * time.Minute

// pickSaveFile and pickOpenFile show the native file dialogs. Tests swap
// them, as they cannot click through one.
var (
	pickSaveFile = func(w fyne.Window, name string, cb func(fyne.URIWriteCloser, error)) {
		d := dialog.NewFileSave(cb, w)
		d.SetFileName(name)
		d.SetFilter(storage.NewExtensionFileFilter([]string{".zip"}))
		d.Show()
	}
	pickOpenFile = func(w fyne.Window, cb func(fyne.URIReadCloser, error)) {
		d := dialog.NewFileOpen(cb, w)
		d.SetFilter(storage.NewExtensionFileFilter([]string{".zip"}))
		d.Show()
	}
)

// askExport asks where to save the archive, then saves it.
func (ui *UI) askExport() {
	pickSaveFile(ui.w, notes.ZipFileName(time.Now()), func(f fyne.URIWriteCloser, err error) {
		if ui.report(err) || f == nil {
			return
		}
		ui.export(f)
	})
}

func (ui *UI) export(f fyne.URIWriteCloser) {
	ctx, cancel := context.WithTimeout(context.Background(), zipTimeout)
	defer cancel()
	err := ui.c.Export(ctx, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		storage.Delete(f.URI()) // never leave half a ZIP behind
		ui.reportOnline(err)
		return
	}
	ui.Status.SetText("")
	dialog.ShowInformation("Export notes", "Saved every note to "+f.URI().Name()+".", ui.w)
}

// askImport asks for an archive, then whether to overwrite notes that
// already exist, and imports it.
func (ui *UI) askImport() {
	pickOpenFile(ui.w, func(f fyne.URIReadCloser, err error) {
		if ui.report(err) || f == nil {
			return
		}
		data, err := io.ReadAll(f)
		f.Close()
		if ui.report(err) {
			return
		}
		// Checked here, so a wrong file gets a clear message.
		list, err := notes.ReadZip(bytes.NewReader(data), int64(len(data)))
		if ui.report(err) {
			return
		}
		if len(list) == 0 {
			dialog.ShowInformation("Import notes", f.URI().Name()+" holds no .md, .txt or .note files.", ui.w)
			return
		}
		overwrite := widget.NewCheck("Overwrite notes that already exist", nil)
		msg := widget.NewLabel("Import the notes in " + f.URI().Name() + "? Notes whose title exists are skipped unless you overwrite them.")
		msg.Wrapping = fyne.TextWrapWord
		d := dialog.NewCustomConfirm("Import notes", "Import", "Cancel", container.NewVBox(msg, overwrite), func(ok bool) {
			if ok {
				ui.importZip(data, overwrite.Checked)
			}
		}, ui.w)
		d.Resize(fyne.NewSize(420, d.MinSize().Height))
		d.Show()
	})
}

func (ui *UI) importZip(data []byte, overwrite bool) {
	ctx, cancel := context.WithTimeout(context.Background(), zipTimeout)
	defer cancel()
	res, err := ui.c.Import(ctx, bytes.NewReader(data), overwrite)
	if ui.reportOnline(err) {
		return
	}
	ui.Status.SetText("")
	ui.Sync()
	dialog.ShowInformation("Import notes", notes.ImportSummary(res.Imported, res.Skipped), ui.w)
}
