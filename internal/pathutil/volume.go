// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license
// that can be found at
// https://github.com/golang/go/blob/go1.26.5/LICENSE.
//
// Derived from src/internal/filepathlite/path_windows.go in Go
// 1.26.5[1], with only the two unexported helpers renamed.
//
// [1]: https://github.com/golang/go/blob/go1.26.5/src/internal/filepathlite/path_windows.go

package pathutil

// HasVolumeName takes one whole path and reports whether it has a
// Windows volume prefix, independent of the host. It is equivalent to
// filepath.VolumeName(p) != "" evaluated on Windows with Go 1.26; Go
// 1.27 reports no volume name for a candidate holding a ".." component
// (golang/go#58451), and this package keeps the Go 1.26 answer, which
// refuses such a path on both toolchains. Like Go, it treats a drive
// prefix as a single byte followed by a colon; it does not recognize
// a multibyte UTF-8 drive letter.
func HasVolumeName(p string) bool { return volumeNameLen(p) != 0 }

// volumeNameLen returns length of the leading volume name on Windows.
// It returns 0 elsewhere.
//
// See:
// https://learn.microsoft.com/en-us/dotnet/standard/io/file-path-formats
// https://googleprojectzero.blogspot.com/2016/02/the-definitive-guide-on-win32-to-nt.html
func volumeNameLen(path string) int {
	switch {
	case len(path) >= 2 && path[1] == ':':
		// Path starts with a drive letter.
		//
		// Not all Windows functions necessarily enforce the requirement that
		// drive letters be in the set A-Z, and we don't try to here.
		//
		// We don't handle the case of a path starting with a non-ASCII character,
		// in which case the "drive letter" might be multiple bytes long.
		return 2

	case len(path) == 0 || !isPathSeparator(path[0]):
		// Path does not have a volume component.
		return 0

	case pathHasPrefixFold(path, `\\.\UNC`):
		// We're going to treat the UNC host and share as part of the volume
		// prefix for historical reasons, but this isn't really principled;
		// Windows's own GetFullPathName will happily remove the first
		// component of the path in this space, converting
		// \\.\unc\a\b\..\c into \\.\unc\a\c.
		return uncLen(path, len(`\\.\UNC\`))

	case pathHasPrefixFold(path, `\\.`) ||
		pathHasPrefixFold(path, `\\?`) || pathHasPrefixFold(path, `\??`):
		// Path starts with \\.\, and is a Local Device path; or
		// path starts with \\?\ or \??\ and is a Root Local Device path.
		//
		// We treat the next component after the \\.\ prefix as
		// part of the volume name, which means Clean(`\\?\c:\`)
		// won't remove the trailing \. (See #64028.)
		if len(path) == 3 {
			return 3 // exactly \\.
		}
		_, rest, ok := cutPath(path[4:])
		if !ok {
			return len(path)
		}
		return len(path) - len(rest) - 1

	case len(path) >= 2 && isPathSeparator(path[1]):
		// Path starts with \\, and is a UNC path.
		return uncLen(path, 2)
	}
	return 0
}

// pathHasPrefixFold tests whether the path s begins with prefix,
// ignoring case and treating all path separators as equivalent.
// If s is longer than prefix, then s[len(prefix)] must be a path separator.
func pathHasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if isPathSeparator(prefix[i]) {
			if !isPathSeparator(s[i]) {
				return false
			}
		} else if asciiToUpper(prefix[i]) != asciiToUpper(s[i]) {
			return false
		}
	}
	if len(s) > len(prefix) && !isPathSeparator(s[len(prefix)]) {
		return false
	}
	return true
}

// uncLen returns the length of the volume prefix of a UNC path.
// prefixLen is the prefix prior to the start of the UNC host;
// for example, for "//host/share", the prefixLen is len("//")==2.
func uncLen(path string, prefixLen int) int {
	count := 0
	for i := prefixLen; i < len(path); i++ {
		if isPathSeparator(path[i]) {
			count++
			if count == 2 {
				return i
			}
		}
	}
	return len(path)
}

// cutPath slices path around the first path separator.
func cutPath(path string) (before, after string, found bool) {
	for i := range path {
		if isPathSeparator(path[i]) {
			return path[:i], path[i+1:], true
		}
	}
	return path, "", false
}

func isPathSeparator(c byte) bool { return c == '\\' || c == '/' }

func asciiToUpper(c byte) byte {
	if 'a' <= c && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}
