//go:build windows

package pathutil

import "strings"

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

// IsWindowsReservedName reports whether part is a Windows reserved
// device name, matching upstream Git's is_valid_win32_path
// (compat/mingw.c), which is compiled only into Windows-native and
// Cygwin builds.
func IsWindowsReservedName(part string) bool {
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
