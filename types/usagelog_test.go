package types

import (
	"encoding/json"
	"testing"
)

func TestRawJSON_MarshalJSON(t *testing.T) {
	cases := []struct {
		name string
		in   RawJSON
		want string
	}{
		{"empty", "", `null`},
		{"object", `{"a":1}`, `{"a":1}`},
		{"array", `[1,2]`, `[1,2]`},
		{"json string", `"hi"`, `"hi"`},
		{"number", `42`, `42`},
		{"plain text", "hello", `"hello"`},
		{"error body", "Not Found", `"Not Found"`},
		{"truncated json", `{"a":`, `"{\"a\":"`},
		{"invalid utf-8", RawJSON([]byte{'a', 0xff, 'b'}), `"a�b"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// A UsageLog with non-JSON bodies must encode to valid JSON.
func TestUsageLog_NonJSONBodiesEncode(t *testing.T) {
	out, err := json.Marshal([]UsageLog{{Request: "hello", Response: "Bad Request"}, {Request: `{"ok":true}`, Response: ""}})
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", out)
	}
}
