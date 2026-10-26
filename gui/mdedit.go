package gui

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The Markdown editing of #54: the formatting toolbar and shortcuts, lists
// that continue on Enter and smart paste, with the same rules as the web
// editor (#53). Each change is worked out here, without a window, and
// TextField applies it. Offsets count runes, as Fyne's Entry does.

// mdOp replaces n runes at offset at with s.
type mdOp struct {
	at, n int
	s     string
}

// mdEdit is a change to the editor's text and where it leaves the
// selection.
type mdEdit struct {
	// ops are last in the text first, with offsets into the text before
	// the edit, so each one leaves the offsets of the rest as they were.
	ops []mdOp
	// selStart and selEnd are the selection afterwards, in the new text;
	// equal for a cursor and no selection.
	selStart, selEnd int
}

// apply returns text with e's ops made.
func (e mdEdit) apply(text string) string {
	r := []rune(text)
	for _, op := range e.ops {
		r = append(r[:op.at:op.at], append([]rune(op.s), r[op.at+op.n:]...)...)
	}
	return string(r)
}

func runeLen(s string) int { return len([]rune(s)) }

// wrapSelection puts marker on both sides of the selection [start, end)
// and keeps it selected, or, with none, puts the cursor between the pair.
func wrapSelection(start, end int, marker string) mdEdit {
	m := runeLen(marker)
	return mdEdit{
		ops:      []mdOp{{at: end, s: marker}, {at: start, s: marker}},
		selStart: start + m, selEnd: end + m,
	}
}

// insertLink makes the selection [start, end) the text of a link and
// selects its "url" to type over. With no selection the cursor goes
// between the brackets.
func insertLink(start, end int) mdEdit {
	if start == end {
		return mdEdit{ops: []mdOp{{at: start, s: "[](url)"}}, selStart: start + 1, selEnd: start + 1}
	}
	u := end + runeLen("[](")
	return mdEdit{
		ops:      []mdOp{{at: end, s: "](url)"}, {at: start, s: "["}},
		selStart: u, selEnd: u + runeLen("url"),
	}
}

// The line prefixes of the toolbar. Each takes the line's index in the
// selection.
func headingPrefix(int) string  { return "### " }
func quotePrefix(int) string    { return "> " }
func bulletPrefix(int) string   { return "- " }
func taskPrefix(int) string     { return "- [ ] " }
func numberPrefix(i int) string { return strconv.Itoa(i+1) + ". " }

// prefixLines puts prefix at the start of each line the selection
// [start, end) touches, or of the cursor's line. A selection that ends at
// the start of a line leaves that line out. With a selection the cursor
// goes to the end of its last line; without, it stays in the same place in
// the text.
func prefixLines(text string, start, end int, prefix func(int) string) mdEdit {
	r := []rune(text)
	last := end
	if end > start && r[end-1] == '\n' {
		last--
	}
	var starts []int // the lines' first offsets, first line first
	for i := lineStart(r, start); ; {
		starts = append(starts, i)
		next := indexRune(r, i, '\n')
		if next < 0 || next >= last {
			break
		}
		i = next + 1
	}
	e := mdEdit{}
	added := 0
	for i := len(starts) - 1; i >= 0; i-- {
		p := prefix(i)
		e.ops = append(e.ops, mdOp{at: starts[i], s: p})
		added += runeLen(p)
	}
	if start == end {
		e.selStart = start + runeLen(prefix(0))
	} else {
		e.selStart = lineEnd(r, last) + added
	}
	e.selEnd = e.selStart
	return e
}

// lineStart is the offset of the start of the line holding offset at.
func lineStart(r []rune, at int) int {
	for at > 0 && r[at-1] != '\n' {
		at--
	}
	return at
}

// lineEnd is the offset of the end of the line holding offset at, before
// its newline.
func lineEnd(r []rune, at int) int {
	if i := indexRune(r, at, '\n'); i >= 0 {
		return i
	}
	return len(r)
}

// indexRune is the offset of the first c at or after from, -1 if none.
func indexRune(r []rune, from int, c rune) int {
	for i := from; i < len(r); i++ {
		if r[i] == c {
			return i
		}
	}
	return -1
}

// listItem is a list item: indent, marker ("-", "*" or "1."), an optional
// task box and the text. It matches the web editor's LIST_ITEM.
var listItem = regexp.MustCompile(`^(\s*)([-*]|(\d+)\.) (\[[ xX]\] )?(.*)$`)

// thematicBreak is a line such as "* * *" or "- - -", which is not a list
// item.
var thematicBreak = regexp.MustCompile(`^\s*(?:-(?:\s*-){2,}|\*(?:\s*\*){2,})\s*$`)

// continueList is Enter with the cursor at the end of a list item: it
// starts the next item, numbered one up and with an unchecked box for a
// task. On an empty item it removes the marker instead, which ends the
// list. It reports false for any other Enter.
func continueList(text string, cursor int) (mdEdit, bool) {
	r := []rune(text)
	if lineEnd(r, cursor) != cursor {
		return mdEdit{}, false
	}
	start := lineStart(r, cursor)
	line := string(r[start:cursor])
	m := listItem.FindStringSubmatch(line)
	if m == nil || thematicBreak.MatchString(line) {
		return mdEdit{}, false
	}
	if strings.TrimSpace(m[5]) == "" {
		return mdEdit{ops: []mdOp{{at: start, n: cursor - start}}, selStart: start, selEnd: start}, true
	}
	marker := m[2]
	if m[3] != "" {
		n, err := strconv.Atoi(m[3])
		if err != nil { // too many digits
			return mdEdit{}, false
		}
		marker = strconv.Itoa(n+1) + "."
	}
	next := "\n" + m[1] + marker + " "
	if m[4] != "" {
		next += "[ ] "
	}
	at := cursor + runeLen(next)
	return mdEdit{ops: []mdOp{{at: cursor, s: next}}, selStart: at, selEnd: at}, true
}

// pasteURL is a paste of clip over the selection [start, end): when clip
// is a single http(s) URL and there is a selection, the selection becomes
// the text of a link to it, with the cursor after the link.
func pasteURL(start, end int, clip string) (mdEdit, bool) {
	clip = strings.TrimSpace(clip)
	if start == end || strings.ContainsAny(clip, " \t\r\n") {
		return mdEdit{}, false
	}
	u, err := url.Parse(clip)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return mdEdit{}, false
	}
	tail := "](" + clip + ")"
	at := end + 1 + runeLen(tail)
	return mdEdit{ops: []mdOp{{at: end, s: tail}, {at: start, s: "["}}, selStart: at, selEnd: at}, true
}

// pasteTable is a paste of clip over the selection [start, end): when clip
// is cells copied from a spreadsheet, tab-separated with at least two
// lines of at least two columns each, it becomes a Markdown table with the
// first line as its header, on lines of its own. The cursor goes after it.
func pasteTable(text string, start, end int, clip string) (mdEdit, bool) {
	clip = strings.TrimRight(strings.ReplaceAll(clip, "\r\n", "\n"), "\n")
	lines := strings.Split(clip, "\n")
	if len(lines) < 2 {
		return mdEdit{}, false
	}
	rows := make([][]string, len(lines))
	cols := 0
	for i, l := range lines {
		rows[i] = strings.Split(l, "\t")
		if len(rows[i]) < 2 {
			return mdEdit{}, false
		}
		cols = max(cols, len(rows[i]))
	}
	var b strings.Builder
	r := []rune(text)
	if start > 0 && r[start-1] != '\n' {
		b.WriteString("\n")
	}
	for i, row := range rows {
		if i > 0 {
			b.WriteString("\n")
		}
		writeTableRow(&b, row, cols)
		if i == 0 {
			b.WriteString("\n")
			writeTableRow(&b, slices.Repeat([]string{"---"}, cols), cols)
		}
	}
	table := b.String()
	cursor := start + runeLen(table)
	if end < len(r) && r[end] != '\n' {
		table += "\n"
	}
	return mdEdit{ops: []mdOp{{at: start, n: end - start, s: table}}, selStart: cursor, selEnd: cursor}, true
}

// writeTableRow writes cells as a table row of cols cells, padding it with
// empty ones.
func writeTableRow(b *strings.Builder, cells []string, cols int) {
	b.WriteString("|")
	for i := range cols {
		cell := ""
		if i < len(cells) {
			cell = strings.ReplaceAll(strings.TrimSpace(cells[i]), "|", `\|`)
		}
		b.WriteString(" " + cell + " |")
	}
}
