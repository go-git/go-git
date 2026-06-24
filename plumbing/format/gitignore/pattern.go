package gitignore

import (
	"strings"
)

// MatchResult defines outcomes of a match, no match, exclusion or inclusion.
type MatchResult int

const (
	// NoMatch defines the no match outcome of a match check
	NoMatch MatchResult = iota
	// Exclude defines an exclusion of a file as a result of a match check
	Exclude
	// Include defines an explicit inclusion of a file as a result of a match check
	Include
)

const (
	inclusionPrefix = "!"
	zeroToManyDirs  = "**"
	patternDirSep   = "/"
)

// Pattern defines a single gitignore pattern.
type Pattern interface {
	// Match matches the given path to the pattern.
	Match(path []string, isDir bool) MatchResult
}

type pattern struct {
	domain    []string
	pattern   []string
	inclusion bool
	dirOnly   bool
	isGlob    bool
}

// ParsePattern parses a gitignore pattern string into the Pattern structure.
func ParsePattern(p string, domain []string) Pattern {
	// storing domain, copy it to ensure it isn't changed externally
	domain = append([]string(nil), domain...)
	res := pattern{domain: domain}

	if strings.HasPrefix(p, inclusionPrefix) {
		res.inclusion = true
		p = p[1:]
	}

	if !strings.HasSuffix(p, "\\ ") {
		p = strings.TrimRight(p, " ")
	}

	if strings.HasSuffix(p, patternDirSep) {
		res.dirOnly = true
		p = p[:len(p)-1]
	}

	if strings.Contains(p, patternDirSep) {
		res.isGlob = true
	}

	res.pattern = strings.Split(p, patternDirSep)
	return &res
}

func (p *pattern) Match(path []string, isDir bool) MatchResult {
	if len(path) <= len(p.domain) {
		return NoMatch
	}
	for i, e := range p.domain {
		if path[i] != e {
			return NoMatch
		}
	}

	path = path[len(p.domain):]
	if p.isGlob && !p.globMatch(path, isDir) {
		return NoMatch
	} else if !p.isGlob && !p.simpleNameMatch(path, isDir) {
		return NoMatch
	}

	if p.inclusion {
		return Include
	}
	return Exclude
}

// The wildmatch implementation below ports the matcher from canonical Git's
// wildmatch.c at tag v2.54.0[1]. The algorithm is preserved exactly; the Go
// shape trades C idioms (raw pointers, NUL-terminated strings, goto-based
// control flow) for string slicing, explicit bounds checks, and a regular
// switch. Returned codes match upstream so callers can prune recursion the
// same way.
//
// [1]: https://github.com/git/git/blob/v2.54.0/wildmatch.c

// wildmatch return codes mirror the WM_* constants from upstream wildmatch.h.
// wmAbortToStarStar lets a recursive call signal to its caller that it hit a
// '/' boundary while expanding a non-'**' star, so the outer '*' can prune
// further alternatives instead of re-trying them.
const (
	wmMatch           = 0
	wmNoMatch         = 1
	wmAbortAll        = -1
	wmAbortToStarStar = -2
)

// wildmatch flags mirror the WM_* flag bits in upstream wildmatch.h. The
// current go-git API does not expose case-insensitive matching, and the
// matcher splits paths on '/' before dispatching here, so wmCasefold and
// wmPathname code paths are kept for upstream parity but never exercised by
// the public Match.
const (
	wmCasefold = 1
	wmPathname = 2
)

// wildmatch reports whether text matches the wildcard pattern. It is a thin
// wrapper over dowild; the gitignore matcher splits paths on '/' before
// dispatching, so dowild always operates on a single pattern/text segment
// with flags=0.
func wildmatch(pattern, text string) bool {
	return dowild(pattern, text, 0) == wmMatch
}

// dowild walks pattern and text in lock-step, recursing at each '*' to try
// every text suffix and propagating wmMatch, wmNoMatch, wmAbortAll, or
// wmAbortToStarStar back up so callers can prune work the same way the
// upstream C implementation does (wildmatch.c#L59-L283).
func dowild(p, text string, flags int) int {
	pi, ti := 0, 0
	for pi < len(p) {
		pCh := p[pi]
		var tCh byte
		atEndOfText := ti >= len(text)
		if !atEndOfText {
			tCh = text[ti]
		}
		if atEndOfText && pCh != '*' {
			return wmAbortAll
		}
		if flags&wmCasefold != 0 && isASCIIUpper(tCh) {
			tCh += 'a' - 'A'
		}
		if flags&wmCasefold != 0 && isASCIIUpper(pCh) {
			pCh += 'a' - 'A'
		}

		switch pCh {
		case '\\':
			// Literal match with the following character. A trailing '\'
			// has no character to escape; canonical Git reads NUL (the C
			// string terminator) into p_ch and the default-case compare
			// fails because t_ch can never be NUL (the surrounding check
			// returned wmAbortAll when text was exhausted). We mirror that
			// by returning wmNoMatch directly.
			if pi+1 >= len(p) {
				return wmNoMatch
			}
			pi++
			pCh = p[pi]
			if tCh != pCh {
				return wmNoMatch
			}
			pi++
			ti++
		case '?':
			// Match any character except '/'.
			if flags&wmPathname != 0 && tCh == '/' {
				return wmNoMatch
			}
			pi++
			ti++
		case '*':
			pi++
			var matchSlash bool
			if pi < len(p) && p[pi] == '*' {
				prevPi := pi
				for pi < len(p) && p[pi] == '*' {
					pi++
				}
				switch {
				case flags&wmPathname == 0:
					// Without WM_PATHNAME, '*' == '**'.
					matchSlash = true
				case (prevPi < 2 || p[prevPi-2] == '/') &&
					(pi >= len(p) || p[pi] == '/' ||
						(pi+1 < len(p) && p[pi] == '\\' && p[pi+1] == '/')):
					// At a '/<**>/' boundary: optionally match the slash as
					// nothing, recursing past it so that foo/<*><*>/bar
					// matches both foo/bar and foo/a/bar.
					if pi < len(p) && p[pi] == '/' &&
						dowild(p[pi+1:], text[ti:], flags) == wmMatch {
						return wmMatch
					}
					matchSlash = true
				}
			} else {
				// Single '*': without WM_PATHNAME crosses '/'; with it,
				// does not.
				matchSlash = flags&wmPathname == 0
			}

			if pi >= len(p) {
				// Trailing "**" matches everything; trailing "*" matches only
				// when no '/' remains in text.
				if !matchSlash && strings.IndexByte(text[ti:], '/') >= 0 {
					return wmAbortToStarStar
				}
				return wmMatch
			} else if !matchSlash && p[pi] == '/' {
				// One '*' followed by '/' with WM_PATHNAME: advance text to
				// the next '/' so the outer loop consumes it.
				slash := strings.IndexByte(text[ti:], '/')
				if slash < 0 {
					return wmAbortAll
				}
				ti += slash
				// Fall through to the outer-loop advance.
				pi++
				ti++
				continue
			}

			for {
				if ti >= len(text) {
					return wmAbortAll
				}
				tCh = text[ti]
				// Try to advance faster when '*' is followed by a literal.
				// Everything before the next occurrence of that literal
				// must belong to '*'. With matchSlash=false, stop at the
				// first '/'.
				if !isGlobSpecial(p[pi]) {
					pCh = p[pi]
					if flags&wmCasefold != 0 && isASCIIUpper(pCh) {
						pCh += 'a' - 'A'
					}
					for ti < len(text) {
						tCh = text[ti]
						if !matchSlash && tCh == '/' {
							break
						}
						if flags&wmCasefold != 0 && isASCIIUpper(tCh) {
							tCh += 'a' - 'A'
						}
						if tCh == pCh {
							break
						}
						ti++
					}
					if ti >= len(text) || tCh != pCh {
						if matchSlash {
							return wmAbortAll
						}
						return wmAbortToStarStar
					}
				}
				matched := dowild(p[pi:], text[ti:], flags)
				if matched != wmNoMatch {
					if !matchSlash || matched != wmAbortToStarStar {
						return matched
					}
				} else if !matchSlash && tCh == '/' {
					return wmAbortToStarStar
				}
				ti++
			}
		case '[':
			pi++
			if pi >= len(p) {
				return wmAbortAll
			}
			pCh = p[pi]
			if pCh == '^' {
				pCh = '!'
			}
			negated := pCh == '!'
			if negated {
				pi++
				if pi >= len(p) {
					return wmAbortAll
				}
				pCh = p[pi]
			}
			var prevCh byte
			matched := false
			// The C source uses a do/while loop terminating when p_ch == ']';
			// each iteration ends with prev_ch = p_ch and p_ch = *++p. NUL
			// from the C string is detected here with explicit pi bounds
			// checks before every read.
			for {
				switch {
				case pCh == '\\':
					pi++
					if pi >= len(p) {
						return wmAbortAll
					}
					pCh = p[pi]
					if tCh == pCh {
						matched = true
					}
				case pCh == '-' && prevCh != 0 &&
					pi+1 < len(p) && p[pi+1] != ']':
					pi++
					pCh = p[pi]
					if pCh == '\\' {
						pi++
						if pi >= len(p) {
							return wmAbortAll
						}
						pCh = p[pi]
					}
					if tCh <= pCh && tCh >= prevCh {
						matched = true
					} else if flags&wmCasefold != 0 && isASCIILower(tCh) {
						tUpper := tCh - ('a' - 'A')
						if tUpper <= pCh && tUpper >= prevCh {
							matched = true
						}
					}
					pCh = 0 // resets prev_ch for next iteration
				case pCh == '[' && pi+1 < len(p) && p[pi+1] == ':':
					// POSIX class [:name:]. Walk forward to the next ']';
					// if it isn't preceded by ':' the construct is not a
					// class, so rewind and treat the '[' as a literal.
					s := pi + 2
					pi = s
					for pi < len(p) && p[pi] != ']' {
						pi++
					}
					if pi >= len(p) {
						return wmAbortAll
					}
					nameLen := pi - s - 1
					if nameLen < 0 || p[pi-1] != ':' {
						pi = s - 2
						pCh = '['
						if tCh == pCh {
							matched = true
						}
						// Fall through to the loop tail with pCh='[' so the
						// post-step records it as prev_ch.
						break
					}
					classMatched, valid := matchPOSIXClass(p[s:pi-1], tCh, flags)
					if !valid {
						return wmAbortAll
					}
					if classMatched {
						matched = true
					}
					pCh = 0 // resets prev_ch
				default:
					if tCh == pCh {
						matched = true
					}
				}
				prevCh = pCh
				pi++
				if pi >= len(p) {
					return wmAbortAll
				}
				if p[pi] == ']' {
					break
				}
				pCh = p[pi]
			}
			if matched == negated ||
				(flags&wmPathname != 0 && tCh == '/') {
				return wmNoMatch
			}
			pi++
			ti++
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

// isGlobSpecial mirrors is_glob_special() from upstream ctype.c. Bytes that
// can start or modify a wildmatch sub-pattern are "special"; everything else
// is literal text and may be fast-skipped in the '*' loop.
func isGlobSpecial(c byte) bool {
	switch c {
	case '*', '?', '[', '\\':
		return true
	}
	return false
}

// matchPOSIXClass evaluates a [:name:] character-class entry within a bracket
// expression. Classification is ASCII-only to mirror sane-ctype.h: bytes
// with the high bit set never satisfy any class. valid is false when the
// class name is unrecognized — wildmatch.c propagates that as wmAbortAll
// ("malformed [:class:] string").
func matchPOSIXClass(name string, ch byte, flags int) (matched, valid bool) {
	switch name {
	case "alnum":
		return isASCIIAlpha(ch) || isASCIIDigit(ch), true
	case "alpha":
		return isASCIIAlpha(ch), true
	case "blank":
		return ch == ' ' || ch == '\t', true
	case "cntrl":
		return ch < 0x20 || ch == 0x7f, true
	case "digit":
		return isASCIIDigit(ch), true
	case "graph":
		return ch > ' ' && ch < 0x7f, true
	case "lower":
		return ch >= 'a' && ch <= 'z', true
	case "print":
		return ch >= ' ' && ch < 0x7f, true
	case "punct":
		return isASCIIPunct(ch), true
	case "space":
		return ch == ' ' || ch == '\t' || ch == '\n' ||
			ch == '\v' || ch == '\f' || ch == '\r', true
	case "upper":
		if ch >= 'A' && ch <= 'Z' {
			return true, true
		}
		if flags&wmCasefold != 0 && isASCIILower(ch) {
			return true, true
		}
		return false, true
	case "xdigit":
		return isASCIIDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F'), true
	default:
		return false, false
	}
}

func isASCIIAlpha(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isASCIIDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isASCIIUpper(ch byte) bool {
	return ch >= 'A' && ch <= 'Z'
}

func isASCIILower(ch byte) bool {
	return ch >= 'a' && ch <= 'z'
}

func isASCIIPunct(ch byte) bool {
	return (ch >= '!' && ch <= '/') ||
		(ch >= ':' && ch <= '@') ||
		(ch >= '[' && ch <= '`') ||
		(ch >= '{' && ch <= '~')
}

func (p *pattern) simpleNameMatch(path []string, isDir bool) bool {
	for i, name := range path {
		if !wildmatch(p.pattern[0], name) {
			continue
		}
		if p.dirOnly && !isDir && i == len(path)-1 {
			return false
		}
		return true
	}
	return false
}

func (p *pattern) globMatch(path []string, isDir bool) bool {
	matched := false
	canTraverse := false
	trailingStar := false
	for i, pattern := range p.pattern {
		if pattern == "" {
			canTraverse = false
			continue
		}
		if pattern == zeroToManyDirs {
			if i == len(p.pattern)-1 {
				// Trailing ** matches everything remaining (if there's something left or it's a dir)
				if len(path) > 0 || isDir {
					matched = true
					trailingStar = true
				}
				break
			}
			canTraverse = true
			continue
		}
		// Note: If pattern contains ** but isn't exactly **, it's treated as a regular wildcard pattern
		// (e.g., foo** or **bar) and wildmatch will handle it
		if len(path) == 0 {
			return false
		}
		if canTraverse {
			canTraverse = false
			for len(path) > 0 {
				e := path[0]
				path = path[1:]
				if wildmatch(pattern, e) {
					matched = true
					break
				} else if len(path) == 0 {
					// if nothing left then fail
					matched = false
				}
			}
		} else {
			if !wildmatch(pattern, path[0]) {
				return false
			}
			matched = true
			path = path[1:]
			// files matching dir globs, don't match
			if len(path) == 0 && i < len(p.pattern)-1 {
				matched = false
			}
		}
	}
	// Check dirOnly: either we consumed all path (len(path) == 0) or we matched a trailing **
	if matched && p.dirOnly && !isDir && (len(path) == 0 || trailingStar) {
		matched = false
	}
	return matched
}
