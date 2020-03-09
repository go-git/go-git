package git

import (
	"fmt"
	"strings"
)

// preReceiveHook returns the bytes of a pre-receive hook script
// that prints m before exiting successfully
func preReceiveHook(m string) []byte {
	return []byte(fmt.Sprintf("#!/bin/rc\necho -n %s\n", quote(m)))
}

const quoteChar = '\''

func needsQuote(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == quoteChar || c <= ' ' { // quote, blanks, or control characters
			return true
		}
	}
	return false
}

// Quote adds single quotes to s in the style of rc(1) if they are needed.
// The behaviour should be identical to Plan 9's quote(3).
func quote(s string) string {
	if s == "" {
		return "''"
	}
	if !needsQuote(s) {
		return s
	}
	var b strings.Builder
	b.Grow(10 + len(s)) // Enough room for few quotes
	b.WriteByte(quoteChar)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == quoteChar {
			b.WriteByte(quoteChar)
		}
		b.WriteByte(c)
	}
	b.WriteByte(quoteChar)
	return b.String()
}
