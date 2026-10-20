package main

import "testing"

func TestUnifiedDiff(t *testing.T) {
	cases := map[string]struct {
		a, b, want string
	}{
		"same text": {"# A\nx\n", "# A\nx\n", ""},
		"one line changed": {
			"# A\none\ntwo\nthree\n", "# A\none\n2\nthree\n",
			"--- A r1\n+++ A r2\n@@ -1,4 +1,4 @@\n # A\n one\n-two\n+2\n three\n",
		},
		"added at end": {
			"# A\n", "# A\nnew\n",
			"--- A r1\n+++ A r2\n@@ -1 +1,2 @@\n # A\n+new\n",
		},
		"from empty": {
			"", "# A\n",
			"--- A r1\n+++ A r2\n@@ -0,0 +1 @@\n+# A\n",
		},
		"removed from the middle": {
			"1\n2\n3\n4\n5\n6\n7\n", "1\n2\n3\n5\n6\n7\n",
			"--- A r1\n+++ A r2\n@@ -1,7 +1,6 @@\n 1\n 2\n 3\n-4\n 5\n 6\n 7\n",
		},
		"two hunks": {
			"a\n1\n2\n3\n4\n5\n6\n7\n8\nb\n", "A\n1\n2\n3\n4\n5\n6\n7\n8\nB\n",
			"--- A r1\n+++ A r2\n@@ -1,4 +1,4 @@\n-a\n+A\n 1\n 2\n 3\n@@ -7,4 +7,4 @@\n 6\n 7\n 8\n-b\n+B\n",
		},
		"newline added at end": {
			"x", "x\n",
			"--- A r1\n+++ A r2\n@@ -1 +1 @@\n-x\n\\ No newline at end of file\n+x\n",
		},
		"close changes share a hunk": {
			"a\n1\n2\n3\n4\n5\n6\nb\n", "A\n1\n2\n3\n4\n5\n6\nB\n",
			"--- A r1\n+++ A r2\n@@ -1,8 +1,8 @@\n-a\n+A\n 1\n 2\n 3\n 4\n 5\n 6\n-b\n+B\n",
		},
	}
	for name, tc := range cases {
		if got := unifiedDiff("A r1", "A r2", tc.a, tc.b, false); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", name, got, tc.want)
		}
	}
}

func TestUnifiedDiffColour(t *testing.T) {
	got := unifiedDiff("A r1", "A r2", "x\n", "y\n", true)
	want := "\x1b[1m--- A r1\x1b[0m\n\x1b[1m+++ A r2\x1b[0m\n\x1b[36m@@ -1 +1 @@\x1b[0m\n\x1b[31m-x\x1b[0m\n\x1b[32m+y\x1b[0m\n"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
