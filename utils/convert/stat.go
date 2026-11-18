package convert

import "io"

// Stat holds statistics about the content being analyzed.
type Stat struct {
	NUL, LoneCR, LoneLF, CRLF uint
	Printable, NonPrintable   uint
}

// IsBinary detects if data is binary based on:
// https://git.kernel.org/pub/scm/git/git.git/tree/convert.c?id=HEAD#n94
func (s Stat) IsBinary() bool {
	if s.NUL > 0 {
		return true
	}
	if s.LoneCR > 0 {
		return true
	}

	mostlyPrintable := (s.Printable >> 7) >= s.NonPrintable

	return !mostlyPrintable
}

// GetStat returns [Stat] of the reader based on:
// https://git.kernel.org/pub/scm/git/git.git/tree/convert.c?id=HEAD#n45
func GetStat(r io.Reader) (stat Stat, err error) {
	var hadCR bool
	var last byte

	buf := make([]byte, 8<<10)

	for {
		n, err := r.Read(buf)
		for _, b := range buf[:n] {
			last = b

			if b != '\n' && hadCR {
				// CR not followed by LF. Lone CR is considered binary.
				stat.LoneCR++
				hadCR = false
			}

			switch {
			case b == '\n':
				if hadCR {
					stat.CRLF++
					hadCR = false
				} else {
					stat.LoneLF++
				}
			case b == '\r':
				hadCR = true
			case b == 127: // DEL
				stat.NonPrintable++
			case b < 32:
				switch b {
				case '\b', '\t', '\033', '\014': // BS, HT, ESC and FF
					stat.Printable++
				case 0:
					stat.NUL++
				default:
					stat.NonPrintable++
				}
			default:
				stat.Printable++
			}
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return Stat{}, err
		}
	}

	if hadCR {
		// Last byte is lone CR.
		stat.LoneCR++
	}

	// If file ends with EOF then don't count this EOF as non-printable.
	if last == '\032' {
		stat.NonPrintable--
	}

	return stat, nil
}
