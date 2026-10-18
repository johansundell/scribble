package notes

import (
	"slices"
	"strings"
	"testing"
)

func TestParseTomboy(t *testing.T) {
	const raw = `<?xml version="1.0" encoding="utf-8"?>
<note version="0.3" xmlns:link="http://beatniksoftware.com/tomboy/link" xmlns:size="http://beatniksoftware.com/tomboy/size" xmlns="http://beatniksoftware.com/tomboy">
  <title>Start Here</title>
  <text xml:space="preserve"><note-content version="0.1">Start Here

Welcome to Tomboy!

Try linking to <link:internal>Using Links in Tomboy</link:internal> or <link:internal>Ideas</link:internal>.
</note-content></text>
  <last-change-date>2026-10-18T08:00:00.0000000+02:00</last-change-date>
  <create-date>2026-10-18T08:00:00.0000000+02:00</create-date>
</note>`

	got, err := ParseTomboy(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	want := "# Start Here\n\nWelcome to Tomboy!\n\nTry linking to [[Using Links in Tomboy]] or [[Ideas]].\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if TitleOf(got) != "Start Here" {
		t.Errorf("TitleOf = %q, want %q", TitleOf(got), "Start Here")
	}
}

func TestParseTomboyFormattingAndLists(t *testing.T) {
	const raw = `<?xml version="1.0" encoding="utf-8"?>
<note version="0.3" xmlns:link="http://beatniksoftware.com/tomboy/link" xmlns:size="http://beatniksoftware.com/tomboy/size" xmlns="http://beatniksoftware.com/tomboy">
  <title>Shopping &amp; Tasks</title>
  <text xml:space="preserve"><note-content version="0.1"><size:huge>Shopping &amp; <link:internal>Tasks</link:internal></size:huge>

Need <bold>urgent</bold>, <italic>fresh</italic>, <strikethrough>done</strikethrough>, and <monospace>go test</monospace> items (<highlight>important</highlight>).
See <link:url>https://example.com/a&amp;b</link:url> and <link:internal>Groceries</link:internal>.

<list>
  <list-item dir="ltr">Milk &amp; bread</list-item>
  <list-item dir="ltr">Check <link:internal><bold>Pantry</bold></link:internal><list><list-item dir="ltr">Flour</list-item><list-item dir="ltr">Sugar</list-item></list></list-item>
  <list-item dir="ltr">Apples</list-item>
</list>
After list with *literal* _marks_ and [brackets].
</note-content></text>
</note>`

	got, err := ParseTomboy(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"# Shopping & Tasks",
		"",
		"Need **urgent**, *fresh*, ~~done~~, and `go test` items (important).",
		"See https://example.com/a&b and [[Groceries]].",
		"",
		"- Milk & bread",
		"- Check [[Pantry]]",
		"  - Flour",
		"  - Sugar",
		"- Apples",
		"",
		`After list with \*literal\* \_marks\_ and \[brackets\].`,
		"",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	if gotLinks := Links(got); !slices.Equal(gotLinks, []string{"Groceries", "Pantry"}) {
		t.Errorf("Links = %q, want [Groceries Pantry]", gotLinks)
	}
}

func TestParseTomboyPreservesHashTitleAndLiteralCode(t *testing.T) {
	const raw = `<?xml version="1.0" encoding="utf-8"?>
<note version="0.3" xmlns:link="http://beatniksoftware.com/tomboy/link" xmlns="http://beatniksoftware.com/tomboy">
  <title>#Ideas</title>
  <text xml:space="preserve"><note-content version="0.1">#Ideas

Link to <link:internal>#Ideas</link:internal> and <link:internal>C*Note_1</link:internal>.
Code <monospace>*raw* ` + "`" + `tick` + "`" + `</monospace> and literal ~tilde~ ` + "`" + `backtick` + "`" + `.
</note-content></text>
</note>`

	got, err := ParseTomboy(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if TitleOf(got) != "#Ideas" {
		t.Errorf("TitleOf = %q, want %q", TitleOf(got), "#Ideas")
	}
	if gotLinks := Links(got); !slices.Equal(gotLinks, []string{"#Ideas", "C*Note_1"}) {
		t.Errorf("Links = %q, want [#Ideas C*Note_1]", gotLinks)
	}
	want := "# #Ideas\n\nLink to [[#Ideas]] and [[C*Note_1]].\nCode `` *raw* `tick` `` and literal \\~tilde\\~ \\`backtick\\`.\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestParseTomboyEdgeCases(t *testing.T) {
	// Title-only note (no body after the repeated title).
	titleOnly := `<note xmlns="http://beatniksoftware.com/tomboy"><title>Empty Note</title><text><note-content>Empty Note</note-content></text></note>`
	got, err := ParseTomboy(strings.NewReader(titleOnly))
	if err != nil {
		t.Fatal(err)
	}
	if got != "# Empty Note\n" {
		t.Errorf("title-only got %q, want %q", got, "# Empty Note\n")
	}

	// Note where <note-content> does not repeat the title on line 1.
	noRepeat := `<note xmlns="http://beatniksoftware.com/tomboy"><title>Quick Jot</title><text><note-content>First body line</note-content></text></note>`
	got, err = ParseTomboy(strings.NewReader(noRepeat))
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Quick Jot\n\nFirst body line\n"; got != want {
		t.Errorf("no-repeat got %q, want %q", got, want)
	}

	// Malformed XML or missing title must return an error.
	for _, bad := range []string{
		"",
		"<not-a-note><title>X</title></not-a-note>",
		"<note><title>   </title><text><note-content>body</note-content></text></note>",
		"<note><title>Unclosed",
	} {
		if _, err := ParseTomboy(strings.NewReader(bad)); err == nil {
			t.Errorf("ParseTomboy(%q) succeeded, want error", bad)
		}
	}
}

