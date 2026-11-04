package auth_test

import (
	"testing"
	"time"

	"github.com/johansundell/scribble/auth"
)

// rfcKey is the SHA-1 key of RFC 6238's test vectors.
var rfcKey = []byte("12345678901234567890")

func TestCode_RFC6238Vectors(t *testing.T) {
	// RFC 6238 appendix B gives 8 digits; the last 6 are the 6-digit code.
	for _, tc := range []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	} {
		if got := auth.Code(rfcKey, time.Unix(tc.unix, 0)); got != tc.want {
			t.Errorf("Code at %d = %s, want %s", tc.unix, got, tc.want)
		}
	}
}

func TestDecodeSecret(t *testing.T) {
	const canonical = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	for _, in := range []string{
		canonical,
		"gezdgnbvgy3tqojqgezdgnbvgy3tqojq",
		"GEZD GNBV GY3T QOJQ GEZD GNBV GY3T QOJQ",
		" " + canonical + "\n",
	} {
		key, err := auth.DecodeSecret(in)
		if err != nil || string(key) != string(rfcKey) {
			t.Errorf("DecodeSecret(%q) = %q, %v", in, key, err)
		}
	}
	for _, in := range []string{"", "not base32!", "GEZDGNBVGY3TQOJQ"} {
		if _, err := auth.DecodeSecret(in); err == nil {
			t.Errorf("DecodeSecret(%q): expected an error", in)
		}
	}
	if got := auth.EncodeSecret(rfcKey); got != canonical {
		t.Errorf("EncodeSecret = %s, want %s", got, canonical)
	}
}
