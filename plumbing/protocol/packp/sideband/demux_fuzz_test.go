package sideband

import (
	"bytes"
	"testing"
)

func FuzzSidebandSanitize(f *testing.F) {
	f.Add([]byte("Counting objects: 100% (3/3), done.\r\n"))
	f.Add([]byte("\x1b[2J\x1b[H\x1b[3A"))
	f.Add([]byte("\x1b[1;31merror\x1b[m\n\x1b[38:2::255:0:0m"))
	f.Add([]byte("\x1b]0;title\x07\x1b[31;xm\x1b["))
	f.Add([]byte{0x00, 0x08, 0x7f, 0xc3, 0xa9, 0xff})

	f.Fuzz(func(t *testing.T, message []byte) {
		out := sanitize(message)

		if bytes.Equal(out, message) != !hasMaskedByte(message) {
			t.Fatalf("sanitize(%q) = %q: only messages carrying a masked byte should change", message, out)
		}
		if again := sanitize(out); !bytes.Equal(again, out) {
			t.Fatalf("sanitize(%q) = %q, but sanitizing it again gives %q", message, out, again)
		}
		for i := 0; i < len(out); i++ {
			if n := allowedControlLen(out[i:]); n > 0 {
				i += n - 1
			} else if out[i] < 0x20 || out[i] == 0x7f {
				t.Fatalf("sanitize(%q) = %q: control byte %#x left at %d", message, out, out[i], i)
			}
		}
	})
}

// hasMaskedByte reports whether message holds a control byte that sanitize
// masks: anything but tab, line feed, carriage return, and the bytes of an ANSI
// SGR color sequence.
func hasMaskedByte(message []byte) bool {
	for i := 0; i < len(message); i++ {
		if n := allowedControlLen(message[i:]); n > 0 {
			i += n - 1
		} else if message[i] < 0x20 || message[i] == 0x7f {
			return true
		}
	}
	return false
}

// allowedControlLen returns how many bytes at the start of b sanitize passes
// through although they begin with a control byte: one for tab, line feed and
// carriage return, the whole sequence for ESC [ <digits, colons, semicolons> m,
// and 0 for anything else.
func allowedControlLen(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	switch b[0] {
	case '\t', '\n', '\r':
		return 1
	case 0x1b:
		if len(b) < 3 || b[1] != '[' {
			return 0
		}
		for i := 2; i < len(b); i++ {
			switch c := b[i]; {
			case c == 'm':
				return i + 1
			case c >= '0' && c <= '9', c == ':', c == ';':
			default:
				return 0
			}
		}
	}
	return 0
}
