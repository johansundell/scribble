package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// The note editor's Markdown editing (#54), on TextField.

// SetText replaces the text, which also clears the undo history.
func (f *TextField) SetText(text string) {
	f.undo, f.redo = nil, nil
	f.Entry.SetText(text)
}

// KeyDown is called when a key is pressed down.
func (f *TextField) KeyDown(e *fyne.KeyEvent) {
	if isShift(e) {
		f.shift = true
	}
	f.Entry.KeyDown(e)
}

// FocusLost is called when the field loses focus. Shift may be released
// while another widget has it.
func (f *TextField) FocusLost() {
	f.shift = false
	f.Entry.FocusLost()
}

// TypedRune is called when a character is typed. Straight after a change
// that took more than one undo step, Entry would merge the character into
// that change's last step, and Ctrl+Z would then undo the character with
// half the change. Pasted, it is a step of its own.
func (f *TextField) TypedRune(r rune) {
	if n := len(f.undo); f.markdown && n > 0 && f.undo[n-1].after == f.Text {
		f.Entry.TypedShortcut(&fyne.ShortcutPaste{Clipboard: textClipboard(string(r))})
		return
	}
	f.Entry.TypedRune(r)
}

// KeyUp is called when a key is released.
func (f *TextField) KeyUp(e *fyne.KeyEvent) {
	if isShift(e) {
		f.shift = false
	}
	f.Entry.KeyUp(e)
}

func isShift(e *fyne.KeyEvent) bool {
	return e.Name == desktop.KeyShiftLeft || e.Name == desktop.KeyShiftRight
}

// TypedKey is called when a key is typed. Enter at the end of a list item
// continues the list; Shift+Enter is a plain newline.
func (f *TextField) TypedKey(e *fyne.KeyEvent) {
	if f.markdown && (e.Name == fyne.KeyReturn || e.Name == fyne.KeyEnter) && !f.shift && f.SelectedText() == "" {
		if edit, ok := continueList(f.Text, f.CursorTextOffset()); ok {
			f.applyEdit(edit)
			return
		}
	}
	f.Entry.TypedKey(e)
}

// TypedShortcut is called for a keyboard shortcut, or an Edit menu item
// passing one on.
func (f *TextField) TypedShortcut(s fyne.Shortcut) {
	if f.markdown {
		switch s := s.(type) {
		case *fyne.ShortcutUndo:
			if f.undoGroup() {
				return
			}
		case *fyne.ShortcutRedo:
			if f.redoGroup() {
				return
			}
		case *fyne.ShortcutPaste:
			if f.smartPaste(s.Clipboard) {
				return
			}
		case *desktop.CustomShortcut:
			if fm := formatKeys[s.KeyName]; fm != nil && s.Modifier == fyne.KeyModifierShortcutDefault {
				f.format(fm)
				return
			}
		}
	}
	f.Entry.TypedShortcut(s)
}

// format makes the change fm works out from the selection or cursor.
func (f *TextField) format(fm formatting) {
	start, end := f.selection()
	f.applyEdit(fm(f.Text, start, end))
}

// smartPaste pastes a URL over selected text as a link, and cells from a
// spreadsheet as a table. It reports false, with the selection as it was,
// for a paste Entry should make as usual.
func (f *TextField) smartPaste(clip fyne.Clipboard) bool {
	if clip == nil {
		return false
	}
	content := clip.Content()
	start, end := f.selection()
	edit, ok := pasteURL(start, end, content)
	if !ok {
		edit, ok = pasteTable(f.Text, start, end, content)
	}
	if !ok {
		f.setSelection(start, end)
		return false
	}
	f.applyEdit(edit)
	return true
}

var (
	shiftKey = &fyne.KeyEvent{Name: desktop.KeyShiftLeft}
	rightKey = &fyne.KeyEvent{Name: fyne.KeyRight}
)

// selection returns the start and end of the selection, equal at the
// cursor when there is none. Entry does not say which end its cursor is
// at, so this moves the cursor to the end, as Right does, which ends the
// selection.
func (f *TextField) selection() (start, end int) {
	n := runeLen(f.SelectedText())
	f.clearSelection()
	end = f.CursorTextOffset()
	return end - n, end
}

// clearSelection ends the selection, if any, with the cursor at its end.
func (f *TextField) clearSelection() {
	if f.SelectedText() != "" {
		f.Entry.KeyUp(shiftKey) // with Shift held, Right would grow it
		f.Entry.TypedKey(rightKey)
	}
}

// setCursor puts the cursor at offset p. Entry places
// its cursor by row and column, and with word wrap a row is a line on the
// screen, not one of the text's: the row is the last one starting at or
// before p.
func (f *TextField) setCursor(p int) {
	row, begin := 0, 0
	for r := 1; ; r++ {
		f.CursorRow, f.CursorColumn = r, 0
		b := f.CursorTextOffset() // 0 past the last row
		if b <= begin || b > p {
			break
		}
		row, begin = r, b
	}
	f.CursorRow, f.CursorColumn = row, p-begin
	f.Refresh()
}

// setSelection selects [start, end), or puts the cursor at start when they
// are equal. Entry has no API for it, so it selects the way Shift+Right
// does.
func (f *TextField) setSelection(start, end int) {
	f.clearSelection()
	f.setCursor(start)
	if end <= start {
		return
	}
	f.Entry.KeyDown(shiftKey)
	f.setCursor(end - 1)
	// Right from the end of a wrapped row goes to the start of the next,
	// the same offset.
	for i := 0; i < 2 && f.CursorTextOffset() < end; i++ {
		f.Entry.TypedKey(rightKey)
	}
	f.Entry.KeyUp(shiftKey)
}

// applyEdit makes e's change with Entry's own editing, so that undo works.
// Entry has no API to replace text, but a paste inserts its text as one
// undo step, and erasing a selection is one more. So each op selects what it replaces, if anything,
// and pastes its text. One that takes more than one step, such as wrapping
// a selection, is kept in undo, and TypedShortcut undoes all its steps on
// one Ctrl+Z while the text is still as the change left it.
func (f *TextField) applyEdit(e mdEdit) {
	before, steps := f.Text, 0
	for _, op := range e.ops {
		f.setSelection(op.at, op.at+op.n)
		if op.n > 0 {
			steps++
			if op.s == "" {
				f.Entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})
				continue
			}
		}
		f.Entry.TypedShortcut(&fyne.ShortcutPaste{Clipboard: textClipboard(op.s)})
		steps++
	}
	f.setSelection(e.selStart, e.selEnd)
	if f.shift {
		f.Entry.KeyDown(shiftKey)
	}
	f.redo = nil
	if steps > 1 {
		f.undo = append(f.undo, undoGroup{before: before, after: f.Text, steps: steps})
	}
}

// undoGroup undoes the last Markdown change in one go when the text is as
// it left it, and reports whether it did. The Undo of Entry's own
// right-click menu bypasses it and goes a step at a time.
func (f *TextField) undoGroup() bool {
	n := len(f.undo)
	if n == 0 || f.undo[n-1].after != f.Text {
		return false
	}
	g := f.undo[n-1]
	f.undo = f.undo[:n-1]
	f.clearSelection()
	for range g.steps {
		f.Entry.Undo()
	}
	f.redo = append(f.redo, g)
	return true
}

// redoGroup is undoGroup's opposite.
func (f *TextField) redoGroup() bool {
	n := len(f.redo)
	if n == 0 || f.redo[n-1].before != f.Text {
		return false
	}
	g := f.redo[n-1]
	f.redo = f.redo[:n-1]
	f.clearSelection()
	for range g.steps {
		f.Entry.Redo()
	}
	f.undo = append(f.undo, g)
	return true
}

// textClipboard is a clipboard holding a fixed text, for pasting it.
type textClipboard string

func (c textClipboard) Content() string   { return string(c) }
func (c textClipboard) SetContent(string) {}
