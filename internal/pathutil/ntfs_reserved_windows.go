//go:build windows

package pathutil

import "strings"

// IsWindowsReservedName reports whether part is a Windows reserved
// device name, matching upstream Git's is_valid_win32_path
// (compat/mingw.c), which is compiled only into Windows-native and
// Cygwin builds.
func IsWindowsReservedName(part string) bool {
	for _, name := range WindowsReservedNames {
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
