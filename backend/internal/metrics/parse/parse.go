// Package parse turns the text of /proc and /sys files into numbers. It is
// pure: no file access, no network, no clock. Everything it reads is untrusted,
// so every parser is bounded (lines, fields, digits, value ranges) and returns
// "not ok" for input it cannot read instead of guessing. The parsers are fuzzed.
package parse

import (
	"bytes"
	"math"
	"strconv"
)

const (
	// maxLines is the most lines any parser looks at. The real files are far
	// smaller; a larger input is hostile or broken and is cut off.
	maxLines = 8192
	// maxCounter is the largest counter value a parser accepts. Real values
	// stay far below it (jiffies of 4 cores for centuries); larger numbers are
	// treated as garbage, and sums of a few of them cannot overflow uint64.
	maxCounter = 1 << 56
)

// lines splits data into at most maxLines lines.
func lines(data []byte) [][]byte {
	out := bytes.SplitN(data, []byte{'\n'}, maxLines+1)
	if len(out) > maxLines {
		out = out[:maxLines]
	}
	return out
}

// fields splits a line at ASCII blanks. It is bytes.Fields without the
// Unicode space handling, which these kernel files never use.
func fields(line []byte) []string {
	var out []string
	start := -1
	for i, c := range line {
		blank := c == ' ' || c == '\t' || c == '\r'
		switch {
		case !blank && start < 0:
			start = i
		case blank && start >= 0:
			out = append(out, string(line[start:i]))
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, string(line[start:]))
	}
	return out
}

// counter parses a non-negative decimal counter up to maxCounter.
func counter(s string) (uint64, bool) {
	if s == "" || len(s) > 19 || s[0] == '+' || s[0] == '-' {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil || v > maxCounter {
		return 0, false
	}
	return v, true
}

// finite parses a decimal number that is finite and within [0, max].
func finite(s string, max float64) (float64, bool) {
	if s == "" || len(s) > 32 {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > max {
		return 0, false
	}
	return v, true
}
