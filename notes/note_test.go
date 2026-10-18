package notes

import (
	"slices"
	"testing"
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
	if got := LinkifyForView("go [[My Note]]"); got != "go [My Note](note://My%20Note)" {
		t.Errorf("got %q", got)
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
