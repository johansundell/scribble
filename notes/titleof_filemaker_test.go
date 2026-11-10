package notes

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"testing"
	"unicode"
)

// The FileMaker custom function Scribble_TitleOf (#67) mirrors TitleOf. It
// can't run here, so these tests pin the parts Go can see: the shared case
// table matches TitleOf, the FileMaker test script carries the same table,
// and the function's whitespace set is Go's.

const (
	titleOfCasesFile  = "../docs/filemaker/titleof-cases.json"
	titleOfFunction   = "../docs/filemaker/CustomFunctions/Scribble_TitleOf.fmfn"
	titleOfTestScript = "../docs/filemaker/scripts/Scribble Test TitleOf.xml"
)

type titleOfCase struct {
	Name  string `json:"name"`
	Body  string `json:"body"`
	Title string `json:"title"`
}

func readTitleOfCases(t *testing.T, data []byte) []titleOfCase {
	t.Helper()
	var cases []titleOfCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse cases: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	return cases
}

func TestTitleOfSharedCases(t *testing.T) {
	data, err := os.ReadFile(titleOfCasesFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range readTitleOfCases(t, data) {
		if got := TitleOf(c.Body); got != c.Title {
			t.Errorf("%s: TitleOf(%q) = %q, want %q", c.Name, c.Body, got, c.Title)
		}
	}
}

func TestTitleOfFileMakerTestScriptHasSharedCases(t *testing.T) {
	data, err := os.ReadFile(titleOfCasesFile)
	if err != nil {
		t.Fatal(err)
	}
	want := readTitleOfCases(t, data)

	raw, err := os.ReadFile(titleOfTestScript)
	if err != nil {
		t.Fatal(err)
	}
	var snippet struct {
		Steps []struct {
			Name  string `xml:"name,attr"`
			Text  string `xml:"Text"`
			Field string `xml:"Field"`
		} `xml:"Script>Step"`
	}
	if err := xml.Unmarshal(raw, &snippet); err != nil {
		t.Fatalf("parse script XML: %v", err)
	}
	for _, s := range snippet.Steps {
		if s.Name == "Insert Text" && s.Field == "$cases" {
			if got := readTitleOfCases(t, []byte(s.Text)); !reflect.DeepEqual(got, want) {
				t.Errorf("the test script's cases differ from %s; copy the file into its Insert Text step", titleOfCasesFile)
			}
			return
		}
	}
	t.Fatal("no Insert Text step targeting $cases in the test script")
}

func TestTitleOfFileMakerWhitespaceMatchesGo(t *testing.T) {
	src, err := os.ReadFile(titleOfFunction)
	if err != nil {
		t.Fatal(err)
	}
	def := regexp.MustCompile(`(?s)whitespace\s*=\s*(.*?);`).FindSubmatch(src)
	if def == nil {
		t.Fatal("no `whitespace = ...;` definition in the function")
	}
	var got []rune
	for _, m := range regexp.MustCompile(`Char \( (\d+) \)`).FindAllSubmatch(def[1], -1) {
		n, _ := strconv.Atoi(string(m[1]))
		got = append(got, rune(n))
	}
	slices.Sort(got)

	var want []rune
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if unicode.IsSpace(r) {
			want = append(want, r)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("whitespace code points = %v, want unicode.IsSpace's %v", got, want)
	}
}
