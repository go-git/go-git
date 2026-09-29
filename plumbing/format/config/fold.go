package config

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// foldKey returns a canonical form of name, such that two names produce the
// same key if and only if strings.EqualFold considers them equal. It is meant
// to be used as a map key, to replace a linear scan of strings.EqualFold
// comparisons with a single lookup.
//
// strings.ToLower cannot be used for this: strings.EqualFold("ſ", "s") is
// true, because U+017F is in the same case folding orbit as 's', but the two
// lowercase to different strings. Each rune is therefore mapped to the
// smallest rune in its unicode.SimpleFold orbit, which is exactly the
// equivalence strings.EqualFold implements, and then lowercased when that is
// an ASCII letter. Lowercasing picks a different representative of the same
// orbit, so it does not change which names share a key, but it leaves an
// all-lowercase ASCII name unchanged, which is the common case and the one
// foldKey returns without allocating.
func foldKey(name string) string {
	i := 0
	for ; i < len(name); i++ {
		if c := name[i]; c >= utf8.RuneSelf || ('A' <= c && c <= 'Z') {
			break
		}
	}

	// An all-ASCII name with no uppercase letters is already canonical, so
	// it can be used as is. This is the common case, and it does not
	// allocate.
	if i == len(name) {
		return name
	}

	var b strings.Builder
	b.Grow(len(name))
	b.WriteString(name[:i])
	for _, r := range name[i:] {
		b.WriteRune(foldRune(r))
	}
	return b.String()
}

// foldRune returns the representative of the case folding orbit of r: the
// smallest rune r folds to, lowercased when that is an ASCII letter.
//
// The ASCII branch reaches that same answer without walking the orbit. The
// smallest rune in the orbit of an ASCII letter is always its uppercase
// codepoint, because every other member is either the lowercase letter or
// lies above U+00FF, and no ASCII rune that is not a letter has a fold partner
// at all.
//
// The lowercasing is what makes an all-lowercase ASCII name its own key, so
// foldKey's fast path depends on it: without it foldKey("Core") would fold to
// "CORE" while foldKey("core") took the fast path and returned "core".
func foldRune(r rune) rune {
	if r < utf8.RuneSelf {
		if 'A' <= r && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}

	smallest := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f < smallest {
			smallest = f
		}
	}
	if 'A' <= smallest && smallest <= 'Z' {
		smallest += 'a' - 'A'
	}
	return smallest
}
