package main

import (
	"fmt"
	"strconv"
	"strings"
)

// revision is a note's revision number, shown as r12.
type revision int64

// String returns r as it is shown, such as r12.
func (r revision) String() string {
	return "r" + strconv.FormatInt(int64(r), 10)
}

// parseRevision reads a revision written as 12 or r12.
func parseRevision(s string) (revision, error) {
	n, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(s), "r"), 10, 64)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%q is not a revision (write 12 or r12)", s)
	}
	return revision(n), nil
}
