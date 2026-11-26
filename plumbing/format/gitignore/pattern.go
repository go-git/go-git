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

	// Like git, read a segment made only of three or more asterisks as "**".
	// A trailing one in a directory-only rule is left alone: "**" there would
	// stop matching files below the directory, which "*" still does.
	for i, seg := range res.pattern {
		if res.dirOnly && i == len(res.pattern)-1 {
			break
		}
		if len(seg) > len(zeroToManyDirs) && strings.Trim(seg, "*") == "" {
			res.pattern[i] = zeroToManyDirs
		}
	}

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

// matchSegment reports whether name, one path component, matches glob, one
// component of a gitignore pattern. Neither contains a '/'. The whole of
// name has to match the whole of glob, byte for byte:
//
//   - '*' matches any run of bytes, an empty one included;
//   - '?' matches any single byte;
//   - a bracket expression matches one byte out of its set;
//   - '\' makes the byte after it literal;
//   - any other byte matches itself.
//
// A glob that ends in a lone '\' or holds a malformed bracket expression
// matches nothing.
//
// brackets caches the bracket expressions of glob; the caller owns it and
// resets it before reusing it for another glob.
//
// Only the latest '*' is ever reconsidered. When the rest of the glob
// fails, that '*' takes one more byte and the rest is tried again; an
// earlier '*' taking more could only hide bytes the later one is free to
// skip anyway. The work is therefore bounded by len(glob) * len(name),
// however many '*' the glob has.
func matchSegment(glob, name string, brackets *bracketCache) bool {
	var (
		g, n = 0, 0 // next byte of glob and of name
		// resumeG is the glob position after the latest '*', or -1 before
		// the first one; resumeN is where in name its current attempt
		// started.
		resumeG, resumeN = -1, 0
	)

	for {
		if g < len(glob) && glob[g] == '*' {
			g++
			if g == len(glob) {
				return true
			}
			resumeG, resumeN = g, n
			continue
		}
		if n == len(name) {
			// Nothing is left for a '*' to give back.
			return g == len(glob)
		}

		matched, width := false, 1
		if g < len(glob) {
			switch glob[g] {
			case '?':
				matched = true
			case '[':
				set := brackets.get(glob, g)
				if set.malformed {
					return false
				}
				matched, width = set.has(name[n]), set.end-g+1
			case '\\':
				if g+1 == len(glob) {
					return false
				}
				matched, width = glob[g+1] == name[n], 2
			default:
				matched = glob[g] == name[n]
			}
		}
		if matched {
			g += width
			n++
			continue
		}

		if resumeG < 0 {
			return false
		}
		resumeN++
		// When a plain byte follows the '*', only its occurrences in name
		// are worth trying.
		if next := glob[resumeG]; strings.IndexByte(`?[\`, next) < 0 {
			skip := strings.IndexByte(name[resumeN:], next)
			if skip < 0 {
				return false
			}
			resumeN += skip
		}
		g, n = resumeG, resumeN
	}
}

// bracketSet is one bracket expression of a pattern, read once: the bytes
// it accepts as a 256-bit set with negation already applied, and where it
// ends. A malformed expression accepts nothing, and neither does the
// pattern segment it is part of.
type bracketSet struct {
	accept    [4]uint64
	end       int // index of the closing ']'
	malformed bool
}

func (b *bracketSet) add(ch byte) { b.accept[ch>>6] |= 1 << (ch & 63) }

// addRange adds every byte from lo to hi. A range written backwards holds
// nothing.
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
func (c *bracketCache) get(p string, start int) *bracketSet {
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
		readBracket(&c.sets[i], p, start)
		return &c.sets[i]
	}
	b := new(bracketSet)
	readBracket(b, p, start)
	if c.overflow == nil {
		c.overflow = make(map[int]*bracketSet, 1)
	}
	c.overflow[start] = b
	return b
}

// readBracket reads the bracket expression that starts at p[start] into b.
//
// After the '[' an optional '!' or '^' negates the expression. Its members
// run up to the next ']', except that a ']' in first position is a member
// itself. A member is a byte, "\" and the byte it makes literal, a range
// "a-z", or a class such as "[:digit:]".
//
// The expression is malformed when p ends before the closing ']', when a
// "\" has nothing to escape, or when a class name is not known.
func readBracket(b *bracketSet, p string, start int) {
	// b stays malformed, and empty, until the closing bracket is reached.
	*b = bracketSet{malformed: true}
	var set bracketSet

	i := start + 1
	negated := i < len(p) && (p[i] == '!' || p[i] == '^')
	if negated {
		i++
	}

	// rangeFrom is the member a following '-' would extend into a range,
	// or -1 after a range or a class, which have no single byte to offer.
	rangeFrom := -1
	// nextClose is the first ']' at or after the last "[:" looked at.
	nextClose := -1
	for first := i; i < len(p); i++ {
		ch := p[i]
		switch {
		case ch == ']' && i > first:
			if negated {
				for w := range set.accept {
					set.accept[w] = ^set.accept[w]
				}
			}
			set.end = i
			*b = set
			return

		case ch == '-' && rangeFrom >= 0 && i+1 < len(p) && p[i+1] != ']':
			// Whatever follows the '-' is the upper end, a '[' included;
			// only an escape is looked into.
			i++
			to := p[i]
			if to == '\\' {
				if i++; i == len(p) {
					return
				}
				to = p[i]
			}
			set.addRange(byte(rangeFrom), to)
			rangeFrom = -1
			continue

		case ch == '[' && i+1 < len(p) && p[i+1] == ':':
			// "[:" opens a class only if a ":" of its own stands before
			// the next ']'. Otherwise the '[' is an ordinary member.
			if nextClose < i+2 {
				// The search only ever moves forward, so a bracket is read
				// in time linear in its length however many "[:" it holds.
				nextClose = len(p)
				if at := strings.IndexByte(p[i+2:], ']'); at >= 0 {
					nextClose = i + 2 + at
				}
			}
			if nextClose == len(p) {
				// No ']' is left, so the expression is never closed.
				return
			}
			if nextClose > i+2 && p[nextClose-1] == ':' {
				class, known := posixClasses[p[i+2:nextClose-1]]
				if !known {
					return
				}
				for w := range set.accept {
					set.accept[w] |= class[w]
				}
				i = nextClose
				rangeFrom = -1
				continue
			}

		case ch == '\\':
			if i++; i == len(p) {
				return
			}
			ch = p[i]
		}

		set.add(ch)
		rangeFrom = int(ch)
	}
}

// posixClasses holds the bytes of each [:name:] character class a bracket
// expression may contain. Only ASCII is classified: no byte with the high
// bit set belongs to any class.
var posixClasses = buildPOSIXClasses()

func buildPOSIXClasses() map[string][4]uint64 {
	members := map[string]func(ch byte) bool{
		"alnum": func(ch byte) bool { return isASCIIAlpha(ch) || isASCIIDigit(ch) },
		"alpha": isASCIIAlpha,
		"blank": func(ch byte) bool { return ch == ' ' || ch == '\t' },
		"cntrl": func(ch byte) bool { return ch < 0x20 || ch == 0x7f },
		"digit": isASCIIDigit,
		"graph": func(ch byte) bool { return ch > ' ' && ch < 0x7f },
		"lower": func(ch byte) bool { return ch >= 'a' && ch <= 'z' },
		"print": func(ch byte) bool { return ch >= ' ' && ch < 0x7f },
		"punct": isASCIIPunct,
		// Git leaves vertical tab and form feed out of [:space:].
		"space": func(ch byte) bool { return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' },
		"upper": func(ch byte) bool { return ch >= 'A' && ch <= 'Z' },
		"xdigit": func(ch byte) bool {
			return isASCIIDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
		},
	}

	classes := make(map[string][4]uint64, len(members))
	for name, isMember := range members {
		var set [4]uint64
		for c := range 256 {
			if isMember(byte(c)) {
				set[c>>6] |= 1 << (c & 63)
			}
		}
		classes[name] = set
	}
	return classes
}

func isASCIIAlpha(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isASCIIDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
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
		if !matchSegment(p.pattern[0], name, &brackets) {
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
			if i > 0 {
				// Only the first empty component anchors a pattern at the root.
				return false
			}
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
		// (e.g., foo** or **bar) and matchSegment will handle it
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
				if matchSegment(pattern, e, &brackets) {
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
			if !matchSegment(pattern, path[0], &brackets) {
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
