package gui

import (
	"strings"
	"testing"
)

// The cases write the editor as text with its selection marked «like this»,
// or its cursor as ‸.

// parseState returns marked's text and its selection, in runes.
func parseState(t *testing.T, marked string) (text string, start, end int) {
	t.Helper()
	r := []rune(marked)
	var out []rune
	start, end = -1, -1
	for _, c := range r {
		switch c {
		case '«', '‸':
			start = len(out)
			if c == '‸' {
				end = start
			}
		case '»':
			end = len(out)
		default:
			out = append(out, c)
		}
	}
	if start < 0 || end < 0 {
		t.Fatalf("%q marks no cursor or selection", marked)
	}
	return string(out), start, end
}

// markState is the opposite of parseState.
func markState(text string, start, end int) string {
	r := []rune(text)
	if start == end {
		return string(r[:start]) + "‸" + string(r[start:])
	}
	return string(r[:start]) + "«" + string(r[start:end]) + "»" + string(r[end:])
}

// after is the editor once e is applied to text.
func after(text string, e mdEdit) string {
	return markState(e.apply(text), e.selStart, e.selEnd)
}

func TestWrapSelection(t *testing.T) {
	for _, c := range []struct {
		name, marker, in, want string
	}{
		{"bold selection", "**", "say «hi» there", "say **«hi»** there"},
		{"bold no selection", "**", "say ‸", "say **‸**"},
		{"italic selection", "_", "«a b»", "_«a b»_"},
		{"italic no selection", "_", "a‸b", "a_‸_b"},
		{"code selection", "`", "run «go test» now", "run `«go test»` now"},
		{"code no selection", "`", "‸", "`‸`"},
		{"multibyte", "**", "å«ä»ö", "å**«ä»**ö"},
	} {
		t.Run(c.name, func(t *testing.T) {
			text, start, end := parseState(t, c.in)
			if got := after(text, wrapSelection(start, end, c.marker)); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestInsertLink(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"selection", "see «docs» here", "see [docs](«url») here"},
		{"no selection", "see ‸", "see [‸](url)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			text, start, end := parseState(t, c.in)
			if got := after(text, insertLink(start, end)); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestPrefixLines(t *testing.T) {
	for _, c := range []struct {
		name   string
		prefix func(int) string
		in     string
		want   string
	}{
		{"heading cursor line", headingPrefix, "one\ntw‸o\nthree", "one\n### tw‸o\nthree"},
		{"heading empty text", headingPrefix, "‸", "### ‸"},
		{"quote lines", quotePrefix, "«a\nb»\nc", "> a\n> b‸\nc"},
		{"quote from mid line", quotePrefix, "x«y\nz»", "> xy\n> z‸"},
		{"bullets", bulletPrefix, "«a\nb»", "- a\n- b‸"},
		{"numbers count up", numberPrefix, "«a\nb\nc»", "1. a\n2. b\n3. c‸"},
		{"number cursor line", numberPrefix, "a‸", "1. a‸"},
		{"tasks", taskPrefix, "«a\n\nb»", "- [ ] a\n- [ ] \n- [ ] b‸"},
		{"task empty line", taskPrefix, "a\n‸", "a\n- [ ] ‸"},
		{"selection ending at a line start leaves that line", bulletPrefix, "«a\n»b", "- a‸\nb"},
	} {
		t.Run(c.name, func(t *testing.T) {
			text, start, end := parseState(t, c.in)
			if got := after(text, prefixLines(text, start, end, c.prefix)); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestContinueList(t *testing.T) {
	for _, c := range []struct {
		name, in, want string // want "" when Enter is left to the editor
	}{
		{"dash", "- a‸", "- a\n- ‸"},
		{"star", "* a‸", "* a\n* ‸"},
		{"number counts up", "9. a‸", "9. a\n10. ‸"},
		{"indented", "  - a‸", "  - a\n  - ‸"},
		{"done task starts unchecked", "- [x] done‸", "- [x] done\n- [ ] ‸"},
		{"open task", "- [ ] todo‸", "- [ ] todo\n- [ ] ‸"},
		{"numbered task", "1. [X] a‸", "1. [X] a\n2. [ ] ‸"},
		{"middle of the text", "- a‸\nb", "- a\n- ‸\nb"},
		{"empty item ends the list", "a\n- ‸", "a\n‸"},
		{"empty number ends the list", "1. a\n2. ‸", "1. a\n‸"},
		{"empty task ends the list", "- [ ] ‸", "‸"},
		{"empty indented item", "  - ‸\nb", "‸\nb"},
		{"not at the end of the line", "- a‸b", ""},
		{"not a list", "a‸", ""},
		{"no space after marker", "-a‸", ""},
		{"rule", "* * *‸", ""},
		{"dash rule", "- - -‸", ""},
		{"empty text", "‸", ""},
		{"number too long", "99999999999999999999. a‸", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			text, cur, _ := parseState(t, c.in)
			e, ok := continueList(text, cur)
			if c.want == "" {
				if ok {
					t.Fatalf("continued the list to %q, want a plain newline", after(text, e))
				}
				return
			}
			if !ok {
				t.Fatal("did not continue the list")
			}
			if got := after(text, e); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestPasteURL(t *testing.T) {
	for _, c := range []struct {
		name, in, clip, want string // want "" for a plain paste
	}{
		{"url over selection", "see «docs»!", "https://x.io/a?b=1", "see [docs](https://x.io/a?b=1)‸!"},
		{"http and whitespace", "«a»", " http://x.io\n", "[a](http://x.io)‸"},
		{"no selection", "a‸", "https://x.io", ""},
		{"not a url", "«a»", "hello", ""},
		{"other scheme", "«a»", "ftp://x.io", ""},
		{"two words", "«a»", "https://x.io and more", ""},
		{"no host", "«a»", "https://", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			text, start, end := parseState(t, c.in)
			e, ok := pasteURL(start, end, c.clip)
			if c.want == "" {
				if ok {
					t.Fatalf("pasted %q, want a plain paste", after(text, e))
				}
				return
			}
			if !ok {
				t.Fatal("pasted plainly, want a link")
			}
			if got := after(text, e); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestPasteTable(t *testing.T) {
	table := "| a | b |\n| --- | --- |\n| 1 | 2 |"
	for _, c := range []struct {
		name, in, clip, want string // want "" for a plain paste
	}{
		{"cells", "‸", "a\tb\n1\t2", table + "‸"},
		{"spreadsheet line ends", "‸", "a\tb\r\n1\t2\r\n", table + "‸"},
		{"after text on the line", "x‸", "a\tb\n1\t2", "x\n" + table + "‸"},
		{"before text on the line", "‸y", "a\tb\n1\t2", table + "‸\ny"},
		{"on its own line", "x\n‸\ny", "a\tb\n1\t2", "x\n" + table + "‸\ny"},
		{"over a selection", "«old»", "a\tb\n1\t2", table + "‸"},
		{"ragged rows", "‸", "a\tb\tc\n1\t2", "| a | b | c |\n| --- | --- | --- |\n| 1 | 2 |  |‸"},
		{"pipes escaped", "‸", "a|b\tc\n1\t2", "| a\\|b | c |\n| --- | --- |\n| 1 | 2 |‸"},
		{"empty cell", "‸", "a\t\n1\t2", "| a |  |\n| --- | --- |\n| 1 | 2 |‸"},
		{"one line", "‸", "a\tb", ""},
		{"a line with one column", "‸", "a\tb\nc", ""},
		{"no tabs", "‸", "a\nb", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			text, start, end := parseState(t, c.in)
			e, ok := pasteTable(text, start, end, c.clip)
			if c.want == "" {
				if ok {
					t.Fatalf("pasted %q, want a plain paste", after(text, e))
				}
				return
			}
			if !ok {
				t.Fatal("pasted plainly, want a table")
			}
			if got := after(text, e); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// Every edit's ops are in the order the editor applies them: last in the
// text first, so the offsets of the ones still to come hold.
func TestEditOpsRunBackwards(t *testing.T) {
	text := strings.Repeat("line\n", 4) + "end"
	edits := []mdEdit{
		wrapSelection(2, 8, "**"),
		insertLink(2, 8),
		prefixLines(text, 0, len(text), numberPrefix),
	}
	for _, e := range edits {
		for i := 1; i < len(e.ops); i++ {
			if e.ops[i].at+e.ops[i].n > e.ops[i-1].at {
				t.Fatalf("ops %+v are not last-first", e.ops)
			}
		}
	}
}

func TestInsertNoteLink(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"no selection", "see ‸ now", "see [[Recipes]]‸ now"},
		{"selection", "see «recipes» now", "see [[Recipes]]‸ now"},
		{"multibyte", "å«ä»ö", "å[[Recipes]]‸ö"},
	} {
		t.Run(c.name, func(t *testing.T) {
			text, start, end := parseState(t, c.in)
			if got := after(text, insertNoteLink(start, end, "Recipes")); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
