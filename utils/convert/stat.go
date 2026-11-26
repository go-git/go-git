package convert

import "io"

// Stat holds statistics about the content being analyzed.
type Stat struct {
	NUL, LoneCR, LoneLF, CRLF uint
	Printable, NonPrintable   uint
}

// IsBinary reports whether the content the statistics describe is binary,
// as opposed to text that line-ending conversion applies to.
//
// Text has no NUL, no CR without an LF after it, and at most one
// non-printable byte for every 128 printable ones.
func (s Stat) IsBinary() bool {
	return s.NUL > 0 || s.LoneCR > 0 || s.NonPrintable > s.Printable/128
}

const (
	asciiDEL = 0x7f
	// asciiSUB is Ctrl-Z, which DOS tools left at the end of text files.
	asciiSUB = 0x1a
)

// GetStat reads r to the end and returns the statistics of its content.
//
// A CR directly followed by an LF counts as one CRLF; any other CR or LF
// counts as a lone one. Every remaining byte is NUL, printable or
// non-printable. A Ctrl-Z that ends the content is not counted.
func GetStat(r io.Reader) (stat Stat, err error) {
	var (
		buf       = make([]byte, 4096)
		pendingCR bool
		last      byte
	)

	for {
		n, readErr := r.Read(buf)
		for _, b := range buf[:n] {
			if pendingCR {
				pendingCR = false
				if b == '\n' {
					stat.CRLF++
					continue
				}
				stat.LoneCR++
			}

			switch {
			case b == '\r':
				pendingCR = true
			case b == '\n':
				stat.LoneLF++
			case b == 0:
				stat.NUL++
			case isPrintable(b):
				stat.Printable++
			default:
				stat.NonPrintable++
			}
		}
		if n > 0 {
			last = buf[n-1]
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return Stat{}, readErr
		}
	}

	if pendingCR {
		stat.LoneCR++
	}
	if last == asciiSUB {
		stat.NonPrintable--
	}

	return stat, nil
}

// isPrintable reports whether b counts towards the printable bytes of text
// content: everything from space up except DEL, which includes all bytes
// of multi-byte encodings, and the control characters backspace, tab, form
// feed and escape.
func isPrintable(b byte) bool {
	switch b {
	case '\b', '\t', '\f', '\x1b':
		return true
	}
	return b >= ' ' && b != asciiDEL
}
