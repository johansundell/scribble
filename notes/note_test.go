package notes

import "testing"

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
