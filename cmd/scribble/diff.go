package main

import (
	"fmt"
	"strconv"
	"strings"
)

// diffContext is how many unchanged lines a hunk shows around a change.
const diffContext = 3

// diffOp is one line of an edit script: ' ' kept, '-' removed, '+' added.
type diffOp struct {
	kind byte
	line string
}

// unifiedDiff returns a unified diff from a to b with diffContext lines of
// context, or "" when they are the same. Headers name aName and bName. With
// colour, lines get ANSI colours.
func unifiedDiff(aName, bName, a, b string, colour bool) string {
	ops := lineDiff(splitLines(a), splitLines(b))
	paint := func(code, s string) string {
		if !colour {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}
	var out strings.Builder
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		// A hunk runs from diffContext lines before this change to
		// diffContext lines after the last change that is no more than
		// 2*diffContext unchanged lines from the one before it.
		start := max(i-diffContext, 0)
		end, kept := i, 0
		for j := i; j < len(ops) && kept <= 2*diffContext; j++ {
			if ops[j].kind == ' ' {
				kept++
			} else {
				end, kept = j, 0
			}
		}
		end = min(end+1+diffContext, len(ops))

		if out.Len() == 0 {
			out.WriteString(paint("1", "--- "+aName) + "\n")
			out.WriteString(paint("1", "+++ "+bName) + "\n")
		}
		aStart, bStart := lineNumbers(ops[:start])
		aLen, bLen := lineNumbers(ops[start:end])
		out.WriteString(paint("36", fmt.Sprintf("@@ -%s +%s @@", hunkRange(aStart, aLen), hunkRange(bStart, bLen))) + "\n")
		for _, op := range ops[start:end] {
			line, noEOL := strings.CutSuffix(string(op.kind)+op.line, noNewline)
			switch op.kind {
			case '-':
				line = paint("31", line)
			case '+':
				line = paint("32", line)
			}
			out.WriteString(line + "\n")
			if noEOL {
				out.WriteString("\\ No newline at end of file\n")
			}
		}
		i = end
	}
	return out.String()
}

// lineNumbers counts the lines ops covers in the old and the new text.
func lineNumbers(ops []diffOp) (a, b int) {
	for _, op := range ops {
		if op.kind != '+' {
			a++
		}
		if op.kind != '-' {
			b++
		}
	}
	return a, b
}

// hunkRange writes a hunk's range as diff -u does: the first line and the
// count, the count left out when it is 1, and the line before when it is 0.
func hunkRange(before, n int) string {
	switch n {
	case 0:
		return fmt.Sprintf("%d,0", before)
	case 1:
		return strconv.Itoa(before + 1)
	}
	return fmt.Sprintf("%d,%d", before+1, n)
}

// noNewline marks a last line that has no newline, so that it differs
// from the same line with one, as in diff -u.
const noNewline = "\x00no newline"

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	if !strings.HasSuffix(s, "\n") {
		s += noNewline + "\n"
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// lineDiff returns an edit script from a to b, using the longest common
// subsequence of the lines left once the common start and end are trimmed.
func lineDiff(a, b []string) []diffOp {
	var head, tail []diffOp
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		head = append(head, diffOp{' ', a[0]})
		a, b = a[1:], b[1:]
	}
	for len(a) > 0 && len(b) > 0 && a[len(a)-1] == b[len(b)-1] {
		tail = append([]diffOp{{' ', a[len(a)-1]}}, tail...)
		a, b = a[:len(a)-1], b[:len(b)-1]
	}

	// lcs[i][j] is the LCS length of a[i:] and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	ops := head
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i, j = i+1, j+1
		case j == len(b) || (i < len(a) && lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	return append(ops, tail...)
}
