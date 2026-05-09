package git

import (
	"runtime"
	"strings"
)

// defaultProtectNTFS returns the default value for core.protectNTFS
// when not explicitly configured. Matches upstream Git behaviour:
// enabled by default on Windows.
func defaultProtectNTFS() bool {
	return runtime.GOOS == "windows"
}

// windowsPathReplacer defines the chars that need to be replaced
// as part of windowsValidPath.
var windowsPathReplacer = strings.NewReplacer(" ", "", ".", "")

func windowsValidPath(part string) bool {
	// Bare ".git" is allowed at this layer; rejection of root-level or
	// non-final ".git" components is handled by validPath. This check
	// only catches the Windows-specific variants (`.git ` / `.git.` /
	// `.git::$INDEX_ALLOCATION` etc.) that get normalised back to ".git".
	if len(part) > 4 && strings.EqualFold(part[:4], GitDirName) {
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
