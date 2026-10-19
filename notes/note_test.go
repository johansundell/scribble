package notes

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2/widget"
)

func TestTitleOf(t *testing.T) {
	cases := map[string]string{
		"# Hello\nbody":    "Hello",
		"\n\n## Deep  \nx": "Deep",
		"Plain first line": "Plain first line",
		"":                 "",
		"   \n  \n":        "",
	}
	for in, want := range cases {
		if got := TitleOf(in); got != want {
			t.Errorf("TitleOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetTitleKeepsHeading(t *testing.T) {
	if got := SetTitle("## Old\ntext", "New"); got != "## New\ntext" {
		t.Errorf("got %q", got)
	}
	if got := SetTitle("Old\ntext", "New"); got != "New\ntext" {
		t.Errorf("got %q", got)
	}
}

func TestLinks(t *testing.T) {
	body := "see [[Foo]] and [[ bar baz ]] but not [[]] or [x]"
	got := Links(body)
	if len(got) != 2 || got[0] != "Foo" || got[1] != "bar baz" {
		t.Errorf("Links = %v", got)
	}
	if !HasLinkTo(body, "FOO") {
		t.Error("HasLinkTo should be case-insensitive")
	}
	if got := RewriteLinks(body, "foo", "Qux"); got != "see [[Qux]] and [[ bar baz ]] but not [[]] or [x]" {
		t.Errorf("RewriteLinks = %q", got)
	}
}

func TestLinkifyForView(t *testing.T) {
	cases := map[string]string{
		"go [[My Note]]":      "go [My Note](note:///My%20Note)",
		"T\n*see [[ Plan ]]*": "T\n*see [Plan](note:///Plan)*",
		"T\n| [[a/b (c)]] |":  "T\n| [a/b (c)](note:///a%2Fb%20%28c%29) |",
		"T\n[[*a* `b`]]":      "T\n[&#42;a&#42; &#96;b&#96;](note:///%2Aa%2A%20%60b%60)",
	}
	for in, want := range cases {
		if got := LinkifyForView(in); got != want {
			t.Errorf("LinkifyForView(%q)\n got  %q\n want %q", in, got, want)
		}
	}
}

func TestLinkifyForViewLeavesMarkdownIntact(t *testing.T) {
	unchanged := []string{
		"`[[code]]`",
		"``` go\n[[fenced]]\n```",
		"    [[indented]]",
		"[see [[x]]](https://example.com)",
		"- [ ] [[task]]", // see TestLinkifyProjectsForViewLeavesMarkdownIntact
	}
	for _, md := range unchanged {
		body := "T\n\n" + md + "\n\n[[b]]"
		want := "T\n\n" + md + "\n\n[b](note:///b)"
		if got := LinkifyForView(body); got != want {
			t.Errorf("LinkifyForView(%q)\n got  %q\n want %q", body, got, want)
		}
	}
}

func TestLinkTitleReadsBackLinkifiedTitles(t *testing.T) {
	for _, title := range []string{"Plan", "My Note", "a/b (c)", "50% #off?", "Göteborg & co"} {
		md := LinkifyForView("[[" + title + "]]")
		dest := md[strings.Index(md, "](")+2 : len(md)-1]
		u, err := url.Parse(dest)
		if err != nil {
			t.Fatalf("%q: link %q does not parse: %v", title, dest, err)
		}
		if got, ok := LinkTitle(u); !ok || got != title {
			t.Errorf("LinkTitle(%q) = %q, %v; want %q", dest, got, ok, title)
		}
	}
	u, _ := url.Parse("project://home")
	if _, ok := LinkTitle(u); ok {
		t.Error("LinkTitle accepted a project:// link")
	}
}

func TestProjects(t *testing.T) {
	cases := map[string][]string{
		"# Title\nwork on #Scribble and #home-lab today":   {"home-lab", "scribble"},
		"#Title line is skipped\n#work\n#WORK again #work": {"work"},
		"\n\n  #notitle here\nbody":                        nil,
		"T\nissue #42, url http://x.y/#anchor, a#b":        nil,
		"T\n## Heading\n# Heading\nend #a_1.":              {"a_1"},
		"T\n\t#tabbed\n#first-col":                         {"first-col", "tabbed"},
		"##\n#title-after-bare-hashes\n#tag":               {"tag"},
		"":                                                 nil,
	}
	for in, want := range cases {
		if got := Projects(in); !slices.Equal(got, want) {
			t.Errorf("Projects(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHasProject(t *testing.T) {
	body := "# Plans\nsee #Scribble"
	for _, p := range []string{"scribble", "SCRIBBLE", "#scribble"} {
		if !HasProject(body, p) {
			t.Errorf("HasProject(%q) = false", p)
		}
	}
	for _, p := range []string{"scrib", "plans", ""} {
		if HasProject(body, p) {
			t.Errorf("HasProject(%q) = true", p)
		}
	}
}

func TestLinkifyProjectsForView(t *testing.T) {
	got := LinkifyProjectsForView("# Title #x\nsee #Scribble, #42 and a#b\n#home-lab")
	want := "# Title #x\nsee [#Scribble](project://scribble), #42 and a#b\n[#home-lab](project://home-lab)"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestTitleLine(t *testing.T) {
	cases := map[string]string{
		"\n\n#scribble\n":        "#scribble",
		"  #home-lab  \nbody":    "#home-lab",
		"Roadmap\n\n#scribble\n": "Roadmap",
		"##\n# Roadmap\n#x":      "# Roadmap",
		"":                       "",
	}
	for in, want := range cases {
		if got := TitleLine(in); got != want {
			t.Errorf("TitleLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLinkifyProjectsForViewLeavesMarkdownIntact(t *testing.T) {
	// Tags inside links, link destinations, reference definitions, code and
	// HTML stay as they are; only the plain #b is linked.
	unchanged := []string{
		"[See #home](https://example.com)",
		"[docs]( #overview)",
		"[docs](\n#overview\n)",
		"![pic #x](img.png)",
		"[ref #x][r]\n\n[r]: https://example.com",
		"[See #home]\n\n[See #home]: https://example.com",
		"[See\n#home](https://example.com)",
		"[docs]: #overview",
		"[target]:\n  #overview",
		"   [docs]: <#overview> \"title\"",
		"`#code`",
		"``` go\n#fenced\n```",
		"~~~\n#fenced\n~~~",
		"    #indented code",
		`<span title=" #x">hi</span>`,
		"_see #home_", // the regex would take the closing '_' too
		// Fyne renders a task item from the one text node after its
		// checkbox; a link there panics it or drops the rest.
		"- [ ] #home",
		"- [x] do #home more",
	}
	for _, md := range unchanged {
		// Blank lines: definitions and indented code cannot interrupt a
		// paragraph, so right after "T" they would be plain text.
		body := "T\n\n" + md + "\n\n#b"
		want := "T\n\n" + md + "\n\n[#b](project://b)"
		if got := LinkifyProjectsForView(body); got != want {
			t.Errorf("LinkifyProjectsForView(%q)\n got  %q\n want %q", body, got, want)
		}
	}
}

func TestLinkifyProjectsForViewLinksTagsInPlainText(t *testing.T) {
	cases := map[string]string{
		// Brackets without a matching definition are plain text.
		"T\n[TODO #home]":         "T\n[TODO [#home](project://home)]",
		"T\n*see #home*":          "T\n*see [#home](project://home)*",
		"T\n## Part #home":        "T\n## Part [#home](project://home)",
		"T\n| a |\n| - |\n| #x |": "T\n| a |\n| - |\n| [#x](project://x) |",
	}
	for in, want := range cases {
		if got := LinkifyProjectsForView(in); got != want {
			t.Errorf("LinkifyProjectsForView(%q)\n got  %q\n want %q", in, got, want)
		}
	}
}

func TestAutoLink(t *testing.T) {
	titles := []string{"Go", "Go Tips", "Shopping", "C++", "Self"}
	cases := map[string]struct{ in, want string }{
		"plain mention":       {"# T\nbuy from Shopping list", "# T\nbuy from [[Shopping]] list"},
		"longest match wins":  {"# T\nread Go Tips now", "# T\nread [[Go Tips]] now"},
		"falls back to short": {"# T\nGo Tipsy", "# T\n[[Go]] Tipsy"},
		"case-insensitive":    {"# T\nsee go tips and SHOPPING", "# T\nsee [[go tips]] and [[SHOPPING]]"},
		"word boundaries":     {"# T\nGoogle, Gopher, ago, Go_x, Go2", "# T\nGoogle, Gopher, ago, Go_x, Go2"},
		"unicode boundary":    {"# T\nGoä and Go!", "# T\nGoä and [[Go]]!"},
		"punctuated title":    {"# T\nI like C++.", "# T\nI like [[C++]]."},
		"every mention":       {"# T\nGo, then Go", "# T\n[[Go]], then [[Go]]"},
		"existing links":      {"# T\n[[Go Tips]] and [[go]] and [[Shopping list]]", "# T\n[[Go Tips]] and [[go]] and [[Shopping list]]"},
		"self link":           {"# Self\nSelf reference to Go", "# Self\nSelf reference to [[Go]]"},
		"title line":          {"# Go Tips\nnothing", "# Go Tips\nnothing"},
		"urls":                {"# T\nhttps://example.com/Go and www.go.dev/Go", "# T\nhttps://example.com/Go and www.go.dev/Go"},
		"code":                {"# T\n`Go` and\n\n```\nGo\n```\n", "# T\n`Go` and\n\n```\nGo\n```\n"},
		"markdown links":      {"# T\n[Go](http://x) and <http://go>", "# T\n[Go](http://x) and <http://go>"},
		"emphasis":            {"# T\n*Go* and **Shopping**", "# T\n*[[Go]]* and **[[Shopping]]**"},
		// Fyne cannot render a link in a task item (see plainTextSpans).
		"task item":       {"# T\n- [ ] Shopping\n- Shopping", "# T\n- [ ] Shopping\n- [[Shopping]]"},
		"project tag":     {"# T\n#go and #shopping-list", "# T\n#go and #shopping-list"},
		"email":           {"# T\nmail go@example.com or me@go.dev", "# T\nmail go@example.com or me@go.dev"},
		"bracketed":       {"# T\n[Go] and [Shopping]: x", "# T\n[Go] and [Shopping]: x"},
		"inside brackets": {"# T\n[Learn Go today] then Go", "# T\n[Learn Go today] then [[Go]]"},
		"combining mark":  {"# T\nGöteborg", "# T\nGöteborg"},
		"html text":       {"# T\n<b>Go</b>", "# T\n<b>[[Go]]</b>"},
		"heading & table": {"# T\n## Go\n| Go |\n| - |", "# T\n## [[Go]]\n| [[Go]] |\n| - |"},
	}
	for name, tc := range cases {
		if got, _ := AutoLink(tc.in, titles); got != tc.want {
			t.Errorf("%s: AutoLink(%q)\n got  %q\n want %q", name, tc.in, got, tc.want)
		}
	}
	if got, n := AutoLink("# T\nGo", nil); got != "# T\nGo" || n != 0 {
		t.Errorf("no titles: got %q, %d", got, n)
	}
}

func TestAutoLinkOverlaps(t *testing.T) {
	cases := []struct {
		titles   []string
		in, want string
		n        int
	}{
		// The longest title wins even when a shorter one starts earlier.
		{[]string{"New York", "York City Hall"}, "T\nNew York City Hall", "T\nNew [[York City Hall]]", 1},
		{[]string{"A B", "B C D"}, "T\nA B C D", "T\nA [[B C D]]", 1},
		// Equal lengths: the earlier one wins; what is left can still match.
		{[]string{"A B", "B C", "C"}, "T\nA B C", "T\n[[A B]] [[C]]", 2},
		// Length is counted in characters: the Kelvin sign is 3 bytes.
		{[]string{"K A", "A BC"}, "T\nK A BC", "T\nK [[A BC]]", 1},
	}
	for _, tc := range cases {
		got, n := AutoLink(tc.in, tc.titles)
		if got != tc.want || n != tc.n {
			t.Errorf("AutoLink(%q, %q) = %q, %d; want %q, %d", tc.in, tc.titles, got, n, tc.want, tc.n)
		}
	}
}

func TestAutoLinkFoldsCase(t *testing.T) {
	// The store folds only ASCII case when it resolves a link, so a mention
	// that differs from the title in any other way links the title's spelling.
	cases := map[string]struct{ title, in, want string }{
		"ascii case":   {"Straße", "T\nSTRAßE", "T\n[[STRAßE]]"},
		"sharp s":      {"Straße", "T\nSTRAẞE", "T\n[[Straße]]"},
		"kelvin sign":  {"kilo", "T\nKilo", "T\n[[kilo]]"},
		"accents":      {"École", "T\nécole", "T\n[[École]]"},
		"bad title":    {"a]]b", "T\na]]b", "T\na]]b"},
		"newline only": {"\n", "T\nx", "T\nx"},
	}
	for name, tc := range cases {
		if got, _ := AutoLink(tc.in, []string{tc.title}); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestAutoLinkPrefersTitleAsWritten(t *testing.T) {
	// "École" and "école" can both exist, since the store folds only ASCII
	// case. A mention links the one it spells, whatever the title order.
	for _, titles := range [][]string{{"École", "école"}, {"école", "École"}} {
		for in, want := range map[string]string{
			"T\nécole": "T\n[[école]]",
			"T\nÉCOLE": "T\n[[ÉCOLE]]",
		} {
			if got, _ := AutoLink(in, titles); got != want {
				t.Errorf("AutoLink(%q, %q) = %q, want %q", in, titles, got, want)
			}
		}
	}
}

func TestFlattenTaskItemsForView(t *testing.T) {
	cases := map[string]string{
		// Untouched cases
		"plain text without tasks": "T\n- regular bullet\n- another bullet",
		"code block with task":     "T\n```\n- [ ] **not a task**\n```",

		// Issue #11 cases
		"bold item":          "T\n- [ ] **bold** item",
		"link item":          "T\n- [ ] [a link](https://example.com) item",
		"code item":          "T\n- [ ] ``  code  `` item",
		"plain then bold":    "T\n- [ ] plain then **bold**",
		"checked bold":       "T\n- [x] **bold** item",
		"nested task":        "T\n- item\n  - [ ] **nested** task",
		"multiline task":     "T\n- [ ] first line\n  second line",
		"mixed formatting":   "T\n- [ ] *italic* and `code` and ~~strike~~",
		"autolink":           "T\n- [ ] <https://example.com> rest",
		"special characters": "T\n- [ ] *a* & <b> [c]",
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			out := FlattenTaskItemsForView(in)
			// Must never panic in Fyne's markdown renderer
			rt := widget.NewRichTextFromMarkdown(out)
			if rt == nil {
				t.Fatal("NewRichTextFromMarkdown returned nil")
			}
		})
	}

	// Verify the exact text extracted on checkboxes
	body := "# Notes\n\n- [ ] **bold** item\n- [ ] [a link](https://example.com) item\n- [ ] ``  code  `` item\n- [ ] plain then **bold**\n"
	rewritten := FlattenTaskItemsForView(body)
	rt := widget.NewRichTextFromMarkdown(rewritten)
	list, ok := rt.Segments[2].(*widget.ListSegment)
	if !ok {
		t.Fatalf("expected ListSegment at segment 2, got %T", rt.Segments[2])
	}
	wants := []string{
		"bold item",
		"a link item",
		"code item",
		"plain then bold",
	}
	for i, want := range wants {
		p := list.Items[i].(*widget.ParagraphSegment)
		cb := p.Texts[0].(*widget.CheckBoxSegment)
		if cb.Text != want {
			t.Errorf("item %d = %q, want %q", i, cb.Text, want)
		}
	}
}
