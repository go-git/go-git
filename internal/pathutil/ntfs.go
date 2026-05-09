package pathutil

import "strings"

// dotGit is the canonical Git metadata directory name. Used as a
// constant by the NTFS variant detection helpers to avoid coupling
// internal/pathutil to the root `git` package's GitDirName constant.
const dotGit = ".git"

// windowsPathReplacer strips trailing spaces and periods. NTFS
// silently trims them from filename suffixes, so a path like
// `.git . . .` resolves back to `.git` once normalised.
var windowsPathReplacer = strings.NewReplacer(" ", "", ".", "")

// WindowsValidPath reports whether part is a valid Windows / NTFS
// path component for the worktree filesystem abstraction. It rejects
// the NTFS-specific variants of `.git` (trailing spaces, periods,
// Alternate Data Streams) and Windows reserved device names. Bare
// `.git` itself is allowed at this layer; the caller decides whether
// it is permissible at the current path position.
func WindowsValidPath(part string) bool {
	if len(part) > 4 && strings.EqualFold(part[:4], dotGit) {
		// For historical reasons, file names that end in spaces or periods are
		// automatically trimmed. Therefore, `.git . . ./` is a valid way to refer
		// to `.git/`.
		if windowsPathReplacer.Replace(part[4:]) == "" {
			return false
		}

		// For yet other historical reasons, NTFS supports so-called "Alternate Data
		// Streams", i.e. metadata associated with a given file, referred to via
		// `<filename>:<stream-name>:<stream-type>`. There exists a default stream
		// type for directories, allowing `.git/` to be accessed via
		// `.git::$INDEX_ALLOCATION/`.
		//
		// For performance reasons, _all_ Alternate Data Streams of `.git/` are
		// forbidden, not just `::$INDEX_ALLOCATION`.
		if part[4:5] == ":" {
			return false
		}
	}
	return !isWindowsReservedName(part)
}

// windowsReservedNames lists the Windows reserved device names.
// A path component is reserved if its base name (ignoring trailing
// spaces, extensions, and NTFS Alternate Data Streams) matches one of
// these case-insensitively.
//
// See upstream Git compat/mingw.c is_valid_win32_path().
var windowsReservedNames = []string{
	"CON", "PRN", "AUX", "NUL",
	"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
	"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
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
		// Exact match or followed by space, dot, colon (ADS), or separator.
		if len(part) == len(name) {
			return true
		}
		switch part[len(name)] {
		case ' ', '.', ':':
			return true
		}
	}
	return false
}

// IsNTFSDot ports upstream Git's is_ntfs_dot_generic. It detects NTFS
// path-component variants of a dotfile name that attackers can use to
// bypass case-insensitive comparisons against the canonical name on
// Windows. The dotgit parameter is the lowercase name without the
// leading dot (e.g. "gitmodules"); shortnamePrefix is the canonical
// 6-character NTFS short-name prefix used as a fall-back match
// (e.g. "gi7eba" for ".gitmodules").
//
// Reference: upstream Git path.c is_ntfs_dot_generic at L1451-L1507
// in tag v2.54.0[1].
//
// [1]: https://github.com/git/git/blob/v2.54.0/path.c#L1451-L1507
func IsNTFSDot(name, dotgit, shortnamePrefix string) bool {
	// onlySpacesAndPeriods returns true when the suffix from start
	// onwards consists only of trailing spaces and periods, possibly
	// terminated by a NTFS Alternate Data Stream colon. Mirrors the
	// only_spaces_and_periods label in upstream's is_ntfs_dot_generic.
	onlySpacesAndPeriods := func(start int) bool {
		for i := start; i < len(name); i++ {
			c := name[i]
			if c == ':' {
				return true
			}
			if c != ' ' && c != '.' {
				return false
			}
		}
		return true
	}

	// Pattern 1: ".<dotgit>" prefix + trailing spaces / periods / ADS.
	if len(name) >= len(dotgit)+1 && name[0] == '.' &&
		strings.EqualFold(name[1:1+len(dotgit)], dotgit) {
		if onlySpacesAndPeriods(len(dotgit) + 1) {
			return true
		}
	}

	// Pattern 2: standard NTFS short name <dotgit[:6]>~[1-4].
	if len(dotgit) >= 6 && len(name) >= 8 &&
		strings.EqualFold(name[:6], dotgit[:6]) &&
		name[6] == '~' && name[7] >= '1' && name[7] <= '4' {
		if onlySpacesAndPeriods(8) {
			return true
		}
	}

	// Pattern 3: fall-back NTFS short name keyed by shortnamePrefix.
	if len(shortnamePrefix) < 6 || len(name) < 8 {
		return false
	}
	sawTilde := false
	i := 0
	for i < 8 {
		c := name[i]
		switch {
		case sawTilde:
			if c < '0' || c > '9' {
				return false
			}
		case c == '~':
			i++
			if i >= len(name) || name[i] < '1' || name[i] > '9' {
				return false
			}
			sawTilde = true
		case i >= 6:
			return false
		case c&0x80 != 0:
			return false
		default:
			if asciiToLower(c) != shortnamePrefix[i] {
				return false
			}
		}
		i++
	}
	return onlySpacesAndPeriods(8)
}

// IsNTFSDotGitmodules reports whether part is an NTFS-equivalent of
// ".gitmodules" — the file name (or any of its variants that NTFS
// would resolve to it) that attackers can use to plant submodule
// configuration disguised as a symlink. The 6-character canonical
// short-name prefix "gi7eba" mirrors upstream Git's is_ntfs_dotgitmodules.
func IsNTFSDotGitmodules(part string) bool {
	return IsNTFSDot(part, "gitmodules", "gi7eba")
}

func asciiToLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
