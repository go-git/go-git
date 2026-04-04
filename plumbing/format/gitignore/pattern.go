package gitignore

import (
	"strings"
	"unicode"
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

// wildmatch implements gitignore-compatible pattern matching with support for
// POSIX character classes and proper bracket expression handling
func wildmatch(pattern, text string) bool {
	pi, ti := 0, 0
	plen, tlen := len(pattern), len(text)

	// Track star positions for backtracking
	starIdx, matchIdx := -1, -1

	for ti < tlen {
		if pi < plen {
			pc := pattern[pi]

			switch {
			case pc == '\\' && pi+1 < plen:
				// Handle escaped characters
				pi++
				if pattern[pi] == text[ti] {
					pi++
					ti++
					continue
				}
				return false

			case pc == '?':
				// Question mark matches any single character
				pi++
				ti++
				continue

			case pc == '*':
				// Star matches zero or more characters
				starIdx = pi
				matchIdx = ti
				pi++
				continue

			case pc == '[':
				// Bracket expression
				bracketEnd := findBracketEnd(pattern, pi)
				if bracketEnd > pi && matchBracket(pattern[pi:bracketEnd+1], text[ti]) {
					pi = bracketEnd + 1
					ti++
					continue
				}
				// If bracket match failed and we have a star, backtrack
				if starIdx >= 0 {
					pi = starIdx + 1
					matchIdx++
					ti = matchIdx
					continue
				}
				return false

			case pc == text[ti]:
				// Exact character match
				pi++
				ti++
				continue
			}
		}

		// No match, try backtracking to last star
		if starIdx >= 0 {
			pi = starIdx + 1
			matchIdx++
			ti = matchIdx
			continue
		}

		return false
	}

	// Skip trailing stars in pattern
	for pi < plen && pattern[pi] == '*' {
		pi++
	}

	return pi == plen
}

// findBracketEnd finds the closing ] of a bracket expression starting at position start
func findBracketEnd(pattern string, start int) int {
	if start >= len(pattern) || pattern[start] != '[' {
		return -1
	}

	i := start + 1
	// Handle negation
	if i < len(pattern) && (pattern[i] == '!' || pattern[i] == '^') {
		i++
	}
	// Handle ] at start
	if i < len(pattern) && pattern[i] == ']' {
		i++
	}

	for i < len(pattern) {
		if pattern[i] == ']' {
			return i
		}
		switch {
		case pattern[i] == '\\' && i+1 < len(pattern):
			i += 2
		case pattern[i] == '[' && i+1 < len(pattern) && pattern[i+1] == ':':
			// Potential character class - look for :]
			j := i + 2
			foundClass := false
			for j < len(pattern) && pattern[j] != ']' {
				j++
			}
			// Check if we found :] (]: must be preceded by :)
			if j < len(pattern) && j > i+2 && pattern[j-1] == ':' {
				// Valid character class, skip past it
				i = j + 1
				foundClass = true
			}
			if !foundClass {
				// Not a valid character class, treat [ as literal and continue
				i++
			}
		default:
			i++
		}
	}
	return -1
}

// matchBracket matches a single character against a bracket expression
// The pattern should be the complete bracket expression including [ and ]
// Based on Git's wildmatch.c implementation
func matchBracket(bracketExpr string, ch byte) bool {
	if len(bracketExpr) < 2 || bracketExpr[0] != '[' {
		return false
	}

	i := 1
	var prevCh byte
	var pCh byte
	negate := false
	matched := false

	// Check for negation
	if i >= len(bracketExpr) {
		return false
	}
	pCh = bracketExpr[i]
	if pCh == '^' {
		pCh = '!'
	}
	if pCh == '!' {
		negate = true
		i++
	}

	prevCh = 0

	// Loop through bracket expression until we find the closing ]
	for {
		if i >= len(bracketExpr) {
			return false
		}
		pCh = bracketExpr[i]

		switch {
		case pCh == '\\':
			// Escaped character
			i++
			if i >= len(bracketExpr) {
				return false
			}
			pCh = bracketExpr[i]
			if ch == pCh {
				matched = true
			}
		case pCh == '-' && prevCh != 0 && i+1 < len(bracketExpr) && bracketExpr[i+1] != ']':
			// Range: prev_ch through next character
			i++
			pCh = bracketExpr[i]
			if pCh == '\\' {
				i++
				if i >= len(bracketExpr) {
					return false
				}
				pCh = bracketExpr[i]
			}
			// Check if ch is in range [prevCh, pCh]
			if ch <= pCh && ch >= prevCh {
				matched = true
			}
			pCh = 0 // Reset prev_ch
		case pCh == '[' && i+1 < len(bracketExpr) && bracketExpr[i+1] == ':':
			// Potential character class
			classStart := i + 2
			j := classStart
			for j < len(bracketExpr) && bracketExpr[j] != ']' {
				j++
			}
			if j >= len(bracketExpr) {
				return false
			}
			// Check if it ends with :]
			classLen := j - classStart
			if classLen > 0 && bracketExpr[j-1] == ':' {
				className := bracketExpr[classStart : j-1]
				classMatch, valid := matchCharClass(className, ch)
				if !valid {
					// Malformed [:class:] string - entire pattern fails
					return false
				}
				if classMatch {
					matched = true
				}
				i = j
				pCh = 0 // Reset prev_ch after character class
			} else if ch == '[' {
				// Didn't find ":]", so treat [ as a literal character in the set
				matched = true
				// pCh stays as '[', will be set to prev_ch at bottom of loop
			}
		case ch == pCh:
			// Literal character match
			matched = true
		}

		prevCh = pCh
		i++
		// Check for the closing bracket
		if i < len(bracketExpr) && bracketExpr[i] == ']' {
			break
		}
	}

	if negate {
		return !matched
	}
	return matched
}

// matchCharClass checks if a character matches a POSIX character class
// Returns (matched, valid) where valid indicates if the class name was recognized
func matchCharClass(class string, ch byte) (bool, bool) {
	r := rune(ch)

	switch class {
	case "alnum":
		return unicode.IsLetter(r) || unicode.IsDigit(r), true
	case "alpha":
		return unicode.IsLetter(r), true
	case "blank":
		return ch == ' ' || ch == '\t', true
	case "cntrl":
		return unicode.IsControl(r), true
	case "digit":
		return unicode.IsDigit(r), true
	case "graph":
		return unicode.IsGraphic(r) && !unicode.IsSpace(r), true
	case "lower":
		return unicode.IsLower(r), true
	case "print":
		return unicode.IsPrint(r), true
	case "punct":
		return unicode.IsPunct(r), true
	case "space":
		return unicode.IsSpace(r), true
	case "upper":
		return unicode.IsUpper(r), true
	case "xdigit":
		return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F'), true
	default:
		// Malformed/unknown character class
		return false, false
	}
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
