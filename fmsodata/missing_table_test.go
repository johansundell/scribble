package fmsodata

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsMissingTable(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"table missing":   {&StatusError{StatusCode: 404, Body: `{"error":{"code":"-1020","message":"Table 'Notes' not defined in database"}}`}, true},
		"wrapped":         {fmt.Errorf("check: %w", &StatusError{StatusCode: 404, Body: `{"error":{"code":"-1020"}}`}), true},
		"record missing":  {&StatusError{StatusCode: 404, Body: `{"error":{"code":"-1023"}}`}, false},
		"no body":         {&StatusError{StatusCode: 404}, false},
		"other status":    {&StatusError{StatusCode: 400, Body: `{"error":{"code":"-1020"}}`}, false},
		"network failure": {errors.New("connection refused"), false},
		"nil":             {nil, false},
	}
	for name, c := range cases {
		if got := IsMissingTable(c.err); got != c.want {
			t.Errorf("%s: IsMissingTable = %v, want %v", name, got, c.want)
		}
	}
}
