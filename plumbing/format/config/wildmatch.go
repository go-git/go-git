package config

import "bytes"

// Return values mirroring wildmatch.c, so that the "**" backtracking
// short-circuits behave the same way.
const (
	wmAbortAll        = -1
	wmAbortToStarStar = -2
	wmMatch           = 0
	wmNoMatch         = 1
)

// wildmatch reports whether text matches pattern using Git's wildmatch
// rules with WM_PATHNAME always enabled, which is how git-config(1)
// evaluates "gitdir:" conditions:
//
//   - '*' and '?' match anything except '/'.
//   - '**' matches across '/', but only when it forms a whole path
//     component ("**/foo", "foo/**", "foo/**/bar" or a bare "**").
//   - '[...]' supports ranges, negation via '!' or '^', and POSIX
//     character classes such as [:alpha:].
//   - '\' escapes the following character.
//
// When icase is true the comparison folds ASCII case, matching
// wildmatch's WM_CASEFOLD flag.
func wildmatch(pattern, text string, icase bool) bool {
	return doWildmatch([]byte(pattern), []byte(text), icase) == wmMatch
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

func upperASCII(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}

func isAlnumASCII(c byte) bool {
	return isAlphaASCII(c) || (c >= '0' && c <= '9')
}

func isAlphaASCII(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// doWildmatch is a transliteration of dowild() from git's wildmatch.c.
// It walks pattern and text in lockstep, recursing at '*' to try every
// possible split point.
func doWildmatch(p, text []byte, icase bool) int {
	pi, ti := 0, 0

	for pi < len(p) {
		pCh := p[pi]
		var tCh byte
		atEnd := ti >= len(text)
		if atEnd {
			// Only '*' can match the empty remainder; anything else
			// means no suffix of text can ever match.
			if pCh != '*' {
				return wmAbortAll
			}
		} else {
			tCh = text[ti]
		}
		if icase {
			pCh = lowerASCII(pCh)
			tCh = lowerASCII(tCh)
		}

		switch pCh {
		case '\\':
			pi++
			if pi >= len(p) {
				return wmNoMatch
			}
			lit := p[pi]
			if icase {
				lit = lowerASCII(lit)
			}
			if tCh != lit {
				return wmNoMatch
			}
			pi++
			ti++

		case '?':
			if tCh == '/' {
				return wmNoMatch
			}
			pi++
			ti++

		case '[':
			m, np := matchBracket(p, pi, tCh, icase)
			if m == wmAbortAll {
				return wmAbortAll
			}
			if m == wmNoMatch || tCh == '/' {
				return wmNoMatch
			}
			pi = np
			ti++

		case '*':
			matchSlash := false
			pi++

			if pi < len(p) && p[pi] == '*' {
				// Index of the character preceding the first '*'.
				prevIdx := pi - 2
				for pi < len(p) && p[pi] == '*' {
					pi++
				}

				prevOK := prevIdx < 0 || p[prevIdx] == '/'
				nextOK := pi >= len(p) || p[pi] == '/' ||
					(pi+1 < len(p) && p[pi] == '\\' && p[pi+1] == '/')

				if prevOK && nextOK {
					// A whole-component "**": it may also match zero
					// components, so try skipping the trailing slash.
					if pi < len(p) && p[pi] == '/' {
						if doWildmatch(p[pi+1:], text[ti:], icase) == wmMatch {
							return wmMatch
						}
					}
					matchSlash = true
				}
			}

			if pi >= len(p) {
				// Trailing star: a single '*' must not swallow slashes.
				if !matchSlash && bytes.IndexByte(text[ti:], '/') >= 0 {
					return wmNoMatch
				}
				return wmMatch
			}

			if !matchSlash && p[pi] == '/' {
				// "*/" matches exactly the remainder of one component.
				idx := bytes.IndexByte(text[ti:], '/')
				if idx < 0 {
					return wmNoMatch
				}
				ti += idx + 1
				pi++
				continue
			}

			for ti < len(text) {
				matched := doWildmatch(p[pi:], text[ti:], icase)
				if matched != wmNoMatch {
					if !matchSlash || matched != wmAbortToStarStar {
						return matched
					}
				} else if !matchSlash && text[ti] == '/' {
					return wmAbortToStarStar
				}
				ti++
			}
			return wmAbortAll

		default:
			if tCh != pCh {
				return wmNoMatch
			}
			pi++
			ti++
		}
	}

	if ti < len(text) {
		return wmNoMatch
	}
	return wmMatch
}

// posixClasses maps the POSIX bracket class names git's wildmatch
// understands to their ASCII membership test.
var posixClasses = map[string]func(byte) bool{
	"alnum":  isAlnumASCII,
	"alpha":  isAlphaASCII,
	"blank":  func(c byte) bool { return c == ' ' || c == '\t' },
	"cntrl":  func(c byte) bool { return c < 0x20 || c == 0x7f },
	"digit":  func(c byte) bool { return c >= '0' && c <= '9' },
	"graph":  func(c byte) bool { return c > 0x20 && c < 0x7f },
	"lower":  func(c byte) bool { return c >= 'a' && c <= 'z' },
	"print":  func(c byte) bool { return c >= 0x20 && c < 0x7f },
	"punct":  func(c byte) bool { return c > 0x20 && c < 0x7f && !isAlnumASCII(c) },
	"space":  func(c byte) bool { return c == ' ' || (c >= '\t' && c <= '\r') },
	"upper":  func(c byte) bool { return c >= 'A' && c <= 'Z' },
	"xdigit": func(c byte) bool { return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') },
}

// matchBracket evaluates the bracket expression starting at p[pi] (which
// is '[') against tCh. It returns wmMatch, wmNoMatch or wmAbortAll along
// with the pattern index just past the closing ']'.
func matchBracket(p []byte, pi int, tCh byte, icase bool) (int, int) {
	pi++ // skip '['
	if pi >= len(p) {
		return wmAbortAll, pi
	}

	negated := p[pi] == '!' || p[pi] == '^'
	if negated {
		pi++
		if pi >= len(p) {
			return wmAbortAll, pi
		}
	}

	var prevCh byte
	matched := false

	for {
		if pi >= len(p) {
			return wmAbortAll, pi
		}
		pCh := p[pi]

		switch {
		case pCh == '\\':
			pi++
			if pi >= len(p) {
				return wmAbortAll, pi
			}
			pCh = p[pi]
			if eqFold(tCh, pCh, icase) {
				matched = true
			}

		case pCh == '-' && prevCh != 0 && pi+1 < len(p) && p[pi+1] != ']':
			pi++
			pCh = p[pi]
			if pCh == '\\' {
				pi++
				if pi >= len(p) {
					return wmAbortAll, pi
				}
				pCh = p[pi]
			}
			if tCh <= pCh && tCh >= prevCh {
				matched = true
			} else if icase && isAlphaASCII(tCh) {
				// tCh was already folded to lower case by the caller;
				// an upper-case range such as [A-Z] still has to match.
				up := upperASCII(tCh)
				if up <= pCh && up >= prevCh {
					matched = true
				}
			}
			// Prevent the range end from starting another range.
			pCh = 0

		case pCh == '[' && pi+1 < len(p) && p[pi+1] == ':':
			end := bytes.Index(p[pi+2:], []byte(":]"))
			if end < 0 {
				// Not a class after all; treat '[' literally.
				if eqFold(tCh, pCh, icase) {
					matched = true
				}
				break
			}
			fn, ok := posixClasses[string(p[pi+2:pi+2+end])]
			if !ok {
				return wmAbortAll, pi
			}
			if fn(tCh) || (icase && fn(upperASCII(tCh))) {
				matched = true
			}
			pi += 2 + end + 1 // land on the ']' of ":]"
			pCh = 0

		default:
			if eqFold(tCh, pCh, icase) {
				matched = true
			}
		}

		prevCh = pCh
		pi++
		if pi < len(p) && p[pi] == ']' {
			break
		}
		if pi >= len(p) {
			return wmAbortAll, pi
		}
	}

	pi++ // skip ']'
	if matched == negated {
		return wmNoMatch, pi
	}
	return wmMatch, pi
}

func eqFold(a, b byte, icase bool) bool {
	if a == b {
		return true
	}
	return icase && lowerASCII(a) == lowerASCII(b)
}
