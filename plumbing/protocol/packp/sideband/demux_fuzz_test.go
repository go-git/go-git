package sideband

import (
	"bytes"
	"testing"
)

// FuzzSidebandSanitize keeps its invariants inside the fuzz function: the
// OSS-Fuzz build strips every other top-level declaration from this file
// before compiling the target.
func FuzzSidebandSanitize(f *testing.F) {
	f.Add([]byte("Counting objects: 100% (3/3), done.\r\n"))
	f.Add([]byte("\x1b[2J\x1b[H\x1b[3A"))
	f.Add([]byte("\x1b[1;31merror\x1b[m\n\x1b[38:2::255:0:0m"))
	f.Add([]byte("\x1b]0;title\x07\x1b[31;xm\x1b["))
	f.Add([]byte{0x00, 0x08, 0x7f, 0xc3, 0xa9, 0xff})

	f.Fuzz(func(t *testing.T, message []byte) {
		out := sanitize(message)

		for i := 0; i < len(out); i++ {
			switch c := out[i]; {
			case c == '\t', c == '\n', c == '\r':
			case c == 0x1b:
				n := colorSequenceLen(out[i:])
				if n == 0 {
					t.Fatalf("sanitize(%q) = %q: bare escape left at %d", message, out, i)
				}
				i += n - 1
			case c < 0x20, c == 0x7f:
				t.Fatalf("sanitize(%q) = %q: control byte %#x left at %d", message, out, c, i)
			}
		}

		if again := sanitize(out); !bytes.Equal(again, out) {
			t.Fatalf("sanitize(%q) = %q, but sanitizing it again gives %q", message, out, again)
		}

		nothingToMask := true
		for _, c := range message {
			if c == '\t' || c == '\n' || c == '\r' {
				continue
			}
			if c < 0x20 || c == 0x7f {
				nothingToMask = false
				break
			}
		}
		if nothingToMask && !bytes.Equal(out, message) {
			t.Fatalf("sanitize(%q) = %q: a message with nothing to mask should come back unchanged", message, out)
		}
	})
}
