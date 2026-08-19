//go:build !windows

package pathutil

// IsWindowsReservedName reports whether part is a Windows reserved
// device name. On non-Windows platforms there are no reserved device
// names: names such as prn.sh, CON, or aux.txt are legitimate
// filenames, mirroring upstream Git, whose is_valid_win32_path
// (compat/mingw.c) is not compiled into non-Windows builds.
func IsWindowsReservedName(part string) bool {
	return false
}
