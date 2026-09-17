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
	// Match reports how the pattern applies to path. Path is an ordered sequence
	// of logical path components. Patterns created with ParsePattern match only
	// paths beginning with their domain. isDir reports whether the final path
	// component is a directory. For a pattern ending in "/", isDir only
	// restricts a match at the candidate endpoint; descendants of a matched
	// directory may still match.
	Match(path []string, isDir bool) MatchResult
}

type pattern struct {
	domain    []string
	pattern   []string
	inclusion bool
	dirOnly   bool
	isGlob    bool
}

// ParsePattern parses a gitignore pattern string into a Pattern. The domain is
// an ordered prefix of logical path components that scopes the pattern.
// Matching applies to the components after that prefix. A nil or empty domain
// applies the pattern without a prefix.
//
// ReadPatterns uses the path of the directory containing a .gitignore file as
// its domain. When the filesystem is rooted at a repository, that path is
// repository-relative.
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
// wildmatch.c at tag v2.54.0[1]. The Go shape trades C idioms (raw pointers,
// NUL-terminated strings, goto-based control flow) for explicit bounds checks
// and a regular switch. Returned codes match upstream so callers can prune
// recursion the same way.
//
// Bracket expressions are the one deliberate departure. Upstream re-reads a
// bracket from its '[' every time it is reached, and dowild reaches the same
// '[' once per text offset a preceding '*' retries, so the reading grows with
// the pattern and the text together. Here a bracket is read once per pattern
// offset into the set of bytes it accepts (see bracketCache) and every later
// visit is a bit test. That matters because ignore patterns are repository
// content, and matching runs on every path walked by Status and Add.
// Positions in the pattern are absolute indices rather than reslices of p so
// that the cache can key on them.
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
//
// brackets caches the bracket expressions of pattern and nothing else. The
// caller owns it so that matching one pattern segment against several path
// components reads each bracket once, and must reset it before reusing it
// for a different pattern string or a different set of flags.
func wildmatch(pattern, text string, brackets *bracketCache) bool {
	return dowild(pattern, 0, text, 0, brackets) == wmMatch
}

// bracketSet holds everything the '[' case needs to know about one bracket
// expression, so that a repeat visit costs a bit test instead of a re-read.
// accept has negation and the WM_PATHNAME rule for '/' already folded in, so
// a clear bit means the bracket rejects that byte.
type bracketSet struct {
	accept [4]uint64
	end    int  // index of the closing ']'
	abort  bool // malformed; wildmatch.c answers WM_ABORT_ALL for any byte
}

func (b *bracketSet) add(ch byte) { b.accept[ch>>6] |= 1 << (ch & 63) }

// addRange adds every byte from lo to hi, or nothing when the range is
// inverted, which is how the comparison it replaces behaves.
func (b *bracketSet) addRange(lo, hi byte) {
	if lo > hi {
		return
	}
	first, last := int(lo)>>6, int(hi)>>6
	for i := first; i <= last; i++ {
		m := ^uint64(0)
		if i == first {
			m &= ^uint64(0) << (lo & 63)
		}
		if i == last {
			m &= ^uint64(0) >> (63 - hi&63)
		}
		b.accept[i] |= m
	}
}

func (b *bracketSet) has(ch byte) bool { return b.accept[ch>>6]&(1<<(ch&63)) != 0 }

// inlineBrackets is how many bracket expressions a cache holds before it
// reaches for a map. Patterns with no more brackets than this match without
// allocating, which covers everything an ignore file realistically contains.
const inlineBrackets = 4

// bracketCache memoizes bracketSets by the offset of their '[' in the
// pattern. Offsets are compared in order because there are at most
// inlineBrackets of them before the map takes over.
type bracketCache struct {
	offsets  [inlineBrackets]int
	sets     [inlineBrackets]bracketSet
	n        int
	overflow map[int]*bracketSet
}

// reset drops everything cached, leaving the cache ready for a different
// pattern. Entries at or above n are never read, so nothing needs clearing.
func (c *bracketCache) reset() {
	c.n = 0
	c.overflow = nil
}

// get returns the set for the bracket opening at p[start], reading the
// bracket the first time it is asked for.
func (c *bracketCache) get(p string, start, flags int) *bracketSet {
	for i := range c.n {
		if c.offsets[i] == start {
			return &c.sets[i]
		}
	}
	if b, ok := c.overflow[start]; ok {
		return b
	}
	if c.n < len(c.offsets) {
		i := c.n
		c.offsets[i] = start
		c.n++
		readBracket(&c.sets[i], p, start, flags)
		return &c.sets[i]
	}
	b := new(bracketSet)
	readBracket(b, p, start, flags)
	if c.overflow == nil {
		c.overflow = make(map[int]*bracketSet, 1)
	}
	c.overflow[start] = b
	return b
}

// readBracket walks the bracket expression opening at p[start] once and
// records which bytes it accepts. It follows the '[' case of wildmatch.c
// member for member; the difference is only that each member contributes to
// a set instead of being compared against one byte, so the answer does not
// depend on the text. Every way that loop can report WM_ABORT_ALL (a bracket
// with no ']', a trailing backslash, an unknown [:class:]) is independent of
// the text too, and is recorded as abort.
//
// The C source uses a do/while loop terminating when p_ch == ']'; each
// iteration ends with prev_ch = p_ch and p_ch = *++p. NUL from the C string
// is detected here with explicit pi bounds checks before every read.
func readBracket(b *bracketSet, p string, start, flags int) {
	// The slot may still hold the bracket a previous pattern put there.
	*b = bracketSet{}
	pi := start + 1
	if pi >= len(p) {
		b.abort = true
		return
	}
	pCh := p[pi]
	if pCh == '^' {
		pCh = '!'
	}
	negated := pCh == '!'
	if negated {
		pi++
		if pi >= len(p) {
			b.abort = true
			return
		}
		pCh = p[pi]
	}
	var prevCh byte
	// closeAt is the ']' found by the most recent "[:" scan below. Those
	// scans ask for the first ']' at or after a position that only ever
	// moves forward, so remembering the last answer keeps their combined
	// cost linear in the bracket instead of quadratic.
	closeAt := -1
	for {
		switch {
		case pCh == '\\':
			pi++
			if pi >= len(p) {
				b.abort = true
				return
			}
			pCh = p[pi]
			b.add(pCh)
		case pCh == '-' && prevCh != 0 &&
			pi+1 < len(p) && p[pi+1] != ']':
			pi++
			pCh = p[pi]
			if pCh == '\\' {
				pi++
				if pi >= len(p) {
					b.abort = true
					return
				}
				pCh = p[pi]
			}
			b.addRange(prevCh, pCh)
			if flags&wmCasefold != 0 {
				// A folded lowercase byte also matches when its
				// uppercase form falls in the range.
				for c := byte('a'); c <= 'z'; c++ {
					if u := c - ('a' - 'A'); u <= pCh && u >= prevCh {
						b.add(c)
					}
				}
			}
			pCh = 0 // resets prev_ch for next iteration
		case pCh == '[' && pi+1 < len(p) && p[pi+1] == ':':
			// POSIX class [:name:]. Walk forward to the next ']';
			// if it isn't preceded by ':' the construct is not a
			// class, so rewind and treat the '[' as a literal.
			s := pi + 2
			if closeAt < s {
				closeAt = s
				for closeAt < len(p) && p[closeAt] != ']' {
					closeAt++
				}
			}
			pi = closeAt
			if pi >= len(p) {
				b.abort = true
				return
			}
			nameLen := pi - s - 1
			if nameLen < 0 || p[pi-1] != ':' {
				pi = s - 2
				pCh = '['
				b.add(pCh)
				// Fall through to the loop tail with pCh='[' so the
				// post-step records it as prev_ch.
				break
			}
			class, valid := posixClassSet(p[s:pi-1], flags)
			if !valid {
				b.abort = true
				return
			}
			for i := range b.accept {
				b.accept[i] |= class[i]
			}
			pCh = 0 // resets prev_ch
		default:
			b.add(pCh)
		}
		prevCh = pCh
		pi++
		if pi >= len(p) {
			b.abort = true
			return
		}
		if p[pi] == ']' {
			break
		}
		pCh = p[pi]
	}
	b.end = pi
	if negated {
		for i := range b.accept {
			b.accept[i] = ^b.accept[i]
		}
	}
	if flags&wmPathname != 0 {
		b.accept['/'>>6] &^= 1 << ('/' & 63)
	}
}

// dowild walks pattern and text in lock-step, recursing at each '*' to try
// every text suffix and propagating wmMatch, wmNoMatch, wmAbortAll, or
// wmAbortToStarStar back up so callers can prune work the same way the
// upstream C implementation does (wildmatch.c#L59-L283).
//
// Matching starts at p[pStart]. Upstream recurses on a pointer into the
// pattern; this recurses on an index into the same string, so a bracket keeps
// the same offset in every call and brackets can be shared across the whole
// call tree.
func dowild(p string, pStart int, text string, flags int, brackets *bracketCache) int {
	pi, ti := pStart, 0
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
				case (prevPi-2 < pStart || p[prevPi-2] == '/') &&
					(pi >= len(p) || p[pi] == '/' ||
						(pi+1 < len(p) && p[pi] == '\\' && p[pi+1] == '/')):
					// At a '/<**>/' boundary: optionally match the slash as
					// nothing, recursing past it so that foo/<*><*>/bar
					// matches both foo/bar and foo/a/bar.
					if pi < len(p) && p[pi] == '/' &&
						dowild(p, pi+1, text[ti:], flags, brackets) == wmMatch {
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
				matched := dowild(p, pi, text[ti:], flags, brackets)
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
			// The bracket is read once per pattern offset; see
			// bracketCache. tCh has already been case-folded, and
			// accept accounts for negation and WM_PATHNAME.
			b := brackets.get(p, pi, flags)
			if b.abort {
				return wmAbortAll
			}
			if !b.has(tCh) {
				return wmNoMatch
			}
			pi = b.end + 1
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

// posixClasses holds the [:name:] character classes a bracket expression may
// contain. Classification is ASCII-only to mirror sane-ctype.h: bytes with
// the high bit set never satisfy any class. A name missing from this table
// is what wildmatch.c calls a "malformed [:class:] string".
var posixClasses = map[string]func(ch byte, flags int) bool{
	"alnum": func(ch byte, _ int) bool { return isASCIIAlpha(ch) || isASCIIDigit(ch) },
	"alpha": func(ch byte, _ int) bool { return isASCIIAlpha(ch) },
	"blank": func(ch byte, _ int) bool { return ch == ' ' || ch == '\t' },
	"cntrl": func(ch byte, _ int) bool { return ch < 0x20 || ch == 0x7f },
	"digit": func(ch byte, _ int) bool { return isASCIIDigit(ch) },
	"graph": func(ch byte, _ int) bool { return ch > ' ' && ch < 0x7f },
	"lower": func(ch byte, _ int) bool { return ch >= 'a' && ch <= 'z' },
	"print": func(ch byte, _ int) bool { return ch >= ' ' && ch < 0x7f },
	"punct": func(ch byte, _ int) bool { return isASCIIPunct(ch) },
	"space": func(ch byte, _ int) bool {
		return ch == ' ' || ch == '\t' || ch == '\n' ||
			ch == '\v' || ch == '\f' || ch == '\r'
	},
	"upper": func(ch byte, flags int) bool {
		return (ch >= 'A' && ch <= 'Z') ||
			(flags&wmCasefold != 0 && isASCIILower(ch))
	},
	"xdigit": func(ch byte, _ int) bool {
		return isASCIIDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
	},
}

// posixClassSets is posixClasses evaluated over every byte once, so reading a
// [:class:] out of a bracket costs a map lookup. Index 1 is the WM_CASEFOLD
// variant; [:upper:] is the only class that reads the flag.
var posixClassSets = buildPOSIXClassSets()

func buildPOSIXClassSets() [2]map[string][4]uint64 {
	var sets [2]map[string][4]uint64
	for i := range sets {
		flags := 0
		if i == 1 {
			flags = wmCasefold
		}
		sets[i] = make(map[string][4]uint64, len(posixClasses))
		for name, in := range posixClasses {
			var set [4]uint64
			for c := range 256 {
				if in(byte(c), flags) {
					set[c>>6] |= 1 << (c & 63)
				}
			}
			sets[i][name] = set
		}
	}
	return sets
}

// posixClassSet returns the bytes matched by the class named [:name:]. valid
// is false when the name is unrecognized — wildmatch.c propagates that as
// wmAbortAll.
func posixClassSet(name string, flags int) (set [4]uint64, valid bool) {
	i := 0
	if flags&wmCasefold != 0 {
		i = 1
	}
	set, valid = posixClassSets[i][name]
	return set, valid
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
	var brackets bracketCache
	for i, name := range path {
		if !wildmatch(p.pattern[0], name, &brackets) {
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
	var brackets bracketCache
	for i, pattern := range p.pattern {
		if pattern == "" {
			canTraverse = false
			continue
		}
		if pattern == zeroToManyDirs {
			if i == len(p.pattern)-1 {
				// A trailing `**` matches the entries below whatever the
				// earlier segments consumed, so it needs either a remaining
				// component or a directory candidate standing in for them.
				// Assigning matched rather than only raising it stops an
				// exhausted path from inheriting the previous segment's
				// result, which would make `a/**/*/**` match `a/f.txt`.
				matched = len(path) > 0 || isDir
				trailingStar = matched
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
		// Every segment is a pattern of its own, so the offsets cached for
		// the previous one no longer mean anything.
		brackets.reset()
		if canTraverse {
			canTraverse = false
			for len(path) > 0 {
				e := path[0]
				path = path[1:]
				if wildmatch(pattern, e, &brackets) {
					matched = true
					break
				}
				if len(path) == 0 {
					// A `**` that never finds the segment following it is a
					// definitive non-match. Returning here rather than
					// clearing matched keeps a trailing `**` from reviving
					// the pattern once the path is exhausted, which would
					// make `**/bar/**` match directories containing no bar.
					return false
				}
			}
		} else {
			if !wildmatch(pattern, path[0], &brackets) {
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
