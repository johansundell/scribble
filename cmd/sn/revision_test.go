package main

import "testing"

func TestParseRevision(t *testing.T) {
	for in, want := range map[string]revision{"12": 12, "r12": 12, "R3": 3} {
		if got, err := parseRevision(in); err != nil || got != want {
			t.Errorf("parseRevision(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "r", "x1", "0", "-2", "r1.5"} {
		if _, err := parseRevision(in); err == nil {
			t.Errorf("parseRevision(%q) should fail", in)
		}
	}
}
