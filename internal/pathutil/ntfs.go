package pathutil

import "strings"

// IsNTFSDotGit reports whether part, a path component, is a spelling NTFS
// resolves to ".git": the name itself or its 8.3 short name "git~1", in any
// case, followed by any number of spaces and periods, which NTFS drops, and
// optionally by an alternate data stream suffix (":<stream>").
//
// Git refuses the same components under core.protectNTFS; see the
// description of that setting in git-config(1).
func IsNTFSDotGit(part string) bool {
	// Every alias starts with one of the two names, so most components are
	// settled by their first bytes.
	if !hasASCIIFoldPrefix(part, ".git") && !hasASCIIFoldPrefix(part, "git~1") {
		return false
	}
	base, _, _ := strings.Cut(part, ":")
	base = strings.TrimRight(base, " .")
	return asciiEqualFold(base, ".git") || asciiEqualFold(base, "git~1")
}

// IsNTFSDotDot reports whether part is an NTFS spelling of ".."
// that a comparison against the literal ".." misses: ".." followed
// by a tail of spaces and periods containing at least one space, or
// ".." followed by an Alternate Data Stream colon. ".. ", ".. .",
// "..:$DATA" and "..::$INDEX_ALLOCATION" match. ".." itself does
// not; the literal comparison in IsDotOrDotDotName owns that.
//
// A tail of periods alone ("...", "....") does not match. C Git
// carries such names on POSIX. Win32 trims trailing spaces and
// periods, except for exactly "." and "..". validPath consults
// Win32ValidPath only on Windows with core.protectNTFS enabled.
func IsNTFSDotDot(part string) bool {
	// Only the literal-dot pattern in IsNTFSDot matches a needle
	// this short. It requires two leading periods, so the slice
	// below needs no additional length guard.
	if !IsNTFSDot(part, ".", "") {
		return false
	}
	return strings.ContainsAny(part[2:], " :")
}

// IsNTFSDotCurrent reports whether part is an NTFS spelling of "."
// that a comparison against the literal "." misses: "." followed by a
// tail of spaces and periods containing at least one space, or "."
// followed by an Alternate Data Stream colon. ". ", ". .", ".:$DATA"
// and ".::$INDEX_ALLOCATION" match.
//
// The empty needle degenerates IsNTFSDot: the length guards skip both
// short-name patterns, leaving a leading period followed only by
// spaces, periods or an ADS colon. That admits ".", ".." and runs of
// periods, which this function excludes the way IsNTFSDotDot does —
// by requiring a space or a colon in the tail. IsDotName and
// IsDotOrDotDotName own the literal spellings.
func IsNTFSDotCurrent(part string) bool {
	// Only the literal-dot pattern in IsNTFSDot matches an empty
	// needle. It requires one leading period, so the slice below
	// needs no additional length guard.
	if !IsNTFSDot(part, "", "") {
		return false
	}
	return strings.ContainsAny(part[1:], " :")
}

// Win32ValidPath checks one path component for illegal Win32 characters,
// trailing spaces or periods, and reserved device names. The worktree
// wrapper applies it on Windows with core.protectNTFS enabled; metadata
// aliases are checked separately on every host.
//
// Follows upstream Git's is_valid_win32_path with allow_literal_nul=false.
// The caller splits path separators and rejects volume prefixes before
// checking components, so every colon here is invalid. Embedded NUL bytes
// are also rejected rather than treated as C string terminators.
//
// https://github.com/git/git/blob/v2.54.0/compat/mingw.c#L3347-L3469
func Win32ValidPath(part string) bool {
	for i := 0; i < len(part); i++ {
		if part[i] < 0x20 {
			return false
		}
		switch part[i] {
		case ':', '<', '>', '"', '|', '?', '*':
			return false
		}
	}
	if endsInSpaceOrPeriod(part) {
		return false
	}
	return !isWindowsReservedName(part)
}

// windowsReservedNames lists the Windows reserved device names.
// A path component is reserved if its base name (ignoring trailing
// spaces, extensions, and NTFS Alternate Data Streams) matches one of
// these case-insensitively.
//
// See upstream Git's is_valid_win32_path reserved-name scanner at
// compat/mingw.c#L3372-L3448 in tag v2.54.0[1].
//
// [1]: https://github.com/git/git/blob/v2.54.0/compat/mingw.c#L3372-L3448
var windowsReservedNames = []string{
	"CON", "PRN", "AUX", "NUL",
	"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
	"LPT0", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
	"CONIN$", "CONOUT$",
}

func isWindowsReservedName(part string) bool {
	for _, name := range windowsReservedNames {
		if len(part) < len(name) {
			continue
		}
		if !strings.EqualFold(part[:len(name)], name) {
			continue
		}
		suffix := strings.TrimLeft(part[len(name):], " ")
		if suffix == "" {
			return true
		}
		switch suffix[0] {
		case '.', ':':
			return true
		}
	}
	return false
}

// endsInSpaceOrPeriod reports whether part ends in a space or a
// period, with "." and ".." excepted. Win32 path canonicalisation
// strips both characters from the end of a component, so a component
// that ends in one names something other than what it spells: "foo "
// opens "foo", and ".gitattributes " collides with
// ".gitattributes".
//
// Upstream Git makes the same check at each segment boundary in
// is_valid_win32_path, at compat/mingw.c L3363-L3366 in tag
// v2.54.0[1]: a segment may not end in a space or a period, unless
// it is made of periods only and is no longer than two of them. That
// leaves "." and ".." as the only exceptions, which is how the rule
// is written below.
//
// [1]: https://github.com/git/git/blob/v2.54.0/compat/mingw.c#L3363-L3366
func endsInSpaceOrPeriod(part string) bool {
	if part == "" || part == "." || part == ".." {
		return false
	}
	c := part[len(part)-1]
	return c == ' ' || c == '.'
}

// IsNTFSDot reports whether name, a path component, is a spelling NTFS
// resolves to the file "." + dotgit. dotgit is the lowercase name without
// its leading dot ("gitmodules"), and shortnamePrefix the six-character
// prefix of the hash-style short name NTFS generates for it ("gi7eba"), or
// empty to leave that form out.
//
// NTFS compares names without regard to case, drops trailing spaces and
// periods, and takes everything from the first colon on as the name of an
// alternate data stream of the same file. Besides its long name a file
// answers to an 8.3 short name: the first six characters, "~" and a digit
// from 1 to 4, or, once those are taken, a prefix derived from a hash of
// the name, "~" and a number, eight characters in all.
//
// An empty dotgit matches every component NTFS reduces to a bare dot,
// "." and ".." included.
func IsNTFSDot(name, dotgit, shortnamePrefix string) bool {
	const shortNameLen = 8

	// Every alias starts with a dot or has a tilde in its short name, so
	// most names are settled without looking further.
	if name == "" || (name[0] != '.' && strings.IndexByte(name[:min(shortNameLen, len(name))], '~') < 0) {
		return false
	}
	// A name that starts with a dot can only be the long form.
	if name[0] == '.' && !hasASCIIFoldPrefix(name[1:], dotgit) {
		return false
	}

	base, _, _ := strings.Cut(name, ":")

	if rest, ok := strings.CutPrefix(base, "."); ok && len(rest) >= len(dotgit) &&
		asciiEqualFold(rest[:len(dotgit)], dotgit) &&
		strings.Trim(rest[len(dotgit):], " .") == "" {
		return true
	}

	if len(base) < shortNameLen || strings.Trim(base[shortNameLen:], " .") != "" {
		return false
	}
	stem, number, ok := strings.Cut(base[:shortNameLen], "~")
	if !ok {
		return false
	}

	if len(stem) == 6 && len(dotgit) >= 6 && asciiEqualFold(stem, dotgit[:6]) &&
		number[0] >= '1' && number[0] <= '4' {
		return true
	}

	return len(shortnamePrefix) == 6 && len(stem) <= 6 &&
		asciiEqualFold(stem, shortnamePrefix[:len(stem)]) &&
		number[0] != '0' && strings.Trim(number, "0123456789") == ""
}

// IsNTFSDotGitmodules reports whether part is a name NTFS resolves to
// ".gitmodules": the name itself in any case, with trailing spaces,
// periods or a stream suffix, or one of its short names. "gi7eba" is the
// prefix of the hash-style short name NTFS gives ".gitmodules".
func IsNTFSDotGitmodules(part string) bool {
	return IsNTFSDot(part, "gitmodules", "gi7eba")
}

// IsNTFSDotGitattributes reports whether part is a name NTFS resolves to
// ".gitattributes", whose hash-style short name starts with "gi7d29".
func IsNTFSDotGitattributes(part string) bool {
	return IsNTFSDot(part, "gitattributes", "gi7d29")
}

// IsNTFSDotGitignore reports whether part is a name NTFS resolves to
// ".gitignore", whose hash-style short name starts with "gi250a".
func IsNTFSDotGitignore(part string) bool {
	return IsNTFSDot(part, "gitignore", "gi250a")
}

// IsNTFSDotMailmap reports whether part is a name NTFS resolves to
// ".mailmap", whose hash-style short name starts with "maba30".
func IsNTFSDotMailmap(part string) bool {
	return IsNTFSDot(part, "mailmap", "maba30")
}

func asciiToLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// asciiEqualFold reports whether a and b are equal when ASCII letters are
// compared without regard to case. Unlike strings.EqualFold it folds
// nothing else, so no non-ASCII character is taken for a letter.
// hasASCIIFoldPrefix reports whether s starts with prefix, ASCII case aside.
func hasASCIIFoldPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && asciiEqualFold(s[:len(prefix)], prefix)
}

func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if asciiToLower(a[i]) != asciiToLower(b[i]) {
			return false
		}
	}
	return true
}
