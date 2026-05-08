package git

import (
	"runtime"
	"unicode"
)

// defaultProtectHFS returns the default value for core.protectHFS
// when not explicitly configured. Matches upstream Git behaviour:
// enabled by default on macOS.
func defaultProtectHFS() bool {
	return runtime.GOOS == "darwin"
}

// hfsIgnoredCodepoints contains Unicode code points that HFS+ ignores
// during path normalization. A path containing these characters between
// the characters of ".git" will be treated as ".git" by HFS+.
//
// See upstream Git utf8.c next_hfs_char() for the full list.
var hfsIgnoredCodepoints = map[rune]struct{}{
	0x200c: {}, // ZERO WIDTH NON-JOINER
	0x200d: {}, // ZERO WIDTH JOINER
	0x200e: {}, // LEFT-TO-RIGHT MARK
	0x200f: {}, // RIGHT-TO-LEFT MARK
	0x202a: {}, // LEFT-TO-RIGHT EMBEDDING
	0x202b: {}, // RIGHT-TO-LEFT EMBEDDING
	0x202c: {}, // POP DIRECTIONAL FORMATTING
	0x202d: {}, // LEFT-TO-RIGHT OVERRIDE
	0x202e: {}, // RIGHT-TO-LEFT OVERRIDE
	0x206a: {}, // INHIBIT SYMMETRIC SWAPPING
	0x206b: {}, // ACTIVATE SYMMETRIC SWAPPING
	0x206c: {}, // INHIBIT ARABIC FORM SHAPING
	0x206d: {}, // ACTIVATE ARABIC FORM SHAPING
	0x206e: {}, // NATIONAL DIGIT SHAPES
	0x206f: {}, // NOMINAL DIGIT SHAPES
	0xfeff: {}, // ZERO WIDTH NO-BREAK SPACE
}

// isHFSDotGit returns true if the given path component would be
// treated as ".git" on an HFS+ filesystem after stripping ignored
// Unicode code points and folding to lower case.
func isHFSDotGit(part string) bool {
	const needle = "git"

	runes := []rune(part)
	i := 0

	// skip ignored code points, then expect '.'
	for i < len(runes) {
		if _, ok := hfsIgnoredCodepoints[runes[i]]; !ok {
			break
		}
		i++
	}
	if i >= len(runes) || runes[i] != '.' {
		return false
	}
	i++

	// match "git" case-insensitively, skipping ignored code points
	for _, expected := range needle {
		for i < len(runes) {
			if _, ok := hfsIgnoredCodepoints[runes[i]]; !ok {
				break
			}
			i++
		}
		if i >= len(runes) {
			return false
		}
		r := runes[i]
		if r > 127 {
			return false
		}
		if unicode.ToLower(r) != unicode.ToLower(expected) {
			return false
		}
		i++
	}

	// skip trailing ignored code points
	for i < len(runes) {
		if _, ok := hfsIgnoredCodepoints[runes[i]]; !ok {
			break
		}
		i++
	}

	// must be at end of component
	return i == len(runes)
}
